> RESULT = BlockedByRealAndroidReturnPathRegression
> ReadyForUserAB is revoked. Real Android Baseline at 68e722d42b997c17460d890bfedd83de8b07d234 received zero bytes; ordinary v1.0.0 works with the same profile. Performance A/B is suspended. Prior measurements below are synthetic historical evidence only.

# Local measurements and reproduction

Measured on Windows amd64, Intel i7-11800H (16 logical CPUs), Go 1.26.4.
Base: `8566f727c8238436728758f139130cef433147b7`, Android v1.0.0 plus logo change.
All credentials and payloads are synthetic. No Yandex, exit node or VPS traffic.

## Candidate selection

The staged **one-factor** search holds the other knobs fixed: workers first,
queue second, outer batching third, inner batching fourth. It is not an
exhaustive factorial search or proof of globally optimal parameters. See
`results/windows-amd64-search.json` for all 44 measurements (including baseline).

At 60 ms RTT, 64 workers approached the offered rate with lower p95 than 16/32;
128/256 did not improve that workload enough to justify extra goroutines. Larger
queues did not help a no-loss run. 16/32 KiB outer batches reduced HTTP requests;
0–1 ms linger reduced interactive delays. Inner timeout has more influence than
inner batch count at this offered rate. These observations produce candidates,
not production defaults or a carrier ranking.

| Profile | Workers | Relay queue | HTTP idle per host / total | Inner count / timeout | Outer soft bytes / count / linger / queue | Receive |
| --- | ---: | ---: | --- | --- | --- | --- |
| baseline | 2000 | 1000000 | 2000 / 4000 | 20 / 2 ms | 8192 / 64 / 5 ms / 4096 | poll 2 ms |
| balanced | 64 | 2048 | 64 / 128 | 20 / 0.5 ms | 16384 / 64 / 1 ms / 2048 | blocking |
| low_latency | 64 | 512 | 64 / 128 | 8 / 0 ms | 8192 / 64 / 0 ms / 512 | blocking |
| throughput | 64 | 4096 | 64 / 128 | 32 / 1 ms | 32768 / 64 / 2 ms / 4096 | blocking |

Inner byte cap remains 4 MiB. No 64 KiB outer candidate: Volga has uint16 record
lengths, and the outer threshold may overshoot by one packet plus framing/AES.
The app MTU remains 576–1500. Frame/zstd/LZ4/AES code and CLI defaults are unchanged.
Baseline includes the documented unused-channel, bounded-pool and stop fixes.

## Final matrix

80 cases: four profiles × bulk/mixed/burst/interactive × RTT 20/60/120 ms or
60 ms plus every 23rd request stalled by 300/1000 ms. Fixed-seed payload bytes;
timing is subject to the host scheduler. JSON in `results/windows-amd64-matrix.json`
includes every case, p50/p95/p99, counters, allocations, heap, stacks, GC and
goroutines. All 80 cases delivered every offered packet, with zero queue drops.

The fake RoundTripper executes real relay JSON/base64 framing and the actual
outer codec. Each concurrent request has independent service delay, approximating
HTTP/2 multiplexing; this does **not** model TLS, bandwidth sharing, stream limits,
Yandex quotas, mobile radio, downstream WS buffering, TCP congestion or Android JNI.
The matrix uses plaintext packets; AES and legacy wire compatibility are verified
separately in bidirectional tests. Bulk offers 64 × 1400-byte packets every 10 ms
for one second (nominal 8.96 MB/s), then drains. MB/s includes drain time and is
decimal MB/s, not Mbps. This is a rate-limited comparison, not maximum capacity.

Bulk, 60 ms RTT, no stall:

| Profile | MB/s | HTTP requests/s | p95 ms | p99 ms | Peak heap bytes |
| --- | ---: | ---: | ---: | ---: | ---: |
| baseline | 8.352 | 1025.30 | 68.68 | 69.41 | 65021264 |
| balanced | 8.402 | 562.66 | 62.21 | 62.81 | 11366528 |
| low_latency | 8.394 | 918.99 | 61.59 | 62.11 | 12949760 |
| throughput | 8.393 | 281.01 | 65.04 | 65.45 | 10713080 |

The low_latency label is a hypothesis, not a guarantee: at 120 ms bulk RTT its
p95 rose to 194.93 ms (baseline 128.44; balanced 123.59; throughput 124.90),
showing worker contention/queueing at this offered rate. At 1000 ms stalls all
profiles have ~1062–1064 ms p99. No candidate removes a remote stall. Throughput
cuts request count, but no large throughput improvement was measured. Balanced
is a reasonable first A/B candidate, **not a production winner**.

## Memory and idle

Original relay startup: 51,612,656-byte heap / 2002 goroutines, and the first
1400-byte base64 encode raises heap to 68,394,600 bytes. Removing the dead queue
halves its channel allocation; bounded base64 pooling removes the 16 MiB cold
scratch allocation. See `ANDROID_VOLGA_BASELINE.md` for original phase data.

The real `YandexVolgaTransport.Start()`/Stop control path was also measured with
per-instance synthetic auth and a cancellable idle WS substitute. Each run used
a fresh process. This includes relay workers, keepalive, stats and WS lifecycle
loops, but excludes real HTTP auth/TLS/WS buffer allocations and AES derivation.
It is a lower bound for a real phone session, not Android PSS.

| Phase / profile | HeapAlloc | HeapSys | HeapInuse | StackInuse | Goroutines | NumGC |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| before Start, baseline | 465024 | 3899392 | 1171456 | 294912 | 2 | 1 |
| after Start, baseline with fixes | 27591400 | 33652736 | 28622848 | 16678912 | 2005 | 3 |
| after Stop, baseline | 1603864 | 44072960 | 2981888 | 6258688 | 2 | 4 |
| before Start, balanced | 465200 | 3932160 | 1064960 | 262144 | 2 | 1 |
| after Start, balanced | 616544 | 7569408 | 1310720 | 819200 | 69 | 2 |
| after Stop, balanced | 518120 | 7569408 | 1179648 | 819200 | 2 | 3 |

HeapSys need not return to baseline after GC; retained runtime arenas are not
live payloads. The after-Start delta is ~27.1 MB for baseline and ~151 KB for
balanced in this synthetic control path.

Separate five-minute **receive-only** idle runs (no network, keepalive or UI):

| Mode | Duration | Read calls | Wait calls | Total allocated bytes | Before / after heap | End goroutines |
| --- | ---: | ---: | ---: | ---: | --- | ---: |
| polling | 300.005 s | 103916 | 0 | 6280 | 606160 / 604792 | 2 |
| blocking | 300.002 s | 1 | 1 | 1104 | 607280 / 600864 | 2 |

Windows timer scheduling produced ~346 polls/s rather than the nominal maximum
500/s. The blocking reader woke on Stop and performed no periodic polling.
This is wakeup/read-call evidence, not a measured Android CPU/battery saving.
Phone diagnostics include process CPU time, PSS, Java/native heaps for manual A/B.

A second pair of 300-second runs measured OS process CPU time (Windows
GetProcessTimes; Linux/Darwin reproduction uses getrusage). Polling: 104075 reads,
**1.546875 CPU seconds**; blocking: one read/one wait, **0 CPU seconds reported**,
meaning below the accounting resolution, not zero energy use. Both ended with
two goroutines. They ran in separate processes; no radio, keepalive, JNI or UI
was active. Raw output is in `results/idle-windows-amd64.txt`. Android CPU and
battery still require the phone A/B. The diagnostics sampler runs only while
its dialog is visible and is shut down when the Activity stops.

## Reproduction

Go 1.26.4 or a compatible toolchain, no credentials needed. From repository root:

```sh
go test ./...
go test -race ./...
go vet ./...
go build ./...
(cd mobile && go test ./... && go test -race ./... && go vet ./... && go build ./...)
OPENFLUX_PERF_SEARCH=1 OPENFLUX_PERF_OUTPUT=search.json go test ./transport/yandex -run '^TestPerfLab$' -count=1 -v
OPENFLUX_PERF_OUTPUT=matrix.json go test ./transport/yandex -run '^TestPerfLab$' -count=1 -v
OPENFLUX_PERF_MEMORY=1 go test ./transport/yandex -run '^TestPerf(Memory|VolgaStartMemory)$' -count=1 -v
OPENFLUX_PERF_MEMORY=1 OPENFLUX_PERF_MEMORY_PROFILE=balanced go test ./transport/yandex -run '^TestPerf(Memory|VolgaStartMemory)$' -count=1 -v
go test ./transport/yandex -run '^$' -bench BenchmarkPerfBase64Candidates -benchmem
(cd mobile && OPENFLUX_PERF_IDLE=polling go test -run '^TestPerfIdle$' -count=1 -v)
(cd mobile && OPENFLUX_PERF_IDLE=blocking go test -run '^TestPerfIdle$' -count=1 -v)
BUILD_TYPE=perflab ./build_android_app.sh
```

On PowerShell set `$env:OPENFLUX_PERF_OUTPUT='matrix.json'` (and the other variables)
before the corresponding command; remove them afterwards. Benchmark tests skip
unless explicitly enabled; CI only runs ordinary regression tests.

The APK workflow also runs Java profile JSON tests and verifies both application
ID and label with aapt. Unit/race tests cover 100 repeated transport/mobile
lifecycles, concurrent Send/Stop, packet/Stop/timeout wakes, bounded drop-oldest
receive queue, unchanged defaults/env compatibility and sanitized snapshots.

## Remaining phone checks and adjacent findings

Real Android installation, permissions, UI, TUN, JNI costs, encrypted startup
PSS, idle CPU, reconnect and carrier speed remain manual checks. Services and
PendingIntents are explicit class components; notification channels, private
preferences and Keystore entries are UID-scoped. The VPN excludes its actual
`getPackageName()`, including `.perflab`; the Tile carries the saved codec/profile.
Production application ID stays `io.openflux.app`.

DNS uses an unbounded cached executor: a burst can grow thread count. It remains
unchanged to isolate Volga/receive A/B; a bounded DNS experiment needs independent
burst/latency tests. Blob/JSON pools and 4 MiB WS buffers still merit measurement.
AES scrypt startup also has substantial transient memory; its security parameters
are deliberately unchanged. No unsafe zero-copy, RACK, auth-refresh or wire fixes
were introduced. No exit change is required; long-session auth expiry is not fixed
by this branch. Perf Lab disables raw Go debug logs; copied diagnostics use only
an explicit numeric/enum allowlist and never serialize profiles or log buffers.
