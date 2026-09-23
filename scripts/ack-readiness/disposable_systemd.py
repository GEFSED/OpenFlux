"""Real systemd v255 integration on an ephemeral GitHub-hosted VM ONLY.

No OpenFlux execution/import, provider network, SSH or credentials. Mutations
are confined to one unique /run unit and its /run fixture. Never run on a VPS.
Manager reexec/reboot are deliberately source-audited, not applied to CI PID1.
"""
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import time
import uuid
from probe_boundary import atomic_write, manager_context, ProbeFailure
from probe_invocation import InvocationEpoch, read_ledger


def run(output):
    assert sys.platform=='linux' and os.geteuid()==0
    assert os.environ.get('GITHUB_ACTIONS')=='true' and os.environ.get('RUNNER_ENVIRONMENT')=='github-hosted'
    assert Path('/proc/1/comm').read_text().strip()=='systemd'
    ident=uuid.uuid4().hex
    unit='codex-oneshot-test-'+ident+'.service'
    root=Path('/run')/('codex-oneshot-test-'+ident)
    conf=Path('/run/systemd/system')/unit
    anchor_name='codex-oneshot-test-'+ident+'.target'
    anchor=Path('/run/systemd/system')/anchor_name
    drop=Path(str(conf)+'.d')
    root.mkdir(mode=0o755)
    evidence={'cases':{},'integration':{},'disposable_unit':unit}

    def command(*args):
        p=subprocess.run(args,capture_output=True,text=True,timeout=30)
        if p.returncode:raise RuntimeError('DISPOSABLE_COMMAND_FAILED:'+args[0]+':'+args[1])
        return p.stdout
    def ctl(action,*args):
        assert action in ('start','stop','restart','reset-failed','kill','daemon-reload')
        return command('systemctl',action,*args,*([unit] if action!='daemon-reload' else []))
    def state():
        return dict(x.split('=',1) for x in command('systemctl','show',unit,
            '-pMainPID','-pExecMainPID','-pInvocationID','-pNRestarts','-pRestart',
            '-pExecMainStartTimestampMonotonic','-pActiveState','-pSubState','-pLoadState').splitlines())
    def wait(predicate):
        until=time.monotonic()+20
        while time.monotonic()<until:
            s=state()
            if predicate(s):return s
            time.sleep(.05)
        raise AssertionError('DISPOSABLE_TIMEOUT')
    def mode(value):(root/'mode').write_text(value)
    def policy(value):
        drop.mkdir(exist_ok=True)
        (drop/'90-test.conf').write_text('[Service]\nRestart='+value+'\n')
        ctl('daemon-reload')
    def seed():
        ctl('stop');policy('always');mode('fail');ctl('start')
        wait(lambda s:int(s['NRestarts'])>=3)
        mode('hold')
        s=wait(lambda s:s['MainPID']!='0' and s['SubState']=='running')
        time.sleep(.4)
        return state()
    def boundary():
        command('journalctl','--sync')
        anchor=json.loads(command('journalctl','--all','--no-pager','-o','json','-n','1',
            '--output-fields=__CURSOR,__MONOTONIC_TIMESTAMP'))
        s=state();context=manager_context(command)
        b=dict(target_unit=unit,baseline_invocation=s['InvocationID'],baseline_nrestarts=int(s['NRestarts']),
            context=context,scope=dict(cursor=anchor['__CURSOR'],start_monotonic_us=int(anchor['__MONOTONIC_TIMESTAMP'])))
        p=root/('boundary-'+uuid.uuid4().hex+'.json');atomic_write(p,b,exclusive=True)
        assert json.loads(p.read_text())==b
        return b
    def begin():
        b=boundary();epoch=InvocationEpoch(b);epoch.consume_start();ctl('start')
        wait(lambda s:s['MainPID']!='0');time.sleep(.1)
        epoch.observe(state(),manager_context(command),read_ledger(command,b));epoch.finish()
        return epoch
    def check(e):e.observe(state(),manager_context(command),read_ledger(command,e.boundary))
    def invalid(e,action):
        action();time.sleep(.4)
        try:check(e)
        except ProbeFailure as error:
            assert error.code=='ADDITIONAL_INVOCATION_OBSERVED',error.code
            return error.code
        raise AssertionError('SECOND_INVOCATION_NOT_DETECTED')

    try:
        version=command('systemctl','--version').splitlines()[0]
        assert version.startswith('systemd 255 '),version
        evidence['systemd_version']=version
        (root/'fixture.py').write_text('''import time
from pathlib import Path
mode=Path(__file__).with_name('mode')
print('DISPOSABLE_PROCESS_START',flush=True)
time.sleep(.2)
while mode.read_text()=='hold':time.sleep(.04)
raise SystemExit(1)
''')
        conf.write_text('[Unit]\nDescription=Disposable one-shot lifecycle fixture\nStartLimitIntervalSec=0\n'
            '[Service]\nType=simple\nExecStart=/usr/bin/python3 '+str(root/'fixture.py')+'\nRestart=always\nRestartSec=100ms\n')
        # An active target's Wants reference keeps this unit loaded after stop.
        # Without it GC can discard the object before we observe deferred flush.
        anchor.write_text('[Unit]\nDescription=Disposable reference anchor\nWants='+unit+'\n')
        mode('hold')
        ctl('daemon-reload')
        command('systemctl','start',anchor_name)
        s=seed();n=int(s['NRestarts']);assert n>0
        ctl('daemon-reload');assert int(state()['NRestarts'])==n
        evidence['cases']['daemon_reload_active']=[n,int(state()['NRestarts'])]
        policy('no');assert int(state()['NRestarts'])==n
        evidence['cases']['runtime_dropin_add']=[n,int(state()['NRestarts'])]
        ctl('stop');s=state();assert s['MainPID']=='0'
        evidence['cases']['stop_active']=[n,int(s['NRestarts'])]
        assert int(s['NRestarts'])==n
        e=begin();assert e.post_baseline==0
        evidence['cases']['stopped_to_explicit_start']=[n,0]
        evidence['integration']['historical_counter_reset_one_invocation']=e.fields()
        before=state();ctl('start');time.sleep(.1);check(e);e.finish()
        assert state()['InvocationID']==before['InvocationID']
        evidence['integration']['duplicate_start_already_active']='PASS_NO_NEW_INVOCATION'
        evidence['integration']['external_restart']=invalid(e,lambda:ctl('restart'))
        ctl('stop');e=begin()
        mode('fail');wait(lambda s:s['MainPID']=='0');check(e);e.finish()
        evidence['integration']['exit_restart_no']='PASS_NO_RETRY'
        mode('hold')
        evidence['integration']['external_start_after_exit']=invalid(e,lambda:ctl('start'))
        ctl('stop');e=begin()
        evidence['integration']['refresh_like_restart']=invalid(e,lambda:ctl('restart'))
        # True automatic restart: collect journal-only second invocation before
        # checking the secondary counter or changed effective policy.
        ctl('stop');policy('no');e=begin();policy('always')
        old=state()['InvocationID'];mode('fail')
        wait(lambda s:s['InvocationID']!=old);mode('hold');time.sleep(.3)
        policy('no')
        try:check(e)
        except ProbeFailure as error:
            assert error.code=='ADDITIONAL_INVOCATION_OBSERVED',error.code
            evidence['integration']['automatic_restart']=error.code
        else:raise AssertionError('AUTOMATIC_RESTART_NOT_DETECTED')
        s=seed();n=int(s['NRestarts']);policy('no');mode('fail')
        wait(lambda s:s['ActiveState']=='failed');s=state()
        assert int(s['NRestarts'])==n
        mode('hold');ctl('start');assert int(state()['NRestarts'])==0
        evidence['cases']['failed_to_explicit_start']=[n,0]
        s=seed();n=int(s['NRestarts']);ctl('restart');time.sleep(.1)
        assert int(state()['NRestarts'])==0
        evidence['cases']['explicit_restart_running']=[n,0]
        s=seed();n=int(s['NRestarts']);ctl('reset-failed');assert int(state()['NRestarts'])==0
        evidence['cases']['reset_failed_running']=[n,0]
        s=seed();n=int(s['NRestarts']);policy('no');(drop/'90-test.conf').unlink();ctl('daemon-reload')
        assert int(state()['NRestarts'])==n
        evidence['cases']['runtime_dropin_remove']=[n,n]
        ctl('stop');n=int(state()['NRestarts']);ctl('daemon-reload')
        assert int(state()['NRestarts'])==n
        evidence['cases']['daemon_reload_inactive']=[n,n]
        command('systemctl','stop',anchor_name);anchor.unlink()
        saved=conf.read_text();conf.unlink();drop.rmdir();ctl('daemon-reload')
        # Unit GC is asynchronous; a freshly loaded object has no restart history.
        time.sleep(2)
        unloaded=state()
        assert unloaded['LoadState']=='not-found' and int(unloaded['NRestarts'])==0
        evidence['cases']['unloaded_unit_object']={'LoadState':unloaded['LoadState'],'NRestarts':int(unloaded['NRestarts'])}
        conf.write_text(saved);ctl('daemon-reload');mode('hold');ctl('start')
        evidence['cases']['unit_remove_reload_start']=[n,int(state()['NRestarts'])]
        assert int(state()['NRestarts'])==0
        evidence['source_only_cases']=['manager-reexec preserves serialized count/flush flag','reboot creates fresh unit/boot context']
        evidence['RESULT']='PASS'
    finally:
        ctl('stop')
        if anchor.exists():
            command('systemctl','stop',anchor_name)
            anchor.unlink()
        if conf.exists():conf.unlink()
        if drop.exists():shutil.rmtree(drop)
        ctl('daemon-reload')
        assert root.parent==Path('/run') and root.name=='codex-oneshot-test-'+ident
        shutil.rmtree(root)
        evidence['DISPOSABLE_CLEANUP']='PASS'
        Path(output).write_text(json.dumps(evidence,indent=2)+'\n')
    return evidence

if __name__=='__main__':
    run(sys.argv[1])
