"""Exact production grounding plus an explicit startup-observation-only diff."""
import hashlib
import json
from pathlib import Path
import re
import subprocess
import sys

BASE='081d214300c1067f17f6c0d02f84a8491f1a7b98'
FROZEN='6a2c677fb5d049dbde232289128eb681148e019d'
def blob(path):return subprocess.check_output(['git','show',FROZEN+':'+path])
def changed_since(ref):return set(subprocess.check_output(['git','diff','--name-only',ref,'HEAD']).decode().splitlines())

allowed={
    'main.go','internal/ackdiag/runtime.go','internal/ackdiag/startup.go',
    'internal/ackdiag/startup_test.go','startup_process_test.go',
    'utils/ack_diag_log.go','utils/debug.go',
    'scripts/ack-readiness/schema4.py','scripts/ack-readiness/journal_validator.py',
    'scripts/ack-readiness/measurement.py','scripts/ack-readiness/operations.py',
    'scripts/ack-readiness/test_journal_validator.py','scripts/ack-readiness/test_end_to_end.py',
    'scripts/ack-readiness/test_startup.py','scripts/ack-readiness/startup_simulated_invocation.py',
    'scripts/ack-diag-verify.py','.github/workflows/ci.yml','docs/ACK_STARTUP_OBSERVABILITY.md',
}
assert not subprocess.check_output(['git','status','--porcelain']).strip(), 'dirty_source'
assert changed_since(FROZEN)<=allowed, sorted(changed_since(FROZEN)-allowed)

# Every old packet/HTTP/WS hook and network/auth/proxy function is byte-identical.
# No dependency, configuration, buffer, timer, worker, or queue change is allowed.
files=subprocess.check_output(['git','ls-tree','-r','--name-only',FROZEN,
    'transport','tunnel','network','mobile','go.mod','go.sum',
    'internal/ackdiag/correlator.go','internal/ackdiag/lifecycle.go',
    'internal/ackdiag/correlator_test.go','internal/ackdiag/lifecycle_test.go',
    'internal/ackdiag/schema3_fixture_test.go','scripts/ack-diag-patches.json',
    'scripts/ack-readiness/argv_validator.py','scripts/ack-readiness/schema3.py']).decode().splitlines()
for path in files:assert Path(path).read_bytes()==blob(path),('frozen_source_changed',path)

# Removing the enumerated observation calls reconstructs exact previous main.
main=Path('main.go').read_text()
banner='''\tif _, err := fmt.Print("written by p1neappleXpress\\n"); err != nil {
\t\tackdiag.StartupFailure(ackdiag.DiagnosticInit, ackdiag.SchemaFailure)
\t}
'''
assert main.count(banner)==1
main=main.replace(banner,'\tfmt.Print("written by p1neappleXpress\\n")\n')
main=re.sub(r'^[ \t]*ackdiag\.(?:BeginStartup|StartupOK|StartupBegin|StartupFailure|StartupTransportFailure)\([^\n]*\)\n','',main,flags=re.M)
assert main.encode()==blob('main.go'),'main_control_flow_changed'

# Shared logger changes are limited to a format-only observer and closed
# suppression in diagnostic mode. Non-diagnostic logging is preserved.
debug=Path('utils/debug.go').read_text()
debug=debug.replace('\t"universal-bypass-tool/internal/ackdiag"\n','')
debug=debug.replace('\t"log"\n','\t"log"\n\t"os"\n')
debug=debug.replace('log.New(ackDebugOutput(),','log.New(os.Stderr,')
debug=debug.replace('\tackdiag.ObserveStartupFormat(format)\n','')
debug=debug.replace('\t\t\tif ackLogging.Load(){message="[ACK-LOG] suppressed=1"}\n','')
assert debug.encode()==blob('utils/debug.go'),'nonlogging_debug_change'

log=Path('utils/ack_diag_log.go').read_text()
for added in ('\t"sync/atomic"\n','\nvar ackLogging atomic.Bool\n','\tackLogging.Store(true)\n',
    'func ackDebugOutput() io.Writer {\n\tif ackLogging.Load(){return ackRedactedWriter{out:os.Stderr}}\n\treturn os.Stderr\n}\n'):
    assert log.count(added)==1
    log=log.replace(added,'')
assert log.encode()==blob('utils/ack_diag_log.go'),'unexpected_log_change'

runtime=Path('internal/ackdiag/runtime.go').read_text()
runtime=runtime.replace('\tStartupOK(SnapshotLoop)\n','')
runtime=runtime.replace('values:=SnapshotForLog(Default.Snapshot())','values:=Default.Snapshot()')
runtime=runtime.replace('\tif err!=nil{StartupFailure(SnapshotLoop,SchemaFailure);return}\n\tif err=logger.Output(2,"[ACK-DIAG] "+string(data));err!=nil{StartupFailure(SnapshotLoop,SchemaFailure)}',
                        '\tif err==nil{logger.Printf("[ACK-DIAG] %s",data)}')
assert runtime.encode()==blob('internal/ackdiag/runtime.go'),'correlation_attachment_changed'

head=subprocess.check_output(['git','rev-parse','HEAD']).decode().strip()
changed=changed_since(BASE)
manifest=dict(production_base=BASE,diagnostic_commit=head,
    diagnostics_only_source_verification='PASS',startup_contract=4,
    startup_base=FROZEN,network_code_match=True,hook_site_match=True,
    authorization_semantics_unchanged=True,proxy_semantics_unchanged=True,
    source_files={p:hashlib.sha256(Path(p).read_bytes()).hexdigest() for p in sorted(changed)})
out=Path(sys.argv[1]);out.mkdir(parents=True,exist_ok=True)
(out/'source-manifest.json').write_text(json.dumps(manifest,indent=2)+'\n')
print('PASS: frozen network/auth/proxy/hooks/correlator; diagnostic startup/logging additions only')
