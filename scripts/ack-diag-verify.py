"""Exact production grounding plus enumerated startup/bootstrap observations."""
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
    'internal/ackdiag/bootstrap.go','internal/ackdiag/bootstrap_test.go',
    'transport/yandex/vyandex.go','transport/yandex/bootstrap_observation_test.go',
    'scripts/ack-readiness/schema5.py','scripts/ack-readiness/test_bootstrap.py',
    'scripts/ack-readiness/bootstrap_simulated_invocation.py',
    'docs/BOOTSTRAP_RESPONSE_OBSERVABILITY.md',
    'mobile/go.mod','mobile/go.sum',
    'scripts/check-mobile-dependency-selection.py',
    'internal/ackdiag/bootstrap_structure.go','internal/ackdiag/bootstrap_structure_test.go',
    'scripts/ack-readiness/schema6.py','scripts/ack-readiness/test_structure.py',
    'docs/BOOTSTRAP_STRUCTURE_SCHEMA6.md',
    'scripts/ack-readiness/probe_boundary.py','scripts/ack-readiness/probe_controller.py',
    'scripts/ack-readiness/isolated_probe.py','scripts/ack-readiness/test_probe_boundary.py',
    'scripts/ack-readiness/test_isolated_probe.py','docs/ISOLATED_PROBE_HARNESS.md',
 'scripts/ack-readiness/probe_invocation.py','scripts/ack-readiness/test_probe_invocation.py',
 'scripts/ack-readiness/test_real_systemd.py','scripts/ack-readiness/disposable_systemd.py',
 'docs/SYSTEMD_V255_RESTART_EPOCH.md',
}
assert not subprocess.check_output(['git','status','--porcelain']).strip(), 'dirty_source'
assert changed_since(FROZEN)<=allowed, sorted(changed_since(FROZEN)-allowed)

# This task may extend only diagnostic records, their validation, tests and docs.
SCHEMA5_BASE='3774ace1f58feb2c303df7db7220826c946d545e'
SCHEMA6_ALLOWED={
 'internal/ackdiag/bootstrap.go','internal/ackdiag/startup.go',
 'internal/ackdiag/bootstrap_structure.go','internal/ackdiag/bootstrap_structure_test.go',
 'internal/ackdiag/bootstrap_test.go',
 'scripts/ack-readiness/schema6.py','scripts/ack-readiness/test_structure.py',
 'scripts/ack-readiness/journal_validator.py','scripts/ack-readiness/measurement.py',
 'scripts/ack-readiness/operations.py','scripts/ack-readiness/test_startup.py',
 'scripts/ack-readiness/test_bootstrap.py','scripts/ack-diag-verify.py',
 '.github/workflows/ci.yml','docs/BOOTSTRAP_STRUCTURE_SCHEMA6.md',
 'scripts/ack-readiness/probe_boundary.py','scripts/ack-readiness/probe_controller.py',
 'scripts/ack-readiness/isolated_probe.py','scripts/ack-readiness/test_probe_boundary.py',
 'scripts/ack-readiness/test_isolated_probe.py','docs/ISOLATED_PROBE_HARNESS.md',
 'scripts/ack-readiness/probe_invocation.py','scripts/ack-readiness/test_probe_invocation.py',
 'scripts/ack-readiness/test_real_systemd.py','scripts/ack-readiness/disposable_systemd.py',
 'docs/SYSTEMD_V255_RESTART_EPOCH.md',
}
assert changed_since(SCHEMA5_BASE)<=SCHEMA6_ALLOWED,'schema6_nonobservability_change'
# Harness repair cannot alter ANY runtime/emitter/validator implementation.
HARNESS_BASE='34ba35a407296038a223c54cd048dcc0e6b67ca5'
HARNESS_ALLOWED={
 'scripts/ack-readiness/probe_boundary.py','scripts/ack-readiness/probe_controller.py',
 'scripts/ack-readiness/isolated_probe.py','scripts/ack-readiness/test_probe_boundary.py',
 'scripts/ack-readiness/test_isolated_probe.py','docs/ISOLATED_PROBE_HARNESS.md',
 'scripts/ack-readiness/probe_invocation.py','scripts/ack-readiness/test_probe_invocation.py',
 'scripts/ack-readiness/test_real_systemd.py','scripts/ack-readiness/disposable_systemd.py',
 'docs/SYSTEMD_V255_RESTART_EPOCH.md',
 'scripts/ack-diag-verify.py','.github/workflows/ci.yml',
}
assert changed_since(HARNESS_BASE)<=HARNESS_ALLOWED,'non_harness_change'
for path in subprocess.check_output(['git','ls-tree','-r','--name-only',HARNESS_BASE]).decode().splitlines():
    if path not in HARNESS_ALLOWED:
        assert Path(path).read_bytes()==subprocess.check_output(['git','show',HARNESS_BASE+':'+path]),('harness_changed_frozen_file',path)
for path in subprocess.check_output(['git','ls-tree','-r','--name-only',SCHEMA5_BASE,
    'transport','tunnel','network','mobile','main.go','utils','go.mod','go.sum',
    'internal/ackdiag/runtime.go','internal/ackdiag/correlator.go','internal/ackdiag/lifecycle.go',
    'scripts/ack-readiness/argv_validator.py','scripts/ack-readiness/schema5.py']).decode().splitlines():
    assert Path(path).read_bytes()==subprocess.check_output(['git','show',SCHEMA5_BASE+':'+path]),('schema6_frozen_source_changed',path)
old_startup=subprocess.check_output(['git','show',SCHEMA5_BASE+':internal/ackdiag/startup.go'])
assert Path('internal/ackdiag/startup.go').read_bytes().replace(b'const LogSchema = 6',b'const LogSchema = 5')==old_startup,'startup_semantics_changed'

# Every old packet/HTTP/WS hook and network/auth/proxy function is byte-identical.
# No dependency, configuration, buffer, timer, worker, or queue change is allowed.
files=subprocess.check_output(['git','ls-tree','-r','--name-only',FROZEN,
    'transport','tunnel','network','mobile','go.mod','go.sum',
    'internal/ackdiag/correlator.go','internal/ackdiag/lifecycle.go',
    'internal/ackdiag/correlator_test.go','internal/ackdiag/lifecycle_test.go',
    'internal/ackdiag/schema3_fixture_test.go','scripts/ack-diag-patches.json',
    'scripts/ack-readiness/argv_validator.py','scripts/ack-readiness/schema3.py']).decode().splitlines()
for path in files:
    if path not in ('transport/yandex/vyandex.go','mobile/go.mod','mobile/go.sum'):
        assert Path(path).read_bytes()==blob(path),('frozen_source_changed',path)

# Only dependency bookkeeping for the HTML tokenizer. The separate Linux
# graph check proves v0.59.0 was already selected by the original mobile graph.
dep='\tgolang.org/x/net v0.59.0 // indirect\n'
mobile_mod=Path('mobile/go.mod').read_text()
assert mobile_mod.count(dep)==1
assert mobile_mod.replace(dep,'').encode()==blob('mobile/go.mod')
checksums=('golang.org/x/net v0.59.0 h1:5zfYln+w5XCxwrnMMJPufRgNoXEaGxl0wo5GqPXyues=\n'
           'golang.org/x/net v0.59.0/go.mod h1:2DA/G1UfVbCpQPeWTmMPGY7Cs2PkBkwu743bVX5PIVg=\n')
assert len(checksums.splitlines())==2
mobile_sum=Path('mobile/go.sum').read_text()
assert mobile_sum.count(checksums)==1
assert mobile_sum.replace(checksums,'').encode()==blob('mobile/go.sum')

# Remove ONLY enumerated metadata calls. Even the ignored ReadAll error remains
# ignored by every production decision. Byte equality proves that regex,
# requests, redirects, statuses, auth, relay and HTTP/WS hooks are unchanged.
auth=Path('transport/yandex/vyandex.go').read_text()
for added in (
    '\tvar bootstrap *ackdiag.BootstrapResponse\n',
    '\tdefer func() { ackdiag.EmitBootstrap(bootstrap) }()\n',
    '\t\t\tbootstrap = ackdiag.ObserveBootstrapTerminal("NETWORK_ERROR", currentURL, err)\n',
    '\t\tbootstrap = ackdiag.ObserveBootstrapResponse(currentURL, resp, body, bodyErr, reClientConfig)\n',
    '\t\t\tackdiag.EmitBootstrap(bootstrap)\n',
    '\t\t\tbootstrap = nil\n',
    '\t\tbootstrap = ackdiag.ObserveBootstrapTerminal("REDIRECT_LIMIT", docURL, nil)\n',
    '\tackdiag.BootstrapSearch(bootstrap, len(m) >= 2)\n',
    '\t\tackdiag.BootstrapParsed(bootstrap, err)\n',
    '\tackdiag.BootstrapParsed(bootstrap, nil)\n',
):
    assert auth.count(added)==1, 'bootstrap_attachment_count'
    auth=auth.replace(added,'')
assert auth.count('body, bodyErr := io.ReadAll(resp.Body)')==1
auth=auth.replace('body, bodyErr := io.ReadAll(resp.Body)','body, _ := io.ReadAll(resp.Body)')
assert auth.encode()==blob('transport/yandex/vyandex.go'),'bootstrap_production_behavior_changed'

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
    diagnostics_only_source_verification='PASS',startup_contract=6,
    bootstrap_contract=6,bootstrap_production_projection='PASS',schema5_baseline=SCHEMA5_BASE,
    startup_base=FROZEN,network_code_match=True,hook_site_match=True,
    authorization_semantics_unchanged=True,proxy_semantics_unchanged=True,
    source_files={p:hashlib.sha256(Path(p).read_bytes()).hexdigest() for p in sorted(changed)})
out=Path(sys.argv[1]);out.mkdir(parents=True,exist_ok=True)
(out/'source-manifest.json').write_text(json.dumps(manifest,indent=2)+'\n')
print('PASS: frozen network/auth/proxy/hooks/correlator; enumerated diagnostic bootstrap/startup observations only')
