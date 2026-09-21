package yandex

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"os"
	"runtime"
	"sync"
	"testing"
	"time"
)

type memoryPoint struct {
	Phase                                                          string
	HeapAlloc, HeapSys, HeapInuse, StackInuse, TotalAlloc, Mallocs uint64
	Goroutines                                                     int
	NumGC                                                          uint32
}

func memoryAt(phase string) memoryPoint {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return memoryPoint{phase, m.HeapAlloc, m.HeapSys, m.HeapInuse, m.StackInuse, m.TotalAlloc, m.Mallocs, runtime.NumGoroutine(), m.NumGC}
}

// Explicit opt-in: no Yandex I/O, only the real relay's allocation/worker path.
// This separates local Start costs from auth HTML, TLS and WebSocket memory.
func TestPerfMemory(t *testing.T) {
	if os.Getenv("OPENFLUX_PERF_MEMORY") == "" {
		t.Skip("opt-in local measurement")
	}
	runtime.GC()
	points := []memoryPoint{memoryAt("before")}
	jar, _ := cookiejar.New(nil)
	cfg := DefaultVolgaConfig()
	if os.Getenv("OPENFLUX_PERF_MEMORY_PROFILE") == "balanced" {
		cfg.WorkerCount = 64
		cfg.QueueSize = 2048
		cfg.MaxIdleConns = 128
		cfg.MaxIdleConnsPerHost = 64
		cfg.BatchTimeout = 500 * time.Microsecond
	}
	r := newRelayClient(&volgaAuth{Session: &http.Client{Jar: jar}}, cfg, &VolgaStats{})
	points = append(points, memoryAt("relay_constructed"))
	r.Start()
	time.Sleep(100 * time.Millisecond)
	runtime.GC()
	points = append(points, memoryAt("workers_started"))
	_ = base64Encode(make([]byte, 1400))
	points = append(points, memoryAt("first_base64_1400"))
	r.Stop()
	runtime.GC()
	points = append(points, memoryAt("stopped"))
	data, _ := json.Marshal(points)
	t.Log(string(data))
}

func BenchmarkPerfBase64Candidates(b *testing.B) {
	for _, size := range []int{1400, 8192, 32768, 65535, 4 << 20} {
		for _, initial := range []int{4 << 10, 16 << 10, 64 << 10, 16 << 20} {
			for _, limit := range []int{64 << 10, 256 << 10, 1 << 20, 16 << 20} {
				name := fmtSize(size) + "/initial=" + fmtSize(initial) + "/retain=" + fmtSize(limit)
				b.Run(name, func(b *testing.B) {
					pool := sync.Pool{New: func() any { return make([]byte, 0, initial) }}
					data := make([]byte, size)
					b.SetBytes(int64(size))
					b.ReportAllocs()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						buf := pool.Get().([]byte)
						need := base64.StdEncoding.EncodedLen(len(data))
						if cap(buf) < need {
							buf = make([]byte, need)
						} else {
							buf = buf[:need]
						}
						base64.StdEncoding.Encode(buf, data)
						encoded := string(buf)
						runtime.KeepAlive(encoded)
						if cap(buf) <= limit {
							pool.Put(buf[:0])
						}
					}
				})
			}
		}
	}
}
func fmtSize(n int) string {
	if n >= 1024 {
		return fmtInt(n/1024) + "KiB"
	}
	return fmtInt(n) + "B"
}
func fmtInt(n int) string {
	const d = "0123456789"
	if n < 10 {
		return string(d[n])
	}
	return fmtInt(n/10) + string(d[n%10])
}
