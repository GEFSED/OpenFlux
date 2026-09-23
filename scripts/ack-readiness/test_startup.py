import copy
import json
import os
from pathlib import Path
import subprocess
import sys
import unittest
from unittest.mock import patch
import uuid

import operations
from argv_validator import await_stable,expected_tokens,observe_process
from journal_validator import validate_records,validate_journal_json,Rejected
from measurement import readiness_observation
from schema4 import DiagnosticStartupFailed,validate_startup,startup_outcome
from test_journal_validator import entry,SCOPE

CASES={'transport_failure':('transport_start','transport_start_other'),
    'missing':('authorization','auth_client_config_missing'),
    'captcha':('authorization','auth_challenge_or_captcha_classified'),
    'auth_other':('authorization','auth_other'),
    'relay_failure':('relay_workers','relay_workers_failure'),
    'proxy_failure':('proxy_init','proxy_init_failure'),
    'diagnostic_failure':('diagnostic_init','diagnostic_init_failure'),
    'banner_failure':('diagnostic_init','schema_emit_failure'),
    'snapshot_failure':('snapshot_loop','schema_emit_failure'),
    'unknown':('proxy_init','unknown_startup_failure'),
    'long_error':('authorization','auth_client_config_missing'),
    'exit_before_loop':('proxy_init','unknown_startup_failure')}
def lines(name):
    return json.loads((Path(os.environ['ACK_STARTUP_FIXTURE_DIR'])/(name+'.json')).read_text())
def entries(texts):
    return [entry(s,__CURSOR='new-%d'%i,__MONOTONIC_TIMESTAMP=str(100+i*2000000),
                  __REALTIME_TIMESTAMP=str(100000000+i*2000000)) for i,s in enumerate(texts)]
def scope4():return dict(SCOPE,schema=4)

class StartupTests(unittest.TestCase):
    def test_success_through_first_snapshot(self):
        texts=lines('success');records,_=validate_records(entries(texts+[texts[-1]]*8),scope4())
        proof=readiness_observation(records,30,schema=4)
        self.assertEqual(4,proof['LOG_SCHEMA']);self.assertEqual('PASS',proof['LOG_VALIDATION'])

    def test_every_classified_failure_before_snapshot(self):
        for name,(stage,kind) in CASES.items():
            with self.subTest(name=name):
                records,_=validate_records(entries(lines(name)),scope4())
                self.assertFalse(any('values' in r for r in records))
                with self.assertRaises(DiagnosticStartupFailed) as c:
                    readiness_observation(records,0,process_exited=True,schema=4)
                self.assertEqual(stage,c.exception.safe['STARTUP_STAGE'])
                self.assertEqual(kind,c.exception.safe['STARTUP_FAILURE_CLASS'])
                self.assertFalse(c.exception.safe['READY'])
                self.assertEqual(name!='exit_before_loop',c.exception.safe['CLASSIFIED_EVENT_OBSERVED'])
                self.assertNotIn('SECRET_SENTINEL',json.dumps(records)+json.dumps(c.exception.safe))

    def test_unknown_startup_error_does_not_infer_captcha(self):
        with self.assertRaises(DiagnosticStartupFailed) as c:startup_outcome([],True)
        self.assertEqual('unknown_startup_failure',c.exception.safe['STARTUP_FAILURE_CLASS'])

    def test_forbidden_fields_enums_types_and_flags_rejected(self):
        original=json.loads(lines('success')[0].split(' ',1)[1])
        changes=[{k:'SECRET_SENTINEL'} for k in ('address','port','seq','ack','payload','url','key','token','cookie')]
        changes += [dict(schema=3),dict(schema=True),dict(event='other'),dict(ordinal=0),
            dict(startup_stage='unknown'),dict(failure_class='unknown'),dict(startup_result='unknown'),
            dict(transport_started=1),dict(proxy_initialized=True),dict(ordinal=2)]
        for change in changes:
            with self.subTest(change=next(iter(change))):
                bad=dict(original,**change)
                with self.assertRaises(Rejected) as c:validate_records(entries(['[ACK-STARTUP] '+json.dumps(bad)]),scope4())
                self.assertNotIn('SECRET_SENTINEL',json.dumps(c.exception.safe))

    def test_missing_structure_duplicate_or_reordered_events(self):
        values=lines('success')
        for bad in (values[1:],values[:2]+values[1:],values[:1]+[values[2],values[1]]+values[3:]):
            with self.assertRaises(Rejected):validate_records(entries(bad),scope4())
        e=json.loads(values[0].split(' ',1)[1]);del e['failure_class']
        with self.assertRaises(Rejected):validate_records(entries(['[ACK-STARTUP] '+json.dumps(e)]),scope4())

    def test_mixed_schema_and_long_null_robustness(self):
        values=lines('success');raw=json.loads(values[-1].split('[ACK-DIAG] ',1)[1])
        self.assertGreater(len(values[-1]),4096)
        bad=values[:-1]+['2026/09/23 14:00:00.000000 [ACK-DIAG] '+json.dumps(dict(raw,schema=3))]
        with self.assertRaises(Rejected):validate_records(entries(bad),scope4())
        for msg in (None,[255],{},'[ACK-STARTUP] {}','[ACK-STARTUP] {broken'):
            with self.assertRaises(Rejected):validate_records(entries(values[:-1]+[msg]),scope4())
        with self.assertRaises(Rejected) as c:validate_records(entries([None]),scope4())
        self.assertEqual('message_unavailable_possible_journal_elision',c.exception.safe['reason'])

    def test_current_invocation_cursor_and_manager_scoping(self):
        es=entries(lines('missing'))
        old=entry('SECRET_SENTINEL',__MONOTONIC_TIMESTAMP='99',_SYSTEMD_INVOCATION_ID='old')
        manager=entry(None,_PID='1',_COMM='systemd',_EXE='/usr/lib/systemd/systemd',_TRANSPORT='journal',_SYSTEMD_UNIT='init.scope')
        records,_=validate_records([old,manager]+es,scope4())
        self.assertEqual(len(es),len(records))
        es[-1]['_SYSTEMD_INVOCATION_ID']='other'
        with self.assertRaises(Rejected):validate_records(es,scope4())

    def test_unavailable_or_unknown_logs_override_classified_failure(self):
        # The failure enum cannot excuse unknown/redacted/unavailable records.
        for tail in (None,'SECRET_SENTINEL'):
            with self.assertRaises(Rejected):validate_records(entries(lines('captcha')+[tail]),scope4())

    def test_nonfatal_original_proxy_error_can_continue_but_never_ready(self):
        records,_=validate_records(entries(lines('proxy_failure')+[lines('success')[-1]]),scope4())
        with self.assertRaises(DiagnosticStartupFailed):readiness_observation(records,30,schema=4)

    def test_early_exit_grounding_required(self):
        with patch.object(operations,'journal') as read:
            operations.classify_early_exit([],{'InvocationID':'old'})
            read.assert_not_called()
        records,_=validate_records(entries(lines('missing')),scope4())
        sample=dict(coherent=True,exe_match=True,comparison={'argv_match':True},InvocationID='new',MainPID='123')
        with patch.object(operations,'load',return_value=scope4()),patch.object(operations,'save'),patch.object(operations,'journal',return_value=records):
            with self.assertRaises(DiagnosticStartupFailed):operations.classify_early_exit([sample],{'InvocationID':'old'})

    def test_real_linux_proc_end_to_end_all_cases(self):
        for name in ('success',*CASES):
            with self.subTest(name=name):self.run_invocation(name)

    def run_invocation(self,name):
        exe=str(Path(sys.executable).resolve())
        script=str(Path(__file__).with_name('startup_simulated_invocation.py').resolve())
        expected=expected_tokens(exe,['-u',script,name])
        p=subprocess.Popen([exe,'-u',script,name],stdin=subprocess.PIPE,stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True)
        inv=uuid.uuid4().hex
        state=dict(MainPID=str(p.pid),InvocationID=inv,NRestarts='0',ActiveState='active',SubState='running')
        try:
            _,proof=await_stable(lambda:observe_process(state,expected),{'MainPID':'0','InvocationID':'old'},expected)
            self.assertTrue(proof['ARGV_MATCH'] and proof['MAINPID_STABLE'])
            p.stdin.write('GO\n');p.stdin.flush()
            count=len(lines(name))+(8 if name=='success' else 0)
            emitted=[p.stdout.readline().rstrip('\n') for _ in range(count)]
            scope=dict(scope4(),pid=str(p.pid),exe=exe,invocation=inv,uid=str(os.getuid()),gid=str(os.getgid()))
            es=entries(emitted)
            for e in es:e.update(_PID=str(p.pid),_EXE=exe,_SYSTEMD_INVOCATION_ID=inv,_UID=scope['uid'],_GID=scope['gid'])
            raw='\n'.join(json.dumps(e) for e in es)
            def command(*args):
                self.assertIn('--all',args);self.assertIn('--after-cursor=before',args);return raw
            with patch.object(operations,'command',command),patch.object(operations,'load',return_value=scope),patch.object(operations,'save'):
                records=operations.journal(inv)
            if name=='success':
                self.assertEqual('PASS',readiness_observation(records,30,schema=4)['LOG_VALIDATION'])
                p.stdin.write('STOP\n');p.stdin.flush()
            else:
                self.assertEqual(1,p.wait(timeout=5))
                with self.assertRaises(DiagnosticStartupFailed):readiness_observation(records,0,True,4)
            p.stdin.close();self.assertEqual(0 if name=='success' else 1,p.wait(timeout=5))
            self.assertEqual('',p.stderr.read())
        finally:
            if p.poll() is None:p.kill();p.wait(timeout=5)
            for stream in (p.stdin,p.stdout,p.stderr):stream.close()

if __name__=='__main__':unittest.main()
