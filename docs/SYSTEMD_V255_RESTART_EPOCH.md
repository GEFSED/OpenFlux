# systemd v255 restart-counter lifecycle audit

NRestarts counts automatic restart jobs, not all starts or all invocations.
The counter is in-memory Service state, not a monotonic boot-wide sequence.

Primary sources (tag v255):

- [service.c](https://github.com/systemd/systemd/blob/v255/src/core/service.c):
  service_enter_dead (1770-1840), service_enter_restart (around 2301),
  service_start (2503-2506), service_stop (2520-2557), service_serialize
  (2685-2686), service_deserialize_item (3049-3064), service_reset_failed (4335).
- [unit.c](https://github.com/systemd/systemd/blob/v255/src/core/unit.c):
  unit_new allocates zeroed state; unit_acquire_invocation_id assigns activation
  identity; unit_set_invocation_id updates the manager lookup.
- [systemctl.xml](https://github.com/systemd/systemd/blob/v255/man/systemctl.xml):
  reset-failed resets counters; daemon-reexec serializes/deserializes state.
- [job.c](https://github.com/systemd/systemd/blob/v255/src/core/job.c) and
  [sd-messages.h](https://github.com/systemd/systemd/blob/v255/src/systemd/sd-messages.h):
  structured UNIT_STARTING/UNIT_STARTED events distinguish starts from text.

| Operation/state | Counter semantics |
|---|---|
| Automatic restart job enqueued | Increment; clear deferred-flush flag. |
| Fully stopped/failed with no restart planned | service_enter_dead sets flush_n_restarts, retains count for introspection. |
| Next explicit start with flush flag | service_start resets count to zero and clears flag. |
| Start while already active | No new invocation; no reset from a no-op command. |
| Stop while active | No unconditional immediate reset; shutdown can arm deferred flush. |
| Stop in auto-restart/queued state | Special branch cancels restart and enters dead state; does not itself execute the same deferred-flush assignment. Retention is possible. |
| Explicit restart | Stop + new activation; count may reset through deferred flush. |
| reset-failed | Direct reset of count and flush flag, including while running. |
| daemon-reload / drop-in add or remove | No dedicated reset; serialization preserves count/flag for retained objects. GC/recreation is a separate cause of state loss. |
| Unit unload followed by fresh allocation | New object has no old in-memory history. |
| Manager reexec | n-restarts and flush-n-restarts are serialized/deserialized; not an unconditional reset. |
| Reboot | New boot/manager/unit state; old observation epoch invalid. |

Best-supported historical explanation: the earlier harness's intentional stop
of a running diagnostic entered the no-auto-restart dead path under Restart=no,
arming flush_n_restarts. The next explicit start consumed that flag and reset
541 to 0. This fits unchanged boot/PID1/D-Bus owner and zero scheduled restarts.
The reset mechanism is source-proven and reproduced; confidence MEDIUM for
the exact historical cause because the internal flag was not recorded. Stop during auto-restart
can also explain why another earlier explicit start retained 541.

The disposable v255 integration records actual before/after values for
stop/start, failed/start, explicit restart, reset-failed, drop-ins, daemon-reload
and unit removal/reload. It tests the real detector against automatic, external
and refresh-like restarts and a no-op duplicate start. CI archives
real-systemd-evidence.json. Reexec/reboot are source-audited, not applied to
the runner's host manager. No production/OpenFlux unit is used as a fixture.
