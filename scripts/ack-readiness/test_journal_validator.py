import json
import unittest
from fixtures import fixture
from journal_validator import FIELDS, Rejected, journal_command, validate_records

SCOPE = dict(schema=3, cursor='before', boot_id='boot', start_monotonic_us=100,
             pid='123', exe='/root/openflux/openflux-ack-diag',
             unit='openflux-user2.service', invocation='invocation')


def entry(message='[ACK-LOG] suppressed=1', **updates):
    value = {'__CURSOR': 'after', '_BOOT_ID': 'boot', '__MONOTONIC_TIMESTAMP': '110',
             '__REALTIME_TIMESTAMP': '1000', '_PID': '123', '_UID': '0', '_GID': '0',
             '_EXE': SCOPE['exe'], '_COMM': 'openflux-ack-di', '_SYSTEMD_UNIT': SCOPE['unit'],
             '_SYSTEMD_INVOCATION_ID': 'invocation', '_TRANSPORT': 'stdout', 'MESSAGE': message}
    value.update(updates)
    return value


def diagnostic(values=None):
    return '2026/09/22 20:00:00.123456 [ACK-DIAG] ' + json.dumps(
        fixture() if values is None else values, separators=(',', ':'))


class ValidatorTests(unittest.TestCase):
    def rejected(self, value, reason=None):
        with self.assertRaises(Rejected) as caught:
            validate_records([value], SCOPE)
        safe = caught.exception.safe
        self.assertNotIn('MESSAGE', safe)
        self.assertNotIn('MESSAGE', safe['metadata'])
        if reason:
            self.assertEqual(reason, safe['reason'])
        return safe

    def test_allowed_ack_diag(self):
        accepted, _ = validate_records([entry(diagnostic())], SCOPE)
        self.assertEqual(set(accepted[0]['values']), FIELDS)

    def test_allowed_suppressed_and_banner(self):
        accepted, _ = validate_records([entry(), entry('written by p1neappleXpress', __CURSOR='banner')], SCOPE)
        self.assertEqual([r['event'] for r in accepted], ['suppressed', 'banner'])

    def test_systemd_manager_record_separate(self):
        accepted, meta = validate_records([entry([255], _PID='1', _COMM='systemd',
            _EXE='/usr/lib/systemd/systemd', _SYSTEMD_UNIT='init.scope', _TRANSPORT='journal')], SCOPE)
        self.assertEqual([], accepted)
        self.assertEqual('system_manager', meta[0]['class'])

    def test_invalid_utf8_diagnostic_fails(self):
        self.assertFalse(self.rejected(entry([255]))['properties']['MESSAGE_VALID_UTF8'])

    def test_embedded_nul_fails(self):
        self.rejected(entry('[ACK-LOG] suppressed=1\0'), 'embedded_nul')

    def test_unknown_text_fails_without_content(self):
        safe = self.rejected(entry('PRIVATE_SENTINEL'), 'unknown_text')
        self.assertNotIn('PRIVATE_SENTINEL', json.dumps(safe))

    def test_multiline_fails(self):
        self.rejected(entry('[ACK-LOG] suppressed=1\n[ACK-LOG] suppressed=1'), 'control_or_multiline')

    def test_unrelated_service_not_application(self):
        accepted, meta = validate_records([entry([255], _PID='9', _EXE='/usr/bin/other',
                                                _SYSTEMD_UNIT='unrelated.service')], SCOPE)
        self.assertEqual([], accepted)
        self.assertEqual('unrelated', meta[0]['class'])

    def test_fresh_cursor_scoping(self):
        command = journal_command(SCOPE)
        self.assertIn('--after-cursor=before', command)
        self.assertIn('--all', command)
        accepted, _ = validate_records([entry('opaque old', __CURSOR='before'), entry()], SCOPE)
        self.assertEqual(1, len(accepted))

    def test_old_prestart_records_excluded(self):
        accepted, _ = validate_records([entry('opaque old', __MONOTONIC_TIMESTAMP='99'),
                                        entry('other boot', _BOOT_ID='old'), entry()], SCOPE)
        self.assertEqual(1, len(accepted))

    def test_large_valid_snapshot_not_rejected(self):
        text = diagnostic()
        self.assertGreater(len(text.encode()), 4096)
        self.assertEqual(1, len(validate_records([entry(text)], SCOPE)[0]))
        self.rejected(entry(None), 'message_unavailable_possible_journal_elision')

    def test_unexpected_app_pid_rejected(self):
        self.rejected(entry(_PID='124'), 'unexpected_application_pid')

    def test_app_wrong_exe_or_invocation_rejected(self):
        self.rejected(entry(_EXE='/usr/bin/other'), 'diagnostic_process_metadata_mismatch')
        self.rejected(entry(_SYSTEMD_INVOCATION_ID='other'), 'diagnostic_process_metadata_mismatch')

    def test_schema_rejects_extra_missing_boolean_negative(self):
        base = fixture()
        for values in [dict(base, extra=1), {k: v for k, v in base.items() if k != 'schema'},
                       dict(base, schema=True), dict(base, schema=-1)]:
            self.rejected(entry(diagnostic(values)), None)

    def test_duplicate_key_and_non_object_rejected(self):
        self.rejected(entry(diagnostic()[:-1] + ',"schema":0}'), 'invalid_snapshot_json')
        self.rejected(entry('2026/09/22 20:00:00.123456 [ACK-DIAG] []'), None)

    def test_byte_array_is_not_silently_ignored(self):
        self.rejected(entry(list(b'[ACK-LOG] suppressed=1')), 'message_not_text')

    def test_previous_process_not_current_application(self):
        scope = dict(SCOPE, previous_pid='122')
        accepted, meta = validate_records([entry([255], _PID='122',
            _EXE='/root/openflux/openflux'), entry()], scope)
        self.assertEqual(1, len(accepted))
        self.assertEqual('previous_process', meta[0]['class'])

    def test_system_manager_identity_not_inferred_from_label(self):
        self.rejected(entry([255], _PID='1', _COMM='systemd', _EXE='/usr/bin/impostor'),
                      'unexpected_application_pid')



    def test_schema_version_three_required(self):
        base = fixture()
        for version in (0, 1, 2, 4):
            self.rejected(entry(diagnostic(dict(base, schema=version))), 'unsupported_schema_version')

    def test_nested_float_string_overflow_rejected_safely(self):
        base = fixture()
        for bad in ({'private': 'SECRET_SENTINEL'}, [1], 1.0, 'SECRET_SENTINEL', 1 << 64):
            safe = self.rejected(entry(diagnostic(dict(base, http_requests=bad))), None)
            self.assertNotIn('SECRET_SENTINEL', json.dumps(safe))

    def test_boolean_counter_rejects_non_boolean_integer(self):
        base = fixture()
        self.rejected(entry(diagnostic(dict(base, ws_connected=2))), 'invalid_boolean_counter')

    def test_all_known_histograms_and_nonzero_snapshot(self):
        from journal_validator import HISTOGRAMS, BUCKETS
        base = fixture()
        for prefix in HISTOGRAMS:
            base[prefix + '_count'] = 1
            base[prefix + '_sum_ns'] = base[prefix + '_max_ns'] = 1000
            base[prefix + '_bytes'] = 100
            base[prefix + '_' + BUCKETS[0]] = 1
        base['http_requests'] = base['http_successes'] = 1
        self.assertEqual(3, validate_records([entry(diagnostic(base))], SCOPE)[0][0]['values']['schema'])

    def test_malformed_histogram_rejected(self):
        base = fixture()
        for key in ('http_duration_count', 'http_duration_sum_ns', 'http_duration_max_ns', 'http_duration_bytes'):
            self.rejected(entry(diagnostic(dict(base, **{key: 1}))), 'invalid_histogram_structure')

    def test_invalid_measurement_is_collected_not_hidden(self):
        base = fixture()
        base.update(correlation_valid=0, counter_consistency=0, sequence_constraint_violations=5)
        self.assertEqual(0, validate_records([entry(diagnostic(base))], SCOPE)[0][0]['values']['correlation_valid'])

    def test_large_unknown_log_content_not_exported(self):
        secret = 'SECRET_SENTINEL' * 400
        safe = self.rejected(entry(secret), 'unknown_text')
        self.assertNotIn('SECRET_SENTINEL', json.dumps(safe))

    def test_invalid_realtime_timestamp_fails_safely(self):
        self.rejected(entry(__REALTIME_TIMESTAMP='SECRET_SENTINEL'), 'invalid_realtime_timestamp')

if __name__ == '__main__':
    unittest.main()
