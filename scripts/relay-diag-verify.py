"""Linux CI: prove the complete production files differ only by audited hooks."""
import hashlib
import json
from pathlib import Path
import subprocess
import sys

BASE = '081d214300c1067f17f6c0d02f84a8491f1a7b98'
root = Path.cwd()
def original(path):
    return subprocess.check_output(['git', 'show', BASE + ':' + path]).decode()

path = 'transport/yandex/vyandex.go'
production = original(path)
expected = production
for pair in json.loads(Path('scripts/relay-diag-patches.json').read_text()):
    assert expected.count(pair['from']) == 1, pair['from']
    expected = expected.replace(pair['from'], pair['to'])
assert Path(path).read_bytes() == expected.encode(), 'unlisted transport change'
assert Path('main.go').read_bytes() == original('main.go').replace(
    'func main() {\n', 'func main() {\n\tutils.ConfigureRelayDiagnosticLogging()\n').encode()
assert Path('utils/debug.go').read_bytes() == original('utils/debug.go').replace(
    'func Debugf(format string, args ...interface{}) {\n',
    'func Debugf(format string, args ...interface{}) {\n\tif relayDiagnosticMode.Load() {\n\t\treturn\n\t}\n').encode()
frozen = production[production.index('func (r *relayClient) sendBatch('):production.index('func (r *relayClient) SetFrontier(')]
frozen = frozen.replace('sendBatch(', 'sendBatchProductionReference(')
reference = Path('transport/yandex/relay_diag_frozen_test.go').read_text()
assert reference[reference.index('func (r *relayClient) sendBatchProductionReference('):] == frozen.rstrip() + '\n'
allowed = {
    '.github/workflows/ci.yml', 'docs/EXIT_VOLGA_RELAY_DIAG.md',
    'scripts/relay-diag-patches.json', 'scripts/relay-diag-verify.py',
    'main.go', 'utils/debug.go', 'utils/relay_diag_log.go', 'utils/relay_diag_log_test.go',
    'transport/yandex/vyandex.go', 'transport/yandex/relay_diag.go',
    'transport/yandex/relay_diag_test.go', 'transport/yandex/relay_diag_frozen_test.go',
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
print('Exact production base + audited observation hooks: PASS')
