package yandex

import (
	"errors"
	"net"
	"sync/atomic"
	"time"

	"universal-bypass-tool/utils"
)

// Observability only. These atomics do not control request admission or retries.
type relayDiagnostics struct {
	requests, successes, failures atomic.Uint64
	network, timeout, otherStatus atomic.Uint64
	http4xx, http5xx atomic.Uint64
	status400, status401, status403, status404, status409, status429 atomic.Uint64
	bodyAttempted, bodySuccess, bodyFailed atomic.Uint64
	payloadAttempted, payloadSuccess, payloadFailed, payload429 atomic.Uint64
	prepareErrors, bodyReadErrors atomic.Uint64
	durationCount, durationSum, durationMax, intervalDurationMax atomic.Uint64
	durationBuckets [8]atomic.Uint64
	inflight atomic.Int64
	inflightPeak, workersPeak, queuePeak atomic.Uint64
	batches, packets, bytes, maxPackets, maxBytes atomic.Uint64
	wsConnected, wsConnects, wsConnectFailures, wsDisconnects atomic.Uint64
	wsMessages, wsMessageBytes, wsPayloadBytes atomic.Uint64
	wsJSONErrors, wsBase64Errors atomic.Uint64
}

func relayDiagMax(counter *atomic.Uint64, value uint64) {
	for old := counter.Load(); value > old; old = counter.Load() {
		if counter.CompareAndSwap(old, value) {
			return
		}
	}
}

func (d *relayDiagnostics) observeWorkers(busy int64) {
	relayDiagMax(&d.workersPeak, uint64(busy))
}

func (d *relayDiagnostics) observeQueue(length int) {
	relayDiagMax(&d.queuePeak, uint64(length))
}

func (d *relayDiagnostics) batch(packets, payload int) {
	d.batches.Add(1)
	d.packets.Add(uint64(packets))
	d.bytes.Add(uint64(payload))
	relayDiagMax(&d.maxPackets, uint64(packets))
	relayDiagMax(&d.maxBytes, uint64(payload))
}

// HTTP attempts mean http.Client.Do calls, not TCP transmissions/redirect hops.
func (d *relayDiagnostics) begin(body, payload int) time.Time {
	d.requests.Add(1)
	d.bodyAttempted.Add(uint64(body))
	d.payloadAttempted.Add(uint64(payload))
	relayDiagMax(&d.inflightPeak, uint64(d.inflight.Add(1)))
	return time.Now()
}

func relayDurationBucket(duration time.Duration) int {
	limits := [...]time.Duration{50 * time.Millisecond, 100 * time.Millisecond,
		250 * time.Millisecond, 500 * time.Millisecond, time.Second,
		2 * time.Second, 5 * time.Second}
	for i, limit := range limits {
		if duration < limit {
			return i
		}
	}
	return len(limits)
}

func (d *relayDiagnostics) finish(start time.Time, body, payload, status int, err error, bodyReadError bool) {
	duration := time.Since(start)
	d.durationCount.Add(1)
	d.durationSum.Add(uint64(duration))
	relayDiagMax(&d.durationMax, uint64(duration))
	relayDiagMax(&d.intervalDurationMax, uint64(duration))
	d.durationBuckets[relayDurationBucket(duration)].Add(1)
	if bodyReadError {
		d.bodyReadErrors.Add(1)
	}
	// Preserve the exact old acceptance rule, including ignored body-read errors.
	if err == nil && (status == 200 || status == 204) {
		d.successes.Add(1)
		d.bodySuccess.Add(uint64(body))
		d.payloadSuccess.Add(uint64(payload))
	} else {
		d.failures.Add(1)
		d.bodyFailed.Add(uint64(body))
		d.payloadFailed.Add(uint64(payload))
		var netErr net.Error
		switch {
		case err != nil && errors.As(err, &netErr) && netErr.Timeout():
			d.timeout.Add(1)
		case err != nil:
			d.network.Add(1)
		case status >= 400 && status < 500:
			d.http4xx.Add(1)
			switch status {
			case 400:
				d.status400.Add(1)
			case 401:
				d.status401.Add(1)
			case 403:
				d.status403.Add(1)
			case 404:
				d.status404.Add(1)
			case 409:
				d.status409.Add(1)
			case 429:
				d.status429.Add(1)
				d.payload429.Add(uint64(payload))
			}
		case status >= 500 && status < 600:
			d.http5xx.Add(1)
		default:
			d.otherStatus.Add(1)
		}
	}
	d.inflight.Add(-1)
}

// Snapshot loads are individually atomic, not a transaction. Compare settled
// snapshots and report in-flight boundary requests instead of inventing losses.
func (d *relayDiagnostics) snapshot(s *VolgaStats, r *relayClient, started bool) map[string]uint64 {
	values := map[string]uint64{
		"schema": 1,
		"relay_requests_total": d.requests.Load(),
		"relay_successes_total": d.successes.Load(),
		"relay_failures_total": d.failures.Load(),
		"relay_failures_network": d.network.Load(),
		"relay_failures_timeout": d.timeout.Load(),
		"relay_failures_4xx": d.http4xx.Load(),
		"relay_failures_400": d.status400.Load(),
		"relay_failures_401": d.status401.Load(),
		"relay_failures_403": d.status403.Load(),
		"relay_failures_404": d.status404.Load(),
		"relay_failures_409": d.status409.Load(),
		"relay_failures_429": d.status429.Load(),
		"relay_failures_5xx": d.http5xx.Load(),
		"relay_failures_other_status": d.otherStatus.Load(),
		"relay_prepare_errors": d.prepareErrors.Load(),
		"relay_body_read_errors_ignored": d.bodyReadErrors.Load(),
		"relay_errors_legacy_total": s.HTTPReqsFailed.Load(),
		"relay_bytes_attempted": d.bodyAttempted.Load(),
		"relay_bytes_success": d.bodySuccess.Load(),
		"relay_bytes_failed": d.bodyFailed.Load(),
		"relay_payload_bytes_attempted": d.payloadAttempted.Load(),
		"relay_payload_bytes_success": d.payloadSuccess.Load(),
		"relay_payload_bytes_failed": d.payloadFailed.Load(),
		"relay_payload_bytes_429": d.payload429.Load(),
		"relay_request_duration_count": d.durationCount.Load(),
		"relay_request_duration_sum_ns": d.durationSum.Load(),
		"relay_request_duration_max_ns": d.durationMax.Load(),
		"relay_interval_duration_max_ns": d.intervalDurationMax.Swap(0),
		"relay_inflight_current": uint64(d.inflight.Load()),
		"relay_inflight_peak": d.inflightPeak.Load(),
		"worker_count": uint64(r.workers),
		"workers_busy": uint64(s.WorkerBusy.Load()),
		"workers_busy_peak": d.workersPeak.Load(),
		"send_queue_len": uint64(len(r.batchQueue)),
		"send_queue_cap": uint64(cap(r.batchQueue)),
		"send_queue_peak_observed": d.queuePeak.Load(),
		"send_queue_drops": s.QueueDrops.Load(),
		"batches_total": d.batches.Load(),
		"packets_batched": d.packets.Load(),
		"bytes_batched": d.bytes.Load(),
		"max_packets_per_batch_seen": d.maxPackets.Load(),
		"max_bytes_per_batch_seen": d.maxBytes.Load(),
		"ws_connected": d.wsConnected.Load(),
		"ws_connect_successes": d.wsConnects.Load(),
		"ws_connect_failures": d.wsConnectFailures.Load(),
		"ws_disconnects": d.wsDisconnects.Load(),
		"ws_messages_total": d.wsMessages.Load(),
		"ws_message_bytes_total": d.wsMessageBytes.Load(),
		"ws_payload_bytes_total": d.wsPayloadBytes.Load(),
		"ws_json_errors": d.wsJSONErrors.Load(),
		"ws_base64_errors": d.wsBase64Errors.Load(),
		"ws_decode_errors": d.wsJSONErrors.Load() + d.wsBase64Errors.Load(),
		"ws_reconnects": s.WSReconnects.Load(),
		"ws_payload_packets": s.PacketsRecv.Load(),
		"transport_started": 0,
	}
	if started {
		values["transport_started"] = 1
	}
	names := [...]string{"lt_50ms", "50_100ms", "100_250ms", "250_500ms", "500ms_1s", "1s_2s", "2s_5s", "ge_5s"}
	for i, name := range names {
		values["latency_"+name] = d.durationBuckets[i].Load()
	}
	return values
}

func (d *relayDiagnostics) emit(s *VolgaStats, r *relayClient, started bool) {
	utils.RelayDiagnosticSnapshot(d.snapshot(s, r, started))
}
