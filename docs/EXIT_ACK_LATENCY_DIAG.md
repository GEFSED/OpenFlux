# Server-only TCP ACK correlation

Exact production base: `081d214300c1067f17f6c0d02f84a8491f1a7b98`.
No Android changes, transport tuning, retry, new wire headers or protocol changes.

## Hooks audited against that source

* `tunnel/endpoint.go`, `TunnelLinkEndpoint.WritePackets`: IPv4/TCP data just before
  `onOutgoingPacket` sends it through the production wrapper chain.
* `CompressedTransport.Send` and `EncryptedTransport.Send`: transfer an in-memory
  observation identity from the old buffer to its newly constructed buffer. No
  payload mutation, hashing, logging, changed compression or changed encryption.
* `relayClient.Send`: transfer identity to the existing queue copy; timestamp just
  before its original nonblocking enqueue select. A failed enqueue retains its
  original error/drop semantics; it is not counted as an HTTP attempt.
* `relayClient.sendBatch`: timestamp immediately before `httpClient.Do`, then after
  the original response drain or Do error. Only HTTP 200/204 count as success.
  Body-read errors remain ignored by production and are counted separately.
* `wsListener.handleMessage`: timestamp when the complete WS message is available;
  pass that stamp through original bundle decoding and receive wrappers in memory.
* `TunnelLinkEndpoint.InjectInbound`: inspect returning cumulative ACK before
  gVisor input; timestamp once TCP header parsing completes. The second timestamp
  is immediately before `DeliverNetworkPacket`, after observation/bookkeeping and
  creation of the same PacketBuffer. This measures diagnostic overhead too.

DOWNLINK_TCP_HOOK = TunnelLinkEndpoint.WritePackets
HTTP_START_HOOK = relayClient.sendBatch immediately before Do
HTTP_COMPLETE_HOOK = relayClient.sendBatch after Do/error or body drain
RETURN_ACK_HOOK = TunnelLinkEndpoint.InjectInbound, before DeliverNetworkPacket

All stamps use the same Go monotonic clock. Sequence/ACK values, IPv4 addresses and
ports exist only in private flow state. No such fields appear in JSON. IPv4 TCP
header lengths/total lengths/fragments are checked for safe observation; unsupported
packets continue through production unchanged. Non-TCP packets are not correlated.

## TCP accounting

Flow identity is the IPv4 5-tuple (protocol fixed to TCP), with reversed tuple for
return ACKs. SYN anchors the sequence epoch, consumes one sequence-space byte, and
is not application data. FIN similarly advances the observed high-water mark but
not data counters. A new SYN with a different initial sequence resets the epoch;
any outstanding old records are explicitly evicted, invalidating that measurement.

Signed 32-bit serial differences unwrap around the current 64-bit high-water mark.
Forward/backward jumps >=2^30 bytes are rejected by the observer and counted.
This is narrower than the ambiguous TCP half-space; normal bounded windows fit.
The observer does not alter a packet, ACK acceptance or gVisor's TCP state.

Merged seen ranges include previously ACKed bytes. Novel disjoint ranges contribute
unique bytes; overlap contributes retransmitted bytes. A cumulative ACK retires
all covered unique ranges, including partial coverage. Duplicate/old ACKs do not
retire bytes again. ACKs beyond observed sequence space are flagged, not accepted.
An observed flow without SYN is flagged as unanchored, so the result cannot silently
claim a complete history. ACK progress byte count means newly ACKed unique data,
not SYN/FIN or raw ACK-number advancement.

`downlink_tcp_segments_created` includes retransmitted data transmissions.
`outstanding_segments_current` counts original transmissions with novel bytes still
outstanding; split novel intervals retain the same owner. `segments_acked` counts
those owners once all their novel bytes have been cumulatively ACKed.

## Latency attribution and retransmission ambiguity

Tunnel-out-to-ACK measures elapsed time from the **first observed transmission**
of each novel range. It includes recovery time when retransmissions occurred.
One latency sample represents one newly ACKed range portion, not one byte: a partial
ACK can produce multiple progress samples for one original segment. Counts/sum/max
and fixed buckets are reported; means are sample-weighted, not byte-weighted.

There is no safe way to determine which overlapping transmission caused a cumulative
ACK. Once an outstanding range is retransmitted, its original owner is marked
ambiguous. HTTP-start/HTTP-end-to-ACK samples for that owner are excluded and its
ACKed bytes counted in `http_latency_ambiguous_retransmit_bytes`. This is conservative
if only part of a segment overlaps. It avoids falsely attributing ACK latency to
the wrong successful relay request (Karn-style ambiguity handling).

For unambiguous data, ACK time earlier than HTTP completion increments
`ack_before_http_complete` and its byte counter. Negative end-to-ACK durations never
enter the histogram. An ACK following a failed HTTP attempt is counted separately;
HTTP failure is not assumed to prove loss. HTTP-start-to-ACK remains observable.

End-state byte gauges classify **the first observed transmission** of remaining
unique bytes: not started, inflight, success-not-ACKed, or failed-not-ACKed. They
are mutually exclusive and sum to outstanding bytes. Retransmission ambiguity is
reported separately; these gauges do not assert a retransmission's latest HTTP state.
ACKed bytes are unique, and not counted as newly delivered again on retransmission.

## Bounds, overhead and validity

Limits: 16384 outstanding interval records, 1024 flows, 32768 transient buffer tags,
256 merged history ranges per flow. Outstanding records/transient tags expire at
30 s. Fully ACKed sequence history is retained for 10 idle minutes (within the flow
cap), avoiding false uniqueness when a live idle connection resumes after 30 s.
There is no per-packet disk output, goroutine, sleep, capacity wait or network I/O.
Short observer-only mutex sections protect metadata; they are never held across
HTTP, transport locks, callbacks or InjectInbound. Exhaustion drops observation,
never packets. Snapshot scans are bounded by these fixed limits. HTTP observation
references are additionally bounded by the unchanged worker/batch counts.

The existing stats goroutine emits `[ACK-DIAG]` numeric snapshots every 2 s. No new
permanent goroutine is added. Ordinary diagnostic deployment uses the original argv,
without `--debug`; `utils/debug.go` and SafeGo are byte-identical to production.
Standard free-form startup/fatal log text is replaced with `[ACK-LOG] suppressed=1`
to prevent auth URL leakage. Error returns, exit semantics and recovery stay intact.

Any eviction, capacity drop, unsupported sequence jump, unanchored flow, ACK beyond
observed data, ACK without HTTP start or missing WS timestamp sets correlation_valid=0.
Even when conservation still balances after an eviction, causal conclusions are
invalid. Conservation: unique = ACKed + outstanding + evicted unique bytes.
Sampled age is also updated when ACKing/evicting so long waits are not missed.

Histograms: [0,50ms), [50,100), [100,250), [250,500), [500ms,1s), [1s,2s),
[2s,5s), [5s,10s), [10s,infinity). No fabricated exact percentiles.
HTTP payload bytes include encrypted inner packets and existing carrier keepalive;
they exclude uint16/base64/JSON. They are not application download bytes.

## Validation and operations

Linux only: source identity/diff check; root/mobile tests/race/vet/build. Deterministic
tests cover cumulative, multiple/partial/duplicate ACKs, overlap and ACKed history,
wraparound/SYN/FIN, eviction/caps, HTTP-before/after ACK, state conservation, buffer
identity, concurrency and numeric-only output. All production source changes are
enumerated as exact additive observation hooks by scripts/ack-diag-verify.py.

Install `openflux-ack-diag` separately; only user2 gets a runtime /run override.
Capture settled snapshots around one unchanged Android candidate/Legacy/Optimized
test. Then always restore original binary/argv, check hash and other service PID/start
timestamps, remove diagnostic binary and temporary SSH access. No PR or Release.
