"""Verify exact-base source identity except audited observation-only hooks."""
import hashlib
import json
from pathlib import Path
import subprocess
import sys

BASE = '081d214300c1067f17f6c0d02f84a8491f1a7b98'
HARNESS = {
    'scripts/ack-readiness/' + name for name in (
        'argv_validator.py', 'journal_validator.py', 'schema3.py', 'schema.json',
        'measurement.py', 'operations.py', 'fixtures.py', 'simulated_invocation.py',
        'test_argv_validator.py', 'test_journal_validator.py', 'test_measurement.py',
        'test_schema3.py', 'test_end_to_end.py')
}
SCHEMA3_CHANGES = HARNESS | {'internal/ackdiag/schema3_fixture_test.go',
    'docs/ACK_SCHEMA3_READINESS.md', 'scripts/ack-diag-verify.py', '.github/workflows/ci.yml'}
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
    'internal/ackdiag/lifecycle.go', 'internal/ackdiag/lifecycle_test.go',
    'internal/ackdiag/runtime.go', 'utils/ack_diag_log.go', 'utils/ack_diag_log_test.go',
    'transport/ack_diag_test.go',
}
allowed |= SCHEMA3_CHANGES
changed = set(subprocess.check_output(['git', 'diff', '--name-only', BASE, 'HEAD']).decode().splitlines())
assert changed == allowed, sorted(changed ^ allowed)
assert not subprocess.check_output(['git', 'status', '--porcelain']).strip()
# This revision changes observer implementation/tests/documentation only.
# Even the pre-existing observation hook call sites and log filter are frozen.
PREVIOUS = 'f31a49f7953eb910998ca2bdded22952faaed80d'
mutable = {
    'internal/ackdiag/correlator.go', 'internal/ackdiag/correlator_test.go',
    'internal/ackdiag/lifecycle.go', 'internal/ackdiag/lifecycle_test.go',
    'transport/ack_diag_test.go', 'docs/EXIT_ACK_LATENCY_DIAG.md',
    'scripts/ack-diag-verify.py', '.github/workflows/ci.yml',
}
followup = set(subprocess.check_output(['git', 'diff', '--name-only', PREVIOUS, 'HEAD']).decode().splitlines())
assert followup <= mutable | SCHEMA3_CHANGES, sorted(followup - mutable - SCHEMA3_CHANGES)
for path in patches:
    assert Path(path).read_bytes() == subprocess.check_output(['git', 'show', PREVIOUS + ':' + path]), path
for path in ('internal/ackdiag/runtime.go', 'utils/ack_diag_log.go'):
    assert Path(path).read_bytes() == subprocess.check_output(['git', 'show', PREVIOUS + ':' + path]), path
print('Networking, hook sites, runtime logger byte-identical to previous diagnostic: PASS')
VALIDATED = '4e991a6e3856da7f4372be69091d830e5e3421c8'
rst_changes = set(subprocess.check_output(['git', 'diff', '--name-only', VALIDATED, 'HEAD']).decode().splitlines())
assert rst_changes <= {
    'internal/ackdiag/correlator.go', 'internal/ackdiag/correlator_test.go',
    'internal/ackdiag/lifecycle.go', 'internal/ackdiag/lifecycle_test.go',
    'docs/EXIT_ACK_LATENCY_DIAG.md', 'scripts/ack-diag-verify.py',
} | SCHEMA3_CHANGES, sorted(rst_changes)
print('RST follow-up: correlator/tests/docs/verifier only; all network code unchanged: PASS')
FROZEN = '01407ff3c86fc616f49d99ecf4a2942a37a3a861'
schema_changes = set(subprocess.check_output(['git', 'diff', '--name-only', FROZEN, 'HEAD']).decode().splitlines())
assert schema_changes <= SCHEMA3_CHANGES, sorted(schema_changes - SCHEMA3_CHANGES)
for path in ('internal/ackdiag/correlator.go', 'internal/ackdiag/lifecycle.go',
             'internal/ackdiag/runtime.go', 'utils/ack_diag_log.go', *patches):
    assert Path(path).read_bytes() == subprocess.check_output(['git', 'show', FROZEN + ':' + path]), path
print('Schema 3 follow-up: harness/tests/docs/CI only; frozen correlator and all hooks byte-identical: PASS')
head = subprocess.check_output(['git', 'rev-parse', 'HEAD']).decode().strip()
manifest = {'production_base': BASE, 'diagnostic_commit': head,
            'diagnostics_only_source_verification': 'PASS',
            'source_files': {p: hashlib.sha256(Path(p).read_bytes()).hexdigest() for p in sorted(changed)}}
out = Path(sys.argv[1])
out.mkdir(parents=True, exist_ok=True)
(out / 'source-manifest.json').write_text(json.dumps(manifest, indent=2) + '\n')
print('Exact production + audited observation hooks: PASS')
