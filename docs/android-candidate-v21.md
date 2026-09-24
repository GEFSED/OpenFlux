# Candidate v2.1: safe Volga startup observations

Base: `10cd36debd1f50b367690b796e4def8cdb873e3b`. Candidate identity:
`io.openflux.app.candidate`, `1.0.0-candidate.3` (13), arm64-v8a.
Ordinary Android package, version and Java/TUN implementation stay unchanged.

For Standard with the `vyandex` transport selected, the synchronous start error
is inside Volga authorization. BaseTransport.Start returns nil; wrappers forward
the inner error. Authorization completes before relay and WebSocket start.
wsListener.Start launches a goroutine and returns no error. Android establishes
TUN only after Mobile.startWithMode succeeds. Mode alone does not identify the
selected provider. The observed generic error does not identify an auth substage.

VolgaStartupEvent is a closed numeric enum. Its String and FailureClass methods
return only fixed allowlisted strings; unknown values are discarded. A
mutex-protected optional sink is independent of utils.Debugf. The mobile bridge
registers a bounded appendLog callback, so ReadLogs receives safe observations
while configureLogging continues to disable provider debug. No raw error is
sent to this channel. Observer panics are contained without logging their value.

Events cover auth start, document request/read failure, rejected redirect,
captcha detection/start/completion/failure, original-document retry, missing or
invalid client-config, missing officeActionData/action URL/access token,
initial-auth failure, session/bootstrap failure and authorization success.
Failure classes: AUTH_DOCUMENT_REQUEST, AUTH_REDIRECT, AUTH_CAPTCHA,
AUTH_CLIENT_CONFIG, AUTH_INITIAL, AUTH_SESSION. Non-failure events have no class.
Success means authorization completed, not that WebSocket or tunnel traffic works.

The existing captcha helper and its tests are byte-identical to v2. Safe captcha
observations surround its existing call in authorizeWithClient. New standalone
enum emissions are the only changes in vyandex.go; removing exactly those calls
must reconstruct the entire v2 file byte-for-byte. The verifier checks this in
addition to every previous frozen implementation/oracle check. It does not relax
HTTP statuses, request counts, redirect budgets, cookies, timeouts, PoW or retries.
Existing mobile files, Standard/Speed/Optimized scheduling, 429 guard and wire
wrappers remain byte-identical. Only the candidate version changes in Gradle.

net/http may reject a malformed Location internally before returning a response;
that remains a document-request failure. AUTH_REDIRECT identifies failures in
the existing explicit redirect-processing/budget branches. No new header parsing
or network instrumentation is introduced to distinguish those client internals.

Tests use synthetic round trippers or malformed URLs that fail before network
I/O. They exercise safe events with debug off, the actual Standard Start to
ReadLogs boundary, privacy sentinels, invalid enum rejection, concurrent sink
registration/emission, callback failure isolation and existing request/body
lifecycle checks. No live provider validation is included.

Manual POCO F6 next step: install this Candidate separately from the ordinary app,
retry Standard using the already intended test configuration, and retain only
the fixed VOLGA-AUTH event/class lines and generic UI outcome. Stop on failure;
do not proceed to Speed/Optimized automatically. No request is performed by CI.
Historical UNKNOWN_DOCS_HTML remains insufficient evidence for proven CAPTCHA.
