"""Verify exact-base source identity except audited observation-only hooks."""
import hashlib
import json
from pathlib import Path
import subprocess
import sys

BASE = '081d214300c1067f17f6c0d02f84a8491f1a7b98'
patches = json.loads(Path('scripts/ack-diag-patches.json').read_text())
for path, pairs in patches.items():
    expected = subprocess.check_output(['git', 'show', BASE + ':' + path]).decode()
    for pair in pairs:
        assert expected.count(pair['from']) == 1, (path, pair['from'])
        expected = expected.replace(pair['from'], pair['to'])
    assert Path(path).read_bytes() == expected.encode(), ('unaudited change', path)
allowed = set(patches) | {
    '.github/workflows/ci.yml', 'docs/EXIT_ACK_LATENCY_DIAG.md',
    'scripts/ack-diag-patches.json', 'scripts/ack-diag-verify.py',
    'internal/ackdiag/correlator.go', 'internal/ackdiag/correlator_test.go',
    'internal/ackdiag/runtime.go', 'utils/ack_diag_log.go', 'utils/ack_diag_log_test.go',
    'transport/ack_diag_test.go',
}
changed = set(subprocess.check_output(['git', 'diff', '--name-only', BASE, 'HEAD']).decode().splitlines())
assert changed == allowed, sorted(changed ^ allowed)
assert not subprocess.check_output(['git', 'status', '--porcelain']).strip()
head = subprocess.check_output(['git', 'rev-parse', 'HEAD']).decode().strip()
manifest = {'production_base': BASE, 'diagnostic_commit': head,
            'diagnostics_only_source_verification': 'PASS',
            'source_files': {p: hashlib.sha256(Path(p).read_bytes()).hexdigest() for p in sorted(changed)}}
out = Path(sys.argv[1])
out.mkdir(parents=True, exist_ok=True)
(out / 'source-manifest.json').write_text(json.dumps(manifest, indent=2) + '\n')
print('Exact production + audited observation hooks: PASS')
