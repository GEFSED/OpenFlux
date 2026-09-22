package yandex

// VolgaPerformance is a read-only local snapshot. It deliberately excludes auth,
// endpoint paths, document identifiers and packet contents.
type VolgaPerformance struct {
	VolgaReceiveDiagnostics
	VolgaHTTPFailureDiagnostics
	TransportStarted bool    `json:"transport_started"`
	WorkerCount      int     `json:"worker_count"`
	WorkersBusy      int64   `json:"workers_busy"`
	PeakWorkersBusy  int64   `json:"peak_workers_busy"`
	QueueLen         int     `json:"queue_len"`
	QueueCap         int     `json:"queue_cap"`
	QueueDrops       uint64  `json:"queue_drops"`
	HTTPRequests     uint64  `json:"http_requests"`
	HTTPFailures     uint64  `json:"http_failures"`
	Batches          uint64  `json:"batches"`
	PacketsBatched   uint64  `json:"packets_batched"`
	Average          float64 `json:"avg_packets_per_batch"`
	Reconnects       uint64  `json:"reconnects"`
}

func (t *YandexVolgaTransport) Performance() VolgaPerformance {
	t.relayMu.RLock()
	defer t.relayMu.RUnlock()
	return volgaPerformance(t.config, t.stats, t.relayQueueLen(), t.IsRunning())
}

func (t *YandexVolgaTransport) relayQueueLen() int {
	if t.relay != nil {
		return len(t.relay.batchQueue)
	}
	return 0
}

func (t *BaselineV100VolgaTransport) Performance() VolgaPerformance {
	queued := 0
	if t.relay != nil {
		queued = len(t.relay.batchQueue)
	}
	return volgaPerformance(t.config, t.stats, queued, t.IsRunning())
}

func volgaPerformance(cfg VolgaConfig, s *VolgaStats, queued int, started bool) VolgaPerformance {
	n, p := s.BatchesSent.Load(), s.PacketsBatched.Load()
	avg := float64(0)
	if n > 0 {
		avg = float64(p) / float64(n)
	}
	failed := s.HTTPReqsFailed.Load()
	return VolgaPerformance{VolgaReceiveDiagnostics: s.volgaReceiveCounters.snapshot(), VolgaHTTPFailureDiagnostics: s.httpFailures.snapshot(failed), TransportStarted: started, WorkerCount: cfg.WorkerCount, WorkersBusy: s.WorkerBusy.Load(), PeakWorkersBusy: s.PeakWorkerBusy.Load(), QueueLen: queued, QueueCap: cfg.QueueSize, QueueDrops: s.QueueDrops.Load(), HTTPRequests: s.HTTPReqsSent.Load() + failed, HTTPFailures: failed, Batches: n, PacketsBatched: p, Average: avg, Reconnects: s.WSReconnects.Load()}
}
