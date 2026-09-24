# Android Candidate v2: selective captcha compatibility

Base: `00bfb9d9f2551985f3f7684b5196fa335d85da76` on
`codex/android-production-optimized`. Captcha reference:
`ff14ced55966d98301099c17e8bde577f337c9f6`.

The captcha helper is an exact copy of that validated shared Go helper. Only
the authorization section of `vyandex.go` is adapted; no iOS configuration,
DialContext injection, Swift, Keychain, packet extension or Boards code is ported.
Standard and configured Speed/Optimized already call this same authorization
function. Their scheduling, relay, lifecycle, wire, AES, queue and 429 guard
implementations remain identical to Candidate v1, enforced by the verifier.

Android's VpnService excludes its own package from the tunnel (or omits it from
the whitelist), so Go provider sockets avoid recursive tunneling. Authorization
continues to use the existing explicit HTTP transport: idle pools 100/100,
90-second idle timeout, 30-second request timeout, no proxy or injected dialer,
and manually handled redirects. Captcha GET/POST reuse that exact client and jar;
there is no alternative client or fallback network path. Synthetic socket tests
redirect the shared transport to a local TLS server only.

Limits: one captcha solve, ten document GETs including retry, original document
retry after accepted challenge submission (302/303); challenge GET must be 200.
Challenge encoded and decoded body limits are each 1 MiB, SSR 64 KiB, flow timeout
45 seconds, work timeout 5 seconds / ten million attempts. Existing ordinary
document read and non-redirect status policy are retained. New errors/logs expose
only safe events, no document URLs, challenge state, cookies or tokens. Existing
Android provider-log suppression is unchanged. Existing Start/Stop behavior is
not replaced by the iOS lifecycle implementation.

Candidate-only identity: `io.openflux.app.candidate`,
`1.0.0-candidate.2`, version code 12, arm64-v8a, CI debug signing.
Ordinary debug/release remains `io.openflux.app`, version 1.0.0 / 10.
The dedicated workflow builds one candidate APK; the general workflow excludes
this branch so it cannot also build ordinary debug APKs. A new CI debug signing
key may prevent updating an older Candidate in place; this never requires
removing the ordinary working app.

## Manual POCO F6 plan (not executed by CI)

Keep the ordinary app installed. Compare ordinary reference, Candidate Standard,
Candidate Speed, Candidate Optimized in that order under reasonably matched
network/server/codec conditions. Stop on auth, AES, TUN or other material failure.
For each run record CONNECT, VOLGA_AUTH, CAPTCHA_ENCOUNTERED, WS_CONNECTED,
INTERNET, AES, TUN, DOWNLOAD_Mbps, UPLOAD_Mbps, PING_ms, LOADED_DOWNLOAD_ms,
LOADED_UPLOAD_ms, JITTER_ms, workers, requests, HTTP failures, HTTP 429, guard
events, guard waits, queue drops and reconnects. Do not log credentials or URLs.
These measurements are not causal comparisons unless conditions are matched.

No live Yandex request, phone install/test, VPS change, Play Store upload or
Release is part of implementation/CI. Historical UNKNOWN_DOCS_HTML remains
unproven as CAPTCHA; synthetic compatibility validation cannot identify it.
