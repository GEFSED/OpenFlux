"""One-shot control evidence. No provider requests or systemd mutations here."""
import datetime
import hashlib
import json
import os
from pathlib import Path
import re
import select
import signal
import subprocess
import sys
import tempfile

NOT_OBSERVED = 'NOT_OBSERVED'


class ProbeFailure(Exception):
    def __init__(self, code, category='HARNESS_VALIDATION_FAILURE'):
        if not re.fullmatch('[A-Z][A-Z0-9_]*', code):
            raise ValueError('unsafe_failure_code')
        self.code, self.category = code, category
        super().__init__(code)


def counter(value):
    if type(value) is int and 0 <= value <= 0xffffffff:
        return value
    if type(value) is str and re.fullmatch(r'0|[1-9][0-9]{0,9}', value):
        result = int(value)
        if result <= 0xffffffff:
            return result
    raise ProbeFailure('NRESTARTS_INVALID')


def restart_delta(baseline, current):
    before, after = counter(baseline), counter(current)
    if after < before:
        raise ProbeFailure('NRESTARTS_BASELINE_INVALIDATED')
    if after > before:
        raise ProbeFailure('AUTOMATIC_RESTART_OBSERVED')
    return after - before


def manager_context(command):
    """Read-only signals, NOT a universal systemd generation ID.

    PID 1 start time survives reexec. The D-Bus unique owner detects reconnection;
    neither is used to excuse a counter regression. No manager mutation occurs.
    """
    boot = Path('/proc/sys/kernel/random/boot_id').read_text().strip().replace('-', '')
    ticks = Path('/proc/1/stat').read_bytes().rsplit(b')', 1)[1].split()[19].decode('ascii')
    owner = json.loads(command('busctl', '--json=short', 'call', 'org.freedesktop.DBus',
        '/org/freedesktop/DBus', 'org.freedesktop.DBus', 'GetNameOwner', 's',
        'org.freedesktop.systemd1'))['data'][0]
    value = dict(boot_id=boot, manager_start_ticks=ticks, manager_bus_owner=owner)
    validate_context(value)
    return value


def validate_context(value):
    if (type(value) is not dict or set(value) != {'boot_id', 'manager_start_ticks', 'manager_bus_owner'}
        or not re.fullmatch('[a-f0-9]{32}', value.get('boot_id', ''))
        or not re.fullmatch('[1-9][0-9]*', value.get('manager_start_ticks', ''))
        or not re.fullmatch(r':[0-9]+\.[0-9]+', value.get('manager_bus_owner', ''))):
        raise ProbeFailure('MANAGER_CONTEXT_UNAVAILABLE')


def guard_context(baseline, current):
    validate_context(baseline)
    validate_context(current)
    if baseline['boot_id'] != current['boot_id']:
        raise ProbeFailure('BOOT_ID_CHANGED')
    if baseline != current:
        raise ProbeFailure('SYSTEMD_MANAGER_IDENTITY_CHANGED')


def argv_identity(tokens):
    if not tokens or any(type(t) is not bytes or b'\0' in t for t in tokens):
        raise ProbeFailure('EXPECTED_ARGV_INVALID')
    # Only the configured file-path/flag token array is hashed, never file contents.
    return dict(argc=len(tokens), sha256=hashlib.sha256(b'\0'.join(tokens) + b'\0').hexdigest())


def make_boundary(scope, state, context, expected, utc):
    validate_context(context)
    value = dict(version=1, scope=dict(scope), pre_start_utc=utc, target_unit=scope['unit'],
        baseline_nrestarts=counter(state['NRestarts']), baseline_mainpid=state['MainPID'],
        baseline_invocation=state.get('InvocationID', ''), expected_executable=scope['exe'],
        expected_argv_identity=argv_identity(expected), context=dict(context))
    validate_boundary(value)
    return value


def validate_boundary(v):
    keys = {'version', 'scope', 'pre_start_utc', 'target_unit', 'baseline_nrestarts',
            'baseline_mainpid', 'baseline_invocation', 'expected_executable', 'expected_argv_identity', 'context'}
    try:
        assert type(v) is dict and set(v) == keys and type(v['version']) is int and v['version'] == 1
        validate_context(v['context'])
        assert type(v['baseline_nrestarts']) is int
        counter(v['baseline_nrestarts'])
        assert v['baseline_mainpid'] == '0'
        assert re.fullmatch(r'[a-f0-9]{32}|', v['baseline_invocation'])
        assert re.fullmatch(r'openflux-user3\.service', v['target_unit'])
        assert re.fullmatch(r'/[A-Za-z0-9/_.-]{1,1024}', v['expected_executable'])
        when = datetime.datetime.fromisoformat(v['pre_start_utc'])
        assert when.utcoffset() == datetime.timedelta(0)
        s = v['scope']
        assert set(s) == {'schema', 'cursor', 'start_monotonic_us', 'boot_id', 'unit', 'exe', 'previous_pid'}
        assert type(s['schema']) is int and s['schema'] == 6
        assert type(s['cursor']) is str and re.fullmatch(r'[A-Za-z0-9_=;.-]{1,4096}', s['cursor'])
        assert type(s['start_monotonic_us']) is int and s['start_monotonic_us'] > 0
        assert s['boot_id'] == v['context']['boot_id'] and s['previous_pid'] == '0'
        assert s['unit'] == v['target_unit'] and s['exe'] == v['expected_executable']
        a = v['expected_argv_identity']
        assert set(a) == {'argc', 'sha256'} and type(a['argc']) is int and 1 <= a['argc'] <= 256
        assert re.fullmatch('[a-f0-9]{64}', a['sha256'])
    except (AssertionError, KeyError, TypeError, ValueError):
        raise ProbeFailure('JOURNAL_BOUNDARY_UNAVAILABLE') from None


def payload_digest(payload):
    return hashlib.sha256(json.dumps(payload, sort_keys=True, separators=(',', ':')).encode()).hexdigest()


def atomic_write(path, value, *, exclusive=False):
    """Local artifact only. Ack only after file + directory durability/readback.

    Linux fsyncs the directory. Windows uses write-through MoveFileEx for rename.
    Boundary and start intent are immutable; an existing file consumes the run.
    """
    path = Path(path)
    if path.is_symlink() or (exclusive and path.exists()):
        raise ProbeFailure('EVIDENCE_ALREADY_EXISTS', 'OPERATOR_CONTROL_FAILURE')
    data = json.dumps(value, sort_keys=True, separators=(',', ':')).encode() + b'\n'
    tmp = None
    try:
        fd, tmp = tempfile.mkstemp(prefix='.probe-', dir=path.parent)
        with os.fdopen(fd, 'wb') as f:
            f.write(data)
            f.flush()
            os.fsync(f.fileno())
        if exclusive:
            # Hard-link publication is atomic and never replaces an existing run.
            os.link(tmp, path)
            os.unlink(tmp)
        elif os.name == 'nt':
            import ctypes
            if not ctypes.windll.kernel32.MoveFileExW(str(tmp), str(path), 0x1 | 0x8):
                raise OSError('durable_rename_failed')
        else:
            os.replace(tmp, path)
        if os.name != 'nt':
            d = os.open(path.parent, os.O_RDONLY | os.O_DIRECTORY)
            try:
                os.fsync(d)
            finally:
                os.close(d)
        else:
            # Flush the published file as well. Original payload was fsynced.
            with open(path, 'r+b') as f:
                os.fsync(f.fileno())
        if path.read_bytes() != data:
            raise OSError('evidence_readback_failed')
    except OSError:
        raise ProbeFailure('EVIDENCE_WRITE_FAILED', 'OPERATOR_CONTROL_FAILURE') from None
    finally:
        if tmp and os.path.exists(tmp):
            os.unlink(tmp)


class LocalEvidence:
    """Runs on the operator host, outside the SSH worker lifecycle."""
    def __init__(self, directory):
        self.root = Path(directory)
        if self.root.is_symlink():
            raise ProbeFailure('EVIDENCE_DIRECTORY_UNSAFE', 'OPERATOR_CONTROL_FAILURE')
        self.root.mkdir(mode=0o700, parents=True, exist_ok=True)

    def accept(self, name, payload):
        if name == 'boundary':
            validate_boundary(payload)
        elif name == 'scope':
            boundary = json.loads((self.root / 'boundary.json').read_text())
            if (set(payload) != set(boundary['scope']) | {'pid', 'invocation'}
                or any(payload.get(k) != v for k, v in boundary['scope'].items())
                or not re.fullmatch('[1-9][0-9]*', payload.get('pid', ''))
                or not re.fullmatch('[a-f0-9]{32}', payload.get('invocation', ''))
                or payload['invocation'] == boundary['baseline_invocation']):
                raise ProbeFailure('SCOPE_INVALID')
        elif name == 'start_intent':
            boundary = json.loads((self.root / 'boundary.json').read_text())
            if payload != {'boundary_sha256': payload_digest(boundary), 'maximum_start_count': 1}:
                raise ProbeFailure('START_INTENT_INVALID')
        else:
            raise ProbeFailure('UNKNOWN_EVIDENCE_KIND')
        atomic_write(self.root / (name + '.json'), payload, exclusive=True)
        return {'ack': name, 'sha256': payload_digest(payload)}


def remote_receipt(name, payload, timeout=30):
    """Worker blocks BEFORE start until local controller has fsynced evidence."""
    print(json.dumps({'kind': 'durable_evidence', 'name': name, 'payload': payload}), flush=True)
    if not select.select([sys.stdin], [], [], timeout)[0]:
        raise ProbeFailure('LOCAL_EVIDENCE_ACK_TIMEOUT', 'OPERATOR_CONTROL_FAILURE')
    try:
        answer = json.loads(sys.stdin.readline(4096))
    except (ValueError, OSError):
        raise ProbeFailure('LOCAL_EVIDENCE_ACK_FAILED', 'OPERATOR_CONTROL_FAILURE') from None
    if answer != {'ack': name, 'sha256': payload_digest(payload)}:
        raise ProbeFailure('LOCAL_EVIDENCE_ACK_FAILED', 'OPERATOR_CONTROL_FAILURE')


def failure_fields(error, *, cleanup=False):
    category = 'CLEANUP_FAILURE' if cleanup else (
        error.category if isinstance(error, ProbeFailure) else 'HARNESS_VALIDATION_FAILURE')
    code = error.code if isinstance(error, ProbeFailure) else 'UNEXPECTED_HARNESS_EXCEPTION'
    return dict(ERROR_CATEGORY=category, HARNESS_VALID=False,
        HARNESS_FAILURE_CLASS=code if category == 'HARNESS_VALIDATION_FAILURE' else NOT_OBSERVED,
        CONTROL_FAILURE_CLASS=code if category == 'OPERATOR_CONTROL_FAILURE' else NOT_OBSERVED,
        CLEANUP_FAILURE_CLASS=code if cleanup else NOT_OBSERVED,
        APPLICATION_FAILURE_CLASS=NOT_OBSERVED)


def termination_fields(state, cleanup_invocation=None, completion=NOT_OBSERVED):
    code, status = state.get('ExecMainCode'), state.get('ExecMainStatus')
    exited = state.get('MainPID') == '0' and state.get('ActiveState') in ('failed', 'inactive')
    result = dict(PROCESS_TERMINATION_OWNER=NOT_OBSERVED, PROCESS_EXIT_CODE=NOT_OBSERVED,
        PROCESS_EXIT_SIGNAL=NOT_OBSERVED, SYSTEMD_RESULT=state.get('Result', NOT_OBSERVED),
        BOOTSTRAP_COMPLETION_STATE=completion)
    if not exited:
        return result
    if code == '1':
        result.update(PROCESS_TERMINATION_OWNER='APPLICATION_EXIT', PROCESS_EXIT_CODE=int(status))
    elif code in ('2', '3'):
        result['PROCESS_EXIT_SIGNAL'] = signal.Signals(int(status)).name if int(status) in signal.valid_signals() else 'OTHER_SIGNAL'
        # A natural exit racing with stop is not falsely attributed to cleanup.
        owned = cleanup_invocation and cleanup_invocation == state.get('InvocationID') and int(status) in (15, 9)
        result['PROCESS_TERMINATION_OWNER'] = 'HARNESS_CLEANUP' if owned else 'EXTERNAL_OR_UNKNOWN_SIGNAL'
    return result
