package mobile

import "testing"

// Frozen .6 additions on top of the frozen .5 oracle. The new bool defaults
// to false, and no current constructor/default supplies expected values here.
func profileConfigB59(name string) perfConfig {
	switch name {
	case "throughput_w64", "throughput_w48", "throughput_w32":
		p := profileConfigF9("throughput_current")
		workers := 64
		if name == "throughput_w48" {
			workers = 48
		}
		if name == "throughput_w32" {
			workers = 32
		}
		p.volga.WorkerCount = workers
		p.volga.MaxIdleConnsPerHost = workers
		p.volga.MaxIdleConns = 2 * workers
		return p
	default:
		return profileConfigF9(name)
	}
}

func TestExistingProfilesMatchB59(t *testing.T) {
	for _, name := range []string{"baseline", "balanced", "low_latency", "throughput", "throughput_current", "throughput_mem", "throughput_96", "throughput_w64", "throughput_w48", "throughput_w32"} {
		if got, want := profileConfig(name), profileConfigB59(name); got != want || got.volga.RateLimit429GuardEnabled {
			t.Fatalf("%s changed from b59dd844", name)
		}
	}
}

func Test429GuardProfileOneFactor(t *testing.T) {
	fixed := profileConfig("throughput_w32")
	guarded := profileConfig("throughput_w32_429guard")
	if fixed != profileConfigB59("throughput_w32") || fixed.volga.RateLimit429GuardEnabled || !guarded.volga.RateLimit429GuardEnabled {
		t.Fatal("incorrect control or guard opt-in")
	}
	guarded.volga.RateLimit429GuardEnabled = false
	if guarded != fixed {
		t.Fatal("guard changed a field other than its enable flag")
	}
}
