package yandex

// VolgaPerformance is a read-only local snapshot. It deliberately excludes auth,
// endpoint paths, document identifiers and packet contents.
type VolgaPerformance struct {
	WorkerCount     int     `json:"worker_count"`
	WorkersBusy     int64   `json:"workers_busy"`
	PeakWorkersBusy int64   `json:"peak_workers_busy"`
	QueueLen        int     `json:"queue_len"`
	QueueCap        int     `json:"queue_cap"`
	QueueDrops      uint64  `json:"queue_drops"`
	HTTPRequests    uint64  `json:"http_requests"`
	HTTPFailures    uint64  `json:"http_failures"`
	Batches         uint64  `json:"batches"`
	PacketsBatched  uint64  `json:"packets_batched"`
	Average         float64 `json:"avg_packets_per_batch"`
	Reconnects      uint64  `json:"reconnects"`
}

func (t *YandexVolgaTransport) Performance() VolgaPerformance {
	t.relayMu.RLock()
	defer t.relayMu.RUnlock()
	s := t.stats
	n, p := s.BatchesSent.Load(), s.PacketsBatched.Load()
	avg := float64(0)
	if n > 0 {
		avg = float64(p) / float64(n)
	}
	queued, capacity := 0, t.config.QueueSize
	if t.relay != nil {
		queued = len(t.relay.batchQueue)
		capacity = cap(t.relay.batchQueue)
	}
	return VolgaPerformance{t.config.WorkerCount, s.WorkerBusy.Load(), s.PeakWorkerBusy.Load(), queued, capacity, s.QueueDrops.Load(), s.HTTPReqsSent.Load() + s.HTTPReqsFailed.Load(), s.HTTPReqsFailed.Load(), n, p, avg, s.WSReconnects.Load()}
}
