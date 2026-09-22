package mobile

import (
	"openflux/transport"
	"openflux/transport/yandex"
	"time"
)

type referencePerfConfig struct {
	volga yandex.VolgaConfig
	outer transport.BatchedConfig
}

func referenceNormalizeProfile(name string) string {
	switch name {
	case "optimized", "balanced", "low_latency", "throughput", "throughput_current", "throughput_mem", "throughput_96", "throughput_w64", "throughput_w48", "throughput_w32", "throughput_w32_429guard":
		return name
	}
	return "baseline"
}

// Candidates selected from the local one-factor staged search; not a carrier
// ranking. CLI constructors/defaults and non-Volga batching are unchanged.
func referenceProfileConfig(name string) referencePerfConfig {
	// A canonical UI name for the measured .7 candidate, with no retuning.
	// Keep normalization separate so diagnostics retain the selected name.
	if name == "optimized" {
		return referenceProfileConfig("throughput_w32_429guard")
	}
	p := referencePerfConfig{yandex.DefaultVolgaConfig(), transport.DefaultBatchedConfig()}
	switch referenceNormalizeProfile(name) {
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
	case "throughput", "throughput_current", "throughput_mem", "throughput_96", "throughput_w64", "throughput_w48", "throughput_w32", "throughput_w32_429guard":
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
		if name == "throughput_w32" || name == "throughput_w32_429guard" {
			p.volga.WorkerCount = 32
		}
		p.volga.RateLimit429GuardEnabled = name == "throughput_w32_429guard"
	default:
		return p
	}
	p.volga.MaxIdleConnsPerHost = p.volga.WorkerCount
	p.volga.MaxIdleConns = 2 * p.volga.WorkerCount
	return p
}
