# ACK correlator v2: local/CI proof, no deployment

Production base: 081d214300c1067f17f6c0d02f84a8491f1a7b98.
Previous observer: f31a49f7953eb910998ca2bdded22952faaed80d.
This follow-up changes only the observer, tests, source verifier and documentation.
No VPS access, Android changes, phone test, tuning, PR or Release is part of it.

## Exact audit of the previous implementation

All locations below refer to internal/ackdiag/correlator.go at f31a49f:

| Item | Old location / semantics |
| --- | --- |
| FLOW_KEY | line 21, flowKey [12]byte: IPv4 source/destination + TCP ports; TCP implicit |
| SEQ_RANGE_TYPE | line 22, interval {lo, hi int64}, half-open |
| SEQ_COMPARISON | lines 48-50, unwrap uses signed modular int32 difference around high |
| RANGE_INSERT | Outgoing lines 142-160; novel fragments appended to open |
| RANGE_MERGE | merge lines 122-126; sorted union of seen history |
| RETRANSMIT_DETECTION | lines 143-150; size minus novel, marks existing owner's ambiguous flag |
| ACK_ADVANCEMENT | Incoming lines 214-218; unwrap against high, future ACK rejected |
| ACK_RANGE_REMOVAL | lines 219-232; clips/removes covered open ranges |
| TTL_EVICTION | expire lines 251-257 and evictRecord lines 243-249; 30 seconds from first out |
| SEQUENCE_CONSTRAINT_CHECK | line 140, hi-high >= 2^30 OR high-lo >= 2^30 |

The old serial arithmetic already handled an ordinary numeric wrap. It was NOT
plain unsigned integer ordering. The five real violations each mean only that
Outgoing's quarter-space predicate fired. It runs before the size==0 return, so
pure ACK/RST sequence fields can trigger it. Incoming ACK checks cannot increment
this counter. No reason/flags/generation metadata were retained; the actual five
packet triggers cannot be recovered from the aggregate. We do NOT claim that
wraparound, overlaps, malformed packets, or RST caused those particular events.

A frozen-predicate test reproduces five violations with synthetic empty control
packets and a distant SYN. v2 does not treat those SEQ fields as new data. This
proves a model defect, not retrospective identification of unrecorded real packets.
The old parser did not expose RST and retained only one epoch per tuple.

TTL is a separate proven defect: actual age 31.988752140s exceeded 30s; 99 evictions
removed 113827 bytes. The first sequence violation preceded the first eviction.
Retransmission ownership is a third defect: pure retransmissions returned before
allocating an attempt/tag, and partial overlap marked the original whole owner
ambiguous. This excluded 383993 ACKed bytes from HTTP attribution in that run.

## Unique byte ledger and attempts

Each SYN-anchored generation has sorted disjoint atomic ranges. A new transmission
splits at its boundaries, inserts only uncovered gaps, and attaches one attempt to
EVERY intersection, including pure retransmissions and already-delivered history.
An attempt holds out/enqueue/HTTP-start/end/status, independently of unique bytes.
Partial ACKs split a range; cumulative ACKs cover all observed ranges below them.
The earliest applicable ACK timestamp records delivery once. Duplicate/old ACKs
cannot deliver bytes twice. Reordered callback observations can supply earlier
ACK/out timestamps; a future ACK is deferred until its observed sequence boundary
exists. A still-deferred ACK makes a snapshot invalid, and expiration latches failure.
No packet payload or real tuple/sequence value appears in snapshots.

Retained ACKed ranges prevent retransmissions after delivery from being counted
as new application data. Post-delivery transmissions are counted as attempts but
are excluded from attribution to the earlier ACK. No attempt is called causal.

Schema 2 reports six separate count/sum/max/fixed-bucket histograms:

- tunnel_first_out_to_ack, tunnel_last_out_to_ack
- http_first_start_to_ack, http_last_start_to_ack
- http_first_success_end_to_ack, http_last_success_end_to_ack

First/last mean chronological minima/maxima among attempts emitted by ACK time.
HTTP-end samples require successful completion no later than ACK. Pending or
later completion is counted separately as ack_before_first_http_complete and
ack_before_last_http_complete (also byte counts). A completed failed attempt is
not a successful end sample. No negative latency enters any histogram.

Samples are atomic delivered range portions, NOT unique causal sends or packets.
Each histogram also reports covered bytes. Splitting can change sample count;
compare byte coverage and documented range weighting, not fabricated percentiles.
Snapshots recompute from the bounded retained ledger, so later HTTP completion
cannot lose ACK-before-completion evidence. Six metrics replace the old ambiguous
single HTTP latency metrics; downstream journal readers must accept schema 2.

retransmitted_bytes counts repeated bytes on every repeated attempt.
retransmitted_unique_bytes counts the union covered by more than one attempt.
retransmission_attempts counts sends with any overlap.
multi_attempt_acked_bytes counts delivered unique bytes with multiple attempts
emitted at/before delivery. These are intentionally different quantities.

End-state unique byte gauges use any successful attempt first, otherwise any
inflight attempt, otherwise failed started attempts, otherwise not-started. They
are exclusive, sum to outstanding, and do not purport to identify a causal send.

## Sequence arithmetic and lifecycle

The exact dependency gvisor.dev/gvisor/pkg/tcpip/seqnum supplies Value.LessThan
and Size. Each observed SYN anchors a diagnostic epoch strictly shorter than
2^31 sequence positions. Numeric wrap at 2^32 is supported, including intervals
spanning it and retransmissions on either side. Exactly half a space, pre-SYN data
and >=2^31-byte epochs are explicitly unsupported and invalidate observation.
This is a documented scope for a short test, not arbitrary infinite TCP streams.
At 10 Mbps half a space takes about 29 minutes; this task's one-run scale is ~1MB.

SYN and FIN consume one sequence position but zero application bytes. Pure ACK
and RST SEQ fields are not advanced as DATA. RST abandons outstanding observations
as invalidated (not ACKed). A FIN closes a generation only after our FIN is ACKed
and peer FIN observed. A new SYN with a different ISN creates a separate retained
generation. Outstanding old bytes at reset are invalidated. Delayed ACKs matching
only an old generation are routed there; they cannot acknowledge the new one.

Without a connection ID, indistinguishable generations cannot always be resolved.
Same-ISN reuse after data/close, overlapping retained sequence arcs, ambiguous
multi-generation RST, data without an observed SYN, and unsupported serial spans
explicitly invalidate rather than silently guess. Ordinary SYN retransmission
before data is supported. Disappearing connections retain outstanding records
until TTL; disappearance never counts as successful delivery.

## Conservation and bounds

After every TCP event, changed-flow checks assert sorted/disjoint nonempty ranges,
complete attempt coverage, valid ACK provenance and per-flow created-byte totals.
New attempt intersections must sum to exactly its observed data length. Global
snapshots recount allocated state and assert:

unique_created = acked_unique + outstanding_unique + invalidated_unique

ACKed cannot exceed created. End-state gauges must sum to outstanding. A valid
snapshot requires invalidated=0 and zero eviction/cap/unsupported/invariant errors.
An unresolved reordered observation cannot be reported valid. No loss is normalized
away. On capacity failure the diagnostic marks the run invalid; it never blocks,
drops, retries or rewrites a production packet to preserve its own ledger.

Bounds (including ACKed history until diagnostic process exit):

- TTL 120 seconds for outstanding data, deferred ACKs and transient tags.
- 32768 atomic ranges, 65536 attempts, 262144 attempt/range references.
- 4096 retained generations and 65536 ACK history events.
- 4096 buffer identity tags and 16 MiB summed tagged slice capacity.

120s is 3.75x the observed ~32s wait, covers the planned short settled run and is
not a guarantee against all outages. Any TTL/cap eviction INVALIDATES the run.
Delivered history is retained until process exit, not silently recycled. This
trades an explicit short-test cap for reliable post-ACK retransmission accounting.

Memory on Linux amd64 is printed by TestMemoryLayoutAndBoundedFixture using
unsafe.Sizeof and a retained-heap fixture. Provisional structure estimates:
flow ~160B, range ~80B, attempt ~120B, ACK event ~40B, tag ~64B, reference 8B.
Heap allocator rounding, slice spare capacity, map buckets and transient copies
are additional. A conservative planning budget is 1KiB per range with one attempt,
one ACK and one flow per range: 1k ~1MiB; 10k ~10MiB; 100k ~100MiB (NOT admitted by
32768 cap). Typical multiple ranges per flow are cheaper; overlap adds references
and attempts, separately capped. At all caps, reserve ~80MiB for observer metadata,
spare/transient slices and up to 16MiB tagged slice capacity. This is server-only,
not suitable as an iOS/Android memory preset. Shared packet buffers already owned
by the transport are not copied. Slice capacity is charged, but an unusual subslice
can retain a larger underlying allocation; actual heap evidence is also recorded.

Observer mutex sections perform bounded per-flow slice scans. No network, sleeping,
per-packet log, disk writes, extra goroutine or lock spanning production callbacks.
Existing packet/hook/logger source is byte-identical to f31a49f, verified in CI.
Finite CPU/memory overhead of diagnostics is unavoidable and is not a throughput
optimization. First/last histograms are recomputed in the existing snapshot tick.

## Tests and interpretation

Deterministic event tests cover cumulative/duplicate/partial ACKs, exact/partial/
multi-segment retransmissions, ACK before HTTP completion, >30s delays, SYN/FIN,
wrap, reordered observations, tuple reuse/old ACK, TTL and each cap. Real-hook tests
cover parser, buffer Move, HTTP attempts and WS-to-inject timings; the frozen
Legacy/AES peer test remains independent and unchanged except schema metric name.

Seeded stress: 2048 synthetic flows, each closed/reused, random sizes, wrap-capable
ISNs, overlap, delayed cumulative ACKs. Replay-scale: 1838 sends, 592 retransmissions,
2994 ACK observations, ~1.1MB unique, >32s age. No real endpoints or external network.
Linux CI runs verbose observer tests/memory evidence, root/mobile tests/race/vet/build,
source immutability/diff checks and a separate Linux amd64 artifact. No Windows build.

A synthetic PASS establishes supported-model consistency, not a real network
bottleneck or a retrospectively recovered cause of the five real violations.
No deployment is authorized by this task. Any future deployment/readiness and
single phone measurement requires separate authorization and schema-2 validation.
