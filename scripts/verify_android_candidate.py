"""Static provenance checks executed only in Linux CI."""
import pathlib
import subprocess

BASE = "8566f727c8238436728758f139130cef433147b7"
LAB = "53862516bfa8733a91c9c971264f9b1211bfae3e"
def blob(rev, path):
    return subprocess.check_output(["git", "show", rev + ":" + path])
def current(path):
    return pathlib.Path(path).read_bytes()
subprocess.run(["git", "merge-base", "--is-ancestor", BASE, "HEAD"], check=True)
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
added = b"\n\t// Used only by NewYandexVolgaTransportWithConfig; Standard remains disabled.\n\tRateLimit429GuardEnabled bool\n"
assert current(path).count(added) == 1
assert current(path).replace(added, b"") == blob(BASE, path), "original Volga changed"
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
print("EXACT_BASE_AND_FROZEN_ORACLES=PASS")
print("STANDARD_TRANSPORT_AND_PROFILE_STORE_UNCHANGED=PASS")
print("GUARD_IMPLEMENTATION_BYTE_IDENTICAL=PASS")
