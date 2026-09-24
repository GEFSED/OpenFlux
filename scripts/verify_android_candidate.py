"""Candidate provenance: minimal auth UA; frozen relay, WS and other behavior."""
import collections
import pathlib
import re
import subprocess

BASE = "8566f727c8238436728758f139130cef433147b7"
LAB = "53862516bfa8733a91c9c971264f9b1211bfae3e"
CANDIDATE_V1 = "00bfb9d9f2551985f3f7684b5196fa335d85da76"
CAPTCHA_REFERENCE = "ff14ced55966d98301099c17e8bde577f337c9f6"
CANDIDATE_V2 = "10cd36debd1f50b367690b796e4def8cdb873e3b"
CANDIDATE_V21 = "d0cafdf73c072893666483b057d56d76a4e149ef"
def blob(rev, path):
    return subprocess.check_output(["git", "show", rev + ":" + path])
def current(path):
    data = pathlib.Path(path).read_bytes()
    if pathlib.Path(path).suffix in (".go", ".java", ".xml", ".gradle", ".py", ".mod", ".sum", ".md", ".txt", ".yml", ".yaml", ".json"):
        data = data.replace(b"\r\n", b"\n")
    return data
subprocess.run(["git", "merge-base", "--is-ancestor", BASE, "HEAD"], check=True)
subprocess.run(["git", "merge-base", "--is-ancestor", CANDIDATE_V1, "HEAD"], check=True)
subprocess.run(["git", "merge-base", "--is-ancestor", CANDIDATE_V21, "HEAD"], check=True)
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

# Restore ONLY the reviewed auth-UA substitutions, then require the entire
# files to equal v2.1. In particular relay/WS still use the original constant.
auth_ua_declaration = b'\n// Candidate experiment: auth only; relay HTTP and WS keep volgaUserAgent.\nconst volgaAuthUserAgent = "Mozilla/5.0"\n'
volga = current(path)
assert volga.count(auth_ua_declaration) == 1, "auth UA declaration changed"
volga = volga.replace(auth_ua_declaration, b"", 1)
assert volga.count(b"volgaAuthUserAgent") == 3, "auth UA call-site scope changed"
volga = volga.replace(b"volgaAuthUserAgent", b"volgaUserAgent")
assert volga == blob(CANDIDATE_V21, path), "non-UA v2.1 Volga behavior changed"
captcha = current("transport/yandex/captcha.go")
assert captcha.count(b"volgaAuthUserAgent") == 2, "captcha UA call-site scope changed"
captcha = captcha.replace(b"volgaAuthUserAgent", b"volgaUserAgent")
assert captcha == blob(CANDIDATE_V21, "transport/yandex/captcha.go"), "non-UA captcha behavior changed"
captcha_tests = current("transport/yandex/captcha_test.go")
assert captcha_tests.count(b"volgaAuthUserAgent") == 2, "captcha test scope changed"
captcha_tests = captcha_tests.replace(b"volgaAuthUserAgent", b"volgaUserAgent")
assert captcha_tests == blob(CANDIDATE_V21, "transport/yandex/captcha_test.go"), "existing captcha assertions changed"
# Freeze all previously tracked runtime and regression files against v2.1.
v21_paths = subprocess.check_output([
    "git", "ls-tree", "-r", "--name-only", CANDIDATE_V21, "--",
    "mobile", "transport", "tunnel", "tunclient", "socks5", "network", "utils", "android/app/src",
], text=True).splitlines()
for frozen_path in v21_paths:
    if frozen_path not in (path, "transport/yandex/captcha.go", "transport/yandex/captcha_test.go"):
        assert current(frozen_path) == blob(CANDIDATE_V21, frozen_path), "v2.1 path changed: " + frozen_path
def outside_authorization(data):
    before, rest = data.split(b"func authorize(", 1)
    _, after = rest.split(b"func getStr(", 1)
    return before, after
assert outside_authorization(volga) == outside_authorization(blob(CANDIDATE_V1, path)), "non-auth Volga implementation changed"
# The only allowed auth delta from v2 is standalone observation calls with fixed
# enum arguments. Removing them must reconstruct the entire v2 file byte-for-byte.
# This preserves requests, errors, branches, budgets, cookies and retry order.
observations = re.findall(rb"(?m)^\t+emitVolgaStartup\((Volga[A-Za-z]+)\)\n", current(path))
expected_observations = {
    b"VolgaAuthStart": 1, b"VolgaDocumentRequestFailed": 3,
    b"VolgaRedirectRejected": 4, b"VolgaCaptchaDetected": 1,
    b"VolgaCaptchaStarted": 1, b"VolgaCaptchaCompleted": 1,
    b"VolgaCaptchaFailed": 2, b"VolgaAuthRetry": 1,
    b"VolgaClientConfigMissing": 1, b"VolgaClientConfigInvalid": 1,
    b"VolgaOfficeActionMissing": 1, b"VolgaActionURLMissing": 1,
    b"VolgaAccessTokenMissing": 1, b"VolgaAuthInitialFailed": 6,
    b"VolgaSessionFailed": 5, b"VolgaAuthSuccess": 1,
}
assert collections.Counter(observations) == expected_observations, "startup observation scope changed"
without_observations = re.sub(rb"(?m)^\t+emitVolgaStartup\(Volga[A-Za-z]+\)\n", b"", volga)
assert without_observations == blob(CANDIDATE_V2, path), "v2 authorization behavior changed"
assert captcha == blob(CAPTCHA_REFERENCE, "transport/yandex/captcha.go"), "validated captcha algorithm changed"
assert captcha_tests == blob(CANDIDATE_V2, "transport/yandex/captcha_test.go"), "existing captcha/privacy tests changed"
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
assert 'versionNameSuffix "-candidate.4"' in gradle
assert 'output.versionCodeOverride = 14' in gradle
assert gradle.replace('versionNameSuffix "-candidate.4"', 'versionNameSuffix "-candidate.1"').replace('output.versionCodeOverride = 14', 'output.versionCodeOverride = 11').encode() == blob(CANDIDATE_V1, "android/app/build.gradle"), "ordinary Gradle configuration changed"
print("EXACT_BASE_AND_FROZEN_ORACLES=PASS")
print("STANDARD_SCHEDULING_WIRE_AND_PROFILE_STORE_UNCHANGED=PASS")
print("GUARD_IMPLEMENTATION_BYTE_IDENTICAL=PASS")
print("CAPTCHA_ALGORITHM_EXACT_REFERENCE_EXCEPT_AUTH_UA=PASS")
print("ONLY_AUTH_USER_AGENT_CHANGED_FROM_V21=PASS")
print("RELAY_HTTP_AND_WS_USER_AGENT_UNCHANGED=PASS")
print("SAFE_DIAGNOSTICS_BYTE_IDENTICAL=PASS")
print("V2_AUTH_BEHAVIOR_EXACT_WITHOUT_FIXED_OBSERVATIONS=PASS")
print("ANDROID_VPN_AND_MOBILE_PATHS_BYTE_IDENTICAL=PASS")
