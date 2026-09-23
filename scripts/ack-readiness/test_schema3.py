import copy
import json
import os
from pathlib import Path
import unittest

from fixtures import fixture
from schema3 import FIELDS, REASONS, SchemaError, validate_snapshot
from journal_validator import Rejected, validate_journal_json, validate_records
from test_journal_validator import SCOPE, diagnostic, entry


class Schema3Tests(unittest.TestCase):
    def reject(self, v):
        with self.assertRaises(SchemaError):
            validate_snapshot(v)

    def test_all_genuine_go_fixtures_accepted(self):
        files = sorted(Path(os.environ['ACK_SCHEMA_FIXTURE_DIR']).glob('*.json'))
        self.assertGreaterEqual(len(files), 25)
        for path in files:
            with self.subTest(fixture=path.stem):
                v = json.loads(path.read_text())
                validate_snapshot(v)
                self.assertEqual(v, validate_journal_json(json.dumps(entry(diagnostic(v))), SCOPE)[0][0]['values'])

    def test_rst_and_outstanding_are_not_invalidity(self):
        for name in ('rst_outstanding', 'replacement_outstanding', 'outstanding'):
            v = validate_snapshot(fixture(name))
            self.assertEqual(1, v['correlation_valid'])
            self.assertEqual(0, v['delivery_observation_complete'])
        self.assertGreater(fixture('rst_outstanding')['rst_unacked_unique_bytes'], 0)
        self.assertGreater(fixture('replacement_outstanding')['replaced_unacked_unique_bytes'], 0)
        self.assertGreater(fixture('rst_late_ack')['acked_after_rst_bytes'], 0)

    def test_provenance_all_safe_optional_fields(self):
        v = fixture('loss_other')
        validate_snapshot(v)
        for k in ('http_existed', 'http_succeeded', 'retransmitted', 'prior_ack_observed',
                  'late_covering_ack_observed', 'rst_before_loss', 'fin_observed_at_loss', 'epoch_before_loss'):
            self.assertEqual(1, v['loss_0_' + k], k)

    def test_each_reason_conserves_and_is_invalid_not_delivered(self):
        for reason in REASONS:
            v = validate_snapshot(fixture('loss_' + reason))
            self.assertEqual(v['invalidated_unique_bytes'], v['invalidated_' + reason + '_bytes'])
            self.assertEqual(0, v['correlation_valid'])
            self.assertEqual(50, v['acked_unique_downlink_tcp_bytes'])

    def test_negative_required_numbers_and_version(self):
        for key, value in [('schema', 2), ('schema', 4), ('schema', True),
                           ('unique_downlink_tcp_bytes', -1), ('http_requests', '1'),
                           ('http_requests', 1.0), ('http_requests', 1 << 64)]:
            with self.subTest(key=key, value=value):
                v = fixture(); v[key] = value; self.reject(v)
        for key in FIELDS:
            v = fixture(); del v[key]; self.reject(v)

    def test_negative_conservation_and_reason_sum(self):
        for key in ('invalidated_unique_bytes', 'invalidated_rst_bytes', 'unique_downlink_tcp_bytes',
                    'acked_unique_downlink_tcp_bytes', 'outstanding_unique_bytes_current'):
            v = fixture('loss_other'); v[key] += 1; self.reject(v)

    def test_negative_provenance_shapes(self):
        changes = [('loss_0_generation', 0), ('loss_0_generation', 'g1'),
                   ('loss_0_generation', 4097), ('loss_0_reason', 7), ('loss_0_event', 8),
                   ('loss_0_relative_lo', 0), ('loss_0_relative_hi', 1 << 31),
                   ('loss_0_length', 100), ('loss_0_http_existed', 0),
                   ('loss_0_prior_ack_observed', 2)]
        for key, value in changes:
            with self.subTest(key=key):
                v = fixture('loss_other'); v[key] = value; self.reject(v)

    def test_generation_provenance_and_explicit_overflow(self):
        v = validate_snapshot(fixture('explicit_provenance_overflow'))
        self.assertEqual(256, v['generation_evidence_count'])
        self.assertEqual(1, v['provenance_capacity_drops'])
        self.assertEqual(0, v['correlation_valid'])
        for key, value in [('generation_evidence_count', 257), ('provenance_capacity_drops', 0),
                           ('correlation_valid', 1), ('generation_event_0_reason', 0),
                           ('generation_event_0_first_id', 0), ('generation_event_0_candidates', 0),
                           ('generation_event_0_second_id', 1)]:
            bad = dict(v); bad[key] = value; self.reject(bad)

    def test_false_validity_claims(self):
        for key in ('diagnostic_capacity_drops', 'sequence_constraint_violations',
                    'unsupported_serial_range', 'invariant_failures', 'ws_timestamp_missing',
                    'pending_observer_ack_events', 'correlator_evictions'):
            v = fixture(); v[key] = 1; self.reject(v)
        for name in ('ambiguity_ack_overlap', 'explicit_provenance_overflow', 'loss_rst'):
            v = fixture(name); v['correlation_valid'] = 1; self.reject(v)

    def test_forbidden_raw_fields_fail_without_echo(self):
        for key in ('src_ip', 'dst_ip', 'port', 'raw_seq', 'raw_ack', 'payload', 'url',
                    'credentials', 'encryption_key', 'cookie', 'token', 'headers',
                    'loss_0_raw_seq', 'generation_event_0_address'):
            v = fixture(); v[key] = 'PRIVATE_SENTINEL'
            with self.assertRaises(Rejected) as caught:
                validate_records([entry(diagnostic(v))], SCOPE)
            self.assertNotIn('PRIVATE_SENTINEL', json.dumps(caught.exception.safe))
            self.assertNotIn(key, json.dumps(caught.exception.safe))

    def test_large_provenance_and_null_are_distinct(self):
        message = diagnostic(fixture('long_loss_provenance'))
        self.assertGreater(len(message), 16384)
        validate_records([entry(message)], SCOPE)
        with self.assertRaises(Rejected) as caught:
            validate_records([entry(None)], SCOPE)
        self.assertEqual('message_unavailable_possible_journal_elision', caught.exception.safe['reason'])
        self.assertIsNone(caught.exception.safe['properties']['MESSAGE_VALID_UTF8'])

    def test_malformed_journal_and_missing_message(self):
        for raw in ('{', '[]', '{"MESSAGE":"PRIVATE_SENTINEL",',
                    '{"MESSAGE":1,"MESSAGE":2}'):
            with self.assertRaises(Rejected) as caught:
                validate_journal_json(raw, SCOPE)
            self.assertNotIn('PRIVATE_SENTINEL', json.dumps(caught.exception.safe))
        v = entry(); del v['MESSAGE']
        with self.assertRaises(Rejected): validate_records([v], SCOPE)

    def test_mixed_schema_duplicate_and_reordered_fail(self):
        a = entry(diagnostic())
        b = entry(diagnostic(dict(fixture(), schema=2)), __CURSOR='next', __MONOTONIC_TIMESTAMP='120')
        with self.assertRaises(Rejected): validate_records([a, b], SCOPE)
        with self.assertRaises(Rejected): validate_records([a, a], SCOPE)
        b = entry(diagnostic(), __CURSOR='next', __MONOTONIC_TIMESTAMP='109')
        with self.assertRaises(Rejected): validate_records([a, b], SCOPE)
        # Repeated unchanged idle snapshots with distinct cursors ARE legitimate.
        b['__MONOTONIC_TIMESTAMP'] = '120'
        self.assertEqual(2, len(validate_records([a, b], SCOPE)[0]))

    def test_previous_invocation_excluded_but_current_pid_mismatch_fails(self):
        old = entry(diagnostic(dict(fixture(), schema=2)), __MONOTONIC_TIMESTAMP='99',
                    _SYSTEMD_INVOCATION_ID='old')
        self.assertEqual(1, len(validate_records([old, entry(diagnostic())], SCOPE)[0]))
        old['__MONOTONIC_TIMESTAMP'] = '110'
        with self.assertRaises(Rejected): validate_records([old], SCOPE)

    def test_unknown_event_type_and_multiline(self):
        for text in ('2026/09/22 20:00:00.123456 [ACK-UNKNOWN] {}',
                     '[ACK-LOG] suppressed=1\n[ACK-LOG] suppressed=1'):
            with self.assertRaises(Rejected): validate_records([entry(text)], SCOPE)


if __name__ == '__main__':
    unittest.main()
