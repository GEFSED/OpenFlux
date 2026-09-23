"""User2-only ACK observation, external strict journal validator. Linux only."""
import datetime
import hashlib
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import sys
import tempfile
import time
import zipfile
from journal_validator import Rejected, journal_command, validate_journal_json
from argv_validator import ReadinessError, expected_tokens, exec_directive, observe_process, verify_sample, await_stable
from schema6 import DiagnosticStartupFailed, startup_outcome

BASE = '081d214300c1067f17f6c0d02f84a8491f1a7b98'
HEAD = os.environ.get('ACK_DIAGNOSTIC_SOURCE_HEAD', '')
ORIGINAL_HASH = '08fcf4020cd3c7274c7abd78fe386b40d2fcf8515082d3475ced324ad109217c'
DIAG_HASH = os.environ.get('ACK_DIAGNOSTIC_BINARY_SHA256', '')
# A future separately authorized deployment supplies all three verified pins.
# This local/CI task never invokes install or switch.
ZIP_HASH = os.environ.get('ACK_ARTIFACT_ZIP_SHA256', '')
ROOT = Path('/root/openflux')
EVIDENCE = Path('/tmp/openflux-ack-schema6-evidence')
ARCHIVE = Path('/tmp/openflux-ack-schema6.zip')
DROP = Path('/run/systemd/system/openflux-user2.service.d/ack-schema6.conf')
UNIT = 'openflux-user2.service'
OTHERS = ['openflux.service', 'openflux-user3.service', 'openflux-refresh.service']
FIELDS = ['Id', 'ActiveState', 'MainPID', 'ExecMainStartTimestamp', 'ActiveEnterTimestamp', 'NRestarts']
ARGS = ['--exit-node', '--mode=proxy', '--transport=vyandex',
        '--url-file=/root/openflux/user2/document-url',
        '--encryption-key-file=/root/openflux/user2/encryption-key']
from measurement import idle_proof, capture_result, readiness_observation


def require(condition, code):
    if not condition:
        raise RuntimeError(code)


def command(*args):
    result = subprocess.run(args, capture_output=True, text=True, timeout=30)
    require(result.returncode == 0, 'command_failed_' + args[0])
    return result.stdout


def utc():
    return datetime.datetime.now(datetime.timezone.utc).isoformat(timespec='microseconds')


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def show(unit, extra=()):
    result = command('systemctl', 'show', unit, '--no-pager',
                     *['--property=' + k for k in FIELDS + list(extra)])
    return dict(line.split('=', 1) for line in result.splitlines() if '=' in line)


def save(name, data):
    target = EVIDENCE / name
    require(not target.is_symlink(), 'evidence_symlink')
    fd, tmp = tempfile.mkstemp(prefix='.evidence-', dir=EVIDENCE)
    try:
        with os.fdopen(fd, 'w') as stream:
            json.dump(data, stream, indent=2)
            stream.write('\n')
        os.replace(tmp, target)
    finally:
        if os.path.exists(tmp):
            os.unlink(tmp)


def load(name):
    return json.loads((EVIDENCE / name).read_text())


def initialize():
    require(os.geteuid() == 0, 'root_required')
    if not EVIDENCE.exists():
        EVIDENCE.mkdir(mode=0o700)
    info = EVIDENCE.lstat()
    require(stat.S_ISDIR(info.st_mode) and info.st_uid == 0 and
            stat.S_IMODE(info.st_mode) & 0o077 == 0, 'unsafe_evidence_directory')


def original():
    require(ROOT.resolve() == ROOT, 'unexpected_directory')
    require(not (ROOT / 'openflux').is_symlink(), 'original_symlink')
    require(sha(ROOT / 'openflux') == ORIGINAL_HASH, 'original_hash_mismatch')


def argv(state, name):
    expected = expected_tokens(str(ROOT / name), ARGS)
    return verify_sample(observe_process(state, expected), expected)


def restart_and_observe(previous, name):
    expected = expected_tokens(str(ROOT / name), ARGS)
    samples = []
    process = subprocess.Popen(['systemctl', 'restart', UNIT], stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    try:
        def read():
            if process.poll() not in (None, 0):
                raise ReadinessError('systemctl_restart_failed')
            state = show(UNIT, ['ExecStart', 'DropInPaths', 'InvocationID', 'SubState', 'Type'])
            return observe_process(state, expected)
        observed, proof = await_stable(read, previous, expected, on_sample=samples.append)
        process.communicate(timeout=2)
        require(process.returncode == 0, 'systemctl_restart_failed')
        proof['MAINPID_RACE_OBSERVED'] = any(
            s['ActiveState'] == 'active' and s['InvocationID'] != previous['InvocationID']
            and s['MainPID'] == observed['state']['MainPID']
            and (not s['exe_match'] or not s['coherent'] or
                 (s['comparison'] is not None and not s['comparison']['argv_match']))
            for s in samples)
        proof['MISSING_TERMINAL_NUL_OBSERVED'] = any(
            s['comparison'] is not None and s['comparison']['argv_match']
            and not s['comparison']['terminal_nul'] for s in samples)
        save(name + '-argv-proof.json', proof)
        return observed['state'], proof
    except ReadinessError as error:
        save(name + '-argv-failure.json', {'reason': error.reason, 'safe': error.safe})
        print(json.dumps({'ARGV_VALIDATION_FAILURE': {'reason': error.reason, 'safe': error.safe}}))
        if name == 'openflux-ack-diag':
            classify_early_exit(samples, previous)
        raise
    finally:
        save(name + '-argv-samples.json', samples)
        if process.poll() is None:
            process.terminate()
            try:
                process.communicate(timeout=2)
            except subprocess.TimeoutExpired:
                process.kill()
                process.communicate()


def classify_early_exit(samples, previous):
    """Classification only, never readiness. Preserve failed settling proof.

    Require at least one exact coherent /proc exe+argv observation for a new
    invocation. No identity is guessed from a journal label or formatted argv.
    """
    grounded = [s for s in samples if s['coherent'] and s['exe_match']
        and s.get('comparison') and s['comparison']['argv_match']
        and s['InvocationID'] and s['InvocationID'] != previous.get('InvocationID')]
    if not grounded:return
    s=grounded[-1]
    scope=load('journal-boundary.json')
    scope.update(schema=6,pid=s['MainPID'],invocation=s['InvocationID'])
    save('journal-scope.json',scope)
    records=journal(s['InvocationID'])
    startup_outcome(records,process_exited=True)


def others_unchanged():
    require({unit: show(unit) for unit in OTHERS} == load('preflight.json')['others'], 'other_service_changed')


def override():
    return '[Service]\nExecStart=\nExecStart=' + exec_directive(expected_tokens(str(ROOT / 'openflux-ack-diag'), ARGS)) + '\n'


def install():
    require(re.fullmatch('[a-f0-9]{40}',HEAD or '') is not None and
            re.fullmatch('[a-f0-9]{64}',DIAG_HASH or '') is not None and
            re.fullmatch('[a-f0-9]{64}',ZIP_HASH or '') is not None, 'explicit_artifact_pins_required')
    original()
    require(not (EVIDENCE / 'preflight.json').exists(), 'already_initialized')
    state = show(UNIT, ['ExecStart', 'DropInPaths', 'InvocationID'])
    require(state['ActiveState'] == 'active' and state['DropInPaths'] == '', 'user2_not_original_clean')
    argv(state, 'openflux')
    others = {unit: show(unit) for unit in OTHERS}
    require(others['openflux.service']['ActiveState'] == 'active' and
            others['openflux-user3.service']['ActiveState'] == 'active', 'other_service_inactive')
    require(sha(ARCHIVE) == ZIP_HASH, 'archive_hash_mismatch')
    target = ROOT / 'openflux-ack-diag'
    require(not target.exists() and not target.is_symlink(), 'diagnostic_target_exists')
    with zipfile.ZipFile(ARCHIVE) as archive:
        manifest = json.loads(archive.read('source-manifest.json'))
        require(manifest['production_base'] == BASE and manifest['diagnostic_commit'] == HEAD and
                manifest['diagnostics_only_source_verification'] == 'PASS' and
                manifest.get('startup_contract') == 5, 'manifest_mismatch')
        build_info = archive.read('diagnostic-build-info.txt').decode('utf-8')
        require('vcs.revision=' + HEAD in build_info and 'vcs.modified=false' in build_info and
                'GOOS=linux' in build_info and 'GOARCH=amd64' in build_info, 'build_info_mismatch')
        data = archive.read('openflux-ack-diag')
        require(hashlib.sha256(data).hexdigest() == DIAG_HASH and data[:4] == b'\x7fELF' and data[18:20] == b'\x3e\x00', 'diagnostic_hash_or_format_mismatch')
    save('preflight.json', {'utc': utc(), 'user2': state, 'others': others,
                           'original_sha256': ORIGINAL_HASH, 'head': HEAD})
    fd, temporary = tempfile.mkstemp(prefix='.ack-install-', dir=ROOT)
    try:
        with os.fdopen(fd, 'wb') as stream:
            stream.write(data)
            stream.flush()
            os.fsync(stream.fileno())
        os.chmod(temporary, stat.S_IMODE((ROOT / 'openflux').stat().st_mode))
        os.replace(temporary, target)
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)
    original()
    others_unchanged()
    print(json.dumps({'INSTALLED_SEPARATELY': DIAG_HASH, 'USER2_NOT_SWITCHED': True,
                      'ORIGINAL_BINARY_UNCHANGED': True, 'OTHER_SERVICES_UNTOUCHED': True}))


def journal(invocation):
    require(re.fullmatch('[a-f0-9]{32}', invocation) is not None, 'invalid_invocation')
    scope = load('journal-scope.json')
    require(scope['invocation'] == invocation, 'journal_invocation_mismatch')
    raw = command(*journal_command(scope))
    try:
        records, provenance = validate_journal_json(raw, scope)
    except Rejected as error:
        save('rejected-metadata.json', error.safe)
        print(json.dumps({'REJECTED_LOG': error.safe}))
        raise RuntimeError('application_log_rejected')
    save('journal-provenance.json', provenance)
    return records


def before_start_cursor(previous_pid):
    raw = command('journalctl', '--no-pager', '-n', '1', '-o', 'json', '--output-fields=__CURSOR')
    entries = [json.loads(line) for line in raw.splitlines()]
    require(len(entries) == 1 and isinstance(entries[0].get('__CURSOR'), str), 'cursor_missing')
    return {'schema':6,'cursor': entries[0]['__CURSOR'], 'start_monotonic_us': time.monotonic_ns() // 1000,
            'boot_id': Path('/proc/sys/kernel/random/boot_id').read_text().strip().replace('-', ''),
            'unit': UNIT, 'exe': str(ROOT / 'openflux-ack-diag'), 'previous_pid': previous_pid}


def settled(values):
    return (values['http_inflight_requests'] == 0 and
            values['http_requests'] == values['http_successes'] + values['http_failures'] and
            values['http_duration_count'] == values['http_requests'] and
            values['counter_consistency'] == 1)


def rollback():
    original()
    previous = show(UNIT, ['InvocationID'])
    if DROP.exists():
        require(not DROP.is_symlink() and DROP.read_text() == override(), 'foreign_override')
        DROP.unlink()
        try:
            DROP.parent.rmdir()
        except OSError:
            pass
        command('systemctl', 'daemon-reload')
        restart_and_observe(previous, 'openflux')
    state = show(UNIT, ['ExecStart', 'DropInPaths', 'InvocationID'])
    require(state['ActiveState'] == 'active' and state['NRestarts'] == '0' and state['DropInPaths'] == '', 'rollback_not_clean')
    argv(state, 'openflux')
    original()
    others_unchanged()
    target = ROOT / 'openflux-ack-diag'
    if target.exists():
        require(not target.is_symlink() and sha(target) == DIAG_HASH, 'diagnostic_file_changed')
        target.unlink()
    if ARCHIVE.exists():
        require(not ARCHIVE.is_symlink() and sha(ARCHIVE) == ZIP_HASH, 'archive_changed')
        ARCHIVE.unlink()
    save('rollback.json', {'utc': utc(), 'user2': state, 'others': {unit: show(unit) for unit in OTHERS},
                           'original_sha256': ORIGINAL_HASH, 'diagnostic_binary_removed': True})
    print(json.dumps({'ROLLBACK': 'PASS', 'USER2_ACTIVE': True, 'USER2_PID': state['MainPID'],
                      'NRestarts': state['NRestarts'], 'DropInPaths': state['DropInPaths'],
                      'FINAL_EXECSTART': state['ExecStart'], 'ORIGINAL_BINARY_UNCHANGED': True,
                      'OTHER_SERVICES_UNTOUCHED': True, 'DIAGNOSTIC_BINARY_REMOVED': True}))


def switch():
    original()
    others_unchanged()
    require(sha(ROOT / 'openflux-ack-diag') == DIAG_HASH, 'installed_hash_mismatch')
    previous = show(UNIT, ['ExecStart', 'DropInPaths', 'InvocationID'])
    require(previous['DropInPaths'] == '', 'unexpected_override')
    argv(previous, 'openflux')
    save('before-switch.json', {'utc': utc(), 'user2': previous})
    require(not DROP.parent.is_symlink() and not DROP.is_symlink(), 'override_symlink')
    DROP.parent.mkdir(mode=0o755, parents=True, exist_ok=True)
    fd, temporary = tempfile.mkstemp(prefix='.ack-diag-', dir=DROP.parent)
    applied = False
    try:
        with os.fdopen(fd, 'w') as stream:
            stream.write(override())
        os.chmod(temporary, 0o644)
        os.replace(temporary, DROP)
        applied = True
        command('systemctl', 'daemon-reload')
        scope = before_start_cursor(previous['MainPID'])
        save('journal-boundary.json', scope)
        current, argv_proof = restart_and_observe(previous, 'openflux-ack-diag')
        scope.update(pid=current['MainPID'], invocation=current['InvocationID'])
        save('journal-scope.json', scope)
        started = time.monotonic()
        while True:
            state = show(UNIT, ['ExecStart', 'DropInPaths', 'InvocationID'])
            changed = not (state['ActiveState'] == 'active' and state['NRestarts'] == '0' and state['InvocationID'] == current['InvocationID'] and state['MainPID'] == current['MainPID'])
            # Read the originally grounded invocation even if MainPID became 0.
            # Validate ALL its messages before interpreting any failure enum.
            records = journal(current['InvocationID'])
            readiness = readiness_observation(records,time.monotonic()-started,
                                               process_exited=changed,schema=6)
            require(not changed,'startup_failed')
            require(state['DropInPaths'] == str(DROP), 'unexpected_override')
            argv(state, 'openflux-ack-diag')
            snapshots = [r for r in records if 'values' in r]
            if readiness:
                last = snapshots[-1]
                v = last['values']
                require(v['ws_connect_failures'] == 0 and v['ws_reconnects'] == 0, 'carrier_startup_error')
                if settled(v) and v['ws_connected'] == 1:
                    idle_proof(snapshots)
                    original()
                    others_unchanged()
                    evidence = {'ready_utc': utc(), 'user2': state, 'baseline': last,
                                'records': records, 'sha256': DIAG_HASH, 'argv_proof': argv_proof}
                    save('ready.json', evidence)
                    print(json.dumps({'SERVER_READY_FOR_ACK_DIAG': True, 'USER2_PID': state['MainPID'],
                                      'UTC': evidence['ready_utc'], 'DIAG_BINARY_SHA256': DIAG_HASH,
                                      'NRestarts': state['NRestarts'], 'WS_CONNECTED': True,
                                      'SECRETS_IN_LOGS': False, 'LOG_VALIDATION': 'PASS',
                                      'ARGV_PROOF': argv_proof, 'FRESH_CURSOR_USED': True, 'OBSERVATION_SECONDS': time.monotonic() - started,
                                      'ORIGINAL_BINARY_UNCHANGED': True,
                                      'OTHER_SERVICES_UNTOUCHED': True, 'BASELINE': last,
                                      **readiness}))
                    return
            require(time.monotonic() - started < 50, 'ready_timeout')
            time.sleep(2)
    except BaseException as error:
        if isinstance(error,DiagnosticStartupFailed):
            save('startup-failure.json',error.safe)
            print(json.dumps(error.safe))
        if applied:
            rollback()
        raise
    finally:
        if os.path.exists(temporary):
            os.unlink(temporary)


def capture():
    received = utc()
    reply_time_us = time.time_ns() // 1000
    ready = load('ready.json')
    deadline = time.monotonic() + 40
    try:
        while True:
            state = show(UNIT, ['ExecStart', 'DropInPaths', 'InvocationID'])
            require(state['InvocationID'] == ready['user2']['InvocationID'] and state['ActiveState'] == 'active'
                    and state['NRestarts'] == '0', 'service_changed_during_test')
            argv(state, 'openflux-ack-diag')
            records = journal(state['InvocationID'])
            snapshots = [r for r in records if 'values' in r]
            require(bool(snapshots), 'missing_snapshots')
            last = snapshots[-1]
            if last['time_us'] >= reply_time_us and settled(last['values']):
                base = ready['baseline']
                result = capture_result(ready, records, state, received, utc())
                save('complete.json', result)
                others_unchanged()
                print(json.dumps({k: v for k, v in result.items() if k != 'RECORDS'}))
                return
            require(time.monotonic() < deadline, 'no_settled_endpoint')
            time.sleep(2)
    finally:
        rollback()


if __name__ == '__main__':
    os.umask(0o077)
    try:
        initialize()
        action = sys.argv[1]
        if action == 'install':
            install()
        elif action == 'switch':
            switch()
        elif action == 'capture':
            capture()
        elif action == 'rollback':
            rollback()
        else:
            raise RuntimeError('invalid_action')
    except BaseException as error:
        if isinstance(error,DiagnosticStartupFailed):print(json.dumps(error.safe))
        else:
            safe = str(error) if isinstance(error, RuntimeError) else type(error).__name__
            print(json.dumps({'OPERATION_FAILED': safe}))
        sys.exit(1)
