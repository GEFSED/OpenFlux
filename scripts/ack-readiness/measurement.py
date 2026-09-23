"""Schema-3 numeric readiness/capture only. No packet or network behavior."""
from schema3 import LIMITS, VALID_ZERO, HISTOGRAMS, BUCKETS
from schema5 import validate_numeric as validate_snapshot, startup_outcome


def valid(v):
    validate_snapshot(v)
    return (v['correlation_valid'] == 1 and v['counter_consistency'] == 1
            and all(v[k] == 0 for k in VALID_ZERO))


def idle_proof(snapshots):
    if not snapshots:
        raise RuntimeError('missing_idle_snapshots')
    for record in snapshots:
        v = record['values']
        validate_snapshot(v)
        if not valid(v):
            raise RuntimeError('invalid_idle_correlation')
        # Zero data baseline allows exact final TCP totals and latency metrics;
        # no before-test application bytes can contaminate the one phone run.
        if v['unique_downlink_tcp_bytes'] != 0 or v['outstanding_unique_bytes_current'] != 0:
            raise RuntimeError('phone_traffic_before_ready')
    return dict(CORRELATION_VALID=1, CORRELATOR_EVICTIONS=0, CAPACITY_EVICTIONS=0,
                SEQUENCE_CONSTRAINT_VIOLATIONS=0, CONSERVATION='PASS',
                UNEXPLAINED_INVALIDATED_BYTES=0, LIMITS=LIMITS)


def readiness_observation(records, elapsed, process_exited=False, schema=3):
    """Shared deployment/local-simulation gate, after process and log checks."""
    startup=None
    if schema in (4,5):startup=startup_outcome(records,process_exited)
    snapshots = [r for r in records if 'values' in r]
    if not snapshots:
        return None
    proof = idle_proof(snapshots)
    if elapsed < 15 or len(snapshots) < 7:
        return None
    if snapshots[-1]['monotonic_us'] - snapshots[0]['monotonic_us'] < 14000000:
        return None
    v = snapshots[-1]['values']
    if v['ws_connect_failures'] or v['ws_reconnects']:
        raise RuntimeError('carrier_startup_error')
    if v['http_inflight_requests'] or not v['ws_connected']:
        return None
    if not any(r.get('event') == 'banner' for r in records):
        raise RuntimeError('startup_banner_missing')
    if schema in (4,5) and (not startup or not all(startup[k] for k in (
            'transport_started','authorization_completed','relay_workers_started',
            'proxy_initialized','diagnostic_snapshot_loop_started'))):
        return None
    return dict(LOG_SCHEMA=schema, LOG_VALIDATION='PASS', UNKNOWN_DIAGNOSTIC_LOGS=0,
                NON_TEXT_DIAGNOSTIC_LOGS=0, SECRET_LEAKAGE='NONE_OBSERVED',
                VOLGA_CONNECTED=True, IDLE_PROOF=proof)


def capture_result(ready, records, state, received, captured):
    base = ready['baseline']
    snapshots = [r for r in records if 'values' in r and r['time_us'] >= base['time_us']]
    if not snapshots or snapshots[0]['time_us'] != base['time_us']:
        raise RuntimeError('baseline_missing_from_fresh_journal')
    if snapshots[0]['values'] != base['values']:
        raise RuntimeError('baseline_changed')
    last = snapshots[-1]
    b, v = base['values'], last['values']
    consistent = (v['unique_downlink_tcp_bytes'] == v['acked_unique_downlink_tcp_bytes']
                  + v['outstanding_unique_bytes_current'] + v['invalidated_unique_bytes'])
    consistent = consistent and (v['outstanding_unique_bytes_current'] == sum(v[k] for k in (
        'http_not_started_bytes', 'http_inflight_bytes', 'http_success_not_acked_bytes', 'http_failed_not_acked_bytes')))
    counters = ('http_requests', 'http_successes', 'http_failures', 'http_status_429',
                'ws_messages', 'ws_reconnects', 'ws_connect_failures', 'http_body_read_errors_ignored')
    deltas = {key: v[key] - b[key] for key in counters}
    if any(n < 0 for n in deltas.values()):
        raise RuntimeError('counter_reset')
    latency = {}
    for prefix in HISTOGRAMS:
        count = v[prefix + '_count'] - b[prefix + '_count']
        total = v[prefix + '_sum_ns'] - b[prefix + '_sum_ns']
        buckets = {k: v[prefix + '_' + k] - b[prefix + '_' + k] for k in BUCKETS}
        # Range-weighted correlator histograms can change when a retained range
        # is split. The required zero-TCP idle baseline makes their subtraction exact.
        if count < 0 or total < 0 or any(n < 0 for n in buckets.values()) or sum(buckets.values()) != count:
            raise RuntimeError('histogram_window_inconsistent')
        latency[prefix] = dict(count=count, sum_ns=total,
            mean_ns=(total / count if count else None), max_ns=v[prefix + '_max_ns'],
            max_scope=('measurement' if b[prefix + '_count'] == 0 else 'invocation_upper_bound'),
            bytes=v[prefix + '_bytes'] - b[prefix + '_bytes'], histogram=buckets,
            p50=None, p95=None, p99=None, percentiles_supported=False)
    return dict(USER_DONE_RECEIVED_UTC=received, CAPTURE_UTC=captured,
        SNAPSHOT_START_US=base['time_us'], SNAPSHOT_END_US=last['time_us'],
        BASELINE=base, FINAL=last, DELTAS=deltas, LATENCIES=latency,
        HTTP_SUCCESS_NOT_ACKED_PEAK_SAMPLED=max(r['values']['http_success_not_acked_bytes'] for r in snapshots),
        CORRELATION_VALID=consistent and all(valid(r['values']) for r in snapshots),
        COUNTER_CONSISTENCY_RECOMPUTED=consistent,
        USER2=state, RECORDS=records)
