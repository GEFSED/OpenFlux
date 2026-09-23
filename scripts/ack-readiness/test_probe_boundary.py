import copy
import io
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

from probe_boundary import (ProbeFailure, LocalEvidence, atomic_write, restart_delta,
    make_boundary, guard_context, failure_fields, termination_fields, remote_receipt,
    payload_digest, validate_boundary)
from probe_controller import receive
from journal_validator import validate_journal_json, journal_command, Rejected
from test_structure import fixture, sequence
from test_startup import entries, scope5

CONTEXT = dict(boot_id='a'*32, manager_start_ticks='100', manager_bus_owner=':1.0')
EXE = '/root/openflux/openflux-bootstrap-schema6-34ba35a'
STATE = dict(MainPID='0', InvocationID='b'*32, NRestarts='541')
SCOPE = dict(schema=6, cursor='before', start_monotonic_us=100, boot_id='a'*32,
             unit='openflux-user3.service', exe=EXE, previous_pid='0')


def boundary():
    return make_boundary(SCOPE, STATE, CONTEXT, [EXE.encode(), b'--url-file=/synthetic/file'],
                         '2026-09-23T19:00:00+00:00')


class CounterTests(unittest.TestCase):
    def test_nonzero_baselines(self):
        for n in (0, 541, 1000, 99999):
            with self.subTest(n=n):self.assertEqual(0, restart_delta(n, str(n)))

    def test_automatic_restart(self):
        with self.assertRaisesRegex(ProbeFailure, 'AUTOMATIC_RESTART_OBSERVED'):
            restart_delta(541, 542)

    def test_regression_is_invalid_not_zero(self):
        for current in (0, 1, 540):
            with self.assertRaisesRegex(ProbeFailure, 'NRESTARTS_BASELINE_INVALIDATED'):
                restart_delta(541, current)

    def test_counter_strict_types(self):
        for n in (True, None, -1, '541\n', '541.0', 2**32):
            with self.assertRaises(ProbeFailure):restart_delta(541,n)

    def test_boot_identity_change(self):
        with self.assertRaisesRegex(ProbeFailure, 'BOOT_ID_CHANGED'):
            guard_context(CONTEXT, dict(CONTEXT, boot_id='c'*32))

    def test_manager_identity_change(self):
        for change in ({'manager_start_ticks':'101'}, {'manager_bus_owner':':1.2'}):
            with self.assertRaisesRegex(ProbeFailure, 'SYSTEMD_MANAGER_IDENTITY_CHANGED'):
                guard_context(CONTEXT,dict(CONTEXT,**change))

    def test_same_context(self):
        guard_context(CONTEXT,dict(CONTEXT))


class BoundaryTests(unittest.TestCase):
    def test_cursor_survives_exception_and_new_reader(self):
        with tempfile.TemporaryDirectory() as d:
            data=boundary();store=LocalEvidence(d)
            ack=store.accept('boundary',data)
            self.assertEqual(payload_digest(data),ack['sha256'])
            try:raise AssertionError('synthetic harness failure')
            except AssertionError:pass
            disk=json.loads((Path(d)/'boundary.json').read_text())
            self.assertEqual(data,disk)
            self.assertEqual('before',disk['scope']['cursor'])
            self.assertNotIn('--url-file',json.dumps(disk))
            self.assertEqual(541,disk['baseline_nrestarts'])

    def test_write_failure_prevents_receipt(self):
        with tempfile.TemporaryDirectory() as d, patch('probe_boundary.os.fsync',side_effect=OSError):
            with self.assertRaisesRegex(ProbeFailure,'EVIDENCE_WRITE_FAILED'):
                LocalEvidence(d).accept('boundary',boundary())
            self.assertFalse((Path(d)/'boundary.json').exists())

    def test_boundary_and_start_intent_immutable_no_resume(self):
        with tempfile.TemporaryDirectory() as d:
            store=LocalEvidence(d);b=boundary();store.accept('boundary',b)
            store.accept('start_intent',dict(boundary_sha256=payload_digest(b),maximum_start_count=1))
            for name,payload in (('boundary',b),('start_intent',dict(boundary_sha256=payload_digest(b),maximum_start_count=1))):
                with self.assertRaisesRegex(ProbeFailure,'EVIDENCE_ALREADY_EXISTS'):store.accept(name,payload)

    def test_boundary_unavailable(self):
        for cursor in (None,'','\n'):
            b=boundary();b['scope']['cursor']=cursor
            with self.assertRaisesRegex(ProbeFailure,'JOURNAL_BOUNDARY_UNAVAILABLE'):validate_boundary(b)

    def test_local_scope_must_preserve_original_boundary(self):
        with tempfile.TemporaryDirectory() as d:
            store=LocalEvidence(d);b=boundary();store.accept('boundary',b)
            s=dict(b['scope'],pid='1234',invocation='c'*32)
            for changed in (dict(s,cursor='replacement'),dict(s,invocation=b['baseline_invocation'])):
                with self.assertRaisesRegex(ProbeFailure,'SCOPE_INVALID'):store.accept('scope',changed)
            store.accept('scope',s)
            self.assertEqual('before',json.loads((Path(d)/'scope.json').read_text())['cursor'])

    def test_remote_receipt_requires_exact_local_digest(self):
        for reply in ('', '{}', '{bad', json.dumps({'ack':'boundary','sha256':'wrong'})):
            with patch('probe_boundary.select.select',return_value=([1],[],[])), patch('sys.stdin',io.StringIO(reply)), patch('sys.stdout',io.StringIO()):
                with self.assertRaisesRegex(ProbeFailure,'LOCAL_EVIDENCE_ACK_FAILED'):remote_receipt('boundary',boundary())

    def test_remote_receipt_timeout(self):
        with patch('probe_boundary.select.select',return_value=([],[],[])),patch('sys.stdout',io.StringIO()):
            with self.assertRaisesRegex(ProbeFailure,'LOCAL_EVIDENCE_ACK_TIMEOUT'):remote_receipt('boundary',boundary())

    def test_controller_ack_after_durable_write(self):
        with tempfile.TemporaryDirectory() as d:
            b=boundary()
            class Worker:
                stdout=io.StringIO(json.dumps({'kind':'durable_evidence','name':'boundary','payload':b})+'\n')
                stdin=io.StringIO()
                def wait(self,timeout):return 0
            w=Worker();self.assertEqual(0,receive(w,d))
            self.assertEqual(payload_digest(json.loads((Path(d)/'boundary.json').read_text())),json.loads(w.stdin.getvalue())['sha256'])

    def test_replay_old_records_and_other_invocation(self):
        texts=sequence(fixture('public_root'))
        s=dict(scope5(),schema=6)
        records=entries(texts)
        old=copy.deepcopy(records[0]);old['__MONOTONIC_TIMESTAMP']='0';old['MESSAGE']='old raw data must be ignored'
        accepted,_=validate_journal_json('\n'.join(map(json.dumps,[old]+records)),s)
        self.assertEqual(len(records),len(accepted))
        bad=copy.deepcopy(records);bad[0]['_SYSTEMD_INVOCATION_ID']='d'*32
        with self.assertRaises(Rejected):validate_journal_json('\n'.join(map(json.dumps,bad)),s)
        self.assertIn('--all',journal_command(s));self.assertIn('--after-cursor='+s['cursor'],journal_command(s))

    def test_null_long_message_remains_unavailable(self):
        rows=entries(sequence(fixture('public_root')));rows[0]['MESSAGE']=None
        with self.assertRaisesRegex(Rejected,'message_unavailable_possible_journal_elision'):
            validate_journal_json('\n'.join(map(json.dumps,rows)),dict(scope5(),schema=6))


class ExitTests(unittest.TestCase):
    def state(self,code,status):
        return dict(MainPID='0',ActiveState='inactive',InvocationID='c'*32,
                    ExecMainCode=code,ExecMainStatus=status,Result='success')

    def test_historical_cleanup_sigterm_is_not_bootstrap_failure(self):
        v=termination_fields(self.state('2','15'),'c'*32)
        self.assertEqual('HARNESS_CLEANUP',v['PROCESS_TERMINATION_OWNER'])
        self.assertEqual('SIGTERM',v['PROCESS_EXIT_SIGNAL'])
        self.assertEqual('NOT_OBSERVED',v['BOOTSTRAP_COMPLETION_STATE'])
        self.assertEqual('NOT_OBSERVED',v['PROCESS_EXIT_CODE'])

    def test_unrelated_sigterm_not_attributed_to_harness(self):
        for owner in (None,'d'*32):
            v=termination_fields(self.state('2','15'),owner)
            self.assertEqual('EXTERNAL_OR_UNKNOWN_SIGNAL',v['PROCESS_TERMINATION_OWNER'])

    def test_application_exit_1_before_http(self):
        v=termination_fields(self.state('1','1'))
        self.assertEqual('APPLICATION_EXIT',v['PROCESS_TERMINATION_OWNER'])
        self.assertEqual(1,v['PROCESS_EXIT_CODE'])
        self.assertEqual('NOT_OBSERVED',v['BOOTSTRAP_COMPLETION_STATE'])

    def test_natural_exit_racing_cleanup_is_not_claimed_sigterm(self):
        v=termination_fields(self.state('1','1'),'c'*32)
        self.assertEqual('APPLICATION_EXIT',v['PROCESS_TERMINATION_OWNER'])

    def test_error_separation_and_no_raw_error(self):
        secret='https://private.invalid/?token=SECRET'
        v=failure_fields(RuntimeError(secret))
        self.assertEqual('HARNESS_VALIDATION_FAILURE',v['ERROR_CATEGORY'])
        self.assertEqual('NOT_OBSERVED',v['APPLICATION_FAILURE_CLASS'])
        self.assertNotIn(secret,json.dumps(v))
        self.assertEqual('OPERATOR_CONTROL_FAILURE',failure_fields(ProbeFailure('EVIDENCE_WRITE_FAILED','OPERATOR_CONTROL_FAILURE'))['ERROR_CATEGORY'])
        self.assertEqual('CLEANUP_FAILURE',failure_fields(RuntimeError(secret),cleanup=True)['ERROR_CATEGORY'])
