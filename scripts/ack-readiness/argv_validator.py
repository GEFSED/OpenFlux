"""External Linux process-readiness checks. Never export command-line values."""
from collections import Counter
import os
from pathlib import Path
import re
import time


class ReadinessError(Exception):
    def __init__(self, reason, safe=None):
        self.reason = reason
        self.safe = safe or {}
        super().__init__(reason)


def expected_tokens(executable, arguments):
    return [os.fsencode(executable)] + [s.encode('utf-8') for s in arguments]


def exec_directive(tokens):
    # These exact known paths/flags contain no systemd quoting/escaping syntax.
    # Reject unexpected syntax rather than shell-quote or normalize it.
    if any(not re.fullmatch(rb'[A-Za-z0-9_/=.-]+', t) for t in tokens):
        raise ReadinessError('unsafe_expected_token')
    return b' '.join(tokens).decode('ascii')


def parse_cmdline(raw):
    if not raw:
        return []
    tokens = raw.split(b'\0')
    if raw.endswith(b'\0'):
        tokens.pop()  # Remove exactly one terminal delimiter, not real empty args.
    return tokens


def valid_utf8(value):
    try:
        value.decode('utf-8', errors='strict')
        return True
    except UnicodeDecodeError:
        return False


def kind(token, index):
    if token is None:
        return 'ABSENT'
    if index == 0:
        return 'EXECUTABLE_PATH'
    if token.startswith(b'--url-file='):
        return 'URL_FILE_PATH'
    if token.startswith(b'--encryption-key-file='):
        return 'ENCRYPTION_KEY_FILE_PATH'
    if token.startswith(b'--'):
        return 'FLAG'
    return 'OTHER'


def safe_diff(expected, actual, raw):
    indexes = [i for i in range(max(len(expected), len(actual)))
               if i >= len(expected) or i >= len(actual) or expected[i] != actual[i]]
    reordered = len(expected) == len(actual) and Counter(expected) == Counter(actual) and bool(indexes)
    differences = []
    for i in indexes:
        e = expected[i] if i < len(expected) else None
        a = actual[i] if i < len(actual) else None
        if e is None:
            classification = 'EXTRA_TOKEN'
        elif a is None:
            classification = 'MISSING_TOKEN'
        elif reordered:
            classification = 'ORDER_DIFFERENCE'
        elif not valid_utf8(a):
            classification = 'ENCODING_DIFFERENCE'
        elif i == 0:
            classification = 'EXECUTABLE_PATH'
        elif kind(e, i) in ('URL_FILE_PATH', 'ENCRYPTION_KEY_FILE_PATH'):
            classification = 'FLAG_VALUE_PATH' if kind(a, i) == kind(e, i) else 'FLAG_NAME'
        elif e.split(b'=', 1)[0] != a.split(b'=', 1)[0]:
            classification = 'FLAG_NAME'
        else:
            classification = 'OTHER'
        differences.append({'index': i, 'expected_kind': kind(e, i), 'actual_kind': kind(a, i),
                            'byte_equal': False, 'classification': classification})
    return {'expected_argc': len(expected), 'actual_argc': len(actual),
            'argv0_match': bool(actual) and bool(expected) and actual[0] == expected[0],
            'argv_match': expected == actual, 'mismatch_indexes': indexes,
            'mismatch_classification': differences,
            'terminal_nul': raw.endswith(b'\0'), 'empty_tokens': sum(t == b'' for t in actual),
            'valid_utf8': all(valid_utf8(t) for t in actual),
            'has_cr_lf': any(b'\r' in t or b'\n' in t for t in actual),
            'has_literal_quotes': any(b'"' in t or b"'" in t for t in actual),
            'order_difference': reordered,
            'old_parser_would_match': raw.split(b'\0')[:-1] == expected}


def stat_start(proc):
    # The comm field may contain spaces or parentheses; starttime is field 22.
    return (proc / 'stat').read_bytes().rsplit(b')', 1)[1].split()[19].decode('ascii')


def observe_process(state, expected):
    result = {'state': state, 'proc_exists': False, 'cmdline_read': False,
              'exe_match': False, 'coherent': False, 'start_ticks': None,
              'exe_class': 'UNAVAILABLE', 'raw': b'', 'argv': [], 'read_error': None}
    pid = state.get('MainPID', '')
    if not pid.isdecimal() or int(pid) <= 0:
        return result
    proc = Path('/proc') / pid
    result['proc_exists'] = proc.exists()
    try:
        first_start = stat_start(proc)
        first_exe = os.fsencode(os.readlink(proc / 'exe'))
        raw = (proc / 'cmdline').read_bytes()
        result.update(raw=raw, argv=parse_cmdline(raw), cmdline_read=True)
        last_exe = os.fsencode(os.readlink(proc / 'exe'))
        last_start = stat_start(proc)
        result['start_ticks'] = last_start
        result['coherent'] = first_start == last_start and first_exe == last_exe
        result['exe_match'] = last_exe == expected[0]
        result['exe_class'] = ('EXPECTED' if result['exe_match'] else
            'SYSTEMD_EXECUTOR' if last_exe in (b'/usr/lib/systemd/systemd-executor',
                                              b'/lib/systemd/systemd-executor') else 'OTHER')
    except FileNotFoundError:
        result['read_error'] = 'PROC_DISAPPEARED'
    except PermissionError:
        result['read_error'] = 'PROC_PERMISSION'
    except (OSError, ValueError, IndexError, UnicodeError):
        result['read_error'] = 'PROC_READ_ERROR'
    return result


def safe_sample(sample, expected, at):
    state = sample['state']
    return {'monotonic_ns': int(at * 1e9), 'timestamp_ns': time.time_ns(),
            'ActiveState': state.get('ActiveState'), 'SubState': state.get('SubState'),
            'MainPID': state.get('MainPID'), 'InvocationID': state.get('InvocationID'),
            'proc_exists': sample['proc_exists'], 'cmdline_read': sample['cmdline_read'],
            'read_error': sample['read_error'], 'exe_match': sample['exe_match'],
            'exe_class': sample['exe_class'], 'coherent': sample['coherent'],
            'start_ticks': sample['start_ticks'],
            'comparison': safe_diff(expected, sample['argv'], sample['raw']) if sample['cmdline_read'] else None}


def verify_sample(sample, expected):
    if not sample['proc_exists'] or not sample['cmdline_read'] or not sample['coherent']:
        raise ReadinessError('proc_not_coherent')
    if not sample['exe_match']:
        raise ReadinessError('actual_exe_mismatch')
    diff = safe_diff(expected, sample['argv'], sample['raw'])
    if not diff['argv_match']:
        raise ReadinessError('actual_argv_mismatch', diff)
    return diff


def await_stable(read_sample, previous, expected, clock=time.monotonic, sleep=time.sleep,
                 on_sample=lambda value: None, timeout=5.0, settling=0.3, poll=0.05):
    began = clock()
    identity = None
    stable_since = None
    last = None
    while True:
        sample = read_sample()
        now = clock()
        last = safe_sample(sample, expected, now)
        on_sample(last)
        state = sample['state']
        new_invocation = bool(state.get('InvocationID')) and state['InvocationID'] != previous.get('InvocationID')
        pid = state.get('MainPID', '')
        if new_invocation and (state.get('ActiveState') == 'failed' or state.get('NRestarts') != '0'):
            raise ReadinessError('new_invocation_failed', last)
        candidate = (new_invocation and state.get('ActiveState') == 'active'
                     and state.get('SubState') == 'running' and pid.isdecimal() and int(pid) > 0
                     and pid != previous.get('MainPID') and sample['proc_exists']
                     and sample['cmdline_read'] and sample['coherent'] and sample['exe_match'])
        current = (state.get('InvocationID'), pid, sample['start_ticks']) if candidate else None
        if current is None or current != identity:
            identity = current
            stable_since = now if current is not None else None
        if stable_since is not None and now - stable_since >= settling:
            # This sample was read after settling, with /proc stat+exe checked on both sides.
            verify_sample(sample, expected)
            return sample, {'NEW_INVOCATION_CONFIRMED': True, 'MAINPID_STABLE': True,
                            'SETTLING_SECONDS': now - stable_since, 'EXE_MATCH': True,
                            'ARGV_MATCH': True, 'COMPARISON': last['comparison']}
        if now - began >= timeout:
            raise ReadinessError('stable_process_timeout', last)
        sleep(poll)
