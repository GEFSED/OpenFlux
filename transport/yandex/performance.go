package yandex

// VolgaPerformance is a read-only local snapshot. It deliberately excludes auth,
// endpoint paths, document identifiers and packet contents.
type VolgaPerformance struct {
	VolgaReceiveDiagnostics
	VolgaHTTPFailureDiagnostics
	VolgaRateLimitDiagnostics
	TransportStarted bool    `json:"transport_started"`
	WorkerCount      int     `json:"worker_count"`
	WorkersBusy      int64   `json:"workers_busy"`
	PeakWorkersBusy  int64   `json:"peak_workers_busy"`
	QueueLen         int     `json:"queue_len"`
	QueueCap         int     `json:"queue_cap"`
	QueueDrops       uint64  `json:"queue_drops"`
	HTTPRequests     uint64  `json:"http_requests"`
	HTTPFailures     uint64  `json:"http_failures"`
	HTTPSuccesses    uint64  `json:"http_successes"`
	HTTPFailureRate  float64 `json:"http_failure_rate"`
	Batches          uint64  `json:"batches"`
	PacketsBatched   uint64  `json:"packets_batched"`
	Average          float64 `json:"avg_packets_per_batch"`
	Reconnects       uint64  `json:"reconnects"`
}

func (t *ConfiguredVolgaTransport) Performance() VolgaPerformance {
	t.relayMu.RLock()
	defer t.relayMu.RUnlock()
	d := volgaPerformance(t.config, t.stats, t.relayQueueLen(), t.IsRunning())
	if t.relay != nil && t.relay.rateLimit != nil {
		d.VolgaRateLimitDiagnostics = t.relay.rateLimit.snapshot()
	}
	return d
}

func (t *ConfiguredVolgaTransport) relayQueueLen() int {
	if t.relay != nil {
		return len(t.relay.batchQueue)
	}
	return 0
}

func volgaPerformance(cfg VolgaConfig, s *configuredVolgaStats, queued int, started bool) VolgaPerformance {
	n, p := s.BatchesSent.Load(), s.PacketsBatched.Load()
	avg := float64(0)
	if n > 0 {
		avg = float64(p) / float64(n)
	}
	failed := s.HTTPReqsFailed.Load()
	succeeded := s.HTTPReqsSent.Load()
	requests := succeeded + failed
	// Fraction in [0, 1], not percent. Reuse the same loads for all totals.
	failureRate := float64(0)
	if requests > 0 {
		failureRate = float64(failed) / float64(requests)
	}
	return VolgaPerformance{VolgaReceiveDiagnostics: s.volgaReceiveCounters.snapshot(), VolgaHTTPFailureDiagnostics: s.httpFailures.snapshot(failed), VolgaRateLimitDiagnostics: VolgaRateLimitDiagnostics{Enabled: cfg.RateLimit429GuardEnabled}, TransportStarted: started, WorkerCount: cfg.WorkerCount, WorkersBusy: s.WorkerBusy.Load(), PeakWorkersBusy: s.PeakWorkerBusy.Load(), QueueLen: queued, QueueCap: cfg.QueueSize, QueueDrops: s.QueueDrops.Load(), HTTPRequests: requests, HTTPFailures: failed, HTTPSuccesses: succeeded, HTTPFailureRate: failureRate, Batches: n, PacketsBatched: p, Average: avg, Reconnects: s.WSReconnects.Load()}
}
