# Schema 3 readiness validation (local/Linux CI only)

Frozen correlator: `01407ff3c86fc616f49d99ecf4a2942a37a3a861`.
Existing verified binary: `eaaff9cdb021936f7d93189170175658e7c5b2558dc30b7ae47027321870424c`.
Production base: `081d214300c1067f17f6c0d02f84a8491f1a7b98`.

No production deployment or phone measurement is part of this change. No
networking, correlator, timing hook, application logger or Android source changes.
The fixture exporter is a Go `_test.go` file, excluded from application builds.

## Audit of the previously used harness

The actual historical files are preserved at `D:/openflux/ack-final-schema2/`.
They were external deployment scripts, not previously in this Git branch.
This change brings the adapted harness under `scripts/ack-readiness/`.

| Requested location | Historical file/function/line | Current file/function |
|---|---|---|
| VALIDATOR_ENTRYPOINT | operations.py:187 journal; :250 switch | operations.py journal/switch |
| SCHEMA_DISPATCH | journal_validator.py:74 allowed_message | journal_validator.py allowed_message -> schema3.validate_snapshot |
| SCHEMA_VERSION_CHECK | journal_validator.py:103 | schema3.py validate_snapshot |
| JOURNAL_READ | operations.py:187 journal | operations.py journal |
| LONG_MESSAGE_HANDLING | journal_validator.py:118 journal_command, :12 cap | journal_validator.py journal_command / MAX_MESSAGE_BYTES |
| NULL_MESSAGE_HANDLING | journal_validator.py:74 allowed_message | journal_validator.py allowed_message |
| REQUIRED_FIELD_VALIDATION | journal_validator.py:100 | schema3.py validate_snapshot |
| UNKNOWN_FIELD_POLICY | journal_validator.py:100 exact field set | schema3.py exact fixed + indexed field set |
| NUMERIC_FIELD_VALIDATION | journal_validator.py:101 | schema3.py exact int, uint64, no bool/string coercion |
| COUNTER_CONSISTENCY_CHECK | measurement.py:38 capture_result; journal histogram checks | schema3.py validate_snapshot; measurement.py capture_result |
| SECRET_LEAK_CHECK | journal_validator.py:74 closed MESSAGE allowlist | journal_validator.py + schema3.py closed key/value contract |
| FRESH_CURSOR_CHECK | operations.py:202; journal_validator.py:126 | same named functions, strict cursor duplication/order checks added |
| INVOCATION_SCOPE_CHECK | journal_validator.py:126 validate_records | same function, current PID/exe/unit/InvocationID/boot/time |
| ARGV_CHECK | argv_validator.py:28 parse_cmdline, :145 verify_sample | same, unchanged |
| PID_STABILITY_CHECK | argv_validator.py:156 await_stable | same, unchanged |

Previous schema-2 assumptions incompatible with schema 3:

* Exact 221-key schema-2 set and `schema == 2` reject new fixed/indexed fields.
* Fixed 16 KiB MESSAGE cap rejects legitimate retained provenance. The existing
  `journalctl --all` fix is preserved; null remains unavailable evidence, never
  proof that the application wrote non-text data.
* Readiness reported schema 2 and knew only four invalidity counters. It did not
  independently reconcile invalidation reasons, loss records or generation evidence.
* No generation evidence cap/overflow, ordering, relative range or reason-enum checks.
* Duplicate journal cursors and reordered application records were not rejected.
* The old capture script checked conservation, but the log contract did not.

## Contract and failure policy

`schema.json` retains the common schema-2 field-name inventory only; it is not
a schema-2 acceptance path. `schema3.py` extends it with the exact schema-3 fields.
All snapshot values must be unsigned uint64 integers (Python booleans rejected).
No nested values, unknown fields, strings or raw packet/credential fields are
accepted. Rejection reports contain only constant reason codes and safe journal
metadata/properties, never rejected content, keys, fragments or hashes.

Byte conservation and HTTP outstanding partition are recomputed. All six loss
reason totals must equal invalidated unique bytes. Indexed loss records must be
contiguous, complete, non-overlapping per generation, match their reason totals,
and use positive synthetic generation IDs and relative half-space coordinates.
Length equals relative end minus relative start; age cannot exceed time since SYN.
HTTP success requires HTTP existence. Ordering/presence flags are strictly 0/1;
raw absolute timestamps/seq/ACK/identities are not added. The validator cannot
independently reconstruct packet event ordering omitted from the safe schema.

Loss reasons 1..6: TTL, RST, epoch replacement, ambiguous generation, capacity,
other. Event kinds 1..7: DATA, ACK, SYN, FIN, RST, TTL, capacity. Generation
reasons 1..8 follow the frozen `ambiguityNames` list in lifecycle.go.

Generation evidence is capped at **256**. All eight reason totals must equal the
ambiguity counter. Retained evidence count is min(total,256); retained + explicit
provenance drops equals total. Index count above 256 is malformed even if a run
claims invalidity. Overflow/drops cannot coexist with `correlation_valid=1`.

Well-formed correlation-invalid snapshots remain collectable as invalid evidence.
Malformed structures or false validity claims are rejected. Readiness requires
every idle snapshot to be correlation-valid, conserved, loss-free and within
the frozen caps. RST events alone do not invalidate readiness. Outstanding RST
and late ACK states are structurally valid; `delivery_observation_complete=0`
does not mean the ledger is invalid. A zero-data idle baseline is still required
so subsequent phone-window counters are attributable to that window.

The 64 MiB MESSAGE bound covers the worst compact schema: 32768 loss records x
16 keys (keys under 64 bytes + 20-digit uint64), 256 generation records and fixed
fields. Journald may impose lower storage limits. Missing/truncated/elided data
blocks readiness; increasing this validator bound cannot recover lost journal data.

## Journal and process guarantees

The actual operations harness calls `validate_journal_json`, then the same
`readiness_observation` tested in CI. It keeps `--all`, a pre-start cursor,
boot/monotonic boundary, exact current invocation/PID/exe/unit and root UID/GID.
Systemd-manager records and previous/unrelated process records are classified
separately. A current-PID invocation mismatch is rejected, not silently ignored.
Malformed journal JSON is rejected without propagating parser input. Duplicate
application cursors and decreasing monotonic record times are rejected. Equal
idle snapshots at different legitimate cursors/times are allowed.

The argv module is copied unchanged: `/proc/PID/exe`, starttime before/after
cmdline read, NUL-token comparison, only one terminal empty token removed,
new invocation and stable PID/starttime for 300 ms, bounded 5 s wait. No shell
string, path, quote or argument-order normalization.

## Linux verification

CI exports genuine `Engine.Snapshot()` fixtures using
`internal/ackdiag/schema3_fixture_test.go`. Fixtures include idle, ACK progress,
retransmission, RST retained/late ACK, replacement/reuse, all six loss reasons,
all eight ambiguity reasons, explicit provenance overflow, >16 KiB loss evidence,
and real in-process transport-hook HTTP accounting. They contain synthetic data
only and are generated in runner temporary storage, not handwritten schema mocks.

Negative mutations cover missing/unknown/raw fields, bad numeric types/values,
conservation, enum/ID/range/boolean/order failures and false validity claims.
Existing argv and journal regression tests remain, adapted to real schema-3 fixtures.

End-to-end tests launch a local Linux Python emitter using the real Go-produced
JSON, validate its real `/proc` exe/argv/starttime stability, then feed a simulated
systemd/journal envelope through **operations.journal**, idle readiness and final
capture. The journal timestamps/15-second observation span are simulated; no
systemd unit or VPS is involved. RST/late-ACK provenance and clean shutdown are
checked. An intentionally malformed invocation fails in the identical path.

CI also runs root/mobile test, race, vet, build and diff/source checks. The source
verifier compares every frozen correlator/network hook/logger byte to 01407ff.
New CI binaries have a new embedded commit; future deployment harness pins the
existing **01407ff artifact**, not an arbitrary rebuild. A separately authorized
deployment must supply its verified ZIP SHA through `ACK_ARTIFACT_ZIP_SHA256`.
This task does not install or execute any diagnostic binary on a VPS.
