import json
from fixtures import fixture
import unittest
from measurement import idle_proof, capture_result, LIMITS


def baseline():
    return fixture()


def record(v, timestamp=1000):
    return dict(values=v, time_us=timestamp)


class MeasurementTests(unittest.TestCase):
    def test_expected_idle_limits(self):
        self.assertEqual('PASS', idle_proof([record(baseline())])['CONSERVATION'])

    def test_each_limit_enforced(self):
        for key in LIMITS:
            v = baseline(); v[key] += 1
            with self.assertRaisesRegex(ValueError, 'limits_mismatch'):
                idle_proof([record(v)])

    def test_invalid_idle_rejected(self):
        for key in ('correlator_evictions', 'diagnostic_capacity_drops', 'sequence_constraint_violations'):
            v = baseline(); v[key] = 1; v['correlation_valid'] = 0
            with self.assertRaisesRegex(RuntimeError, 'invalid_idle'):
                idle_proof([record(v)])

    def test_phone_traffic_during_idle_rejected(self):
        v = fixture('outstanding')
        with self.assertRaisesRegex(RuntimeError, 'phone_traffic_before_ready'):
            idle_proof([record(v)])

    def test_valid_capture_no_invented_percentiles(self):
        b = record(baseline()); v = baseline()
        v.update(unique_downlink_tcp_bytes=100, acked_unique_downlink_tcp_bytes=100)
        prefix = 'tunnel_first_out_to_ack'
        v[prefix + '_count'] = 1; v[prefix + '_sum_ns'] = v[prefix + '_max_ns'] = 1000
        v[prefix + '_bytes'] = 100; v[prefix + '_lt_50ms'] = 1
        r = capture_result({'baseline': b}, [b, record(v, 2000)], {}, 'done', 'capture')
        self.assertTrue(r['CORRELATION_VALID'])
        self.assertEqual(1000, r['LATENCIES'][prefix]['mean_ns'])
        self.assertIsNone(r['LATENCIES'][prefix]['p95'])

    def test_invalid_intermediate_snapshot_cannot_be_hidden(self):
        b = record(baseline()); bad = baseline(); bad['correlation_valid'] = 0
        r = capture_result({'baseline': b}, [b, record(bad, 1500), record(baseline(), 2000)], {}, '', '')
        self.assertFalse(r['CORRELATION_VALID'])

    def test_nonconservation_invalidates(self):
        b = record(baseline()); v = baseline(); v['unique_downlink_tcp_bytes'] = 1
        r = capture_result({'baseline': b}, [b, record(v, 2000)], {}, '', '')
        self.assertFalse(r['CORRELATION_VALID'])

    def test_missing_or_changed_baseline_rejected(self):
        b = record(baseline())
        with self.assertRaisesRegex(RuntimeError, 'baseline_missing'):
            capture_result({'baseline': b}, [record(baseline(), 2000)], {}, '', '')
        v = baseline(); v['http_requests'] = 1
        with self.assertRaisesRegex(RuntimeError, 'baseline_changed'):
            capture_result({'baseline': b}, [record(v)], {}, '', '')


if __name__ == '__main__':
    unittest.main()
