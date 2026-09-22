# Perf Lab .6: fixed worker-count experiment

Base: `f9adecf6e5c87cadef0401d6f8aa2c6ce5d240f8` (.5, code 14).
New build: `1.0.0-perflab.6`, code 15, `io.openflux.app.perflab`, arm64-v8a.

In the user's .5 cold-start runs, all observed nonzero classified HTTP failures
were 429 responses. The 96-worker run had about 17.28% failures; the failing
64-worker control had about 9.86%, and the memory candidate about 9.39%.
Another 64-worker control run had zero failures despite reaching 64/64 busy
workers. Queue drops were zero. These observations justify testing lower fixed
concurrency, not a causal claim from individual Mbps results or a winner.
Reducing queue capacity did not demonstrate a clear memory/429 improvement.

| Stored name | UI label | Workers | Queue | Batch | Timeout | Idle pool per host / total |
| --- | --- | ---: | ---: | ---: | --- | --- |
| throughput_w64 | Throughput 64 workers (64 / 4096) | 64 | 4096 | 32 | 1 ms | 64 / 128 |
| throughput_w48 | Throughput 48 workers (48 / 4096) | 48 | 4096 | 32 | 1 ms | 48 / 96 |
| throughput_w32 | Throughput 32 workers (32 / 4096) | 32 | 4096 | 32 | 1 ms | 32 / 64 |

All other Volga and outer config fields are identical to .5 throughput_current.
The only factor is WorkerCount plus the two HTTP idle-pool limits derived from
it. Names remain distinct in storage and diagnostics. The seven prior profiles
remain available with unchanged configurations and runtime behavior.

Whole-struct tests compare all three exact configs and reject differences
outside the three permitted fields. A frozen .5 oracle covers all seven old
profiles. CI additionally checks that packet/relay implementation sources and
HTTP classification code are byte-identical to the base. No retries, backoff,
adaptive concurrency, timeout, queue, batch-size or batch-delay changes.

Legacy send remains IP -> Legacy -> AES -> Volga, and receive remains Volga ->
AES -> Legacy -> IP. The independent frozen production peer tests all ten
profiles in both directions; CI also proves the old broken constructors fail
the OFX/version assertion for all ten. Outer batching does not explain the
Legacy real-phone measurements.

## Diagnostics

All existing sanitized HTTP failure classes remain unchanged. Two derived
snapshot fields are added without touching request handling:

- `http_successes`: existing successful relay request counter.
- `http_failure_rate`: `http_failures_total / http_requests`, a fraction from
  0 to 1 (multiply by 100 for percent), or 0 when no requests have completed.

The snapshot reuses the same success/failure loads for these fields and totals.
429 remains a subset of 4xx; timeouts remain separate from other network errors.
Independent class counters may briefly differ while requests finish. No URL,
auth, headers or response content is added. Tests cover existing classes, one
attempt per batch, empty/success/failure/mixed totals and JSON field values.

## Real phone protocol

Initial order: throughput_w64 -> throughput_w48 -> throughput_w32. Test only
these three in this round. For EACH:

1. Turn VPN off and Force Stop Perf Lab.
2. Launch, select user2, Legacy and the candidate.
3. Connect; wait 15 seconds. Check the exact profile string if needed.
4. Run one Speedtest on Elisa Tallinn.
5. Immediately copy diagnostics JSON; stop VPN.

Record download, upload, ping, loaded_down, loaded_up and jitter. Keep JSON with
android_pss_kib, go_heap_alloc, goroutines, worker_count, peak_workers_busy,
queue_len, queue_drops, http_requests, http_failures_total, http_failures_429,
all HTTP failure classes, and AES/TUN errors.

No winner after a single noisy run. A later alternating confirmation round may
be requested if w48 or w32 reduces 429 without destroying throughput. No other
tuning dimensions, VPS changes, PR or Release are part of this task. All Go and
Android executable validation/builds run in Linux CI only; no Windows-generated
executables, antivirus changes, exclusions or quarantine restoration.
