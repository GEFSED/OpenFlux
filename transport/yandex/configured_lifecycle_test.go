package yandex

import (
	"net/http"
	"net/http/cookiejar"
	"openflux/transport"
	"runtime"
	"testing"
	"time"
)

func offlineVolga(cfg VolgaConfig) *ConfiguredVolgaTransport {
	tr, _ := NewYandexVolgaTransportWithConfig("synthetic-context", transport.DefaultConfig(), cfg)
	tr.authorizeFn = func(string) (*volgaAuth, error) {
		jar, _ := cookiejar.New(nil)
		return &volgaAuth{Session: &http.Client{Jar: jar}}, nil
	}
	tr.connectWSFn = func(ws *configuredWSListener) error { <-ws.ctx.Done(); return ws.ctx.Err() }
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

func TestVolgaStopDoesNotBlockCallbackSend(t *testing.T) {
	cfg := DefaultVolgaConfig()
	cfg.WorkerCount = 1
	cfg.QueueSize = 16
	tr := offlineVolga(cfg)
	entered := make(chan struct{})
	tr.connectWSFn = func(ws *configuredWSListener) error { close(entered); <-ws.ctx.Done(); return tr.Send(nil) }
	if err := tr.Start(); err != nil {
		t.Fatal(err)
	}
	<-entered
	stopped := make(chan struct{})
	go func() { tr.Stop(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("Stop held the Send lock while joining a callback")
	}
}
