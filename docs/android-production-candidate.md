# Ordinary Android performance candidate

Base: damnurmum/OpenFlux-Android main
8566f727c8238436728758f139130cef433147b7 (checked before implementation).
Port source: Perf Lab 53862516bfa8733a91c9c971264f9b1211bfae3e.
The branch starts at the ordinary base; laboratory commit history is not merged.

## User behavior and compatibility

The ordinary editor offers Standard, Speed and Optimized. Missing/unknown saved
performanceMode means Standard. ProfileStore, encryption storage, credentials,
codec, document and icon are not migrated. The new field round-trips per profile.
No Perf Lab import or broad migration is introduced. The existing SOCKS5 and
non-Volga carriers use Standard; the selector explicitly labels its packet-VPN,
Yandex Volga scope.

Speed exactly matches the verified laboratory throughput_w32 configuration.
Optimized exactly matches throughput_w32_429guard, differing ONLY in the guard
enable flag. Both use 32 workers, queue 4096, batch 32, timeout 1 ms, HTTP idle
pools 32 per host / 64 total. All other Volga and outer-batching fields are
compared to a frozen test-only copy of the verified laboratory configuration.

For Speed/Optimized Legacy+AES:
send IP -> Legacy -> AES -> Volga; receive Volga -> AES -> Legacy -> IP.
Independent frozen production 081d214 peers test both directions and reject
the historical reversed order. Batched retains the laboratory ordering and is
tested independently against the ordinary 8566f727 peer; this does not establish
compatibility with a Legacy-only exit.

Standard preserves 8566f727 scheduling, constructors, queues and wire order.
That base constructs raw -> codec -> AES; its Legacy order differs from the
older production 081d214 exit. This candidate does NOT silently rewrite Standard.
Standard is tested against its own frozen 8566f727 peer. Safe app logging is the
intentional observability exception: provider debug output and raw error text
are suppressed, for Standard/SOCKS5 as well as the new modes.

## Audited port inventory

| Change | Classification | Reason / scope |
| --- | --- | --- |
| Explicit configured Volga implementation | REQUIRED_FOR_RUNTIME | Preserve Standard constructor unchanged; new modes use verified worker/queue/pool scheduling. Shares original auth, framing decoder and constants. |
| Cancellable WS/relay lifecycle and Stop/Send synchronization | REQUIRED_FOR_SAFETY | Verified Lab path joins owned loops, cancels pending operations, drains stopped queues. No auth-refresh or retry redesign. |
| Bounded base64 pool | REQUIRED_FOR_SAFETY | 4 KiB cold buffer; retain at most 256 KiB; exact base64 output tested. |
| Configured batched transport | REQUIRED_FOR_RUNTIME | Explicit modes can retain Lab Batched scheduling; original constructor and wire codec stay untouched. |
| Packet session ring and blocking ReadWait | REQUIRED_FOR_RUNTIME | Explicit modes avoid 2 ms receive polling; bounded drop-oldest semantics, cleared slots, Stop wakeup. |
| 429 gate | REQUIRED_FOR_RUNTIME | Exact source copy; shared instance gate, Retry-After seconds/date capped at 5 s, fallback 25..1000 ms, success recovery, no batch replay. |
| Sanitized receive/HTTP/guard counters | DIAGNOSTIC_ONLY | Internal counters retained to verify boundaries and guard behavior. App exposes compact mode/workers/drops/requests/429/guard-waits/reconnect summary, at most every 30 s when logs are read. |
| Suppressed provider logs and generic bridge errors | REQUIRED_FOR_SAFETY | No URL, key, cookie, auth token, payload or raw Retry-After in app diagnostics. |
| Full Perf Lab JSON UI, experiment selectors/metadata, TUN classification and EINVAL continuation | PERF_LAB_ONLY, excluded | Ordinary UI and TUN failure behavior retained. |
| Benchmark/memory experiment harness and historical production profile branches | PERF_LAB_ONLY, excluded | Historical configs exist only in an independent test oracle, not application parsing/UI. |

Configured implementations are separate to avoid silently changing Standard or
CLI behavior. Tests compare original files byte-for-byte to the base (except
the new default-false config field). Auth and wire format are not forked.
This deliberate duplication should be reconsidered only with separately
authorized Standard migration and compatibility evidence.

## Real device evidence and limits

Measurements below were supplied from Perf Lab, not from this candidate APK.
Mobile network conditions vary; Speedtest results are not laboratory-stable
or guaranteed rates, and no mode is claimed universally fastest.

* The Legacy/AES wrapper bug was fixed and Internet/AES/TUN success confirmed.
* Original Baseline: 2000 workers, about 2026 goroutines, 473216 KiB PSS,
  about 303 MB Go heap.
* Fixed 32 workers reached 1.62 Mbps download / 10.6 Mbps upload in one run,
  4457 requests, zero HTTP failures/drops, 49 goroutines, 253940 KiB PSS,
  no AES/TUN errors. Another run reached 5.89 Mbps upload with about 4.02% 429.
* Heavy 64/96-worker runs observed roughly 9-10% / 17.3% HTTP 429.
  This is not a matched-condition causal comparison.
* Guard policy passed deterministic local HTTP tests and race tests in Perf Lab:
  cancellation, concurrent deadline extension, Retry-After, fallback, recovery,
  disabled equivalence and exactly one attempt for rejected batches.
* A no-429 phone smoke completed 4005 requests successfully with zero waits.
* A later real Optimized run naturally activated the guard: 10069 requests,
  10057 successes, 12 HTTP 429 (fraction approximately 0.0011918),
  12 guard events, 32 waits, 32476739517 cumulative wait ns, 12 fallbacks,
  max cooldown 1000000000 ns. Queue drops, reconnects and AES/TUN errors were zero.
  This establishes guard activation without tunnel failure, not a causal
  percentage improvement versus an identical uncontrolled network.

## Candidate distribution

Linux Actions builds only arm64-v8a. The candidate uses ordinary app source/UI
with package io.openflux.app.candidate, label OpenFlux Candidate, version
1.0.0-candidate.1 / code 11, CI debug signing. It installs alongside the working
io.openflux.app and cannot update it in place. Each CI runner may generate a
different debug certificate; subsequent candidate update compatibility is not
promised. No working app uninstall, installation, release upload or signing
credential request is part of this task.

Normal debug/release identity and version remain io.openflux.app / 1.0.0 / 10.
Do not distribute a production replacement until the maintainer assigns an
appropriate version and signs it with the ordinary app's existing certificate.

Validation runs only in Linux CI: root/mobile test, race, vet, build; independent
wire peers, exact config oracle, guard/lifecycle tests; Java profile/UI-value
tests; arm64 APK identity and certificate verification. No Windows-built Go or
Android executable is run. No VPS change, upstream PR or Release is made.
