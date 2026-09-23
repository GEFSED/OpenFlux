import json
import os
from pathlib import Path
import subprocess
import sys
import unittest


class DisposableRealSystemdTests(unittest.TestCase):
    @unittest.skipUnless(sys.platform=='linux' and os.environ.get('GITHUB_ACTIONS')=='true'
        and os.environ.get('RUNNER_ENVIRONMENT')=='github-hosted', 'requires disposable GitHub-hosted Linux VM')
    def test_v255_lifecycle_and_invocation_detector(self):
        output=Path(os.environ['RUNNER_TEMP'])/'real-systemd-evidence.json'
        p=subprocess.run(['sudo','--preserve-env=GITHUB_ACTIONS,RUNNER_ENVIRONMENT',sys.executable,
            str(Path(__file__).with_name('disposable_systemd.py')),str(output)],capture_output=True,text=True,timeout=180)
        self.assertEqual(0,p.returncode,p.stderr[-4000:]+(output.read_text() if output.exists() else 'NO_EVIDENCE'))
        report=json.loads(output.read_text())
        self.assertEqual('PASS',report['RESULT'])
        self.assertEqual('PASS',report['DISPOSABLE_CLEANUP'])
        self.assertEqual(0,report['integration']['historical_counter_reset_one_invocation']['POST_START_NRESTARTS_BASELINE'])
        print('REAL_SYSTEMD_INTEGRATION=PASS '+json.dumps(report,sort_keys=True))
