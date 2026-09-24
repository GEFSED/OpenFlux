# Candidate v2.3: CAPTCHA completion continuation

Base: `fb8602e1a6f30e1e9e5a44d936e8207db82fa0cd`.
Identity: `io.openflux.app.candidate`, `1.0.0-candidate.5`, code 15, arm64-v8a.

The only functional experiment is the next bootstrap URL after successful
CAPTCHA POST. Previously the solver validated and discarded Location, returning
only an error, and authorization reset currentURL to the original document.
Now it returns the resolved URL and the existing bounded bootstrap loop uses it.
There is no extra request inside the solver and no original-document fallback.

POST completion still requires 302 or 303. The unchanged redirect resolver
requires nonempty Location, successful URL parsing, HTTP/HTTPS, a host and no
userinfo. Relative Location resolves against the CAPTCHA form action URL.
No host ownership allowlist was added: ordinary bootstrap redirects already use
this same resolver. This experiment neither broadens its accepted targets nor
asserts every accepted host is Yandex-owned. A separate host allowlist would be
another policy change requiring independent scope and compatibility review.

Auth/captcha UA stays Mozilla/5.0, including fingerprint c9. Relay HTTP and WS
retain Firefox 153/macOS 10.15. PoW, fingerprint generation, cookies, shared HTTP
client, timeouts, one CAPTCHA attempt and ten bootstrap requests are unchanged.
Completion processing consumes the existing bootstrap budget. Standard, Speed,
Optimized, 429 guard, AES, wrappers, Android network path, TUN and DNS are frozen.

All safe events remain; VOLGA_CAPTCHA_COMPLETION_FOLLOWED is appended to the
enum without renumbering existing values. It records selection of the validated
continuation for the next loop iteration, not successful retrieval or auth.
The subsequent AUTH_RETRY event remains. No URL, host, Location, token or cookie
is carried by the event. Unrestricted provider debug remains disabled.

Existing 62 CAPTCHA cases retain their assertions, with the common fixture now
explicitly returning the original document as Location and direct solver calls
accepting its typed result. The distinct-target completion tests prove 302/303,
relative resolution, exact request order, no hidden GET/no immediate original
retry, cookies, fail-closed validation, unchanged budgets and safe diagnostics.
Existing UA cases remain unchanged. Provenance compares complete files with
exactly transformed v2.2 blobs and retains all earlier frozen oracle checks.

No live provider or device test is part of implementation/CI. Minimal auth UA
was insufficient on the reported v2.2 device attempt. Historical UNKNOWN_DOCS_HTML
still is not proven to have been CAPTCHA.

## Next manual test

POCO F6, Standard, one connection attempt. Record the complete safe sequence.
Desired: VOLGA_CAPTCHA_COMPLETED -> VOLGA_CAPTCHA_COMPLETION_FOLLOWED -> ... ->
VOLGA_AUTH_SUCCESS. Stop at any new failure class. Do not test Speed/Optimized
until AUTH_SUCCESS. Keep ordinary OpenFlux installed. CI uses an ephemeral debug
certificate, so replacing an older Candidate may require preserving its local
configuration first; no installation is automatic.
