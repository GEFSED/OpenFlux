"""Candidate provenance: frozen scheduling/wire path plus selective captcha port."""
import pathlib
import subprocess

BASE = "8566f727c8238436728758f139130cef433147b7"
LAB = "53862516bfa8733a91c9c971264f9b1211bfae3e"
CANDIDATE_V1 = "00bfb9d9f2551985f3f7684b5196fa335d85da76"
CAPTCHA_REFERENCE = "ff14ced55966d98301099c17e8bde577f337c9f6"
def blob(rev, path):
    return subprocess.check_output(["git", "show", rev + ":" + path])
def current(path):
    data = pathlib.Path(path).read_bytes()
    if pathlib.Path(path).suffix in (".go", ".java", ".xml", ".gradle", ".py", ".mod", ".sum", ".md", ".txt", ".yml", ".yaml", ".json"):
        data = data.replace(b"\r\n", b"\n")
    return data
subprocess.run(["git", "merge-base", "--is-ancestor", BASE, "HEAD"], check=True)
subprocess.run(["git", "merge-base", "--is-ancestor", CANDIDATE_V1, "HEAD"], check=True)
commits = subprocess.check_output(["git", "rev-list", BASE + "..HEAD"], text=True).splitlines()
assert LAB not in commits, "candidate must not descend from laboratory history"
for path in (
    "transport/batched.go", "transport/encrypted.go", "transport/compressor.go",
    "transport/framing.go", "transport/transport.go",
    "android/app/src/main/java/io/openflux/app/ProfileStore.java",
    "android/app/src/main/java/io/openflux/app/SecureSettings.java",
):
    assert current(path) == blob(BASE, path), "ordinary behavior changed: " + path
path = "transport/yandex/vyandex.go"
def outside_authorization(data):
    before, rest = data.split(b"func authorize(", 1)
    _, after = rest.split(b"func getStr(", 1)
    return before, after
assert outside_authorization(current(path)) == outside_authorization(blob(CANDIDATE_V1, path)), "non-auth Volga implementation changed"
assert current("transport/yandex/captcha.go") == blob(CAPTCHA_REFERENCE, "transport/yandex/captcha.go"), "validated captcha helper changed"
# Freeze every existing mobile/TUN/codec/profile/lifecycle/guard implementation,
# test oracle and Android Java/resource file, not only selected config fields.
paths = subprocess.check_output([
    "git", "ls-tree", "-r", "--name-only", CANDIDATE_V1, "--",
    "mobile", "transport", "tunnel", "tunclient", "socks5", "network", "android/app/src",
], text=True).splitlines()
for frozen_path in paths:
    if frozen_path != path:
        assert current(frozen_path) == blob(CANDIDATE_V1, frozen_path), "candidate v1 path changed: " + frozen_path
for path in ("transport/yandex/rate_limit_429.go",
             "transport/yandex/http_failure_diagnostics.go",
             "transport/yandex/receive_diagnostics.go",
             "transport/receive_diagnostics.go"):
    assert current(path) == blob(LAB, path), "verified implementation changed: " + path
ref = blob(LAB, "mobile/perf_profiles.go")
for old, new in ((b"perfConfig", b"referencePerfConfig"),
                 (b"normalizeProfile", b"referenceNormalizeProfile"),
                 (b"profileConfig", b"referenceProfileConfig")):
    ref = ref.replace(old, new)
assert current("mobile/perflab_reference_test.go") == ref, "config oracle diverged"
for directory in ("testsupport/productionv060peer", "testsupport/basev100peer"):
    for path in pathlib.Path(directory).iterdir():
        assert path.read_bytes() == blob(LAB, str(path)), "frozen peer changed"
# No ordinary Android version/package or default promotion.
gradle = current("android/app/build.gradle").decode()
assert 'applicationId "io.openflux.app"' in gradle
assert 'versionCode 10' in gradle and 'versionName "1.0.0"' in gradle
assert 'applicationIdSuffix ".candidate"' in gradle
assert 'versionNameSuffix "-candidate.2"' in gradle
assert 'output.versionCodeOverride = 12' in gradle
assert gradle.replace('versionNameSuffix "-candidate.2"', 'versionNameSuffix "-candidate.1"').replace('output.versionCodeOverride = 12', 'output.versionCodeOverride = 11').encode() == blob(CANDIDATE_V1, "android/app/build.gradle"), "ordinary Gradle configuration changed"
print("EXACT_BASE_AND_FROZEN_ORACLES=PASS")
print("STANDARD_SCHEDULING_WIRE_AND_PROFILE_STORE_UNCHANGED=PASS")
print("GUARD_IMPLEMENTATION_BYTE_IDENTICAL=PASS")
print("CAPTCHA_HELPER_EXACT_REFERENCE=PASS")
print("ANDROID_VPN_AND_MOBILE_PATHS_BYTE_IDENTICAL=PASS")
