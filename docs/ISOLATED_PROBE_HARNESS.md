# One-shot invocation epoch (no production authorization)

The provider-facing binary stays pinned to schema-6 source
`34ba35a407296038a223c54cd048dcc0e6b67ca5`, artifact 10767828054, SHA256
`a61d4c4777d045346590d9fc8613b14500f4a7e0ed5cfbe2f5b8d8f660acd2e0`.
Only harness/tests/docs and CI wiring change. Frozen runtime, emitter, validator,
auth, parser, network and proxy files are byte-compared by CI.

## Historical failures and model

A: pre 541, post 541 was rejected by an old `current == 0` assertion.
B: pre 541, post 0 was rejected by the subsequent pre-start monotonic rule.
Neither run observed a bootstrap response. Both ended in cleanup SIGTERM.
Both pre/post transitions are supported by the new model. See
[the exact v255 lifecycle audit](SYSTEMD_V255_RESTART_EPOCH.md).

Primary invariant: exactly one consumed control start and exactly one new target
InvocationID after the durable cursor, with exactly one trusted systemd
UNIT_STARTED record. New invocation IDs are counted across ALL target-unit
application journal metadata, regardless of executable or MESSAGE; an unexpected
executable cannot hide another start. PID1 UNIT_STARTING/UNIT_STARTED records
provide additional coverage of short-lived invocations. Two start records also
fail closed when their optional INVOCATION_ID fields are absent.

`probe_invocation.InvocationEpoch` merges these records with systemctl
InvocationID, MainPID/ExecMainPID and ExecMainStartTimestampMonotonic. Re-reading
journal records does not double-count cursors. `read_ledger` verifies that the
original cursor still exists, uses --all, synchronizes existing journal records,
never substitutes --since/now, and fails if seen records disappear. It requests
metadata only: no MESSAGE, command line, environment or body. Unrelated records
and untrusted forged PID1 fields do not count as starts.

An additional InvocationID or second structured start event produces
ADDITIONAL_INVOCATION_OBSERVED, even if NRestarts is unchanged or reset. The
source may be automatic restart, an operator, refresh or another controller;
the detector does not guess causality. An external duplicate systemctl start
while already active creates no new invocation and is not a second invocation.
The harness itself is still forbidden to issue a duplicate command.

Historical pre-start NRestarts is evidence only. Once ExecMain PID/start time
establish the authorized invocation, persist POST_START_NRESTARTS_BASELINE.
Growth thereafter produces AUTOMATIC_RESTART_COUNTER_ADVANCED; a decrease
produces POST_START_NRESTARTS_EPOCH_INVALIDATED. Neither is silently rebased.
Scheduled restart metadata also fails closed. Counter equality alone never
proves one-shot.

## Durable control and identity

Boundary version 2 includes operation ID, boot/PID1 ticks/D-Bus owner, original
cursor and its monotonic time, UTC, target, historical PID/invocation/NRestarts,
expected executable, exact argv tokens and their NUL-separated SHA256 identity.
The argv are configured flags/profile FILE PATHS, never file contents.
Boundary version is not the schema-6 emitter version.

The operator-host controller validates, fsyncs, exclusively publishes and reads
back boundary.json before ACK. An immutable start_intent binds operation ID and
boundary digest with maximum_start_count=1. The worker consumes
CONTROL_START_CALL_COUNT=1 BEFORE invoking systemctl. Even a spawn failure,
transport ambiguity or nonzero control result consumes the attempt:
OPERATOR_CONTROL_FAILURE, no retry. No start exists in cleanup.

After invocation establishment, authorized_invocation.json is durably ACKed
with PID, start timestamp, invocation and new counter baseline. Exact /proc exe
and NUL argv verification bind scope.json using the original cursor. All these
documents are immutable and survive application/harness/cleanup failure.
The controller cannot resume an existing evidence directory.

Boot/context changes fail before or after start. PID1 ticks and D-Bus owner are
useful signals, not a universal manager generation counter. Unloading a unit
after its invocation disappears is context loss, not evidence of no restart.

## Error ownership and limits

APPLICATION_STARTUP_FAILURE, HARNESS_VALIDATION_FAILURE,
OPERATOR_CONTROL_FAILURE and CLEANUP_FAILURE remain separate. Missing schema
data stays NOT_OBSERVED; emitted false is only a state observation.
Owner is APPLICATION for normal exit, HARNESS_CLEANUP for an owned stop followed
by SIGTERM/SIGKILL, UNKNOWN for a signal without attributable control evidence.
EXTERNAL requires explicit external control evidence, not just a signal.
Natural exit racing cleanup remains APPLICATION. systemd Result=success after
stop is not bootstrap success.

Full live /proc grounding may be unavailable for a very short-lived process;
that blocks readiness even if invocation metadata was recovered. The model
detects supported lifecycle events with retained journald evidence; it is not
proof against malicious root erasing an entire unseen invocation. Detected
journal loss/truncation blocks readiness. Context checks and final journal sync
supplement polling, not cryptographic causality for external identical actions
racing the start. Windows durable receipts protect against worker/controller
exceptions, not arbitrary storage hardware failure.

## Validation and future use

Mocks execute actual isolated_probe.run with both historical transitions,
durable receipts and unchanged Go-generated schema-6 fixtures. Adversarial
cases cover counters, a second invocation with unchanged counter, unrelated
records, cursor/context loss, cleanup and single-consumption control failure.

Real tests run only on an ephemeral GitHub-hosted Ubuntu 24.04 VM using a unique
codex-oneshot-test service and synthetic local Python fixture. They never invoke
OpenFlux, a provider, production SSH or credentials. Manager reexec/reboot are
source-audited only. CI also runs unchanged root/mobile Go regressions.

Future preparation must stage probe_invocation.py with isolated_probe.py,
probe_boundary.py and probe_controller.py. Old diagnostic/validator pins remain.
A real retry requires separate authorization and a fresh read-only hold check.
