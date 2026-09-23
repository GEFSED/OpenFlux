# Isolated one-shot probe harness repair (no deployment authorization)

Frozen diagnostic/emitter/validator source: `34ba35a407296038a223c54cd048dcc0e6b67ca5`.
Only harness, tests, CI and this document change. The CI source proof compares
every other tracked file byte-for-byte with that commit. Schema remains **6**.
The diagnostic artifact pin in the worker remains the previously validated
artifact, not the new CI build. Network, auth, parser and logging are unchanged.

## Exact historical defect

Archived file: `D:/openflux/user3-isolated-schema6-20260923/probe.py`.
Function `run`, line 229: `require(state['NRestarts']=='0','automatic_restart_detected')`.
The same absolute-zero assumption appeared at lines 263, 272 and 317.
Historical `541 -> 541` was incorrectly treated as a restart. Cleanup sent
SIGTERM after only process_start/diagnostic_init events. There is no provider
response evidence from that invocation. The old script is evidence only and
must not be reused. The replacement is `scripts/ack-readiness/isolated_probe.py`.
The older user2 ACK deployment tool `operations.py` is not this one-shot worker
and must not be substituted for it.

## Restart and manager semantics

Immediately before start, capture baseline NRestarts after installing the
separately authorized override and completing daemon-reload. Require current >=
baseline and delta == 0 at every observation and after cleanup. Growth is
`AUTOMATIC_RESTART_OBSERVED`. Regression is `NRESTARTS_BASELINE_INVALIDATED`;
never rebase automatically or run reset-failed. Examples: 0->0, 541->541,
1000->1000 and 99999->99999 pass; 541->542 and 541->0 fail.

Upstream systemd v255 evidence (the host reports 255.4):

- [service.c](https://github.com/systemd/systemd/blob/v255/src/core/service.c):
  service_enter_restart increments n_restarts for enqueued automatic-restart
  jobs; service_reset_failed resets it. service_enter_dead may defer a reset
  using flush_n_restarts until the next service_start. An explicit start is
  therefore not a guarantee of either zero or retention in all states.
  service_serialize/service_deserialize_item preserve n-restarts and its flush
  flag. A full unit object unload loses that object's in-memory counter.
- [systemctl.xml](https://github.com/systemd/systemd/blob/v255/man/systemctl.xml):
  reset-failed resets service restart counters. daemon-reexec serializes and
  deserializes state; it must not be described as unconditionally resetting it.
  A reboot creates a new boot context. No reset/reexec/reboot is performed here.

Guard boot_id, PID 1 /proc start ticks and the unique D-Bus owner of
org.freedesktop.systemd1. A changed signal fails closed. These are not a universal
manager generation identifier: PID 1 start time survives exec and a reload can
preserve both signals. No mechanism here excuses a lower counter. Exact target
invocation/PID/start ticks, /proc exe and NUL-separated argv checks are retained.
The short window is not claimed to detect a malicious reset-and-increment
between samples; that would require continuous manager event auditing.

## Durable boundary and local controller

`probe_controller.py` runs on the operator machine with a **new local task
evidence directory**. The remote worker emits a closed `boundary` record, then
blocks for a matching SHA256 receipt. The controller validates and writes
boundary.json, flushes/fsyncs, atomically publishes it exclusively, and reads it
back before ACK. Linux also fsyncs the directory. Windows flushes both staged
and published file; this guarantees survival of worker/controller exceptions,
not arbitrary storage hardware failure. The remote /tmp mirror is secondary.

The boundary includes boot/manager context, exact original fresh cursor,
pre-start UTC and monotonic time, unit, baseline counter/PID/invocation, expected
executable, argc and SHA256 of length-delimited-by-NUL argv tokens. No secret
file is read, and raw argv values are not in the boundary.

An immutable local start_intent receipt and remote exclusive marker precede
the sole start call. Missing cursor, fsync failure, disconnect, receipt mismatch
or reused directory prevents a start. There is no automatic resume/retry.
After coherent exact process grounding, scope.json is separately persisted and
ACKed; it contains the same original cursor plus PID/invocation. Assertions never
replace the boundary with a guessed timestamp or a newer cursor.

The controller does not deploy anything. Future authorized preparation must
stage the exact pinned artifact, validators, hold baseline and these harness
modules. Direct old `run` mode is removed; the worker requires
`run-with-durable-controller`. No controller command is executed in this task.

## Error ownership and cleanup

APPLICATION_STARTUP_FAILURE, HARNESS_VALIDATION_FAILURE,
OPERATOR_CONTROL_FAILURE and CLEANUP_FAILURE are separate. Safe failure enums
never contain raw exception strings. A harness failure leaves application
failure class NOT_OBSERVED. Missing schema metadata is not replaced with zero
or false. Schema-emitted false is retained only as an actual observation.

Cleanup records its stop intent for the exact owned invocation before stopping.
An observed SIGTERM/SIGKILL for that invocation following the stop request is
attributed to HARNESS_CLEANUP; a natural exit racing stop is APPLICATION_EXIT.
Unrelated signals have EXTERNAL_OR_UNKNOWN_SIGNAL ownership. Exit status and
signal are separate. Historical aborted case: HARNESS_CLEANUP, SIGTERM,
BOOTSTRAP_COMPLETION_STATE=NOT_OBSERVED. systemd Result=success after a stop
does not mean bootstrap success. Signal attribution has ordinary observation
limits: a simultaneous unrelated identical signal cannot be causally separated.

The worker preserves primary error evidence if cleanup also fails. Cleanup
removes only the additional diagnostic override and inactive binary, never the
five hold guards. Successful processes may be retained only after the required
same-PID observation; Restart=no remains. No recovery start exists in cleanup.

## Linux proof

The tests execute actual worker run() using offline systemctl/proc adapters,
synthetic identities, fake time and real schema-6 Go-emitted fixtures. The
complete 541->541 flow emits startup/HTTP/structure records, exits once,
validates/classifies application failure and restores configuration. The
541->542 flow fails as AUTOMATIC_RESTART_OBSERVED. Other cases cover write
failure, original cursor survival, another invocation, old journal data,
counter regression, changed boot/manager, early exit, unrelated SIGTERM,
cleanup SIGTERM and a retained successful process. No provider traffic or
production systemd access occurs in these tests.

CI runs Python discovery after generating exact Go fixtures, plus root/mobile
go test, race, vet, build and git diff --check. CI does not run the SSH controller.

Any real retry still requires separate explicit authorization and a fresh
read-only hold/boot/SHA check. No new probe is authorized by this document.
