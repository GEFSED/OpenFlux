"""Strict, content-safe journal validation. Runs on Linux; no application changes."""
import json
import re
from schema3 import FIELDS, HISTOGRAMS, BUCKETS, SchemaError, validate_snapshot
SAFE_METADATA = ('__REALTIME_TIMESTAMP', '__MONOTONIC_TIMESTAMP', '_SYSTEMD_UNIT',
                 '_PID', '_UID', '_GID', '_COMM', '_EXE', 'SYSLOG_IDENTIFIER',
                 '_TRANSPORT', 'PRIORITY')
OUTPUT_FIELDS = SAFE_METADATA + ('__CURSOR', '_BOOT_ID', '_SYSTEMD_INVOCATION_ID', 'MESSAGE')
STAMP = re.compile(r'^\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2}\.\d{6} ')
# Maximum 32768 loss records, 16 keys/record, <64-byte keys, uint64 values:
# compact Go JSON is <48 MiB even at the hard cap. Bound input at 64 MiB.
# This does not override journald's own storage limits: truncation fails closed.
MAX_MESSAGE_BYTES = 64 * 1024 * 1024


def message_properties(entry):
    """Only properties leave this function; no bytes, fragments or digest."""
    value = entry.get('MESSAGE')
    representation = ('missing' if 'MESSAGE' not in entry else 'null' if value is None
                      else 'string' if isinstance(value, str)
                      else 'byte_array' if isinstance(value, list) and all(
                          type(x) is int and 0 <= x <= 255 for x in value) else 'other')
    data = (value.encode('utf-8', errors='surrogatepass') if isinstance(value, str)
            else bytes(value) if representation == 'byte_array' else None)
    valid = None
    if data is not None:
        try:
            data.decode('utf-8', errors='strict')
            valid = True
        except UnicodeDecodeError:
            valid = False
    return {
        'MESSAGE_PRESENT': 'MESSAGE' in entry,
        'MESSAGE_REPRESENTATION_TYPE': representation,
        'MESSAGE_BYTE_LENGTH': len(data) if data is not None else None,
        'MESSAGE_VALID_UTF8': valid,
        'MESSAGE_HAS_NUL': (0 in data) if data is not None else None,
        'MESSAGE_CONTROL_BYTE_COUNT': sum(x < 32 or x == 127 for x in data) if data is not None else None,
        'MESSAGE_NEWLINE_COUNT': data.count(b'\n') if data is not None else None,
    }


def metadata(entry):
    # Never include _CMDLINE, arbitrary journal fields, or MESSAGE.
    result = {}
    for key in SAFE_METADATA:
        value = entry.get(key)
        result[key] = value if isinstance(value, str) and len(value) <= 512 and all(
            32 <= ord(c) < 127 for c in value) else None
    return result


class Rejected(Exception):
    def __init__(self, reason, entry):
        self.safe = {'reason': reason, 'metadata': metadata(entry),
                     'properties': message_properties(entry)}
        super().__init__(reason)


def fail(reason, entry):
    raise Rejected(reason, entry)


def unique_pairs(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError('duplicate_key')
        result[key] = value
    return result


def allowed_message(entry):
    props = message_properties(entry)
    if props['MESSAGE_REPRESENTATION_TYPE'] == 'null':
        # Unknown/possibly elided MESSAGE is unavailable, NOT proof of binary
        # application output. Still blocks readiness; never ignore it.
        fail('message_unavailable_possible_journal_elision', entry)
    if props['MESSAGE_REPRESENTATION_TYPE'] != 'string':
        fail('message_not_text', entry)
    if not props['MESSAGE_VALID_UTF8']:
        fail('invalid_utf8', entry)
    if props['MESSAGE_HAS_NUL']:
        fail('embedded_nul', entry)
    if props['MESSAGE_CONTROL_BYTE_COUNT']:
        fail('control_or_multiline', entry)
    if props['MESSAGE_BYTE_LENGTH'] > MAX_MESSAGE_BYTES:
        fail('message_size_limit', entry)
    text = entry['MESSAGE']
    if text == 'written by p1neappleXpress':
        return {'event': 'banner'}
    if text == '[ACK-LOG] suppressed=1':
        return {'event': 'suppressed'}
    # This timestamp and prefix are fixed by internal/ackdiag/runtime.go.
    match = STAMP.match(text)
    if not match or not text[match.end():].startswith('[ACK-DIAG] '):
        fail('unknown_text', entry)
    try:
        values = json.loads(text[match.end() + len('[ACK-DIAG] '):],
                            object_pairs_hook=unique_pairs)
    except (ValueError, TypeError):
        fail('invalid_snapshot_json', entry)
    try:
        validate_snapshot(values)
    except SchemaError as error:
        fail(str(error), entry)
    return {'values': values}


def journal_command(scope):
    # --all prevents journalctl replacing >4096-byte valid text with JSON null.
    # Raw output is captured only inside the Linux process, never shown or saved.
    return ['journalctl', '--no-pager', '--all', '-o', 'json',
            '--after-cursor=' + scope['cursor'], '-u', scope['unit'],
            '--output-fields=' + ','.join(OUTPUT_FIELDS)]


def validate_records(entries, scope):
    accepted, provenance = [], []
    seen = set()
    previous_app_time = -1
    for entry in entries:
        try:
            monotonic = int(entry['__MONOTONIC_TIMESTAMP'])
        except (KeyError, ValueError, TypeError):
            fail('missing_record_time', entry)
        if (entry.get('_BOOT_ID') != scope['boot_id'] or
                monotonic < scope['start_monotonic_us'] or
                entry.get('__CURSOR') == scope['cursor']):
            continue
        same_pid = entry.get('_PID') == scope['pid']
        same_exe = entry.get('_EXE') == scope['exe']
        same_unit = entry.get('_SYSTEMD_UNIT') == scope['unit']
        if same_pid:
            if not (same_exe and same_unit and entry.get('_TRANSPORT') == 'stdout'
                    and entry.get('_SYSTEMD_INVOCATION_ID') == scope['invocation']
                    and entry.get('_UID') == scope.get('uid', '0') and entry.get('_GID') == scope.get('gid', '0')):
                fail('diagnostic_process_metadata_mismatch', entry)
            cursor = entry.get('__CURSOR')
            if not isinstance(cursor, str) or not cursor or cursor in seen:
                fail('missing_or_duplicate_application_cursor', entry)
            if monotonic < previous_app_time:
                fail('reordered_application_record', entry)
            seen.add(cursor)
            previous_app_time = monotonic
            record = allowed_message(entry)
            try:
                record['time_us'] = int(entry['__REALTIME_TIMESTAMP'])
                if record['time_us'] <= 0:
                    fail('invalid_realtime_timestamp', entry)
            except (KeyError, ValueError, TypeError):
                fail('invalid_realtime_timestamp', entry)
            record['monotonic_us'] = monotonic
            accepted.append(record)
            kind = 'application'
        elif (entry.get('_PID') == '1' and entry.get('_COMM') == 'systemd' and
              entry.get('_EXE') in ('/usr/lib/systemd/systemd', '/lib/systemd/systemd')):
            kind = 'system_manager'
        elif (same_unit and entry.get('_PID') == scope.get('previous_pid') and
              entry.get('_EXE') == '/root/openflux/openflux'):
            kind = 'previous_process'
        elif same_unit or same_exe:
            # Never silently accept an unexpected child/restarted process.
            fail('unexpected_application_pid', entry)
        else:
            kind = 'unrelated'
        provenance.append({'class': kind, 'metadata': metadata(entry),
                           'properties': message_properties(entry)})
    return accepted, provenance


def validate_journal_json(raw, scope):
    """Same ingestion path in operations.journal and local integration tests."""
    entries = []
    try:
        for line in raw.splitlines():
            entry = json.loads(line, object_pairs_hook=unique_pairs)
            if not isinstance(entry, dict):
                fail('journal_record_not_object', {})
            entries.append(entry)
    except (ValueError, TypeError, RecursionError):
        fail('malformed_journal_json', {})
    return validate_records(entries, scope)
