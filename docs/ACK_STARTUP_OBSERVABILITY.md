# ACK diagnostic startup observability (schema 4)

This is diagnostic-only work based on `6a2c677fb5d049dbde232289128eb681148e019d`.
It is not authorization to deploy or to request a phone test. The earlier diagnostic's exit 1 remains **unclassified**. CAPTCHA failures of subsequently restored production processes do not prove the cause of that diagnostic exit.

## Exact startup audit at the base revision

| Evidence point | Source location at `6a2c677` |
|---|---|
| PROCESS_ENTRY | `main.go:30`, `main` |
| DIAGNOSTIC_LOG_INIT | `main.go:31–32`; `utils/ack_diag_log.go:10`, `ConfigureAckDiagnosticLogging`; `internal/ackdiag/runtime.go:15`, `Enable` |
| Existing safe banner | `main.go:34`, `fmt.Print("written by p1neappleXpress\\n")` |
| TRANSPORT_START_CALL | `main.go:131`, `trans.Start()` |
| AUTHORIZATION_ENTRY | `transport/yandex/vyandex.go:973–974`, `authorize`; implementation begins at 139 |
| AUTHORIZATION_SUCCESS_POINT | `transport/yandex/vyandex.go:346–348`, fixed auth-OK format immediately before `return a, nil` |
| RELAY_WORKERS_START | `transport/yandex/vyandex.go:981–982`; `relayClient.Start` at 474–480 launches the existing workers and reaches the fixed pool-started format |
| PROXY_CONSTRUCTION | `main.go:135`; `tunnel/tunnel.go:84`, `NewTCPTunnelMode`; proxy setup at 133 |
| SNAPSHOT_LOOP | `transport/yandex/vyandex.go:995` launches `statsLoop`; existing ticker at 1065 and `ackdiag.Emit` at 1077 |
| FIRST_SNAPSHOT_EMIT | `internal/ackdiag/runtime.go:27–32`, `Emit`, after the first existing 2-second ticker event |
| FATAL_STARTUP_EXIT | `main.go:132`, unchanged `log.Fatalf` after transport Start error; earlier config/read errors also retain original fatal exits |
| LOG_SUPPRESSION_PATH | `utils/ack_diag_log.go:14–16`, writer replaces every standard log message with a constant and preserves `log.Fatal` exit semantics |

The numeric Engine is initialized as a package global before main, enabled in main, and emitted only by the transport stats loop. Authorization failure returns before that loop is launched. The old redactor removed the failure class along with sensitive data, leaving only a safe banner/suppression messages and systemd exit status. Debug stage logs are normally disabled. This is the startup observability gap; it does not identify the historical exit cause.

## Contract revision

Schema 3 is a closed numeric snapshot contract. It has no compatible startup envelope; silently adding strings or an additional unversioned event would weaken that contract. Version **4** explicitly adds `[ACK-STARTUP]` JSON records. `[ACK-DIAG]` snapshots carry schema 4 at the log serialization boundary. Engine accounting/schema-3 fixtures remain byte-identical and are still independently tested under an explicitly schema-3 historical scope. Mixing versions within a current invocation fails.

Startup fields are exactly: `schema`, `event`, `ordinal`, `startup_stage`, `startup_result`, `failure_class`, `transport_started`, `authorization_completed`, `relay_workers_started`, `proxy_initialized`, `diagnostic_snapshot_loop_started`. All string values are closed enums; all status fields are booleans; ordinal is a contiguous bounded integer. No arbitrary error string is a field.

Stages: process_start, diagnostic_init, transport_start, authorization, relay_workers, proxy_init, snapshot_loop. Results: begin, ok, failure. Failure classes: auth_client_config_missing, auth_challenge_or_captcha_classified, auth_other, transport_start_other, relay_workers_failure, proxy_init_failure, diagnostic_init_failure, schema_emit_failure, unknown_startup_failure. Successful events use `none`.

`ClassifyTransportFailure` inspects the exact production `auth: ` error wrapper in memory. Only a missing-client-config error with a parsed HTTP(S) URL on yandex.ru or a subdomain and an explicit `/showcaptcha` or `/captcha` path is classified as challenge/CAPTCHA. Query/fragment, free text containing “captcha”, exit code, or an unrelated hostname do not prove a challenge. No parsed value or original error is retained or emitted.

## Observation without network changes

The entire transport, tunnel, network and mobile trees, dependencies, ACK attachment points and Engine range/lifecycle implementation remain byte-identical. Existing `utils.Debugf` call sites supply **only their compile-time format string** to a diagnostic observer even when verbose output is disabled. No arguments are forwarded. This allows precise existing authorization/pool/NIC checkpoints without editing those networking functions or enabling verbose logs. Diagnostic-mode verbose logging and sinks also use constant suppression. Non-diagnostic logging retains the existing output path.

Main adds observation calls before/after existing operations and before existing fatal exits. The original network operation ordering and fatal exit statements are preserved. Logger errors cannot cause retries, change an error return, exit the application or create a worker. A failed logger write attempts a closed failure event on a separate sink; if output is partial or both sinks fail, readiness remains blocked. It is impossible to guarantee classification when all output is lost.

`relayClient.Start` and the proxy constructor have **no error return** in this source. Their failure tests therefore inject observer-stage faults; no fictitious error/retry branch is added to production. The existing NIC-error log is classified without changing the constructor's existing continuation behavior. Unexpected panic/SIGKILL/OOM remains original runtime behavior; no panic recovery is added. Without a valid failure event, a grounded exited invocation is reported as `unknown_startup_failure` at its last observed stage. Arbitrary panic output still fails the strict log allowlist and is never exported.

The snapshot-loop success bit is emitted when the existing loop actually invokes `Emit`, not inferred from goroutine creation. Loop dispatch timing, ticker interval and goroutine count are unchanged.

## Validator and harness

The version-4 validator checks exact fields, enums, booleans, contiguous ordinals, monotonic observed state, reason/stage consistency, and failure terminality. Old numeric byte conservation, provenance, reason counters and secret-field rejection are reused unchanged. Unknown startup records, missing fields, inconsistent flags, mixed schemas, long-field null elision, non-text, malformed JSON and unknown application output remain hard failures. A deliberate failure event cannot excuse an unknown journal message later in the invocation.

The harness consumes the originally grounded invocation's journal **before** requiring the process still be active. A valid failure yields `DiagnosticStartupFailed`, `STARTUP_STAGE`, `STARTUP_FAILURE_CLASS`, and `READY=false`. If the process exits during settling, classification requires at least one coherent exact `/proc` exe/argv observation of the new invocation; failed stability proof never becomes readiness PASS. No identity is guessed from formatted command strings.

Fresh cursor, boot/monotonic bounds, PID/exe/InvocationID, root identity and `journalctl --all` checks are preserved. `argv_validator.py` is byte-identical. A future authorized deployment must supply explicit source/binary/archive pins; stale schema-3 binary pins cannot be used silently. No install/switch action is executed by this task.

## Tests and limits of proof

Linux CI exports startup fixtures with the real Go serializer, alongside frozen Engine fixtures. Tests cover success through first snapshot, generic transport failure, missing config, classified challenge, other auth errors, relay/proxy stage faults, diagnostic initialization, banner/snapshot sink failures, unknown errors, early unclassified exit, and long URL-like secret-bearing synthetic errors. A Go subprocess test retains original exit 1 and proves ordinary/debug error suppression. The actual `Emit` writer failure is tested.

Python integration uses real Linux child PID/exe/NUL-argv grounding and the same `operations.journal` / readiness path, with only the systemd/journal envelope and passage of idle time simulated. It tests every supported failure class plus malformed/secret fields, mixed schemas, previous invocation records, unavailable messages, and unknown text. No network is used by these tests. This does not establish that a real provider startup will succeed.

The source verifier reconstructs exact prior main/log code after removing the enumerated observation changes and checks frozen network/correlator files byte-for-byte. No Windows Go/OpenFlux executable is built or run.

## Passive production observation

2026-09-23 13:41:14.105368–13:56:14.121863 UTC, 900.016495 seconds, 31 samples. User2 NRestarts 129→280 (delta 151), 8 sampled MainPID transitions and 30 sampled invocation transitions; active/running in 6/31 samples. Sampling can miss short processes; these are not claims of exact process-start counts. Journal evidence independently records 151 CAPTCHA/client-config failures and 151 unsuccessful exit-1 cycles. Other auth failures and other failure classes: zero.

Main and user3 retain PID/invocation and zero restart delta, active/running in 31/31 samples, with no new auth failures. No kernel/OOM messages were observed. User2 has no stable recovered invocation; authorization/relay/proxy success is UNKNOWN. The original SHA-256 remains unchanged. Only read-only inspection ran; no service operations, production edits, deployment, phone test or synthetic traffic occurred. Temporary SSH access was removed and reuse rejected; the local private key was removed.

Therefore production stability independently blocks any startup reprobe, even if all local/CI observability tests pass. No deployment is authorized by this report.
