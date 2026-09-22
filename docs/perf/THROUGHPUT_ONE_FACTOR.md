# Perf Lab .5: controlled Throughput experiment

Base: `e3a9c87606c138382f356c1252493d60e66b6e02` (.4, code 13).
New build: `1.0.0-perflab.5`, code 14, `io.openflux.app.perflab`, arm64 only.

Real-phone Legacy + AES acceptance passed in .4. The previously suspended
performance experiment is now authorized. These candidates are not a ranking
or a recommendation; a small number of carrier measurements cannot establish
a winner. No VPS or exit changes are required.

| Stored profile | Workers | Queue | Batch size | Batch timeout | Idle pool per host / total |
| --- | ---: | ---: | ---: | --- | --- |
| throughput_current | 64 | 4096 | 32 | 1 ms | 64 / 128 |
| throughput_mem | 64 | 2048 | 32 | 1 ms | 64 / 128 |
| throughput_96 | 96 | 4096 | 32 | 1 ms | 96 / 192 |

`throughput_current` is an exact alias of the existing `throughput` config.
Memory changes only Volga QueueSize. Concurrency changes only WorkerCount and
the two idle-pool limits derived from it. All other fields, including outer
batching, remain equal. Outer batching is not used by this Legacy test path
and must not be used to explain these real-phone Legacy results.

Baseline, Balanced, Low latency and Throughput remain available and retain
their prior runtime settings. Tests compare their entire configs with frozen
values from the base commit. Whole-struct comparisons reject any unrelated
field changes in the new candidates. Profile names round-trip through Android
JSON persistence and remain distinct in the selector and diagnostics.

The existing independent, frozen production peer at source `081d214` now tests
all seven profiles. Legacy send remains IP -> Legacy -> AES -> Volga; receive
is Volga -> AES -> Legacy -> IP. Both compressed and raw Legacy frames are
checked in both directions, including OFX/version on the wire. The negative
old-order fixture and CI mutation proof remain in place. Batched compatibility
with newer exits is a separate concern.

## HTTP diagnostics

The relay records numeric categories at existing failure returns, in both
baseline and optimized implementations. Requests, success policy (200/204),
errors, retries (none), timeouts, scheduling and queue behavior are unchanged.
No error strings, URLs, bodies, headers, document identifiers or auth are
stored in these counters.

- `http_failures_total` is the same snapshot value as existing `http_failures`.
- `http_failures_timeout`: Do returned a deadline/timeout error.
- `http_failures_network`: other Do errors, including cancellation; excludes
  timeouts. It is not proof of a carrier fault.
- `http_failures_4xx`: rejected 400-499 responses.
- `http_failures_429`: subset of 4xx, not an additional category to sum.
- `http_failures_5xx`: rejected 500-599 responses.
- `http_failures_other_status`: all other rejected status codes, including
  unaccepted 2xx and redirects not followed by net/http.

The total also includes pre-request construction/serialization failures,
which are not assigned an HTTP category. Snapshots use independent atomics,
so in-flight updates can briefly differ; compare settled snapshots. Response
body read errors remain ignored exactly as before. Tests inject outcomes into
both real relay worker paths without sockets and verify exactly one attempt,
unchanged success accounting and sanitized snapshots.

## Phone test

Test only these three new candidates, initially in this order:
`throughput_current`, `throughput_mem`, `throughput_96`.

For each: VPN off; Force Stop Perf Lab; launch; select user2, Legacy and the
candidate; connect; wait 15 seconds; run one Speedtest against Elisa Tallinn;
immediately copy diagnostics JSON; stop VPN. Keep the same phone, network and
server. Do not repeat Baseline/Balanced/Low latency in this round.

Only Linux CI may execute validation or build Go/Android artifacts. No Windows
Go test/build/run or generated binaries, no antivirus exclusions or changes,
no Release and no upstream PR. If validation fails, stop; the APK is not ready.
