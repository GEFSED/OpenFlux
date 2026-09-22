package mobile

import (
	"testing"
	"time"
)

// Frozen .5 configuration oracle: f9adecf6e5c87cadef0401d6f8aa2c6ce5d240f8.
// Reuses the frozen .4 literals, never current profileConfig/defaults.
func profileConfigF9(name string) perfConfig {
	switch name {
	case "throughput_current", "throughput_mem", "throughput_96":
		p := profileConfigE3("throughput")
		if name == "throughput_mem" {
			p.volga.QueueSize = 2048
		}
		if name == "throughput_96" {
			p.volga.WorkerCount = 96
			p.volga.MaxIdleConnsPerHost = 96
			p.volga.MaxIdleConns = 192
		}
		return p
	default:
		return profileConfigE3(name)
	}
}

func TestExistingProfilesMatchF9(t *testing.T) {
	for _, name := range []string{"baseline", "balanced", "low_latency", "throughput", "throughput_current", "throughput_mem", "throughput_96"} {
		if got, want := profileConfig(name), profileConfigF9(name); got != want {
			t.Fatalf("%s changed from f9adecf6: got %+v, want %+v", name, got, want)
		}
	}
}

func TestWorkerProfilesExactConfigsAndOneFactor(t *testing.T) {
	control := profileConfig("throughput_w64")
	if control != profileConfigF9("throughput_current") || control != profileConfig("throughput_current") {
		t.Fatal("w64 differs from the frozen .5 Throughput control")
	}
	for _, tc := range []struct {
		name    string
		workers int
	}{
		{"throughput_w64", 64},
		{"throughput_w48", 48},
		{"throughput_w32", 32},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := profileConfig(tc.name)
			if got.volga.WorkerCount != tc.workers || got.volga.MaxIdleConnsPerHost != tc.workers || got.volga.MaxIdleConns != 2*tc.workers {
				t.Fatal("wrong worker count or derived connection pool")
			}
			if got.volga.QueueSize != 4096 || got.volga.BatchSize != 32 || got.volga.BatchTimeout != time.Millisecond {
				t.Fatal("queue or batching differs from this experiment's control")
			}
			want := profileConfigF9("throughput_current")
			want.volga.WorkerCount = tc.workers
			want.volga.MaxIdleConnsPerHost = tc.workers
			want.volga.MaxIdleConns = 2 * tc.workers
			if got != want {
				t.Fatalf("exact config differs: got %+v, want %+v", got, want)
			}
			// Normalize ONLY the three allowed differences, then compare every
			// Volga and outer field, including future fields and all timeouts.
			got.volga.WorkerCount = control.volga.WorkerCount
			got.volga.MaxIdleConnsPerHost = control.volga.MaxIdleConnsPerHost
			got.volga.MaxIdleConns = control.volga.MaxIdleConns
			if got != control {
				t.Fatal("a field outside the concurrency factor differs")
			}
		})
	}
}
