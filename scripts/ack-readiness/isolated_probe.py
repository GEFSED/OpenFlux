"""One authorized user3 bootstrap start. No retries. Only safe evidence leaves.
Uses the exact CI-pinned validator files. Never run old user2 operations.py.
"""
import datetime
import hashlib
import json
import os
from pathlib import Path
import signal
import stat
import subprocess
import sys
import tarfile
import time
import zipfile
from probe_boundary import (ProbeFailure, restart_delta, manager_context, guard_context,
    make_boundary, remote_receipt, payload_digest, atomic_write, failure_fields,
    termination_fields, NOT_OBSERVED)

HEAD='34ba35a407296038a223c54cd048dcc0e6b67ca5'
BIN_SHA='a61d4c4777d045346590d9fc8613b14500f4a7e0ed5cfbe2f5b8d8f660acd2e0'
PROD_SHA='08fcf4020cd3c7274c7abd78fe386b40d2fcf8515082d3475ced324ad109217c'
ZIP_SHA='d29668c8255bccc79143c78b49cfb65de853f4586bfc15745ad0334fb38e2839'
TAR_SHA='9c4a61d8ed837905a1de9bf2edba3977ec24647046a50fa3662d9f9ab6a49877'
EXPECTED_BOOT_ID='67a7ea38-d0bb-427d-ae1d-bc76acd59e69'
UNIT='openflux-user3.service'
OTHERS=('openflux.service','openflux-user2.service')
PROD=Path('/root/openflux/openflux')
DIAG=Path('/root/openflux/openflux-bootstrap-schema6-34ba35a')
DROP=Path('/run/systemd/system/openflux-user3.service.d/95-schema6-isolated-34ba35a.conf')
ROOT=Path(__file__).resolve().parent
ARGS=['--exit-node','--mode=proxy','--transport=vyandex','--url-file=/root/openflux/user3/document-url','--encryption-key-file=/root/openflux/user3/encryption-key']
FIELDS=['Id','ActiveState','SubState','MainPID','InvocationID','NRestarts','Result','Restart','RestartUSec','DropInPaths','ExecMainStartTimestamp','ActiveEnterTimestamp','ExecMainCode','ExecMainStatus']
REPORT={'SOURCE_HEAD':HEAD,'SCHEMA_VERSION':6,'POST_START_COUNT':0,'DIAGNOSTIC_BINARY_SHA256':BIN_SHA}

class Guard(ProbeFailure):
    def __init__(self, code):
        super().__init__(code.upper())
def require(ok,code):
    if not ok:raise Guard(code)
def utc():return datetime.datetime.now(datetime.timezone.utc).isoformat()
def emit(kind,data):
    try:print(json.dumps({'kind':kind,**data},separators=(',',':')),flush=True)
    except BrokenPipeError:pass
def save():
    atomic_write(ROOT/'result.json', REPORT)
def cmd(*args,timeout=30):
    r=subprocess.run(args,capture_output=True,text=True,timeout=timeout)
    if r.returncode != 0:raise ProbeFailure('CONTROL_COMMAND_FAILED','OPERATOR_CONTROL_FAILURE')
    return r.stdout
def sha(path):return hashlib.sha256(path.read_bytes()).hexdigest()
def show(unit):
    return dict(l.split('=',1) for l in cmd('systemctl','show',unit,'--no-pager',*['-p'+k for k in FIELDS]).splitlines() if '=' in l)
def states():
    # One systemctl transaction for all three requested units.
    raw=cmd('systemctl','show',*OTHERS,UNIT,'--no-pager',*['-p'+k for k in FIELDS])
    parts=[dict(l.split('=',1) for l in b.splitlines() if '=' in l) for b in raw.strip().split('\n\n')]
    result={v['Id']:v for v in parts}
    require(set(result)==set((*OTHERS,UNIT)),'state_unit_inventory_mismatch')
    return result
def exec_args(unit):
    obj=json.loads(cmd('busctl','--json=short','call','org.freedesktop.systemd1','/org/freedesktop/systemd1','org.freedesktop.systemd1.Manager','GetUnit','s',unit))['data'][0]
    value=json.loads(cmd('busctl','--json=short','get-property','org.freedesktop.systemd1',obj,'org.freedesktop.systemd1.Service','ExecStart'))['data']
    found=[]
    def visit(v):
        if type(v) is not list:return
        if len(v)>=2 and type(v[0]) is str and v[0].startswith('/') and type(v[1]) is list and all(type(x) is str for x in v[1]):found.append((v[0],v[1]));return
        for x in v:visit(x)
    visit(value);require(len(found)==1,'unsupported_structured_execstart')
    return found[0]
def no_job():
    rows=json.loads(cmd('busctl','--json=short','call','org.freedesktop.systemd1','/org/freedesktop/systemd1','org.freedesktop.systemd1.Manager','ListJobs'))['data'][0]
    return not any(row[1] in (UNIT,*OTHERS,'openflux-refresh.service','openflux-refresh.timer') for row in rows)
def file_metadata():
    result={}
    baseline=json.loads((ROOT/'hold-baseline.json').read_text())
    for u,item in baseline['inventory'].items():
        for kind,file in item.get('profiles',{}).items():
            p=Path(file['path']);v=p.lstat()
            result[u+':'+kind]=dict(regular=stat.S_ISREG(v.st_mode),uid=v.st_uid,gid=v.st_gid,mode=oct(stat.S_IMODE(v.st_mode)),size=v.st_size,mtime_ns=v.st_mtime_ns,ctime_ns=v.st_ctime_ns,inode=v.st_ino,device=v.st_dev)
    return result

def baseline_metadata():
    baseline=json.loads((ROOT/'hold-baseline.json').read_text())
    return {u+':'+kind:file['metadata'] for u,item in baseline['inventory'].items() for kind,file in item.get('profiles',{}).items()}

def hold_path(unit):return '/run/systemd/system/'+unit+'.d/90-openflux-loop-hold-20260923.conf'

def check_holds():
    for u in (UNIT,*OTHERS,'openflux-refresh.service','openflux-refresh.timer'):
        p=Path(hold_path(u));refresh=u.startswith('openflux-refresh.')
        expected=b'[Unit]\nRefuseManualStart=yes\nConditionPathExists=!/\n' if refresh else b'[Service]\nRestart=no\n'
        require(p.is_file() and not p.is_symlink() and p.read_bytes()==expected,'BlockedByLostRuntimeHoldState')
        props=dict(l.split('=',1) for l in cmd('systemctl','show',u,'--no-pager','-pId','-pActiveState','-pMainPID','-pRestart','-pRefuseManualStart','-pDropInPaths').splitlines() if '=' in l)
        expected_paths={str(p)}|({str(DROP)} if u==UNIT and DROP.exists() else set())
        require(set(props['DropInPaths'].split())==expected_paths,'hold_dropin_scope_changed')
        if refresh:
            require(props['ActiveState']=='inactive' and props.get('MainPID','0')=='0' and props['RefuseManualStart']=='yes','refresh_hold_changed')
            obj=json.loads(cmd('busctl','--json=short','call','org.freedesktop.systemd1','/org/freedesktop/systemd1','org.freedesktop.systemd1.Manager','GetUnit','s',u))['data'][0]
            data=json.loads(cmd('busctl','--json=short','get-property','org.freedesktop.systemd1',obj,'org.freedesktop.systemd1.Unit','Conditions'))['data'];found=[]
            def walk(x):
                if type(x)!=list:return
                if len(x)==5 and type(x[0])==str:found.append(x);return
                for y in x:walk(y)
            walk(data);require(any(x[:4]==['ConditionPathExists',False,True,'/'] for x in found),'refresh_condition_changed')
        else:
            require(props['Restart']=='no','application_restart_hold_changed')
            if u!=UNIT:require(props['MainPID']=='0' and props['ActiveState']=='inactive','other_service_started')
    require(Path('/proc/sys/kernel/random/boot_id').read_text().strip()==EXPECTED_BOOT_ID,'boot_changed')


def ground():
    require(sys.platform=='linux' and os.geteuid()==0,'linux_root_required')
    require(ROOT.parent==Path('/tmp') and ROOT.name.startswith('openflux-schema6-isolated-34ba35a.'),'unexpected_task_directory')
    require(not PROD.is_symlink() and sha(PROD)==PROD_SHA,'production_hash_mismatch')
    before=states()
    require(before[UNIT]['MainPID']=='0','User3UnexpectedlyRecoveredBeforeProbe')
    require(before[UNIT]['ActiveState'] in ('failed','inactive') and before[UNIT]['DropInPaths']==hold_path(UNIT),'user3_not_stopped_clean')
    require(before[UNIT]['Restart']=='no' and before[UNIT]['RestartUSec']=='5s','production_restart_policy_mismatch')
    require(no_job(),'pending_user3_job')
    for u in OTHERS:require(before[u]['ActiveState']=='inactive' and before[u]['MainPID']=='0' and before[u]['Restart']=='no','other_service_not_held')
    check_holds()
    require(file_metadata()==baseline_metadata(),'profile_baseline_changed')
    require(exec_args(UNIT)==(str(PROD),[str(PROD)]+ARGS),'production_execstart_mismatch')
    require(not DIAG.exists() and not DIAG.is_symlink() and not DROP.exists() and not DROP.is_symlink(),'temporary_target_already_exists')
    require(not DROP.parent.is_symlink(),'dropin_directory_symlink')
    require(sha(ROOT/'artifact.zip')==ZIP_SHA and sha(ROOT/'validators.tar')==TAR_SHA,'artifact_archive_mismatch')
    with zipfile.ZipFile(ROOT/'artifact.zip') as z:
        manifest=json.loads(z.read('source-manifest.json'))
        require(manifest['diagnostic_commit']==HEAD and manifest['production_base']=='081d214300c1067f17f6c0d02f84a8491f1a7b98' and manifest['startup_contract']==6 and manifest['bootstrap_contract']==6 and manifest['diagnostics_only_source_verification']=='PASS' and manifest['bootstrap_production_projection']=='PASS','source_manifest_mismatch')
        for flag in ('network_code_match','hook_site_match','authorization_semantics_unchanged','proxy_semantics_unchanged'):require(manifest[flag] is True,'source_grounding_flag')
        build=z.read('diagnostic-build-info.txt').decode()
        require('vcs.revision='+HEAD in build and 'vcs.modified=false' in build and 'GOOS=linux' in build and 'GOARCH=amd64' in build,'binary_build_grounding')
        binary=z.read('openflux-ack-diag')
        require(hashlib.sha256(binary).hexdigest()==BIN_SHA and binary[:4]==b'\x7fELF' and binary[18:20]==b'\x3e\x00','diagnostic_binary_mismatch')
    vd=ROOT/'validator';vd.mkdir(mode=0o700,exist_ok=True)
    with tarfile.open(ROOT/'validators.tar') as t:
        for name in ('argv_validator.py','journal_validator.py','schema3.py','schema4.py','schema5.py','schema6.py','schema.json'):
            path='scripts/ack-readiness/'+name;member=t.getmember(path)
            require(member.isfile() and member.size<500000,'validator_member_invalid')
            data=t.extractfile(member).read()
            require(hashlib.sha256(data).hexdigest()==manifest['source_files'][path],'validator_source_mismatch')
            (vd/name).write_bytes(data)
    sys.path.insert(0,str(vd))
    import argv_validator, journal_validator, schema6
    require(schema6.SCHEMA==6,'validator_contract_mismatch')
    require(argv_validator.parse_cmdline(b'a\0b\0')==[b'a',b'b'],'validator_import_smoke')
    return before,binary

def run():
    before,binary=ground()
    from argv_validator import expected_tokens,exec_directive,observe_process,verify_sample,safe_sample
    from journal_validator import validate_journal_json,journal_command,Rejected
    from schema6 import startup_outcome,DiagnosticStartupFailed
    from schema3 import SchemaError
    expected=expected_tokens(str(DIAG),ARGS)
    drop_text='[Service]\nRestart=no\nExecStart=\nExecStart='+exec_directive(expected)+'\n'
    meta=file_metadata();other_args={u:exec_args(u) for u in OTHERS}
    REPORT.update(PRE_STATE=before,USER3_UNIT=UNIT,USER3_EXECSTART_BEFORE=[str(PROD)]+ARGS,PRODUCTION_BINARY_SHA256_BEFORE=PROD_SHA,TEMP_RUNTIME_DROPIN=str(DROP),DIAGNOSTIC_REMOTE_PATH=str(DIAG),SOURCE_GROUNDING='PASS',VALIDATOR_SOURCE_GROUNDING='PASS',BUILD_PLATFORM='linux/amd64')
    emit('grounding',{k:v for k,v in REPORT.items() if k!='PRE_STATE'});emit('pre_state',{'services':before})
    bin_created=drop_created=dir_created=False
    retained=False;new_pid=None;new_inv=None;scope=None;start_process=None;grounded=None
    last_records=[];last_valid=None;proof=None
    boundary=None;cleanup_invocation=None
    REPORT.update(HARNESS_VALID=True, APPLICATION_FAILURE_CLASS=NOT_OBSERVED,
        HARNESS_FAILURE_CLASS=NOT_OBSERVED, BOOTSTRAP_COMPLETION_STATE=NOT_OBSERVED)

    def validate_counter(state):
        guard_context(boundary['context'], manager_context(cmd))
        REPORT['AUTOMATIC_RESTART_DELTA']=restart_delta(boundary['baseline_nrestarts'],state['NRestarts'])
        require(state['Restart']=='no','restart_guard_changed')

    def bind_scope(sample,state):
        nonlocal new_inv,new_pid,grounded
        invocation=state['InvocationID']
        if new_inv is not None:require(invocation==new_inv,'diagnostic_invocation_changed')
        new_inv=invocation;REPORT['NEW_INVOCATION_ID']=new_inv
        if sample['coherent'] and sample['exe_match'] and sample['cmdline_read']:
            verify_sample(sample,expected)
            if new_pid is None:
                new_pid=state['MainPID']
                scope.update(pid=new_pid,invocation=new_inv)
                # Scope survives later assertions too; boundary remains immutable.
                remote_receipt('scope',scope)
                atomic_write(ROOT/'journal-scope.json',scope,exclusive=True)
            require(state['MainPID']==new_pid,'diagnostic_pid_changed')
            grounded=safe_sample(sample,expected,time.monotonic())
            REPORT.update(NEW_MAINPID=new_pid,PROCESS_GROUNDING=grounded)


    def integrity(current=None):
        current=current or states()
        for u in OTHERS:
            require(current[u]==before[u],'other_service_identity_changed')
            require(exec_args(u)==other_args[u],'other_service_configuration_changed')
        require(file_metadata()==meta,'profile_file_metadata_changed')
        check_holds()
        return current
    def journal():
        require(scope is not None,'no_grounded_invocation')
        # Raw MESSAGE remains inside this Linux process; never stored or printed.
        raw=cmd(*journal_command(scope))
        records,provenance=validate_journal_json(raw,scope)
        REPORT['LOG_VALIDATION']='PASS'
        REPORT['LOG_COUNTS']={k:sum(k in r for r in records) for k in ('startup','bootstrap','values')}
        REPORT['BOOTSTRAP_RECORDS']=[r for r in records if 'bootstrap' in r]
        REPORT['STARTUP_RECORDS']=[r for r in records if 'startup' in r]
        REPORT['JOURNAL_RECORD_CLASSES']={k:sum(p['class']==k for p in provenance) for k in ('application','system_manager','previous_process','unrelated')}
        REPORT.update(UNKNOWN_DIAGNOSTIC_LOGS=0,NON_TEXT_DIAGNOSTIC_LOGS=0,UNAVAILABLE_REQUIRED_JOURNAL_RECORDS=0,FRESH_CURSOR_USED=True,INVOCATION_SCOPE='PASS',SECRET_LEAKAGE='NONE_OBSERVED')
        return records
    def capture_classification(records,exited):
        try:last=startup_outcome(records,process_exited=exited)
        except DiagnosticStartupFailed as e:
            REPORT.update(e.safe)
            classified=e.safe.get('CLASSIFIED_EVENT_OBSERVED',False)
            REPORT.update(ERROR_CATEGORY='APPLICATION_STARTUP_FAILURE',APPLICATION_FAILURE_CLASS=e.safe.get('STARTUP_FAILURE_CLASS',NOT_OBSERVED) if classified else NOT_OBSERVED)
            if not classified:REPORT['STARTUP_FAILURE_CLASS']=NOT_OBSERVED
            else:REPORT['BOOTSTRAP_COMPLETION_STATE']='FAILED'
            return False
        if last:
            REPORT.update(STARTUP_STAGE=last['startup_stage'],STARTUP_RESULT=last['startup_result'],STARTUP_FAILURE_CLASS=last['failure_class'],AUTHORIZATION_COMPLETED=last['authorization_completed'],RELAY_WORKERS_STARTED=last['relay_workers_started'],PROXY_INITIALIZED=last['proxy_initialized'])
            return all(last[k] for k in ('authorization_completed','relay_workers_started','proxy_initialized'))
        return False
    def stop_incomplete():
        nonlocal cleanup_invocation
        state=show(UNIT)
        if state['MainPID']!='0':
            sample=observe_process(state,expected)
            require(sample['coherent'] and sample['exe_match'],'cannot_stop_unidentified_process')
            require(new_inv is not None and state['InvocationID']==new_inv,'cleanup_invocation_not_owned')
            cleanup_invocation=new_inv
            REPORT['CLEANUP_STOP_INTENT_INVOCATION']=new_inv
            save()
            # Cleanup of this one invocation, never another startup attempt.
            cmd('systemctl','stop',UNIT)
            require(show(UNIT)['MainPID']=='0','incomplete_process_not_stopped')
            REPORT['INCOMPLETE_DIAGNOSTIC_STOPPED']=True
    try:
        integrity()
        require(show(UNIT)['MainPID']=='0' and no_job(),'user3_state_changed_before_install')
        fd=os.open(DIAG,os.O_WRONLY|os.O_CREAT|os.O_EXCL|os.O_NOFOLLOW,0o755);bin_created=True
        with os.fdopen(fd,'wb') as f:f.write(binary);f.flush();os.fsync(f.fileno())
        require(sha(DIAG)==BIN_SHA and sha(PROD)==PROD_SHA,'installed_hash_mismatch')
        if not DROP.parent.exists():DROP.parent.mkdir(mode=0o755);dir_created=True
        require(sorted(str(p) for p in DROP.parent.iterdir())==[hold_path(UNIT)],'unexpected_runtime_dropin_file')
        fd=os.open(DROP,os.O_WRONLY|os.O_CREAT|os.O_EXCL|os.O_NOFOLLOW,0o644);drop_created=True
        with os.fdopen(fd,'w') as f:f.write(drop_text);f.flush();os.fsync(f.fileno())
        cmd('systemctl','daemon-reload')
        state=show(UNIT)
        require(state['Restart']=='no' and state['MainPID']=='0' and set(state['DropInPaths'].split())=={hold_path(UNIT),str(DROP)} and no_job(),'restart_guard_not_effective')
        require(exec_args(UNIT)==(str(DIAG),[str(DIAG)]+ARGS),'effective_diagnostic_execstart_mismatch')
        integrity()
        context=manager_context(cmd)
        last=json.loads(cmd('journalctl','--no-pager','--all','-n','1','-o','json','--output-fields=__CURSOR').strip())
        require(type(last.get('__CURSOR')) is str,'journal_boundary_unavailable')
        state=show(UNIT) # immediately-before-start baseline, not the earlier preflight
        guard_context(context,manager_context(cmd))
        require(state['MainPID']=='0' and state['Restart']=='no' and no_job(),'user3_not_stopped_at_boundary')
        scope=dict(schema=6,cursor=last['__CURSOR'],start_monotonic_us=time.monotonic_ns()//1000,boot_id=context['boot_id'],unit=UNIT,exe=str(DIAG),previous_pid='0')
        boundary=make_boundary(scope,state,context,expected,utc())
        # The local operator controller fsyncs its evidence directory and ACKs.
        # A disconnected controller or failed write prevents any start.
        remote_receipt('boundary',boundary)
        atomic_write(ROOT/'journal-boundary.json',boundary,exclusive=True)
        REPORT.update(PRE_START_TIMESTAMP_UTC=boundary['pre_start_utc'],PRE_START_NRESTARTS=state['NRestarts'],JOURNAL_BOUNDARY_PERSISTED=True)
        marker=ROOT/'single-start-issued'
        intent={'boundary_sha256':payload_digest(boundary),'maximum_start_count':1}
        remote_receipt('start_intent',intent)
        atomic_write(marker,intent,exclusive=True)
        current=show(UNIT)
        validate_counter(current)
        require(current['MainPID']=='0' and current['InvocationID']==boundary['baseline_invocation'] and no_job(),'state_changed_before_start')
        REPORT['POST_START_COUNT']=1;REPORT['POST_START_TIMESTAMP_UTC']=utc();save()
        start_at=time.monotonic()
        # THE ONLY start call in this script. Never called again, including cleanup.
        try:
            start_process=subprocess.Popen(['systemctl','start',UNIT],stdout=subprocess.PIPE,stderr=subprocess.PIPE)
        except OSError:
            raise ProbeFailure('START_CONTROL_FAILED','OPERATOR_CONTROL_FAILURE') from None
        emit('single_start',{'POST_START_COUNT':1,'UTC':REPORT['POST_START_TIMESTAMP_UTC'],'Restart':'no','startup_timeout_seconds':90})
        stable_id=None;stable_since=None;last_journal=0;failed=False;succeeded=False
        while time.monotonic()-start_at<90:
            state=show(UNIT);sample=observe_process(state,expected);at=time.monotonic()
            if state['InvocationID'] and state['InvocationID']!=boundary['baseline_invocation']:
                bind_scope(sample,state)
                validate_counter(state)
                if sample['coherent'] and sample['exe_match'] and sample['cmdline_read']:
                    verify_sample(sample,expected)
                    if new_pid is None:new_pid=state['MainPID']
                    require(state['MainPID']==new_pid,'diagnostic_pid_changed')
                    scope.update(pid=new_pid,invocation=new_inv)
                    grounded=safe_sample(sample,expected,at)
                    identity=(new_inv,new_pid,sample['start_ticks'])
                    if identity!=stable_id:stable_id=identity;stable_since=at
                    if at-stable_since>=0.3:
                        proof=dict(NEW_INVOCATION_CONFIRMED=True,MAINPID_STABLE=True,EXE_MATCH=True,ARGV_MATCH='PASS',ARGV_EXPECTED_COUNT=len(expected),ARGV_ACTUAL_COUNT=len(sample['argv']),SETTLING_SECONDS=at-stable_since)
                if state['MainPID']=='0' and state['ActiveState'] in ('failed','inactive'):
                    failed=True;break
                if scope.get('pid') and at-last_journal>=1:
                    last_records=journal();last_journal=at
                    try:last_valid=startup_outcome(last_records)
                    except DiagnosticStartupFailed: last_valid=None
                    if last_valid and all(last_valid[k] for k in ('authorization_completed','relay_workers_started','proxy_initialized')):
                        succeeded=True;break
            else:validate_counter(state)
            if at-start_at>5 and int((at-start_at)*10)%100==0:integrity()
            time.sleep(0.05 if at-start_at<5 else 0.25)
        if start_process.poll() is not None:
            out,err=start_process.communicate();REPORT['SYSTEMCTL_START_RETURN_CODE']=start_process.returncode
        REPORT.update(NEW_MAINPID=new_pid,PROCESS_GROUNDING=grounded,PROCESS_PROOF=proof,STARTUP_OBSERVATION_SECONDS=time.monotonic()-start_at)
        require(grounded is not None and scope.get('pid'),'process_grounding_unavailable')
        if failed:
            time.sleep(1)
            last_records=journal();capture_classification(last_records,True)
            REPORT['RESULT']='User3IsolatedSchema6StructureProbeFailed'
            starts=[r['startup'] for r in last_records if 'startup' in r]
            if starts:
                REPORT['STARTUP_RESULT']=starts[-1]['startup_result']
                for key in ('authorization_completed','relay_workers_started','proxy_initialized'):REPORT[key.upper()]=starts[-1][key]
            state=show(UNIT)
            validate_counter(state)
            require(state['MainPID']=='0','failure_not_stopped_without_retry')
            exit_fields=termination_fields(state)
            REPORT.update(USER3_EXIT_CODE=exit_fields['PROCESS_EXIT_CODE'],USER3_EXIT_SIGNAL=exit_fields['PROCESS_EXIT_SIGNAL'],USER3_EXEC_MAIN_CODE=state['ExecMainCode'],USER3_SYSTEMD_RESULT=state['Result'],USER3_RESTART_DELTA_AFTER_START=0)
        elif succeeded:
            require(proof is not None,'success_pid_not_settled')
            last_records=journal();require(capture_classification(last_records,False),'startup_not_successful')
            since=time.monotonic();samples=0
            emit('startup_success',{'USER3_PID':new_pid,'OBSERVE_SECONDS':120})
            while True:
                current=integrity();state=current[UNIT]
                validate_counter(state)
                require(state['ActiveState']=='active' and state['SubState']=='running' and state['MainPID']==new_pid and state['InvocationID']==new_inv,'successful_invocation_changed')
                verify_sample(observe_process(state,expected),expected)
                records=journal();startup_outcome(records);samples+=1
                elapsed=time.monotonic()-since
                if elapsed>=120:break
                if samples%8==0:emit('passive_success_observation',{'seconds':round(elapsed,3),'samples':samples,'PID_UNCHANGED':True})
                time.sleep(min(2,120-elapsed))
            retained=True
            REPORT['BOOTSTRAP_COMPLETION_STATE']='COMPLETED'
            REPORT.update(RESULT='User3IsolatedSchema6StructureProbeSucceeded',SUCCESSFUL_DIAGNOSTIC_PID_RETAINED=True,USER3_PID_CHANGES=0,USER3_INVOCATION_CHANGES=0,USER3_RESTART_DELTA=0,USER3_AUTH_FAILURES=0,USER3_ACTIVE_RUNNING_SAMPLES=samples,USER3_TOTAL_SAMPLES=samples,SUCCESS_OBSERVATION_SECONDS=time.monotonic()-since)
        else:
            last_records=journal();capture_classification(last_records,False)
            raise Guard('bounded_startup_timeout')
        integrity();save()
        emit('captured',{'RESULT':REPORT['RESULT'],'BOOTSTRAP_RECORDS':REPORT.get('BOOTSTRAP_RECORDS',[]),'STARTUP_RECORDS':REPORT.get('STARTUP_RECORDS',[])})
    except BaseException as error:
        # Never reinterpret a harness/control failure as provider failure.
        REPORT.update(failure_fields(error))
        REPORT['RESULT']='ProbeHarnessFailed' if REPORT['ERROR_CATEGORY']=='HARNESS_VALIDATION_FAILURE' else 'ProbeControlFailed'
        if isinstance(error,Rejected):REPORT['HARNESS_FAILURE_CLASS']='JOURNAL_VALIDATION_FAILED'
        retained=False
        emit('probe_blocker',{k:REPORT[k] for k in ('RESULT','ERROR_CATEGORY','HARNESS_FAILURE_CLASS','CONTROL_FAILURE_CLASS','APPLICATION_FAILURE_CLASS')})
    finally:
        # Do not let a disconnect interrupt restoration. No start/restart here.
        signal.signal(signal.SIGHUP,signal.SIG_IGN)
        signal.signal(signal.SIGTERM,signal.SIG_IGN)
        signal.signal(signal.SIGINT,signal.SIG_IGN)
        try:
            if drop_created:
                if not retained:stop_incomplete()
                require(DROP.is_file() and not DROP.is_symlink() and DROP.read_text()==drop_text,'foreign_dropin_during_cleanup')
                DROP.unlink();drop_created=False
                if dir_created:DROP.parent.rmdir()
                cmd('systemctl','daemon-reload')
            final=show(UNIT)
            REPORT.update(termination_fields(final,cleanup_invocation,REPORT['BOOTSTRAP_COMPLETION_STATE']))
            require(final['DropInPaths']==before[UNIT]['DropInPaths'] and final['Restart']==before[UNIT]['Restart'] and final['RestartUSec']==before[UNIT]['RestartUSec'],'original_policy_not_restored')
            require(exec_args(UNIT)==(str(PROD),[str(PROD)]+ARGS),'original_execstart_not_restored')
            if retained:
                require(final['MainPID']==new_pid and final['InvocationID']==new_inv and final['ActiveState']=='active' and final['SubState']=='running','retained_process_changed')
                verify_sample(observe_process(final,expected),expected)
            elif bin_created:
                require(final['MainPID']=='0' and no_job(),'failure_not_stopped_after_restore')
                require(not DIAG.is_symlink() and sha(DIAG)==BIN_SHA,'diagnostic_changed_before_removal')
                DIAG.unlink();bin_created=False
            time.sleep(3)
            final_all=integrity();final=final_all[UNIT]
            REPORT.update(termination_fields(final,cleanup_invocation,REPORT['BOOTSTRAP_COMPLETION_STATE']))
            if not retained:require(final['MainPID']=='0' and no_job(),'retry_loop_resumed')
            else:require(final['MainPID']==new_pid and final['InvocationID']==new_inv,'retained_process_changed_after_restore')
            if boundary is not None:
                try:validate_counter(final)
                except ProbeFailure as error:
                    REPORT['FINAL_OBSERVATION_GUARD']=failure_fields(error)
                    if REPORT['HARNESS_VALID']:REPORT.update(failure_fields(error),RESULT='ProbeHarnessFailed')
            require(sha(PROD)==PROD_SHA,'production_binary_changed')
            REPORT.update(FINAL_STATE=final_all,FINAL_PRODUCTION_SHA256=PROD_SHA,ORIGINAL_PRODUCTION_BINARY_UNCHANGED=True,TEMP_RUNTIME_DROPIN_REMOVED=not DROP.exists(),HOLD_RESTART_NO_PRESERVED=True,ORIGINAL_EXECSTART_RESTORED=True,PERSISTENT_DROPINS_CREATED=False,NETWORK_SETTINGS_CHANGED=False,MAIN_MUTATED=False,USER2_MUTATED=False,REFRESH_HOLD_REMAINS=True,APPLICATION_HOLD_GUARDS_REMAIN=True,MAIN_RESTART_DELTA=0,USER2_RESTART_DELTA=0,MAIN_PID_CHANGED=False,USER2_PID_CHANGED=False,USER3_FINAL_PID=int(final['MainPID']),UNCONTROLLED_RETRY_LOOP_RESUMED=False,DIAGNOSTIC_BINARY_REMOVED='NO_INTENTIONAL_ACTIVE_PROCESS' if retained else not DIAG.exists(),PROFILE_FILE_METADATA_UNCHANGED=True,CONFIGURATION_RESTORATION='PASS')
        except BaseException as error:
            REPORT.update(CONFIGURATION_RESTORATION='BLOCKED',CLEANUP_ERROR=failure_fields(error,cleanup=True))
        save();emit('final',REPORT)

def interrupted(signum,frame):raise ProbeFailure('PROBE_INTERRUPTED','OPERATOR_CONTROL_FAILURE')
if __name__=='__main__':
    os.umask(0o077)
    for sig in (signal.SIGHUP,signal.SIGTERM,signal.SIGINT):signal.signal(sig,interrupted)
    try:
        if sys.argv[1]=='ground':
            before,_=ground();emit('ground_only',{'SOURCE_HEAD':HEAD,'SCHEMA_VERSION':6,'DIAGNOSTIC_BINARY_SHA256':BIN_SHA,'PRODUCTION_SHA256':PROD_SHA,'SOURCE_GROUNDING':'PASS','VALIDATOR_SOURCE_GROUNDING':'PASS','states':before})
        elif sys.argv[1]=='run-with-durable-controller':run()
        else:raise Guard('invalid_mode')
    except BaseException as error:
        REPORT.update(RESULT='BlockedBeforeOneShotProbe',**failure_fields(error))
        save();emit('blocked',REPORT);raise SystemExit(1)
