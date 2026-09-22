# Exact-production exit HTTP relay observation

Production source: `081d214300c1067f17f6c0d02f84a8491f1a7b98`.
Production Linux amd64 SHA256:
`08fcf4020cd3c7274c7abd78fe386b40d2fcf8515082d3475ced324ad109217c`.
This branch adds counters and safe logging only, not an optimization.

## Old relay error: exact meaning

In production `transport/yandex/vyandex.go`, `relayClient.worker`, the
`flush` closure increments `HTTPReqsFailed` once when `sendBatch(batch)`
returns an error (lines 523-527 in the production file). The previous
RACK diagnostic's `relay_error=1` mapped that debug message to a safe number.

1. The event is one failed **batch**, not one failed IP packet.
2. HTTP execution errors are included.
3. Every status except **200 or 204** fails, including other 2xx.
4. Network errors are included.
5. Timeouts are included, indistinguishably in the old counter.
6. WebSocket errors are separate; they do not increment this counter.
7. JSON encoding / request construction errors are included before HTTP.
   Base64 encoding here has no error return.
8. Queue drops and oversize/empty Send handling are separate, not HTTP errors.
9. The old counter combines preparation, network, timeout and status failures.
10. One `flush` increments once regardless of packet count. TCP retransmission
    can later generate new packets containing the same logical stream bytes.
11. There is no application-level batch retry/requeue in `sendBatch`/`worker`.
    The standard Go client/transport still has its original redirect/connection
    behavior; a logical `Do` is not necessarily one physical wire transmission.
12. `flush` clears its batch after either result. Failed batches are discarded
    at this layer. A transport error can be ambiguous about delivery at Volga.
13. That batch is not automatically replayed by relay. Higher-level TCP can
    regenerate traffic. Failure accounting is not proof of physical data loss.
14. Requests are concurrent.
15. 2000 workers independently pull from a shared `batchQueue`, each issuing
    one synchronous `Do` at a time. Idle pools are 2000/4000, not active limits.
    The separately allocated `queue` field is unused by Send; we observe batchQueue.
16. Each worker batches locally, flushing at 20 packets, 4 MiB accumulated, or
    2 ms after its first packet. Shutdown also flushes using the canceled context.
17. A request includes one batch: length-prefixed packets, then base64, then JSON
    operation metadata. The byte threshold is checked after appending a packet,
    so it can overshoot; per-packet lengths are encoded as uint16 in the old wire
    format. These existing semantics are unchanged. Actual packet/byte sizes are
    counted. Keepalive `{0}` bypasses AES/Legacy and is also a relay payload.

`HTTPReqsSent` in production counts successes, not total attempts.
Production drains response bodies but ignores body-read errors; this is preserved
and counted separately as `relay_body_read_errors_ignored`.

## Direction mapping (exact production code)

Downlink: Internet TCP socket -> `TCPTunnel.handleExitTCP` remote-to-local
`io.CopyBuffer(local, remote, ...)` -> gVisor TCP segmentation ->
`tunnelEndpoint.WritePackets` -> `onOutgoingPacket` -> `trans.Send` ->
`CompressedTransport.Send` (Legacy) -> `EncryptedTransport.Send` ->
`YandexVolgaTransport.Send` -> `relayClient.Send` -> worker `flush` ->
`sendBatch` -> **exit HTTP POST** to Volga -> **phone WebSocket** ->
Volga batch decoding -> AES -> Legacy -> mobile callback -> TUN.

Uplink: phone TUN -> Legacy -> AES -> phone HTTP POST -> Volga ->
**exit WebSocket** `connect` / `handleMessage` / `handleRelayMessage` or
`handleBundle` / `handleBundleItem` / `decodeBatch` -> transport callback ->
AES -> Legacy -> `tunnelEndpoint.InjectInbound` -> gVisor/proxy -> Internet.

Thus relay HTTP failures measured on the exit are directly on the **downlink**
carrier path, including return ACK/control traffic. WS on exit is **uplink**.
HTTP is also used for auth; auth requests are deliberately outside relay counters.
There is no claim that all counted bytes are Speedtest download application data.

## Metrics and window boundaries

`[RELAY-DIAG]` contains a fixed numeric JSON schema, every 2 s, reusing the existing
stats-loop goroutine. No telemetry dependency, locks on admission, retries or gates.
Legacy free-form logs are replaced by `[RELAY-LOG] suppressed=1`; debug text is
suppressed before formatting. Error return values, Fatal exit behavior and all
transport configuration remain unchanged. No `--debug` flag is needed.

* requests = `http.Client.Do` calls; successes/failures = completed Do + body drain.
* failures = network (excluding timeout) + timeout + 4xx + 5xx + other status.
  4xx includes exact 400/401/403/404/409/429; other4xx is its remaining difference.
* prepare_errors occur before Do, and are additional to HTTP failure totals.
* bytes = intended **JSON HTTP body** size, excluding headers/TLS/TCP framing.
* payload_bytes = sum of original inner packet lengths in that batch, excluding
  uint16 prefixes/base64/JSON. This includes AES overhead and carrier keepalives;
  it is not decrypted application bytes. Failed bytes are attempted sizes, not
  an assertion of bytes physically transmitted/lost. 429 has its own payload sum.
* duration = immediately before Do through error or completed response-body drain,
  before deferred Body.Close. Buckets are [0,50ms), [50,100), [100,250), [250,500),
  [500ms,1s), [1s,2s), [2s,5s), **[5s,infinity)** (`ge_5s`). All outcomes included.
* batches/packets/bytes include failed and preparation-failed attempts. The
  original successful-only counters and public Stats behavior are unmodified.
* workers_busy covers batch encoding through HTTP completion, not workers merely
  collecting a pending batch. inflight covers Do/body drain. Peaks are cumulative.
* queue_peak_observed samples len after enqueue; a racing dequeue can make this
  a lower bound. Queue mean is the mean of periodic samples, not a time integral.
* ws_messages/message_bytes count complete received WS messages, including control;
  ws_payload_bytes counts lengths actually passed onward after original decodeBatch.
  decode errors count failed envelope/inner/relay JSON and base64; the permissive
  original decodeBatch fallback is not converted into an error. It remains unchanged.
  ws_connected means a WS handshake succeeded and its read loop has not exited.
  ws_disconnects counts completed established connections, including normal Stop;
  ws_reconnects retains the original count of scheduled reconnect cycles, not successes.

Snapshots are individually atomic loads, not transactions. Use settled start/end
snapshots, record inflight at both boundaries, and check conservation identities.
Do not invent missing bytes from a transient snapshot skew. Duration interval maxima
allow an observed-window maximum; cumulative maxima are labeled session-wide.
Timestamped snapshot deltas define the observation window, which includes the
15-second warm-up and human interaction. It is not the exact Speedtest download
phase unless separately timestamped. Request/byte rates use that full window.
Histogram quantiles are bucket intervals only; no fabricated exact percentiles.

## Scope and validation

Linux CI checks the entire production source via an audited additive hook manifest,
verifies all other production files are byte-identical, and tests HTTP body equality
against a verbatim frozen production `sendBatch` (only method renamed). Tests cover
status/error classification, accounting, ignored body-read errors, no relay retries,
queue drops, concurrent snapshots, duration boundaries and unchanged WS delivery.
Root and mobile test/race/vet/build run in Linux only. No Windows binaries are run.

Only user2 gets a temporary /run override after original hash/argv/service checks.
One phone test, then restore original, verify others' PID/start timestamps, remove
temporary SSH access. No Android, RACK/TLP, network settings, PR or Release changes.
