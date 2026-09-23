import unittest
from argv_validator import (ReadinessError, await_stable, exec_directive, expected_tokens,
                            parse_cmdline, safe_diff)

EXPECTED = expected_tokens('/root/openflux/openflux-ack-diag',
    ['--exit-node', '--mode=proxy', '--transport=vyandex',
     '--url-file=/root/openflux/user2/document-url',
     '--encryption-key-file=/root/openflux/user2/encryption-key'])
RAW = b'\0'.join(EXPECTED) + b'\0'
OLD = {'MainPID': '10', 'InvocationID': 'old'}


def sample(pid='20', inv='new', exe=True, exists=True, raw=RAW, active='active', coherent=True):
    return {'state': {'MainPID': pid, 'InvocationID': inv, 'NRestarts': '0',
                     'ActiveState': active, 'SubState': 'running'},
            'proc_exists': exists, 'cmdline_read': exists, 'exe_match': exe,
            'coherent': coherent, 'start_ticks': '100' if exists else None,
            'exe_class': 'EXPECTED' if exe else 'OTHER', 'raw': raw,
            'argv': parse_cmdline(raw), 'read_error': None if exists else 'PROC_DISAPPEARED'}


class HarnessTests(unittest.TestCase):
    def wait(self, samples, timeout=1.0):
        elapsed, index, observations = [0.0], [0], []
        def read():
            value = samples[min(index[0], len(samples) - 1)]
            index[0] += 1
            return value
        def sleep(seconds):
            elapsed[0] += seconds
        result = await_stable(read, OLD, EXPECTED, clock=lambda: elapsed[0], sleep=sleep,
                              on_sample=observations.append, timeout=timeout)
        return result, observations

    def diff(self, raw):
        return safe_diff(EXPECTED, parse_cmdline(raw), raw)

    def test_exact_argv_match(self):
        self.assertTrue(self.diff(RAW)['argv_match'])

    def test_terminal_nul(self):
        self.assertEqual(EXPECTED, parse_cmdline(RAW))

    def test_no_terminal_nul(self):
        self.assertEqual(EXPECTED, parse_cmdline(RAW[:-1]))
        self.assertTrue(self.diff(RAW[:-1])['argv_match'])
        self.assertFalse(self.diff(RAW[:-1])['old_parser_would_match'])

    def test_extra_empty_token(self):
        d = self.diff(RAW + b'\0')
        self.assertFalse(d['argv_match'])
        self.assertEqual(7, d['actual_argc'])
        self.assertEqual(1, d['empty_tokens'])

    def test_literal_quoted_token(self):
        raw = RAW.replace(b'--mode=proxy', b'"--mode=proxy"')
        self.assertFalse(self.diff(raw)['argv_match'])
        self.assertTrue(self.diff(raw)['has_literal_quotes'])

    def test_missing_argument(self):
        self.assertFalse(self.diff(b'\0'.join(EXPECTED[:-1]) + b'\0')['argv_match'])

    def test_extra_argument(self):
        self.assertFalse(self.diff(RAW + b'--extra\0')['argv_match'])

    def test_reordered_arguments(self):
        tokens = EXPECTED[:]
        tokens[1], tokens[2] = tokens[2], tokens[1]
        self.assertTrue(self.diff(b'\0'.join(tokens) + b'\0')['order_difference'])

    def test_path_mismatch(self):
        self.assertEqual('FLAG_VALUE_PATH', self.diff(RAW.replace(b'user2', b'user3'))[
            'mismatch_classification'][0]['classification'])

    def test_crlf_contamination(self):
        d = self.diff(RAW.replace(b'--mode=proxy', b'--mode=proxy\r\n'))
        self.assertFalse(d['argv_match'])
        self.assertTrue(d['has_cr_lf'])

    def test_utf8_failure(self):
        d = self.diff(RAW.replace(b'--mode=proxy', b'--mode=\xff'))
        self.assertFalse(d['valid_utf8'])
        self.assertFalse(d['argv_match'])

    def test_argv0_not_normalized(self):
        for path in [b'/root/openflux/./openflux-ack-diag', b'openflux-ack-diag', b'/tmp/link']:
            d = self.diff(b'\0'.join([path] + EXPECTED[1:]) + b'\0')
            self.assertFalse(d['argv0_match'])

    def test_systemd_escaping_not_normalized(self):
        self.assertFalse(self.diff(RAW.replace(b'proxy', br'\x70roxy'))['argv_match'])

    def test_duplicate_quotes_not_normalized(self):
        self.assertFalse(self.diff(RAW.replace(b'--exit-node', b'""--exit-node""'))['argv_match'])

    def test_unstable_pid_sequence(self):
        result, observations = self.wait([sample(str(20 + i)) for i in range(5)] + [sample('30')])
        self.assertEqual('30', result[0]['state']['MainPID'])
        self.assertGreaterEqual(result[1]['SETTLING_SECONDS'], 0.3)

    def test_pid_zero_to_stable(self):
        result, _ = self.wait([sample('0', exists=False), sample()])
        self.assertTrue(result[1]['ARGV_MATCH'])

    def test_old_pid_old_invocation_to_new(self):
        result, _ = self.wait([sample('10', inv='old'), sample()])
        self.assertEqual('20', result[0]['state']['MainPID'])

    def test_old_pid_even_with_new_invocation_rejected(self):
        with self.assertRaises(ReadinessError):
            self.wait([sample('10')])

    def test_disappearing_proc(self):
        result, observations = self.wait([sample(), sample(exists=False), sample()])
        self.assertTrue(result[1]['ARGV_MATCH'])
        self.assertTrue(any(not r['proc_exists'] for r in observations))

    def test_exe_mismatch_times_out(self):
        with self.assertRaises(ReadinessError) as caught:
            self.wait([sample(exe=False)])
        self.assertEqual('stable_process_timeout', caught.exception.reason)

    def test_exec_transition(self):
        result, observations = self.wait([sample(exe=False, raw=b'executor\0'), sample()])
        self.assertTrue(result[1]['ARGV_MATCH'])
        self.assertFalse(observations[0]['exe_match'])

    def test_stable_correct_pid_and_argv(self):
        result, observations = self.wait([sample()])
        self.assertTrue(result[1]['MAINPID_STABLE'])
        self.assertGreaterEqual(len(observations), 7)

    def test_stable_genuine_argv_difference_fails(self):
        with self.assertRaises(ReadinessError) as caught:
            self.wait([sample(raw=RAW + b'extra\0')])
        self.assertEqual('actual_argv_mismatch', caught.exception.reason)

    def test_incoherent_proc_does_not_pass(self):
        with self.assertRaises(ReadinessError):
            self.wait([sample(coherent=False)])

    def test_expected_tokens_are_direct_exec_tokens(self):
        self.assertEqual(EXPECTED, exec_directive(EXPECTED).encode().split(b' '))
        with self.assertRaises(ReadinessError):
            exec_directive(EXPECTED + [b'bad space'])

    def test_metadata_never_exports_values(self):
        d = self.diff(RAW.replace(b'--mode=proxy', b'PRIVATE_SENTINEL'))
        self.assertNotIn('PRIVATE_SENTINEL', repr(d))


if __name__ == '__main__':
    unittest.main()
