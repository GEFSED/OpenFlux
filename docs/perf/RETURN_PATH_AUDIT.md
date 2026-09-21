# Baseline return-path regression: diagnostic build 2

Previous result: **BlockedByRealAndroidReturnPathRegression**.
`ReadyForUserAB` for `68e722d42b997c17460d890bfedd83de8b07d234` is revoked.
Performance A/B is suspended. No candidate profile has been retuned.

The phone received zero bytes while ordinary v1.0.0 worked with the same
profile. Successful HTTP relay requests establish outbound HTTP acceptance,
not receipt at the exit or a working return WebSocket. The old counters do not
locate the missing boundary. This failure has **not** been reproduced on a
real device by the author of this change, and no specific causal change has
been proved. Local HTTP/WS tests below pass for both implementations.

## Exact comparison

Reference: `damnurmum/OpenFlux-Android` commit
`8566f727c8238436728758f139130cef433147b7` (v1.0.0 source).
Failed Perf Lab: `68e722d42b997c17460d890bfedd83de8b07d234`.
This inventory covers all runtime changes in the four requested files between
those commits, not merely the profile constants.

### transport/yandex/vyandex.go

1. Base64 pool: initial capacity 16 MiB -> 4 KiB; unlimited retention -> return
   buffers only up to 256 KiB. Both copy to a string before returning the buffer.
2. Removed unused million-element `queue`, retaining `batchQueue`. Allocation
   and GC behavior changed even with identical Baseline config values.
3. Relay `Send`/`Stop`: added enqueue mutex, stopped-context check and a context
   select arm; Stop cancels, joins, closes idle HTTP connections and drains the
   queue instead of closing both channels then joining. These affect ordering,
   post-stop acceptance, cancellation and pending packets.
4. Added atomic peak-worker tracking during flush. Worker batching, HTTP request
   construction, uint16 framing, frontier and relay status handling unchanged.
5. WS goroutine is joined at Stop, previously only cancelled.
6. `Dial` -> `DialContext(w.ctx)`; close error response bodies; register
   `context.AfterFunc` to close an established socket on cancellation. Previously
   ReadMessage could wait for its deadline after Stop. Gorilla's `Dial` delegates
   to `DialContext(context.Background())`; this alone does not prove a handshake
   regression with a live context. Both paths pass real local WS tests.
7. Added per-instance auth/connect test seams (nil in the APK); reconnect uses
   the seam when set. Retry/backoff values and production auth are unchanged.
8. Added validated explicit Volga-config constructor. Default values did not
   change; mobile started using this constructor even for Baseline.
9. Transport Start/Stop serialized with lifecycle mutex; Start became idempotent,
   recreates keepalive-stop channel and stops BaseTransport on auth failure.
10. Relay publication, Send and diagnostics use an RWMutex; keepalive/stats
    goroutines are joined; Stop closes auth HTTP idle connections.
11. `SetConnected(true)` still happens before the asynchronous WS handshake.
    This pre-existing behavior was never proof of a bidirectional carrier.
12. WS URL/query/headers/cookies, auth parsing, self-message filter, SESSION/
    WORKER and relay/exchange parsing, inner decoding and callback registration
    were unchanged. Default worker/pool/queue values and read/write buffers
    were unchanged. Observed 2019 goroutines is consistent with those defaults.

### transport/batched.go

1. Added validated config constructor and queue-depth option; linger stored as
   duration rather than integer milliseconds. Old constructor still reads env;
   Perf Lab Baseline used explicit config and therefore ignored batch env vars.
2. Start serialized/idempotent; creates done channel and tracks flush goroutine.
3. Send serialized against Stop and rejects a non-running transport; base copied
   and enqueued without a running check, including before Start or after Stop.
4. Stop closes done, stops inner transport, joins flush and drains queue. Base
   only cleared running and stopped inner, leaving an idle flush blocked.
5. Flush listens for done while waiting, draining and lingering; cancellation
   can discard the pending batch. Base had no cancellation select arms.
6. Counted packets, batches, drops and inner-send failures (base ignored the send
   result). Batch/framing/zstd encode/decode and receive callback order unchanged.

### mobile/mobile.go

1. Global running/transport/slice replaced by packetSession, a fixed 1024-slot
   ring, per-session mutex, ready/stopped flags and notify/done channels.
2. Start serialized by startMu; session published before construction; constructors
   selected by normalized profile. Failed construction/start now stops the session
   and failed transport; pending-Start Stop is checked before publication.
3. Callback closes over its session instead of global client. Packet copy moved
   inside the lock after stopped check (base copied first). Overflow still drops
   oldest, but ring clears stale references; Read/Stop also clear retained slots.
4. Stop idempotent, retains stopped session for metrics and wakes waiters; base
   cleared global pointers/slice and could publish a late Start or accept a late
   callback into a newer session.
5. Added blocking ReadWait with timeout and stop cancellation. Baseline Java still
   used the original 2 ms polling, but its Go queue was not original.
6. Send/read operate on a session snapshot; Send checks ready and reports generic
   errors. Added counters and runtime.ReadMemStats snapshot; only collected when
   Android diagnostics dialog is visible.
7. Logging initialized once; Perf Lab disables Go debug output, replaces other
   messages with generic text and does not expose raw transport errors. These
   security changes are deliberate and retained in diagnostic Baseline.
8. Construction order **did not change**: raw -> codec wrapper -> AES wrapper.
   Send therefore runs AES -> outer codec -> Volga uint16/base64 relay. Receive
   runs Volga -> outer codec -> AES -> mobile. The base comment about compressing
   plaintext before encryption was inaccurate. KDF URL/fallback context and
   client `exit=false` are unchanged. Receive is registered **before Start** in
   both versions; transport is published **after Start** in both versions.
9. Added profile API/default normalization and references used for sanitized
   diagnostics. Other carrier selection/default URL/key validation unchanged.

### OpenFluxTunnelService.java and APK boundary

1. Reads normalized performance-profile extra, passes it through startTunnel and
   calls StartWithProfile only in Perf Lab.
2. Selects ReadWait only for non-Baseline profiles; Baseline polls Read every 2 ms.
3. Added a session check after Read/ReadWait. Diagnostic Baseline restores the
   original polling-loop order; extra check remains only for blocking reads.
4. VPN session/notification titles use application label instead of literal name.
5. DNS handling, MTU, outgoing buffer, TCP filtering, builder routes and package
   exclusion logic did not change. Own package is excluded using getPackageName,
   so the new application ID is not sent through its own TUN.
6. Side-by-side package, separate app preferences/UID, debug signing, profile
   selector and local metrics UI are intentional app-level differences. No
   assumptions about equivalence of signing/OS/carrier treatment are made.
   Java profile round-trip tests preserve transport, codec and Baseline default.

## Diagnostic control and remaining differences

Baseline now selects `mobile/baseline_v100.go`,
`transport/batched_baseline_v100.go` and
`transport/yandex/vyandex_baseline_v100.go`. These are mechanically derived from
the exact base source with distinct names, **not** the candidate constructors.
They restore the original base64 pool, both relay queues, close-on-Stop relay,
WS Dial/cancel-only Stop, unjoined keepalive/stats, batched lifecycle, global
mobile callback/slice queue, Start publication and Java polling behavior.
Shared auth/config/framing/compression helpers were unchanged in the base diff.

The explicitly retained differences from the reference are atomic/read-only
counters, snapshot locking, sanitized logging/errors, test-only dependency seams,
profile dispatch, app identity/UI and the build version. No payload, key, nonce,
cookie, document identifier, endpoint path or raw error is in diagnostics.
The AES changes split identical rejection conditions into counted branches;
KDF, header, nonce, encryption, replay acceptance and wire order are unchanged.

This is a **temporary diagnostic control**, not a proposal to ship the old
lifecycle upstream. It intentionally retains base high allocations, idle batch
goroutine retention, delayed WS shutdown, and the base's unsafe concurrent
relay Send/Stop and late-start behavior. Passing race tests cover exercised
scenarios; they do not make those known base limitations safe. Use one fresh
connection for the requested retest, then close the diagnostic app. Optimized
paths retain their cancellation/synchronization code unchanged.

## Layer counters and interpretation

Snapshots are cumulative, read-only counts since this transport/session start;
different atomic fields are not a transactional snapshot. Read twice if a packet
is in flight. `transport_started` and old `connected` do not prove WS readiness.
`ws_connected` means an established receive socket only, not Internet access.

1. `ws_connect_success/failures`, `ws_connected`: handshake result and live socket.
2. `ws_raw_messages`, `ws_ping_messages` (JSON application pings), SESSION/WORKER,
   self ignored, relay/exchange, JSON/base64 errors: message parsing boundaries.
3. `volga_inner_packets_decoded/bytes_decoded`: inner records emitted before codec.
4. `outer_receive_frames`, decode success/errors, packets decoded: batched codec
   boundary. Absent for legacy, which retains its original LZ4 fallback behavior.
5. `encrypted_receive_packets`, too short, bad header, wrong direction, decrypt
   fail, replay drop, success: AES boundary. Absent when encryption is disabled.
6. `mobile_callback_packets/bytes`, `receive_ring_enqueued/dropped`: callback and
   bounded queue acceptance. The common queue counter names also describe the
   restored Baseline slice FIFO. Dropped counts eviction of the oldest packet.

Zero raw WS frames is different from a codec/AES rejection. All-self or pings-only
messages are different from return relay traffic. A decrypt failure identifies
the AES boundary but does **not** by itself prove which peer/config is wrong.

## Controlled checks, not a claim of a phone fix

* A: original and bounded base64 outputs/ownership checked at small and oversize
  lengths. No wire difference observed.
* B/C/D: actual local HTTP relay + WS upgrade/read/parser tests run original and
  candidate lifecycle paths, batched/legacy, AES on/off, against a frozen base
  peer; outbound request and returned packet verified. Handshake failure, no WS
  frames, pings, self filtering, relay/exchange and malformed JSON/base64 counted.
* E: real Baseline startup constructor (only raw carrier replaced) and public
  mobile Send/Read round-trip with the independent peer. Original slice and ring
  overflow/delivery compared. Tests assert registration-before-Start and wrapper
  order, not just a symmetric round trip.
* Negative layered tests prove bad codec frames stop before AES, bad AES headers
  and wrong-secret decrypt failures stop before mobile, valid replies reach the
  queue, and short/direction/replay drops each increment their own count.
* `testsupport/basev100peer` contains the exact base batched/framing/compressor/
  encrypted files with only package renamed. SHA-256 provenance is tested. Only
  tests import this peer; it is not linked into the APK.

No local case reproduced zero return traffic with otherwise working HTTP.
Accordingly **ROOT_CAUSE = unproven**, not DialContext, encryption order or any
other guessed change. The single diagnostic APK restores the full original
runtime control and reveals the receive boundary on the actual carrier.

## Retest gate

Build `io.openflux.app.perflab`, versionCode 11, `1.0.0-perflab.2`, arm64 only.
Publish an Actions artifact only. Once Go tests/race/vet/build, Java tests, APK
build/identity checks and secret scan pass, the maximum allowed result is
**ReadyForReturnPathRetest**. Synthetic success cannot restore `ReadyForUserAB`.

Install/update Perf Lab, use the same **Baseline** profile, connect, try one
website, then copy diagnostics immediately. Do not change document, codec, key
or exit to make the test pass. Do not post profile exports or secrets.
If Android rejects an update due to a different ephemeral CI debug signature,
only Perf Lab may need reinstallation; keep the ordinary working app intact.
No signing credentials are requested or published.
