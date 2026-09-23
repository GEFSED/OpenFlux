"""Closed numeric contract for 01407ff schema 3; never serialize rejected input."""
import json
from pathlib import Path

BASE_FIELDS = frozenset(json.loads(Path(__file__).with_name('schema.json').read_text()))
REASONS = ('ttl', 'rst', 'epoch_replacement', 'ambiguous_generation', 'capacity', 'other')
AMBIGUITIES = ('same_isn_syn', 'before_syn', 'data_overlap', 'ack_overlap',
               'ack_no_owner', 'rst_no_owner', 'rst_multiple_owners', 'fin_without_ack')
EXTRA_FIELDS = frozenset('''range_ttl_ns generation_ttl_ns attempt_ttl_ns ack_history_ttl_ns
identity_tag_ttl_ns rst_unknown_flow_events rst_attributed_events expired_identity_tags
provenance_capacity_drops rst_unacked_unique_bytes acked_after_rst_bytes
replaced_unacked_unique_bytes delivery_observation_complete invalidation_records
invalidation_reason_consistency unexplained_invalidated_bytes generation_evidence_count
generation_evidence_cap'''.split())
FIELDS = BASE_FIELDS | EXTRA_FIELDS | {'invalidated_' + x + '_bytes' for x in REASONS} | {
    'generation_ambiguity_' + x for x in AMBIGUITIES}
LOSS_FIELDS = '''generation reason event length relative_lo relative_hi age_ns since_syn_ns
http_existed http_succeeded retransmitted prior_ack_observed late_covering_ack_observed
rst_before_loss fin_observed_at_loss epoch_before_loss'''.split()
LOSS_BITS = LOSS_FIELDS[8:]
EVENT_FIELDS = 'reason event first_id second_id candidates since_first_syn_ns before_first_syn'.split()
HISTOGRAMS = ('tunnel_first_out_to_ack', 'tunnel_last_out_to_ack',
              'http_first_start_to_ack', 'http_last_start_to_ack',
              'http_first_success_end_to_ack', 'http_last_success_end_to_ack',
              'ws_to_ack_decode', 'ack_decode_to_inject', 'http_duration',
              'tunnel_to_enqueue', 'enqueue_to_http_start')
BUCKETS = ('lt_50ms', '50_100ms', '100_250ms', '250_500ms', '500ms_1s',
           '1s_2s', '2_5s', '5_10s', 'ge_10s')
LIMITS = dict(ttl_ns=120000000000, records_cap=32768, flows_cap=4096,
              attempts_cap=65536, references_cap=262144, ack_history_cap=65536,
              tags_cap=4096, retained_tag_capacity_cap=16777216,
              range_ttl_ns=0, generation_ttl_ns=0, attempt_ttl_ns=0,
              ack_history_ttl_ns=0, identity_tag_ttl_ns=120000000000,
              generation_evidence_cap=256)
VALID_ZERO = '''invalidated_unique_bytes correlator_evictions diagnostic_capacity_drops
sequence_constraint_violations ambiguous_flow_generation unanchored_flows
unresolved_ack_events expired_observer_ack_events pending_observer_ack_events
ack_without_http_start_bytes ws_timestamp_missing invariant_failures
provenance_capacity_drops unsupported_serial_range unexplained_invalidated_bytes'''.split()


class SchemaError(ValueError):
    """Codes are constants only. Never include a field name or supplied value."""


def require(condition, code):
    if not condition:
        raise SchemaError(code)


def validate_snapshot(v):
    require(type(v) is dict, 'snapshot_not_object')
    require(type(v.get('schema')) is int and v['schema'] == 3, 'unsupported_schema_version')
    require(FIELDS <= v.keys(), 'required_field_missing')
    require(all(type(x) is int and 0 <= x < 1 << 64 for x in v.values()), 'invalid_numeric_field')
    require(all(v[k] == n for k, n in LIMITS.items()), 'schema3_limits_mismatch')
    losses, events = v['invalidation_records'], v['generation_evidence_count']
    require(losses <= LIMITS['records_cap'] and events <= 256, 'provenance_cap_exceeded')
    expected = set(FIELDS)
    for i in range(losses):
        expected.update('loss_%d_%s' % (i, k) for k in LOSS_FIELDS)
    for i in range(events):
        expected.update('generation_event_%d_%s' % (i, k) for k in EVENT_FIELDS)
    # Closed allowlist rejects addresses, ports, raw seq/ACK, URL, credentials,
    # bodies and any other new field. Do not echo the unknown key or its value.
    require(v.keys() == expected, 'unknown_or_missing_provenance_field')
    for k in ('correlation_valid', 'counter_consistency', 'ws_connected',
              'delivery_observation_complete', 'invalidation_reason_consistency'):
        require(v[k] in (0, 1), 'invalid_boolean_counter')
    unique, acked, outstanding, invalid = (v[k] for k in (
        'unique_downlink_tcp_bytes', 'acked_unique_downlink_tcp_bytes',
        'outstanding_unique_bytes_current', 'invalidated_unique_bytes'))
    require(unique == acked + outstanding + invalid, 'byte_conservation_failed')
    require(outstanding == sum(v[k] for k in ('http_not_started_bytes', 'http_inflight_bytes',
        'http_success_not_acked_bytes', 'http_failed_not_acked_bytes')), 'http_partition_failed')
    totals = [v['invalidated_' + r + '_bytes'] for r in REASONS]
    require(sum(totals) == invalid and v['invalidation_reason_consistency'] == 1
            and v['unexplained_invalidated_bytes'] == 0, 'reason_conservation_failed')
    require(v['delivery_observation_complete'] == int(outstanding == 0 and invalid == 0),
            'delivery_complete_mismatch')
    require(v['rst_unacked_unique_bytes'] <= outstanding and v['replaced_unacked_unique_bytes'] <= outstanding
            and v['acked_after_rst_bytes'] <= acked, 'lifecycle_byte_bound')
    for current, cap in (('records_current', 'records_cap'), ('flows_current', 'flows_cap'),
                         ('attempts_current', 'attempts_cap'), ('references_current', 'references_cap'),
                         ('ack_history_current', 'ack_history_cap'), ('tags_current', 'tags_cap'),
                         ('retained_tag_capacity_bytes', 'retained_tag_capacity_cap')):
        require(v[current] <= v[cap], 'retention_cap_exceeded')
    if v['correlation_valid']:
        require(v['counter_consistency'] == 1 and all(v[k] == 0 for k in VALID_ZERO), 'false_validity_claim')
    for prefix in HISTOGRAMS:
        count = v[prefix + '_count']
        require(count == sum(v[prefix + '_' + b] for b in BUCKETS), 'invalid_histogram_structure')
        require(v[prefix + '_max_ns'] <= v[prefix + '_sum_ns'] and
                (count != 0 or all(v[prefix + '_' + b] == 0 for b in ('sum_ns', 'max_ns', 'bytes'))),
                'invalid_histogram_structure')
    require(v['http_requests'] == v['http_successes'] + v['http_failures'] + v['http_inflight_requests']
            and v['http_duration_count'] == v['http_successes'] + v['http_failures']
            and v['http_status_429'] <= v['http_failures'], 'http_accounting_failed')
    reason_lengths = [0] * 6
    intervals = {}
    for i in range(losses):
        r = {k: v['loss_%d_%s' % (i, k)] for k in LOSS_FIELDS}
        require(1 <= r['generation'] <= v['flows_current'], 'invalid_generation_id')
        require(1 <= r['reason'] <= 6 and 1 <= r['event'] <= 7, 'invalid_provenance_enum')
        require(1 <= r['relative_lo'] < r['relative_hi'] < 1 << 31
                and r['relative_hi'] - r['relative_lo'] == r['length'], 'invalid_relative_range')
        require(all(r[k] in (0, 1) for k in LOSS_BITS), 'invalid_provenance_boolean')
        require(r['http_succeeded'] <= r['http_existed'], 'impossible_http_evidence')
        require(r['age_ns'] <= r['since_syn_ns'], 'impossible_loss_age')
        reason_lengths[r['reason'] - 1] += r['length']
        intervals.setdefault(r['generation'], []).append((r['relative_lo'], r['relative_hi']))
    require(reason_lengths == totals, 'loss_evidence_conservation_failed')
    for ranges in intervals.values():
        ranges.sort()
        require(all(a[1] <= b[0] for a, b in zip(ranges, ranges[1:])), 'overlapping_loss_evidence')
    ambiguity_counts = [v['generation_ambiguity_' + r] for r in AMBIGUITIES]
    total = sum(ambiguity_counts)
    require(total == v['ambiguous_flow_generation'] and events == min(total, 256)
            and events + v['provenance_capacity_drops'] == total, 'generation_evidence_accounting_failed')
    observed = [0] * 8
    for i in range(events):
        r = {k: v['generation_event_%d_%s' % (i, k)] for k in EVENT_FIELDS}
        require(1 <= r['reason'] <= 8 and 1 <= r['event'] <= 7, 'invalid_generation_enum')
        require(1 <= r['first_id'] <= v['flows_current'] and 1 <= r['candidates'] <= v['flows_current'],
                'invalid_generation_id')
        require((r['candidates'] == 1 and r['second_id'] == 0) or (r['candidates'] > 1 and
                1 <= r['second_id'] <= v['flows_current'] and r['second_id'] != r['first_id']), 'invalid_second_generation')
        require(r['before_first_syn'] in (0, 1) and
                (not r['before_first_syn'] or r['since_first_syn_ns'] == 0), 'invalid_generation_ordering')
        observed[r['reason'] - 1] += 1
    require(all(a <= b for a, b in zip(observed, ambiguity_counts)), 'generation_reason_accounting_failed')
    return v
