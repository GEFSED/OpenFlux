# Bootstrap response evidence, diagnostic only

Production base: `081d214300c1067f17f6c0d02f84a8491f1a7b98`.
Previous validated diagnostic: `643e3cac04fcda79f55500e8b091144aac9d555b`.
No VPS deployment or provider request is part of this change. The stopped user3
process must stay stopped. Local HTTP tests use loopback synthetic responses.

## Exact production audit

These locations refer to the production commit, not the instrumented file:

| Requested location | File and line | Behavior |
|---|---|---|
| AUTH_REQUEST_CREATION | transport/yandex/vyandex.go:161 | GET, existing User-Agent and Accept-Language |
| HTTP_CLIENT_CONFIGURATION | transport/yandex/vyandex.go:141 | Fresh cookie jar, 30 second client timeout, existing Transport |
| REDIRECT_POLICY | transport/yandex/vyandex.go:149,160,177 | ErrUseLastResponse, manual loop of at most ten GETs |
| HTTP_DO | transport/yandex/vyandex.go:168 | Error returns before body processing |
| RESPONSE_STATUS_HANDLING | transport/yandex/vyandex.go:177 | 3xx uses Location; every other status becomes finalBody |
| BODY_READER / BODY_READALL | transport/yandex/vyandex.go:172 | io.ReadAll(resp.Body), then Close |
| BODY_READ_ERROR_HANDLING | transport/yandex/vyandex.go:172 | Error discarded; returned bytes retained |
| CONTENT_ENCODING_HANDLING | transport/yandex/vyandex.go:145,168,172 | No explicit decoder; net/http automatic gzip behavior only |
| CONTENT_TYPE_HANDLING | transport/yandex/vyandex.go:190,201 | No acceptance check |
| CLIENT_CONFIG_REGEX | transport/yandex/vyandex.go:81 | Literal format-sensitive regexp |
| CLIENT_CONFIG_EXTRACTION | transport/yandex/vyandex.go:201,213 | First match, JSON decoder with UseNumber |
| CLIENT_CONFIG_MISSING_ERROR | transport/yandex/vyandex.go:208 | Returned error after absent regexp match |
| FINAL_STARTUP_FATAL_EXIT | main.go:128 | Start error reaches log.Fatalf, exit 1 |

2xx, 4xx and 5xx responses follow the same regexp search. A matching config on
403/500 can proceed to the same subsequent validation as a matching 200. A
successful read without a match returns client-config missing. A zero-byte
non-nil body also has no match. A nil final body follows the existing redirect
limit check. Missing Location has its own error. Request and relative redirect
construction are deliberately unchanged, including their existing limitations.

ReadAll may return bytes plus an error. The original parser still searches those
bytes; a complete config before unexpected EOF can parse successfully. An error
without useful bytes can become client-config missing. Transparent gzip decoding
and its errors are observed after net/http; other encoded bytes are not decoded
by this observer. Successful ReadAll means EOF was reached without a reader
error, not proof that the provider's HTML is logically complete.

The earlier real exit-1/missing-config logs do not establish response status,
encoding, completeness, layout, or challenge structure. None of those historical
facts is reconstructed here.

## Contract 5

Schema 4 has exact startup keys and a strict log-kind allowlist, with no generic
response envelope. Adding fields/events under that number would break its
contract. Schema 5 explicitly adds `[ACK-BOOTSTRAP]` with `bootstrap_response`;
startup and ACK accounting shapes remain unchanged. The accounting engine stays
schema 3 internally. Explicit historical schema 3/4 validation remains available.

The closed response fields are defined by `BootstrapResponse` and independently
validated by `schema5.KEYS`: numeric HTTP status and class; content type/encoding
classes; bytes read, completeness and read-error class; route class; response
result; actual production search/match/parse observations; bounded regexp match
count and cap flag; structural booleans and scan completeness. No free-form
field, URL, identity, snippet, hash, body, header, config, cookie or credential is
serialized. The observer cannot return an error to the authorization path.

Route enums: YANDEX_DOCS, YANDEX_AUTH, YANDEX_CAPTCHA_OR_CHALLENGE,
YANDEX_PERMISSION_OR_ERROR, OTHER_YANDEX, UNEXPECTED_HOST_CLASS, UNKNOWN_ROUTE.
Routes are hints about the locally inspected host/path, not proof of why the
provider responded. The query is never used or emitted.

Result enums: CLIENT_CONFIG_FOUND, CLIENT_CONFIG_NOT_FOUND_2XX,
CLIENT_CONFIG_NOT_FOUND_NON2XX, EMPTY_BODY, BODY_READ_FAILED, BODY_READ_PARTIAL,
UNSUPPORTED_ENCODING, REDIRECT, REDIRECT_LIMIT, NETWORK_ERROR, UNKNOWN_RESPONSE.
Read-error enums: NONE, UNEXPECTED_EOF, TIMEOUT, CONNECTION_RESET,
DECOMPRESSION_ERROR, OTHER_IO_ERROR. No error.Error() value is serialized.

Result priority is read failure, non-decoded encoding, redirect, empty body,
match/missing. Other fields preserve orthogonal evidence: BODY_READ_PARTIAL can
coexist with an actual match and successful JSON parse; CLIENT_CONFIG_FOUND does
not imply authorization success. Parse errors remain distinguishable from no
match. Numeric status and `client_config_search_on_non2xx` preserve existing
non-2xx acceptance behavior. Encoding classes distinguish automatic gzip
decoding from gzip/br/deflate/other bytes that remain encoded.

HTML tokenization is bounded to 1 MiB and a 64 KiB token. Scan completeness is
false at either bound. Structural hints do not prove authenticity; absence of
a marker, especially after truncation, proves nothing. A DOM script whose id is
client-config identifies expected bootstrap structure even if the unchanged
regexp misses single quotes or newlines. Challenge requires a captcha action
form plus its answer input, or the SmartCaptcha widget plus its script. Words,
status, URL alone, comments and script text never prove challenge. Auth requires
an auth form plus password input. Permission/error requires an alert with one
of a closed set of error-code attributes. These conservative synthetic fixtures
do not claim to enumerate every real provider page; unknown layouts stay unknown.

No CAPTCHA enum is inferred from the old missing-config error or a challenge-like
URL alone. Startup failure is upgraded only with observed body structure.

Match count allocates at most 65 results: count=64 and capped=true means at least
65. Up to ten existing GET responses plus one terminal observation are allowed.
Overflow explicitly produces schema_emit_failure. Every redirect emits metadata;
the terminal response also records actual regexp/JSON decisions. A failed or
partial output sink blocks readiness, never changes the original exit behavior.

The validator requires exact keys/types/enums and cross-field consistency, ordered
response ordinals, startup scope, and rejects unknown output. It retains fresh
cursor, boot/time, current PID/exe/InvocationID, exact NUL argv, `journalctl --all`,
and unavailable-MESSAGE handling. Null is unavailable data, not proof of binary
output. A classified failure is useful evidence, never readiness success.

## Source immutability and tests

`scripts/ack-diag-verify.py` removes only the enumerated bootstrap observation
calls and the name binding for the previously ignored read error. It requires
byte equality with the already validated frozen authorization implementation.
Every prior packet/HTTP/WS hook, correlator and proxy file is frozen. Thus the
authorization file has instrumentation additions, but its request, redirect,
status acceptance, parser, retry and networking implementation is unchanged.
No regex/read-error/status fix is included.

Fixtures cover all twenty requested categories plus invalid JSON, non-2xx with
config, multiple matches and bounded scanning. Loopback integration invokes the
actual production authorize function and verifies partial-body and non-2xx
acceptance, unchanged request headers, manual redirect count, no retries, gzip
success/failure, and secret-free output. Go-generated fixtures run through the
same strict journal path as future readiness. Linux /proc integration tests
ground an actual local child with stable PID and exact argv; both successful
startup and deliberate startup failures are tested. Synthetic secret sentinels
are required to stay absent from exported evidence. All execution is Linux CI.

## Future one-shot user3 probe: design only

Do not execute the user2-specific `operations.install/switch/rollback` entrypoints
for this probe: they deliberately target user2 and restart it. Reuse only their
tested journal and argv validation components in a separately authorized user3
procedure.

1. Confirm exact user3 unit, PID=0, no job queued, original executable hash, original
   argv tokens/configuration and main/user2 identities. Pin new CI source and
   artifact hashes. Preserve the permanent SSH identity.
2. Install a distinct verified diagnostic binary. Add only a temporary `/run`
   user3 drop-in with Restart=no and the same exact argv, changing only argv[0].
   Reload the manager and verify Restart=no before any start. Do not edit secrets,
   persistent units, main/user2 or networking.
3. Capture a fresh journal cursor, start user3 exactly once, and capture exact
   /proc identity/argv plus the new invocation's schema-5 response/startup records.
   No extra provider request, retry, packet traffic or phone test is generated.
   Missing process grounding or unavailable journal data blocks interpretation.
4. Failure: verify PID=0 and no automatic retries, remove only this runtime drop-in
   and binary/temp files, reload, verify original Restart/ExecStart, and leave
   user3 failed/stopped. Never issue another start after restoring Restart=always.
5. Success: do not restart the successful PID. Restore original unit ExecStart and
   restart policy by removing only this drop-in and reloading; verify same live
   PID/InvocationID. The current process still runs the diagnostic executable.
   Retain its separate executable until an explicitly authorized later transition
   to production. Restoring configuration is not the same as replacing a live
   executable. Do not claim production runtime restored or unlink its executable.
   Passive stability observation is permitted only within the future authorization.

An unexplained response remains UNKNOWN; evidence may narrow a future fix but
does not authorize fixing parsing, credentials, provider state or restart policy.
