#!/usr/bin/env python3
"""Build-source audit: only the explicitly reviewed recovery block may differ."""
import hashlib
import json
from pathlib import Path
import subprocess
import sys

BASE = "081d214300c1067f17f6c0d02f84a8491f1a7b98"
ALLOWED = {
    ".github/workflows/ci.yml",
    "tunnel/tunnel.go", "tunnel/tcp_diag.go", "tunnel/tcp_diag_test.go",
    "utils/debug.go", "utils/carrier_diag.go", "utils/carrier_diag_test.go",
    "scripts/rack-ab-verify.py", "docs/EXIT_RACK_TLP_AB.md",
}
BLOCK = "\t// Sole A/B behavior change: disable RACK/TLP; keep SACK/RTO and all other options.\n\trecovery := tcpip.TCPRecovery(0)\n\tif err := t.gvisorStack.SetTransportProtocolOption(tcp.ProtocolNumber, &recovery); err != nil {\n\t\tpanic(\"TCP-DIAG recovery_apply_failed\")\n\t}\n"

def git(root, *args):
    return subprocess.check_output(["git", "-C", str(root), *args])

def tracked(root):
    return git(root, "ls-files", "-z").decode().strip("\0").split("\0")

def audit_control(root):
    subprocess.run(["git", "-C", str(root), "merge-base", "--is-ancestor", BASE, "HEAD"], check=True)
    changed = set(git(root, "diff", "--name-only", BASE, "HEAD").decode().splitlines())
    assert changed <= ALLOWED, ("unexpected source changes", sorted(changed - ALLOWED))
    original = git(root, "show", BASE + ":tunnel/tunnel.go")
    current = (root / "tunnel/tunnel.go").read_bytes()
    start = original.index(b"func (t *TCPTunnel) printStats() {")
    end = original.index(b"// ---- local IP helpers", start)
    now_start = current.index(b"func (t *TCPTunnel) printStats() {")
    now_end = current.index(b"// ---- local IP helpers", now_start)
    # Preserve every byte outside the logging function and its log import.
    assert current[:now_start].replace(b'\t"log"\n', b"", 1) == original[:start]
    assert current[now_end:] == original[end:]
    assert b"SetTransportProtocolOption(tcp.ProtocolNumber, &recovery)" not in current
    original_debug = git(root, "show", BASE + ":utils/debug.go")
    expected_debug = original_debug.replace(
        b"func Debugf(format string, args ...interface{}) {\n",
        b'func Debugf(format string, args ...interface{}) {\n\tif line := carrierDiagnostic(format); line != "" {\n\t\tlog.Print(line)\n\t}\n',
        1,
    )
    assert (root / "utils/debug.go").read_bytes() == expected_debug
    assert git(root, "status", "--porcelain", "--untracked-files=all") == b"", "control tree must be clean"
    return git(root, "rev-parse", "HEAD").decode().strip()

def main():
    control, experiment, output = map(lambda p: Path(p).resolve(), sys.argv[1:])
    head = audit_control(control)
    assert not experiment.exists(), "refuse to overwrite any workspace"
    subprocess.run(["git", "-C", str(control), "worktree", "add", "--detach", str(experiment), head], check=True)
    source = control / "tunnel/tunnel.go"
    before = source.read_bytes()
    marker = b"\tSetTCPBuffers(t.gvisorStack)\n"
    assert before.count(marker) == 1
    after = before.replace(marker, marker + b"\n" + BLOCK.encode(), 1)
    (experiment / "tunnel/tunnel.go").write_bytes(after)
    differences = []
    manifest = {}
    for name in tracked(control):
        a = (control / name).read_bytes()
        b = (experiment / name).read_bytes()
        if a != b:
            differences.append(name)
        manifest[name] = {
            "control": hashlib.sha256(a).hexdigest(),
            "rack_off": hashlib.sha256(b).hexdigest(),
        }
    assert differences == ["tunnel/tunnel.go"], differences
    assert after.replace(b"\n" + BLOCK.encode(), b"", 1) == before
    status = git(experiment, "status", "--porcelain", "--untracked-files=all").decode()
    assert status == " M tunnel/tunnel.go\n", repr(status)
    subprocess.run(["git", "-C", str(experiment), "diff", "--check"], check=True)
    output.mkdir(parents=True, exist_ok=True)
    (output / "one-factor-manifest.json").write_text(json.dumps({
        "production_base": BASE, "diagnostic_commit": head,
        "one_factor_diff": "PASS", "only_runtime_difference": BLOCK,
        "source_files": manifest,
    }, indent=2) + "\n")
    (output / "recovery-only.patch").write_bytes(git(experiment, "diff", "--", "tunnel/tunnel.go"))
    print("ONE_FACTOR_DIFF=PASS")
    print("CONTROL_DEFAULT_RECOVERY_UNCHANGED=YES")
    print("PRODUCTION_BASE=" + BASE)
    print("DIAGNOSTIC_COMMIT=" + head)

if __name__ == "__main__":
    main()
