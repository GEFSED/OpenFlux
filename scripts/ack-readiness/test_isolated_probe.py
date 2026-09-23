"""Exercise the ACTUAL one-shot worker run(), not a second toy state machine.

All systemctl/proc/clock/artifact adapters are offline fakes. Real schema-6 Go
fixtures pass through the unchanged strict journal validator. No network.
"""
import copy
import hashlib
import json
from pathlib import Path
import tempfile
import unittest
from contextlib import ExitStack
from unittest.mock import patch

import isolated_probe as probe
from probe_boundary import LocalEvidence, ProbeFailure
from test_probe_boundary import CONTEXT
from test_structure import fixture, sequence
from test_startup import lines


class Simulation:
    def __init__(self, root, mode='failure', baseline=541):
        self.root=Path(root);self.mode=mode;self.baseline=baseline
        self.now=1.;self.started_at=None;self.starts=0;self.stops=0;self.reloaded=0
        self.inv='c'*32;self.old='b'*32;self.pid='1234';self.current=baseline
        self.assertion_injected=False
        self.binary=b'ELF-fixture-not-executable';self.prod=self.root/'production'
        self.prod.write_bytes(b'production fixture')
        self.diag=self.root/'openflux-bootstrap-schema6-34ba35a'
        self.drop=self.root/'unit.d'/'95-schema6-isolated-34ba35a.conf'
        self.drop.parent.mkdir();self.hold=self.drop.parent/'90-hold.conf'
        self.hold.write_text('[Service]\nRestart=no\n')
        self.evidence=LocalEvidence(self.root/'operator-evidence')
        self.events=[];self.cleanup_done=False;self.other_states={
            u:dict(Id=u,MainPID='0',ActiveState='inactive',SubState='dead',InvocationID='d'*32,NRestarts='542',Restart='no',RestartUSec='5s') for u in probe.OTHERS}

    def elapsed(self):return self.now-self.started_at if self.started_at else 0

    def state(self):
        dead=self.cleanup_done or (self.starts and self.mode not in ('success','assertion','autorestart','regression','boot','manager','scope_write') and self.elapsed()>=.8)
        if self.starts and self.elapsed()>.15:
            if self.mode=='autorestart':self.current=self.baseline+1
            if self.mode=='regression':self.current=max(0,self.baseline-1)
        s=dict(Id=probe.UNIT,MainPID=self.pid if self.starts and not dead else '0',
            ActiveState='active' if self.starts and not dead else 'inactive',
            SubState='running' if self.starts and not dead else 'dead',
            InvocationID=self.inv if self.starts else self.old,NRestarts=str(self.current),
            Restart='no',RestartUSec='5s',DropInPaths=str(self.hold)+(' '+str(self.drop) if self.drop.exists() else ''),
            ExecMainCode='2' if self.cleanup_done else '1',ExecMainStatus='15' if self.cleanup_done else '1',
            Result='success' if self.cleanup_done else 'exit-code')
        if dead and self.mode=='external_sigterm':s.update(ExecMainCode='2',ExecMainStatus='15',Result='signal')
        return s

    def states(self):return dict(copy.deepcopy(self.other_states),**{probe.UNIT:self.state()})

    def context(self,command):
        value=dict(CONTEXT)
        if self.starts and self.elapsed()>.15:
            if self.mode=='boot':value['boot_id']='e'*32
            if self.mode=='manager':value['manager_bus_owner']=':1.99'
        return value

    def receipt(self,name,payload):
        if self.mode=='boundary_write' and name=='boundary':raise ProbeFailure('EVIDENCE_WRITE_FAILED','OPERATOR_CONTROL_FAILURE')
        if self.mode=='scope_write' and name=='scope':raise ProbeFailure('EVIDENCE_WRITE_FAILED','OPERATOR_CONTROL_FAILURE')
        return self.evidence.accept(name,payload)

    def popen(self,args,**kwargs):
        assert args==['systemctl','start',probe.UNIT]
        assert self.starts==0
        assert (self.evidence.root/'boundary.json').exists()
        assert (self.evidence.root/'start_intent.json').exists()
        if self.mode=='start_control_failure':raise OSError('private control error')
        self.starts+=1;self.started_at=self.now
        owner=self
        class Process:
            returncode=0
            def poll(self):return 0
            def communicate(self,**kwargs):return b'',b''
        return Process()

    def observe(self,state,expected):
        if self.mode in ('assertion','cleanup_failure') and self.elapsed()>.15 and not self.assertion_injected:
            self.assertion_injected=True
            raise AssertionError('secret-bearing underlying error must not escape')
        return dict(state=state,proc_exists=state['MainPID']!='0',cmdline_read=state['MainPID']!='0',
            coherent=True,exe_match=True,start_ticks='150',exe_class='EXPECTED',raw=b'\0'.join(expected)+b'\0',argv=expected,read_error=None)

    def journal(self):
        # Only complete schema records. The process state controls exit timing.
        if self.elapsed()<.4:texts=lines('missing',6)[:3]
        elif self.mode in ('early_exit','external_sigterm'):texts=lines('missing',6)[:3]
        else:texts=sequence(fixture('public_root' if self.mode!='success' else 'legacy'),self.mode=='success')
        b=json.loads((self.evidence.root/'boundary.json').read_text())
        rows=[]
        for i,text in enumerate(texts):
            rows.append(dict(MESSAGE=text,__CURSOR='after-'+str(i),
                __MONOTONIC_TIMESTAMP=str(b['scope']['start_monotonic_us']+i+1),__REALTIME_TIMESTAMP=str(1000000+i),
                _BOOT_ID=CONTEXT['boot_id'],_PID=self.pid,_EXE=str(self.diag),_SYSTEMD_UNIT=probe.UNIT,
                _SYSTEMD_INVOCATION_ID=self.inv,_TRANSPORT='stdout',_UID='0',_GID='0'))
        return '\n'.join(map(json.dumps,rows))

    def cmd(self,*args,**kwargs):
        if args[:2]==('systemctl','daemon-reload'):
            self.reloaded+=1;return ''
        if args[:2]==('systemctl','stop'):
            assert args[2]==probe.UNIT
            if self.mode=='cleanup_failure':raise ProbeFailure('CONTROL_COMMAND_FAILED','OPERATOR_CONTROL_FAILURE')
            self.stops+=1;self.cleanup_done=True;return ''
        if args[0]=='journalctl':
            assert '--all' in args
            if '-n' in args:return '{}' if self.mode=='cursor_missing' else json.dumps({'__CURSOR':'original-cursor'})
            assert '--after-cursor=original-cursor' in args
            return self.journal()
        raise AssertionError('unexpected command')

    def args(self,unit):
        exe=self.diag if unit==probe.UNIT and self.drop.exists() else self.prod
        return (str(exe),[str(exe)]+probe.ARGS)

    def sleep(self,seconds):self.now+=seconds

    def run(self):
        with ExitStack() as stack:
            def obj(where,name,value):stack.enter_context(patch.object(where,name,value))
            for name,value in dict(ROOT=self.root,PROD=self.prod,DIAG=self.diag,DROP=self.drop,
                PROD_SHA=hashlib.sha256(self.prod.read_bytes()).hexdigest(),BIN_SHA=hashlib.sha256(self.binary).hexdigest(),
                REPORT={'POST_START_COUNT':0},ground=lambda:(self.states(),self.binary),
                states=self.states,show=lambda unit:self.state(),file_metadata=lambda:{},exec_args=self.args,
                check_holds=lambda:None,no_job=lambda:True,hold_path=lambda unit:str(self.hold),
                cmd=self.cmd,manager_context=self.context,remote_receipt=self.receipt,
                emit=lambda kind,data:self.events.append((kind,copy.deepcopy(data)))).items():obj(probe,name,value)
            obj(probe.subprocess,'Popen',self.popen)
            obj(probe.time,'monotonic',lambda:self.now)
            obj(probe.time,'monotonic_ns',lambda:int(self.now*1e9))
            obj(probe.time,'sleep',self.sleep)
            obj(probe.signal,'signal',lambda *a:None)
            import argv_validator
            obj(argv_validator,'observe_process',self.observe)
            probe.run()
            return copy.deepcopy(probe.REPORT)


class ActualWorkerTests(unittest.TestCase):
    def simulate(self,mode='failure',baseline=541):
        with tempfile.TemporaryDirectory() as d:
            sim=Simulation(d,mode,baseline);r=sim.run()
            if mode!='cleanup_failure':self.assertFalse(sim.drop.exists())
            self.assertTrue(sim.hold.exists())
            self.assertEqual(b'production fixture',sim.prod.read_bytes())
            self.assertLessEqual(sim.starts,1)
            return sim,r,{p.name:json.loads(p.read_text()) for p in sim.evidence.root.glob('*.json')}

    def test_full_mock_one_shot_nonzero_baseline(self):
        sim,r,files=self.simulate()
        self.assertEqual(1,sim.starts);self.assertEqual(0,sim.stops)
        self.assertTrue(r['HARNESS_VALID']);self.assertEqual(0,r['AUTOMATIC_RESTART_DELTA'])
        self.assertEqual('APPLICATION_STARTUP_FAILURE',r['ERROR_CATEGORY'])
        self.assertEqual('auth_client_config_missing',r['APPLICATION_FAILURE_CLASS'])
        self.assertEqual('APPLICATION_EXIT',r['PROCESS_TERMINATION_OWNER'])
        self.assertEqual(1,r['PROCESS_EXIT_CODE']);self.assertEqual('FAILED',r['BOOTSTRAP_COMPLETION_STATE'])
        self.assertTrue(r['BOOTSTRAP_RECORDS']);self.assertEqual('PASS',r['CONFIGURATION_RESTORATION'])
        self.assertEqual('original-cursor',files['boundary.json']['scope']['cursor'])
        self.assertEqual('original-cursor',files['scope.json']['cursor'])
        print('SIMULATED_POST_START_COUNT=1 SIMULATED_AUTOMATIC_RESTART_DELTA=0 SIMULATED_HARNESS_VALID=YES')

    def test_full_mock_automatic_restart_negative(self):
        sim,r,_=self.simulate('autorestart')
        self.assertEqual(1,sim.starts);self.assertFalse(r['HARNESS_VALID'])
        self.assertEqual('AUTOMATIC_RESTART_OBSERVED',r['HARNESS_FAILURE_CLASS'])
        self.assertEqual(1,r['AUTOMATIC_RESTART_DELTA'])
        self.assertEqual('NOT_OBSERVED',r['APPLICATION_FAILURE_CLASS'])
        self.assertEqual('HARNESS_CLEANUP',r['PROCESS_TERMINATION_OWNER'])
        self.assertEqual('PASS',r['CONFIGURATION_RESTORATION'])

    def test_counter_regression_boot_and_manager_fail_closed(self):
        for mode,reason in (('regression','NRESTARTS_BASELINE_INVALIDATED'),('boot','BOOT_ID_CHANGED'),('manager','SYSTEMD_MANAGER_IDENTITY_CHANGED')):
            with self.subTest(mode=mode):
                sim,r,_=self.simulate(mode)
                self.assertEqual(1,sim.starts);self.assertEqual(reason,r['HARNESS_FAILURE_CLASS'])
                if mode=='regression':self.assertEqual('NOT_OBSERVED',r['AUTOMATIC_RESTART_DELTA'])

    def test_starts_once_and_retains_same_successful_process(self):
        sim,r,_=self.simulate('success')
        self.assertEqual(1,sim.starts);self.assertEqual(0,sim.stops)
        self.assertTrue(r['HARNESS_VALID']);self.assertTrue(r['SUCCESSFUL_DIAGNOSTIC_PID_RETAINED'])
        self.assertGreaterEqual(r['SUCCESS_OBSERVATION_SECONDS'],120)
        self.assertEqual(0,r['AUTOMATIC_RESTART_DELTA'])

    def test_all_historical_nonzero_baselines_in_actual_flow(self):
        for n in (0,541,1000,99999):
            with self.subTest(n=n):
                _,r,_=self.simulate(baseline=n)
                self.assertTrue(r['HARNESS_VALID']);self.assertEqual(0,r['AUTOMATIC_RESTART_DELTA'])

    def test_assertion_cleanup_sigterm_and_durable_cursor(self):
        sim,r,files=self.simulate('assertion')
        self.assertEqual(1,sim.starts);self.assertEqual(1,sim.stops)
        self.assertEqual('HARNESS_VALIDATION_FAILURE',r['ERROR_CATEGORY'])
        self.assertEqual('HARNESS_CLEANUP',r['PROCESS_TERMINATION_OWNER'])
        self.assertEqual('SIGTERM',r['PROCESS_EXIT_SIGNAL'])
        self.assertEqual('NOT_OBSERVED',r['BOOTSTRAP_COMPLETION_STATE'])
        self.assertEqual('NOT_OBSERVED',r['APPLICATION_FAILURE_CLASS'])
        self.assertNotIn('secret-bearing',json.dumps(r))
        self.assertEqual('original-cursor',files['boundary.json']['scope']['cursor'])

    def test_boundary_write_failure_or_cursor_missing_never_starts(self):
        for mode in ('boundary_write','cursor_missing'):
            sim,r,_=self.simulate(mode)
            self.assertEqual(0,sim.starts);self.assertEqual(0,sim.stops)
            self.assertFalse(r['HARNESS_VALID']);self.assertEqual('PASS',r['CONFIGURATION_RESTORATION'])
            self.assertEqual('NOT_OBSERVED',r['PROCESS_TERMINATION_OWNER'])
            self.assertEqual('NOT_OBSERVED',r['PROCESS_EXIT_CODE'])

    def test_scope_write_failure_after_start_preserves_original_boundary(self):
        sim,r,files=self.simulate('scope_write')
        self.assertEqual(1,sim.starts);self.assertEqual(1,sim.stops)
        self.assertEqual('OPERATOR_CONTROL_FAILURE',r['ERROR_CATEGORY'])
        self.assertEqual('original-cursor',files['boundary.json']['scope']['cursor'])
        self.assertNotIn('scope.json',files)

    def test_exit_before_http_does_not_invent_schema_fields(self):
        sim,r,_=self.simulate('early_exit')
        self.assertEqual(0,sim.stops);self.assertEqual('APPLICATION_EXIT',r['PROCESS_TERMINATION_OWNER'])
        self.assertEqual('NOT_OBSERVED',r['STARTUP_FAILURE_CLASS'])
        self.assertEqual('NOT_OBSERVED',r['BOOTSTRAP_COMPLETION_STATE'])
        self.assertEqual([],r['BOOTSTRAP_RECORDS'])

    def test_external_sigterm_distinguished(self):
        sim,r,_=self.simulate('external_sigterm')
        self.assertEqual(0,sim.stops);self.assertEqual('EXTERNAL_OR_UNKNOWN_SIGNAL',r['PROCESS_TERMINATION_OWNER'])
        self.assertEqual('SIGTERM',r['PROCESS_EXIT_SIGNAL'])

    def test_cleanup_failure_keeps_primary_failure(self):
        sim,r,_=self.simulate('cleanup_failure')
        self.assertEqual(1,sim.starts)
        self.assertEqual('HARNESS_VALIDATION_FAILURE',r['ERROR_CATEGORY'])
        self.assertEqual('CLEANUP_FAILURE',r['CLEANUP_ERROR']['ERROR_CATEGORY'])
        self.assertEqual('BLOCKED',r['CONFIGURATION_RESTORATION'])

    def test_start_control_failure_never_retries(self):
        sim,r,files=self.simulate('start_control_failure')
        self.assertEqual(0,sim.starts)
        self.assertEqual(0,r['POST_START_COUNT'])
        self.assertEqual('OPERATOR_CONTROL_FAILURE',r['ERROR_CATEGORY'])
        self.assertEqual('START_CONTROL_FAILED',r['CONTROL_FAILURE_CLASS'])
        self.assertIn('start_intent.json',files)
