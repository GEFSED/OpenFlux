package mobile

import (
	"encoding/json"
	"testing"
	"time"

	"openflux/transport"
	"openflux/transport/yandex"
)

func TestThroughputOneFactorConfigs(t *testing.T) {
	control := profileConfig("throughput_current")
	if control != profileConfig("throughput") {
		t.Fatal("control is not an exact Throughput alias")
	}
	if control.volga.WorkerCount != 64 || control.volga.QueueSize != 4096 || control.volga.BatchSize != 32 || control.volga.BatchTimeout != time.Millisecond || control.volga.MaxIdleConnsPerHost != 64 || control.volga.MaxIdleConns != 128 {
		t.Fatal("unexpected control settings")
	}
	mem := profileConfig("throughput_mem")
	if mem.volga.QueueSize != 2048 {
		t.Fatal("memory candidate queue must be 2048")
	}
	mem.volga.QueueSize = control.volga.QueueSize
	if mem != control {
		t.Fatal("memory candidate changed a field other than QueueSize")
	}
	workers := profileConfig("throughput_96")
	if workers.volga.WorkerCount != 96 || workers.volga.MaxIdleConnsPerHost != 96 || workers.volga.MaxIdleConns != 192 {
		t.Fatal("concurrency candidate must use 96 workers and derived pools")
	}
	workers.volga.WorkerCount = control.volga.WorkerCount
	workers.volga.MaxIdleConnsPerHost = control.volga.MaxIdleConnsPerHost
	workers.volga.MaxIdleConns = control.volga.MaxIdleConns
	// Whole-struct equality includes all timeouts, reconnect/payload limits and
	// outer batching fields, including any fields added in a future change.
	if workers != control {
		t.Fatal("concurrency candidate changed an unrelated field")
	}
}

func TestProfileNamesAndDiagnostics(t *testing.T) {
	baselineActive.Store(false)
	t.Cleanup(Stop)
	for _, name := range []string{"baseline", "balanced", "low_latency", "throughput", "throughput_current", "throughput_mem", "throughput_96", "throughput_w64", "throughput_w48", "throughput_w32", "throughput_w32_429guard", "optimized"} {
		if normalizeProfile(name) != name {
			t.Fatalf("profile name lost: %s", name)
		}
		s := newPacketSession(normalizeProfile(name), "vyandex", 2)
		installSession(s)
		var snapshot struct {
			Profile string `json:"profile"`
		}
		if err := json.Unmarshal([]byte(PerformanceSnapshot()), &snapshot); err != nil {
			t.Fatal(err)
		}
		if snapshot.Profile != name {
			t.Fatalf("diagnostics profile: got %s, want %s", snapshot.Profile, name)
		}
		Stop()
	}
	for _, invalid := range []string{"", "unknown", "Throughput", "throughput_128"} {
		if normalizeProfile(invalid) != "baseline" {
			t.Fatal("unknown profile must fall back to baseline")
		}
	}
}

// Frozen configurations from e3a9c87606c138382f356c1252493d60e66b6e02.
// These literals deliberately do not call the current defaults/normalizer.
func profileConfigE3(name string) perfConfig {
	p := perfConfig{
		volga: yandex.VolgaConfig{
			MaxIdleConnsPerHost: 2000,
			MaxIdleConns:        4000,
			IdleConnTimeout:     90 * time.Second,
			RelayTimeout:        30 * time.Second,
			WorkerCount:         2000,
			QueueSize:           1000000,
			BatchSize:           20,
			BatchTimeout:        2 * time.Millisecond,
			BatchMaxBytes:       4 * 1024 * 1024,
			MaxPayloadBytes:     5_000_000,
			MinPayloadBytes:     200,
			ReconnectMinDelay:   500 * time.Millisecond,
			ReconnectMaxDelay:   30 * time.Second,
			ReconnectMultiplier: 1.5,
			WSHandshakeTimeout:  10 * time.Second,
			WSReadTimeout:       60 * time.Second,
			KeepAliveInterval:   10 * time.Second,
		},
		outer: transport.BatchedConfig{MaxBatchBytes: 8192, MaxBatchCount: 64, Linger: 5 * time.Millisecond, QueueDepth: 4096},
	}
	switch name {
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
	case "throughput":
		p.volga.WorkerCount = 64
		p.volga.QueueSize = 4096
		p.volga.BatchSize = 32
		p.volga.BatchTimeout = time.Millisecond
		p.outer.MaxBatchBytes = 32 << 10
		p.outer.Linger = 2 * time.Millisecond
	default:
		return p
	}
	p.volga.MaxIdleConnsPerHost = p.volga.WorkerCount
	p.volga.MaxIdleConns = 2 * p.volga.WorkerCount
	return p
}

func TestExistingProfilesMatchE3(t *testing.T) {
	for _, name := range []string{"baseline", "balanced", "low_latency", "throughput"} {
		if got, want := profileConfig(name), profileConfigE3(name); got != want {
			t.Fatalf("%s changed from e3a9c876: got %+v, want %+v", name, got, want)
		}
	}
}
