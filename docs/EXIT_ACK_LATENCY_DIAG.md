# ACK correlator v3: RST accounting, local/CI only

Current follow-up base: `4e991a6e3856da7f4372be69091d830e5e3421c8`.
The v2 documentation below is retained as history, not the v3 lifecycle contract.
Production source remains `081d214300c1067f17f6c0d02f84a8491f1a7b98`.
No VPS access/deployment, phone test, Android change, tuning, PR or Release.

## Exact v2 RST audit

All old locations in this table are `internal/ackdiag/correlator.go` at 4e991a6:

| Requested location | Exact code |
|---|---|
| RST_HANDLER | outgoing 155-160; acknowledge 224 |
| RST_FLOW_LOOKUP | outgoing 151-152; acknowledge 222-223, map of tuple to epochs |
| RST_GENERATION_SELECTION | 156/224 reject if len(gs)>1; otherwise latest/only epoch |
| RST_RANGE_INVALIDATION | invalidateOpen 142-146 sets invalid on every unACKed atom |
| RST_ATTEMPT_CLEANUP | none: attempts/references/ranges remain allocated |
| RST_GENERATION_CLEANUP | none: closed=true but map/history are retained |
| AMBIGUOUS_GENERATION_CHECK | 156,163,174,180,183,224,225,231 |
| FLOW_REUSE_AFTER_RST | 162-169, new SYN creates epoch; old open atoms invalidated |

RST invalidation was an observer policy, NOT mandatory TCP semantics or necessary
memory cleanup. RST proves neither delivery nor non-delivery of outstanding bytes.
RFC 9293 sections 3.5.2, 3.5.3 and 3.10.7 distinguish reset processing from data
acknowledgment: https://www.rfc-editor.org/rfc/rfc9293.html#section-3.5.2 .
We do not treat ACK on a RST as an ordinary delivery ACK.

The hook architecture does permit a later ACK observation: endpoint.go Incoming
runs before DeliverNetworkPacket, with no closed-flow filter. The observer does
not inspect gVisor's subsequent acceptance decision. WritePackets and the inbound
WS callback are separate paths. An ACK can therefore be observed after a reset;
this statement describes code reachability, not proof about a particular real packet.
The old applyACKEvent skipped invalid atoms, so later evidence could never resolve
them. An ACK here means a header observed on the authenticated return path, NOT
proof of application consumption at the phone.

The 84823 non-TTL bytes in the last run are accounted for by the old reset policy
(178 reset events, zero epoch resets). The exact triggers of its three ambiguity
events remain UNKNOWN: v2 retained no per-event reason. The eight old predicates:
outgoing multi-epoch RST; same-ISN SYN after data/close; data before every SYN;
data after closed; data overlapping another epoch's arc; incoming multi-epoch RST;
FIN without ACK with multiple epochs; ACK fitting two epochs. Tests reproduce
these mechanisms, not the unidentified real packet contents.

## v3 retained ledger and termination

RST records a terminal observation and leaves the byte ledger/attempts untouched.
New SYN retains old outstanding and ACKed history. No unknown bytes become ACKed.
Late ordinary ACKs may close old atoms when their generation is uniquely identified.
UnACKed bytes after RST remain outstanding/right-censored, including after 122s.
`rst_unacked_unique_bytes` and `replaced_unacked_unique_bytes` expose this coverage
limitation; first/last latency histograms still contain ACKed bytes only. A valid
ledger with censored bytes must not be presented as full delivery or an unbiased
latency sample. Conservation's definition has NOT changed.

RST with a single time-eligible observed epoch is retained there, including SEQ=0.
With tuple reuse, outgoing RST must fit one known downlink serial arc; inbound RST
requires an ACK field fitting one arc. That field selects provenance only, never
delivers data. Missing/multiple candidates invalidate with distinct reasons.
Unknown-tuple RST changes no observed data and is counted separately.
Event timestamps exclude epochs not yet born. A delayed old DATA observation can
use a uniquely disjoint old arc; an observation compatible with two epochs is
never assigned by guessing the newest. SYN same-ISN reuse and overlapping arcs
remain explicitly unsupported. FIN accounting still consumes one sequence byte.

Schema 3 adds eight specific `generation_ambiguity_*` counters: same_isn_syn,
before_syn, data_overlap, ack_overlap, ack_no_owner, rst_no_owner,
rst_multiple_owners, fin_without_ack. The new ack_no_owner case fixes v2's unsafe
fallback to the latest epoch for unmatched ACKs after tuple reuse.
Each ambiguity retains a closed reason/event enum, candidate count and the first
two synthetic epoch IDs, plus relative ordering against SYN. No packet identity,
absolute seq/ACK, header, address, port, payload or credential is serialized.
At most 256 such event records; overflow is explicit observation loss and invalid.

Each forced invalid atom has `loss_N_*` provenance: synthetic epoch ID, reason,
event type, relative lo/hi, length, age, time since SYN, HTTP existence/success,
retransmission flag, prior ACK presence, late covering ACK, observed FIN and
ordering versus reset/replacement. Every invalid atom is included; no sampling.
Reason enums 1..6: TTL, RST, epoch replacement, ambiguous generation, capacity,
other. Event enums 1..7: DATA, ACK, SYN, FIN, RST, TTL, capacity.
`invalidated_{ttl,rst,epoch_replacement,ambiguous_generation,capacity,other}_bytes`
must sum to `invalidated_unique_bytes`; unexplained bytes invalidate the run.
RST/epoch/ambiguity/capacity do not currently force deletion of known atoms, so
their byte counters normally remain zero; their EVENT failure counters still
invalidate when observation is lost. Tests exercise all loss-reason bookkeeping.
A late ACK of an invalid atom is evidence only; it cannot clear invalidity.
Previous schema-2 deployment validators must be updated before any future run.

## Lifetime and memory bounds

No wall-clock TTL applies to ranges, attempts, inactive epochs or ACK history.
All are retained for this short process/session, even after FIN/RST. Hard limits
remain 32768 atoms, 4096 generations, 65536 attempts, 262144 references and 65536
ACK events. No safe epoch-history deletion horizon can be inferred merely from
RST or idle time on this relay; retaining it avoids tuple-reuse contamination.
A missing/future ACK remains pending/invalid until its missing observation arrives;
age alone no longer destroys evidence. This is not an unbounded long-running service.

Only ephemeral buffer-identity tags retain the existing 120s TTL (4096 tags,
16MiB summed slice capacity). Losing a tag invalidates the run; affected open
atoms get explicit TTL provenance. This reflects lost instrumentation linkage,
not timeout/loss of a TCP packet. No transport queue/timeout is changed.

Worst-case logical structure payload at all caps is measured in Linux with
unsafe.Sizeof (test fails above 32MiB); the test includes loss evidence for every
atom. Slice slack/copies, maps, tags, JSON and snapshot dictionaries are additional.
Plan up to 192MiB observer overhead at pathological ALL-invalid caps: ~32MiB
structures + slack/transients, up to 16MiB tagged capacity, up to 32768 x 17
numeric loss fields and JSON encoding. This is a conservative planning estimate,
not a Go RSS guarantee. The inherited pointer-tag design can pin an underlying
allocation larger than a subslice's cap; hence no strict RSS bound is claimed.
That mechanism is unchanged. Linux retained-heap fixture prints actual 10k-range
cost; 1k is about one tenth for ordinary one-attempt records. 100k ranges are
rejected by the hard cap, never admitted. Real scale (2863 emitted segments,
~1.86MB unique) is well below range/attempt limits; churn is bounded separately
by retained generations, including closed ones.

## Validity and tests

Still invalid: capacity/provenance overflow; unresolved generation ambiguity;
unsupported serial span; unanchored DATA; pending unknown-ownership ACK;
lost identity/timestamp; forced unique-byte deletion; invariant or reason mismatch.
RST with deterministically retained accounting is not itself observation loss.
Sequence arithmetic and the <2^31-per-generation scope are unchanged.

Tests cover the 15 requested RST cases plus RST+ACK non-delivery and reordered
old DATA, all eight explicit ambiguity reasons, deferred ACK >120s, all six loss
reason sums/provenance, unchanged hard caps and atomic-range conservation.
RST stress uses two fixed seeds, 1024 tuples, 3072 generations, 6144 resets,
overlaps/retransmissions, reordered partial ACKs and >150s observation delay.
Latest replay: exactly 1864983 unique bytes, 592076 retransmitted unique bytes,
866 retransmission attempts, 4309 ACK observations, 178 resets and >130s age.
Supported fixtures require valid=1, zero evictions/sequence/capacity loss and all
unique bytes eventually ACKed. Unsupported ambiguity explicitly requires valid=0.
The original v2 deterministic, independent byte-ledger, stress and wire tests remain.

CI proves all networking/wire code, attachment points and runtime logger byte-
identical to 4e991a6 and the previously audited f31a49f. Only observer/tests/docs/
verification change. Go root/mobile test, race, vet, build run on Linux only.

---

# Historical v2 design (superseded lifecycle/TTL contract)

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
