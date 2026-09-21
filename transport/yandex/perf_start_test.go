package yandex

import (
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"openflux/transport"
	"os"
	"runtime"
	"testing"
	"time"
)

func offlineVolga(cfg VolgaConfig) *YandexVolgaTransport {
	tr, _ := NewYandexVolgaTransportWithConfig("synthetic-context", transport.DefaultConfig(), cfg)
	tr.authorizeFn = func(string) (*volgaAuth, error) {
		jar, _ := cookiejar.New(nil)
		return &volgaAuth{Session: &http.Client{Jar: jar}}, nil
	}
	tr.connectWSFn = func(ws *wsListener) error { <-ws.ctx.Done(); return ws.ctx.Err() }
	return tr
}
func TestVolgaStartStop100Cycles(t *testing.T) {
	cfg := DefaultVolgaConfig()
	cfg.WorkerCount = 4
	cfg.QueueSize = 16
	tr := offlineVolga(cfg)
	before := runtime.NumGoroutine()
	for i := 0; i < 100; i++ {
		if err := tr.Start(); err != nil {
			t.Fatal(err)
		}
		if !tr.IsRunning() {
			t.Fatal("did not start")
		}
		if err := tr.Stop(); err != nil {
			t.Fatal(err)
		}
		if tr.IsRunning() {
			t.Fatal("did not stop")
		}
	}
	if runtime.NumGoroutine() > before+1 {
		t.Fatal("Volga lifecycle leaked a goroutine")
	}
}

// Complete Start/Stop control path with synthetic auth and an idle cancellable
// WS connection. Excludes real TLS/auth/WS-buffer allocations; phone PSS is
// still required. Never issues an external HTTP request.
func TestPerfVolgaStartMemory(t *testing.T) {
	if os.Getenv("OPENFLUX_PERF_MEMORY") == "" {
		t.Skip("opt-in memory measurement")
	}
	cfg := DefaultVolgaConfig()
	if os.Getenv("OPENFLUX_PERF_MEMORY_PROFILE") == "balanced" {
		cfg.WorkerCount = 64
		cfg.QueueSize = 2048
		cfg.MaxIdleConns = 128
		cfg.MaxIdleConnsPerHost = 64
	}
	runtime.GC()
	points := []memoryPoint{memoryAt("before_constructor")}
	tr := offlineVolga(cfg)
	points = append(points, memoryAt("before_Start"))
	if err := tr.Start(); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	runtime.GC()
	points = append(points, memoryAt("after_Start"))
	tr.Stop()
	runtime.GC()
	points = append(points, memoryAt("after_Stop"))
	data, _ := json.Marshal(points)
	t.Log(string(data))
}
