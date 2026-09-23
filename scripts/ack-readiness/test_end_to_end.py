"""Real Linux /proc identity + simulated journal/systemd envelope, no VPS.

Only the envelope and 2s journal clock are simulated. Application MESSAGE data
is emitted by a child from exact Go-generated snapshots. The operations.journal,
await_stable, readiness_observation and capture_result paths are the deploy paths.
"""
import json
import os
from pathlib import Path
import subprocess
import sys
import unittest
from unittest.mock import patch
import uuid

import operations
from argv_validator import await_stable, expected_tokens, observe_process
from measurement import capture_result, readiness_observation
from test_journal_validator import entry


class EndToEndTests(unittest.TestCase):
    def invocation(self, negative=False):
        exe = str(Path(sys.executable).resolve())
        script = str(Path(__file__).with_name('simulated_invocation.py').resolve())
        expected = expected_tokens(exe, ['-u', script])
        proc = subprocess.Popen([exe, '-u', script], stdin=subprocess.PIPE,
                                stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
        self.addCleanup(lambda: proc.poll() is None and proc.kill())
        inv = uuid.uuid4().hex
        state = dict(MainPID=str(proc.pid), InvocationID=inv, NRestarts='0',
                     ActiveState='active', SubState='running')
        sample, proof = await_stable(lambda: observe_process(state, expected),
                                    {'MainPID': '0', 'InvocationID': 'old'}, expected)
        self.assertTrue(proof['ARGV_MATCH'] and proof['MAINPID_STABLE'] and proof['NEW_INVOCATION_CONFIRMED'])
        scope = dict(cursor='before', boot_id='simulated-boot', start_monotonic_us=100,
                     pid=str(proc.pid), invocation=inv, exe=exe,
                     unit='openflux-user2.service', uid=str(os.getuid()), gid=str(os.getgid()))
        entries = []
        def emit(name):
            proc.stdin.write(name + '\n'); proc.stdin.flush()
            line = proc.stdout.readline().rstrip('\n')
            self.assertTrue(line, 'simulated process produced no record')
            index = len(entries)
            entries.append(entry(line, _PID=str(proc.pid), _EXE=exe, _BOOT_ID=scope['boot_id'],
                _UID=scope['uid'], _GID=scope['gid'], _SYSTEMD_INVOCATION_ID=inv,
                __CURSOR='after-%d' % index, __MONOTONIC_TIMESTAMP=str(100 + index * 2000000),
                __REALTIME_TIMESTAMP=str(100000000 + index * 2000000)))
        def read_actual_path():
            raw = '\n'.join(json.dumps(e) for e in entries)
            def command(*args):
                self.assertEqual('journalctl', args[0])
                self.assertIn('--all', args)
                self.assertIn('--after-cursor=before', args)
                return raw
            with patch.object(operations, 'command', command), \
                 patch.object(operations, 'load', return_value=scope), \
                 patch.object(operations, 'save'), patch('builtins.print'):
                return operations.journal(inv)
        try:
            emit('banner'); emit('suppressed')
            for _ in range(9): emit('idle_rst')
            if negative:
                emit('malformed')
                with self.assertRaisesRegex(RuntimeError, 'application_log_rejected'):
                    read_actual_path()
                return
            records = read_actual_path()
            self.assertIsNone(readiness_observation(records, 14.9))
            ready = readiness_observation(records, 18)
            self.assertEqual(3, ready['LOG_SCHEMA'])
            self.assertEqual('PASS', ready['LOG_VALIDATION'])
            self.assertEqual(0, ready['UNKNOWN_DIAGNOSTIC_LOGS'])
            baseline = records[-1]
            emit('rst_outstanding'); emit('rst_late_ack')
            records = read_actual_path()
            result = capture_result({'baseline': baseline}, records, state, 'simulated', 'simulated')
            self.assertTrue(result['CORRELATION_VALID'])
            self.assertGreater(result['FINAL']['values']['acked_after_rst_bytes'], 0)
            self.assertEqual(0, result['FINAL']['values']['invalidated_unique_bytes'])
        finally:
            proc.stdin.close()
            self.assertEqual(0, proc.wait(timeout=5), 'simulated invocation shutdown')
            self.assertEqual('', proc.stderr.read())
            proc.stdout.close(); proc.stderr.close()

    def test_schema3_end_to_end_validation(self):
        self.invocation()

    def test_schema3_negative_end_to_end(self):
        self.invocation(negative=True)


if __name__ == '__main__':
    unittest.main()
