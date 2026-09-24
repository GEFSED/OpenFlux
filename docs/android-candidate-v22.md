# Candidate v2.2: auth-only minimal User-Agent experiment

Base: `d0cafdf73c072893666483b057d56d76a4e149ef` (Candidate v2.1).
Candidate identity: `io.openflux.app.candidate`, `1.0.0-candidate.4`, code 14,
arm64-v8a. Ordinary app identity and version are unchanged.

`volgaAuthUserAgent = "Mozilla/5.0"` controls document/redirect GETs, original
document retry, initial auth POST, session GET, captcha GET/POST and fingerprint
field `c9`. The existing `volgaUserAgent` remains the original Firefox 153/macOS
10.15 string for Standard and configured relay HTTP and WebSocket handshakes.
There is no UA fallback, selector or rotation.

The provenance verifier restores only these exact UA substitutions in memory
and requires complete file equality with v2.1. Existing safe diagnostics,
captcha algorithm, request order, cookies, budgets, timeouts, HTTP clients,
Standard/Speed/Optimized scheduling, 429 guard, wrappers and Android network
path remain frozen. Existing 62 captcha cases retain all assertions; only their
expected UA binding changes. New synthetic tests check literal request headers,
submitted fingerprint, retry/failure behavior and unchanged relay headers.

No live provider or phone test is part of implementation/CI. Historical
UNKNOWN_DOCS_HTML remains unproven as CAPTCHA.

## Next manual test

On POCO F6 select Candidate v2.2 Standard and make one connection attempt.
Record only safe events. Primary success is VOLGA_AUTH_START followed by
VOLGA_AUTH_SUCCESS without VOLGA_CAPTCHA_DETECTED. If challenged, record the
exact safe sequence. Relay/WS failure after AUTH_SUCCESS is a separate stage.
Do not proceed automatically to Speed/Optimized tests.

CI uses an ephemeral debug signing certificate. A different certificate may
prevent updating an older Candidate in place; preserve its configuration before
manually replacing Candidate if necessary. Keep ordinary OpenFlux installed.
