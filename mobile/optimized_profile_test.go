package mobile

import (
	"testing"
	"time"
)

// Frozen .7 config on top of the independent .6/.5/.4 oracles. Do not use
// the current defaults or profileConfig to supply these expected values.
func profileConfigF4(name string) perfConfig {
	if name == "throughput_w32_429guard" {
		p := profileConfigB59("throughput_w32")
		p.volga.RateLimit429GuardEnabled = true
		return p
	}
	return profileConfigB59(name)
}

func TestExistingProfilesMatchF4(t *testing.T) {
	for _, name := range []string{"baseline", "balanced", "low_latency", "throughput", "throughput_current", "throughput_mem", "throughput_96", "throughput_w64", "throughput_w48", "throughput_w32", "throughput_w32_429guard"} {
		if got, want := profileConfig(name), profileConfigF4(name); got != want {
			t.Fatalf("%s changed from f4bcfeb3751626cfa4c6ca158ef4cb26692636da", name)
		}
	}
}

func TestOptimizedIsExactGuardedW32Alias(t *testing.T) {
	p := profileConfig("optimized")
	if p != profileConfig("throughput_w32_429guard") || p != profileConfigF4("throughput_w32_429guard") {
		t.Fatal("optimized must equal every .7 guarded-w32 Volga and outer config field")
	}
	v := p.volga
	if v.WorkerCount != 32 || v.QueueSize != 4096 || v.BatchSize != 32 || v.BatchTimeout != time.Millisecond || v.MaxIdleConnsPerHost != 32 || v.MaxIdleConns != 64 || !v.RateLimit429GuardEnabled {
		t.Fatal("unexpected optimized settings")
	}
	if normalizeProfile("optimized") != "optimized" || normalizeProfile("") != "baseline" {
		t.Fatal("canonical name or default changed")
	}
}
