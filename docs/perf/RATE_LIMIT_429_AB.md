# Perf Lab .7: fixed 32 workers versus a shared HTTP 429 gate

Base: `b59dd844dd5b8262c92f0310d01d3e17cf49fdd5` (.6, code 15).
Build: `1.0.0-perflab.7`, code 16, `io.openflux.app.perflab`, arm64-v8a.

Real-phone correctness is established for Legacy + AES. The user's 32-worker
runs reached uploads of 5.89 and 10.6 Mbps, with 429 rates of about 4.02% and
0%, respectively. Saturation at 32/32 alone is not evidence of insufficient
concurrency. The next experiment compares a rate-limit gate with unchanged
fixed concurrency; it does not establish a winner from these noisy runs.

| Stored profile | Workers | Queue | Batch | Timeout | Idle pool host / total | Guard |
| --- | ---: | ---: | ---: | --- | --- | --- |
| throughput_w32 | 32 | 4096 | 32 | 1 ms | 32 / 64 | disabled |
| throughput_w32_429guard | 32 | 4096 | 32 | 1 ms | 32 / 64 | enabled |

Only the explicit `RateLimit429GuardEnabled` bool differs. Policy constants
are fixed for this experiment. All other Volga/outer config fields are equal.
All ten prior profiles retain their settings and have the guard disabled.
Neither a profile nor this guard is promoted to the production/default path.

## Algorithm and boundaries

Each opted-in relay owns one mutex-protected gate shared by its existing
workers. Before HTTP Do, a worker obtains admission or waits on a cancellable
timer. No goroutine is created by the gate. No unrelated transport mutex is
held while waiting. An extended deadline is rechecked when the old timer wakes;
this can require another timer, but never a busy loop. Stop cancels the wait.
Before the first 429 there is no artificial delay, just gate bookkeeping.

An HTTP 429 closes/extends the gate as soon as response headers arrive. The
batch retains its existing failed outcome: it is never reconstructed, retried
or replayed. Already admitted/in-flight requests continue; future admissions
wait. On expiry all workers may resume normally, with no jitter, spacing,
adaptive worker changes or permanent concurrency reduction.

Retry-After accepts nonnegative integer seconds (including zero) and current
or future HTTP dates. Valid delays cap at 5 seconds, including huge digit-only
values without integer overflow. Empty/malformed/multiple values and past
dates use fallback; present invalid values are counted separately from absence.
The header is parsed transiently, never logged or stored in diagnostics.

Fallback is global per relay: 25, 50, 100, 200, 400, 800, then 1000 ms maximum.
Every fallback 429 advances this state; concurrent responses never shorten the
gate. A valid Retry-After overrides the selected cooldown but does not itself
advance the fallback. A successful accepted relay response (existing 200/204
policy) from a request admitted after the latest cooldown resets fallback.
Generation checks prevent an old in-flight success from clearing a newer 429
strike, even if that success arrives after the newer cooldown has expired.

Worker count, batch size/timeout, queue size, HTTP timeout, framing and AES
remain unchanged. A worker can now be busy waiting at the gate, so busy-worker
counters must not be equated with concurrent HTTP requests. Gate wait happens
before the HTTP client's existing request timeout starts. Waiting workers can
retain their existing batch; there is no new replay queue.

## Diagnostics

Existing HTTP outcome classes, successes and failure fraction remain intact.
As before, total send failures can include pre-HTTP failures; cancellation at
the gate is also a failed send without an HTTP attempt. Capture phone metrics
before Stop for comparison. New numeric/bool fields are:

- rate_limit_guard_enabled
- rate_limit_429_events
- rate_limit_wait_events (one per request that actually waits, even if extended)
- rate_limit_wait_ns (cumulative completed/cancelled waits)
- rate_limit_retry_after_used
- rate_limit_retry_after_invalid (present but invalid)
- rate_limit_fallback_used
- rate_limit_current_gate_remaining_ns (nonnegative snapshot)
- rate_limit_max_cooldown_ns (maximum selected cooldown during this relay session)

No URLs, raw headers, cookies, tokens, document IDs or payloads are retained in
these diagnostics. Disabled profiles expose false/zero gate fields and allocate
no gate. WebSocket reconnects keep the same relay/gate; a new relay starts a new
gate session. Independent HTTP and gate snapshots may briefly differ in flight.

## Validation

Linux CI alone runs root/mobile test, race, vet and build; Java tests and the
arm64 APK build. Guard tests send real POSTs only to local httptest servers.
An injected clock makes cooldown assertions deterministic, and a real timer
test verifies prompt Stop cancellation. Tests cover successful/no-wait traffic,
Retry-After seconds/dates/overflow/invalid/absent values, bounded fallback,
concurrent extensions, waiter rechecks, stale-success protection, recovery,
exactly one attempt for a rejected batch, disabled behavior and safe JSON.

Whole-config tests compare the two candidates and all historical profiles
against frozen .6 values. CI checks the unchanged packet stack and verifies
that removing only the explicit opt-in hooks recovers the exact .6 relay
source. Independent production-peer Legacy/AES tests cover all eleven profile
names, plus a mutation check that rejects the old wrapper order. Send remains
IP -> Legacy -> AES -> Volga; receive is the inverse. No Windows Go binaries
are built/run, and antivirus settings/quarantine remain untouched.

## Minimal phone experiment

Initially test only throughput_w32, then throughput_w32_429guard. For each:
VPN off; Force Stop; launch; user2 + Legacy + exact profile; connect; wait 15
seconds; one Speedtest Elisa Tallinn; immediately copy JSON; stop VPN.

Collect download/upload, ping, loaded_down/up, jitter, PSS, Go heap, goroutines,
HTTP requests/successes/failure rate/429, all rate_limit fields, queue drops and
AES/TUN failures. A promising result reduces 429 under meaningful load without
catastrophic throughput loss, worse loaded latency, resource regressions or
queue/AES/TUN errors. Zero 429 is not required. Synthetic tests cannot choose
a winner. If network conditions are incomparable, at most one reversed
guard -> fixed confirmation pair may be requested later if necessary.

No VPS changes, production promotion, old-profile deletion, PR or Release.
