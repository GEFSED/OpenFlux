# Android Perf Lab: real-device observations and the Optimized candidate

Date: 2026-09-22. Candidate base: f4bcfeb3751626cfa4c6ca158ef4cb26692636da
(Perf Lab .7, code 16). Candidate build: .8, code 17, io.openflux.app.perflab,
arm64-v8a. Observations below were supplied by the user from real Android
runs; they are not laboratory throughput guarantees.

## Correctness and the wrapper-order regression

The earlier Perf Lab Legacy + AES stack incorrectly sent IP -> AES -> Legacy
-> Volga. The exact production exit (source 081d214300c1067f17f6c0d02f84a8491f1a7b98)
expects IP -> Legacy -> AES -> Volga. Server diagnostics located the failure
at AES header validation, before decryption: the ordinary client had 828 AES
successes and proxy callbacks; Perf Lab had 232 bad AES headers and no AES
successes. The correction was delivered in .4 and confirmed on the real phone.

Send remains IP -> Legacy -> AES -> Volga; receive remains Volga -> AES ->
Legacy -> IP. Real Internet access works. Successful runs have zero AES/TUN
errors and zero queue drops. Independent frozen-production-peer tests verify
both directions and reject the old order. .8 changes none of this wire code.

## Baseline and fixed-worker measurements

The original Baseline cold start used 2000 workers, about 2026 goroutines,
473216 KiB Android PSS and about 303 MB Go heap allocation. Internet
correctness passed, but the resource footprint was large.

Cold starts used Force Stop, the same user2 profile, vyandex, Legacy and AES,
and Speedtest's Elisa Tallinn server. Increasing concurrency was not justified
by the observations: heavy 64-worker runs had about 9-10% HTTP 429, while a
96-worker run reached about 17.3%. Some 64-worker runs had no 429 under lighter
load. 48 workers worked correctly without a demonstrated advantage. Reducing
the queue from 4096 to 2048 did not demonstrate a clear memory or 429 benefit.
A saturated worker counter alone does not establish insufficient capacity.

Two successful 32-worker measurements:

| Metric | Run A | Run B |
| --- | ---: | ---: |
| Download, Mbps | 1.62 | 1.62 |
| Upload, Mbps | 5.89 | 10.6 |
| HTTP requests | 5177 | 4457 |
| HTTP 429 | 208 | 0 |
| Failure fraction | 0.0401777 | 0 |
| Peak busy workers | 32/32 | 32/32 |
| Queue drops | 0 | 0 |
| Goroutines | 48 | 49 |

Run B batched 23609 packets into 4457 batches (5.297 packets/batch), with
253940 KiB PSS and zero AES/TUN errors. 32 workers can sustain the highest
upload observed in this set. The baseline and candidate measurements are
separate runs, not an isolated causal estimate of memory savings.

Classified failures in the reported failing runs were HTTP 429 Too Many
Requests: network, timeout, 5xx and other-status counts were zero. Mobile
network conditions and Speedtest results varied substantially between runs.
A single Mbps result does not establish causation or a universally best profile.
Outer batching does not explain these Legacy-path measurements.

## The .7 guard: established behavior and the remaining limitation

The guard is global per relay, with context-cancellable timer waits, no busy
spin and no goroutine per request. It accepts Retry-After seconds/HTTP dates
with a 5-second cap, otherwise uses bounded 25..1000 ms exponential fallback.
Concurrent 429 responses cannot shorten the gate; a subsequent successful
post-cooldown request resets fallback. Rejected batches are never retried.

Linux tests with local httptest servers, including race detection, cover
no-429/no-wait, header parsing, invalid/absent headers, bounds, shared concurrent
extensions, cancellation, recovery, exactly one attempt per rejected batch
and disabled equivalence. These are synthetic correctness results.

The reported real-phone guarded smoke test had Internet PASS, 32 workers,
zero queue drops and AES/TUN errors, 4005 requests and 4005 successes, zero
429, guard enabled and zero wait events. This establishes no regression when
no 429 occurs. It does NOT establish reduced real-phone rate limiting.

**The real-phone benefit of the guard during an actual HTTP 429 event has not
yet been directly observed.** Its response to 429 is synthetically/race-tested.
No extra traffic should be generated just to force such an event.

## .8 presentation, migration and exact alias

Optimized is an exact semantic alias of throughput_w32_429guard: workers 32,
queue 4096, batch 32, timeout 1 ms, idle HTTP pools 32/64 and guard enabled.
Every other Volga and outer config field is identical. There is no new tuning
or guard algorithm change. Whole-config tests preserve all eleven .7 profiles.

The normal selector shows Baseline, Balanced, Low latency, Throughput and
Optimized. All seven historical experimental names remain accepted and
config-compatible. A hidden current name is explicitly shown as Historical
experiment, with no normal choice preselected, until the user chooses one.

Only Perf Lab migrates stored throughput_w32_429guard to optimized. The
migration changes only that JSON field, preserving IDs, name/icon, transport,
codec, document, encryption secret, MAX fields and unknown fields. It is
idempotent and covers both the main app and quick-settings tile through their
shared profile store. Other names and the baseline default remain unchanged.
Diagnostics retain all counters and report optimized_candidate only for the
canonical optimized profile; counters never claim that a website loaded.

## One final smoke test

Update to .8; open existing user2; confirm the previous guarded selection is
now Optimized and Legacy is unchanged. Connect, open a normal website, browse
for 2-3 minutes, copy diagnostics once, then stop VPN. No Speedtest is required.

Confirm the website loads and profile=optimized, worker_count=32,
queue_cap=4096, rate_limit_guard_enabled=true, connected=true, ws_connected=true,
positive encrypted_receive_success/mobile_callback_packets/download_bytes,
zero queue drops, AES bad headers/decrypt failures and TUN failures. If 429
occurs naturally, retain the guard counters; do not deliberately force load.
This is a candidate, not a new global default or a proven universal fastest
profile. No VPS changes, upstream PR or Release are part of this task.
