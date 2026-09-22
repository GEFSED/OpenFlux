package mobile

import (
	"openflux/transport"
	"openflux/transport/yandex"
	"time"
)

type perfConfig struct {
	volga yandex.VolgaConfig
	outer transport.BatchedConfig
}

func normalizeProfile(name string) string {
	switch name {
	case "balanced", "low_latency", "throughput", "throughput_current", "throughput_mem", "throughput_96", "throughput_w64", "throughput_w48", "throughput_w32":
		return name
	}
	return "baseline"
}

// Candidates selected from the local one-factor staged search; not a carrier
// ranking. CLI constructors/defaults and non-Volga batching are unchanged.
func profileConfig(name string) perfConfig {
	p := perfConfig{yandex.DefaultVolgaConfig(), transport.DefaultBatchedConfig()}
	switch normalizeProfile(name) {
	case "low_latency":
		p.volga.WorkerCount = 64
		p.volga.QueueSize = 512
		p.volga.BatchSize = 8
		p.volga.BatchTimeout = 0
		p.outer.Linger = 0
		p.outer.QueueDepth = 512
	case "balanced":
		p.volga.WorkerCount = 64
		p.volga.QueueSize = 2048
		p.volga.BatchTimeout = 500 * time.Microsecond
		p.outer.MaxBatchBytes = 16 << 10
		p.outer.Linger = time.Millisecond
		p.outer.QueueDepth = 2048
	case "throughput", "throughput_current", "throughput_mem", "throughput_96", "throughput_w64", "throughput_w48", "throughput_w32":
		p.volga.WorkerCount = 64
		p.volga.QueueSize = 4096
		p.volga.BatchSize = 32
		p.volga.BatchTimeout = time.Millisecond
		p.outer.MaxBatchBytes = 32 << 10
		p.outer.Linger = 2 * time.Millisecond
		// One-factor candidates share every other Throughput setting.
		if name == "throughput_mem" {
			p.volga.QueueSize = 2048
		}
		if name == "throughput_96" {
			p.volga.WorkerCount = 96
		}
		if name == "throughput_w48" {
			p.volga.WorkerCount = 48
		}
		if name == "throughput_w32" {
			p.volga.WorkerCount = 32
		}
	default:
		return p
	}
	p.volga.MaxIdleConnsPerHost = p.volga.WorkerCount
	p.volga.MaxIdleConns = 2 * p.volga.WorkerCount
	return p
}
