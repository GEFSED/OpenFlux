import copy
import unittest
from probe_boundary import ProbeFailure, make_boundary, termination_fields
from probe_invocation import InvocationEpoch, UNIT_STARTED, UNIT_STARTING, read_ledger

C=dict(boot_id='a'*32,manager_start_ticks='2',manager_bus_owner=':1.4')
I='c'*32
J='d'*32
U='openflux-user3.service'

def boundary(pre=541):
    return make_boundary(dict(schema=6,cursor='original',start_monotonic_us=100,
        boot_id=C['boot_id'],unit=U,exe='/synthetic/diagnostic',previous_pid='0'),
        dict(MainPID='0',InvocationID='b'*32,NRestarts=str(pre)),C,[b'/synthetic/diagnostic'],
        '2026-09-23T20:15:00+00:00')

def state(post=0,inv=I,pid='123',stamp='110'):
    return dict(InvocationID=inv,MainPID=pid,ExecMainPID='123',ExecMainStartTimestampMonotonic=stamp,
        NRestarts=str(post),Restart='no',ActiveState='active')

def row(inv=I,n=1,kind='app'):
    r=dict(__CURSOR='row'+str(n),__MONOTONIC_TIMESTAMP=str(110+n),_BOOT_ID=C['boot_id'])
    if kind=='app':r.update(_SYSTEMD_UNIT=U,_SYSTEMD_INVOCATION_ID=inv,_PID='123',_EXE='/synthetic/diagnostic')
    else:r.update(UNIT=U,INVOCATION_ID=inv,_PID='1',_EXE='/usr/lib/systemd/systemd',MESSAGE_ID=UNIT_STARTED if kind=='started' else UNIT_STARTING)
    return r

def rows():return [row(kind='started'),row(n=2)]

class InvocationTests(unittest.TestCase):
    def epoch(self,pre=541,post=0):
        e=InvocationEpoch(boundary(pre));e.consume_start();e.observe(state(post),C,rows());return e

    def test_historical_attempts_A_B_and_zero_large(self):
        for pre,post in ((0,0),(541,0),(541,541),(99999,0)):
            with self.subTest(pre=pre,post=post):
                e=self.epoch(pre,post);e.observe(state(post),C,rows());e.finish()
                self.assertEqual(1,e.fields()['OBSERVED_INVOCATION_COUNT'])
                self.assertEqual(post,e.post_baseline)
                self.assertEqual(0,e.fields()['AUTOMATIC_RESTART_COUNTER_DELTA'])

    def test_secondary_counter_growth_and_regression(self):
        for old,new,reason in ((0,1,'AUTOMATIC_RESTART_COUNTER_ADVANCED'),(541,542,'AUTOMATIC_RESTART_COUNTER_ADVANCED'),(541,0,'POST_START_NRESTARTS_EPOCH_INVALIDATED')):
            e=self.epoch(post=old)
            with self.assertRaisesRegex(ProbeFailure,reason):e.observe(state(new),C,rows())

    def test_second_invocation_even_with_no_counter_change(self):
        for counter in (0,1):
            for cause in ('automatic','external-start','external-restart','refresh','dependency'):
                with self.subTest(cause=cause,counter=counter):
                    e=self.epoch()
                    with self.assertRaisesRegex(ProbeFailure,'ADDITIONAL_INVOCATION_OBSERVED'):
                        e.observe(state(counter,J),C,rows()+[row(J,3,'started')])

    def test_fast_second_invocation_seen_only_in_journal(self):
        e=self.epoch()
        with self.assertRaisesRegex(ProbeFailure,'ADDITIONAL_INVOCATION_OBSERVED'):
            e.observe(state(),C,rows()+[row(J,3)])

    def test_two_started_events_without_invocation_metadata_fail(self):
        e=self.epoch();extra=row(J,3,'started');extra.pop('INVOCATION_ID')
        with self.assertRaisesRegex(ProbeFailure,'ADDITIONAL_INVOCATION_OBSERVED'):
            e.observe(state(),C,rows()+[extra])

    def test_duplicate_operator_start_active_is_not_second_invocation(self):
        e=self.epoch();e.observe(state(),C,rows());e.finish()
        self.assertEqual(1,e.fields()['OBSERVED_SYSTEMD_STARTED_EVENTS'])

    def test_failure_restart_no_no_second_start(self):
        e=self.epoch();s=state(pid='0');s['ActiveState']='failed'
        e.observe(s,C,rows());e.finish()

    def test_internal_second_control_call_forbidden(self):
        e=self.epoch()
        with self.assertRaisesRegex(ProbeFailure,'SECOND_CONTROL_START_FORBIDDEN'):e.consume_start()
        self.assertEqual(1,e.control_calls)

    def test_control_consumed_even_if_result_ambiguous(self):
        e=InvocationEpoch(boundary());e.consume_start()
        with self.assertRaises(ProbeFailure):e.consume_start()
        self.assertEqual(1,e.fields()['CONTROL_START_CALL_COUNT'])

    def test_boot_manager_changes_before_and_after_start(self):
        for started in (False,True):
            for key,val,code in (('boot_id','e'*32,'BOOT_ID_CHANGED'),('manager_start_ticks','3','SYSTEMD_MANAGER_IDENTITY_CHANGED'),('manager_bus_owner',':1.5','SYSTEMD_MANAGER_IDENTITY_CHANGED')):
                e=self.epoch() if started else InvocationEpoch(boundary())
                with self.assertRaisesRegex(ProbeFailure,code):e.observe(state(),dict(C,**{key:val}),[])

    def test_start_before_control_is_rejected(self):
        e=InvocationEpoch(boundary())
        with self.assertRaisesRegex(ProbeFailure,'INVOCATION_BEFORE_AUTHORIZED_START'):e.observe(state(),C,rows())

    def test_unrelated_manager_records_not_starts(self):
        e=self.epoch();r=row(J,3,'started');r['UNIT']='unrelated.service'
        e.observe(state(),C,rows()+[r]);e.finish()

    def test_untrusted_forged_manager_fields_not_starts(self):
        e=self.epoch();r=row(J,3,'started');r['_PID']='999'
        e.observe(state(),C,rows()+[r]);e.finish()

    def test_old_preboundary_records_not_current(self):
        e=self.epoch();r=row(J,3);r['__MONOTONIC_TIMESTAMP']='99'
        e.observe(state(),C,rows()+[r]);e.finish()

    def test_missing_target_invocation_id_fails(self):
        e=self.epoch();r=row(J,3);r.pop('_SYSTEMD_INVOCATION_ID')
        with self.assertRaisesRegex(ProbeFailure,'INVOCATION_ID_UNAVAILABLE'):e.observe(state(),C,rows()+[r])

    def test_journal_loss_fails(self):
        e=self.epoch()
        with self.assertRaisesRegex(ProbeFailure,'INVOCATION_JOURNAL_HISTORY_LOST'):e.observe(state(),C,[])

    def test_missing_started_event_never_claims_full_proof(self):
        e=InvocationEpoch(boundary());e.consume_start();e.observe(state(),C,[row()])
        with self.assertRaisesRegex(ProbeFailure,'ONE_SHOT_INVOCATION_PROOF_INCOMPLETE'):e.finish()

    def test_stale_cursor_cannot_be_silently_replaced(self):
        def command(*args):return '' if args==('journalctl','--sync') else '{"__CURSOR":"wrong"}'
        with self.assertRaisesRegex(ProbeFailure,'JOURNAL_BOUNDARY_UNAVAILABLE'):read_ledger(command,boundary())

    def test_changed_pid_or_exec_start_is_not_same_invocation_proof(self):
        for s in (state(pid='456'),state(stamp='120')):
            e=self.epoch()
            with self.assertRaisesRegex(ProbeFailure,'AUTHORIZED_PROCESS_IDENTITY_CHANGED'):e.observe(s,C,rows())

    def test_historical_reset_does_not_discard_durable_boundary(self):
        b=boundary();original=copy.deepcopy(b);e=InvocationEpoch(b);e.consume_start()
        e.observe(state(0),C,rows());e.finish();self.assertEqual(original,b)
