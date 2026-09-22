package mobile

import (
	"bytes"
	"encoding/json"
	"openflux/transport"
	"openflux/transport/yandex"
	"os"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

type packetPeer struct {
	mu    sync.Mutex
	cb    func([]byte)
	other *packetPeer
}

func (p *packetPeer) Start() error                    { return nil }
func (p *packetPeer) Stop() error                     { return nil }
func (p *packetPeer) IsConnected() bool               { return true }
func (p *packetPeer) Stats() transport.TransportStats { return transport.TransportStats{} }
func (p *packetPeer) Receive(cb func([]byte))         { p.mu.Lock(); p.cb = cb; p.mu.Unlock() }
func (p *packetPeer) Send(data []byte) error {
	p.other.mu.Lock()
	cb := p.other.cb
	p.other.mu.Unlock()
	if cb != nil {
		cb(append([]byte(nil), data...))
	}
	return nil
}
func installSession(s *packetSession) { client.mu.Lock(); client.session = s; client.mu.Unlock() }

func TestProfilesWireCompatibility(t *testing.T) {
	for _, profile := range []string{"baseline", "balanced", "low_latency", "throughput"} {
		for _, codec := range []string{"batched", "legacy"} {
			for _, secret := range []string{"", "synthetic-test-key-only"} {
				t.Run(profile+"/"+codec+"/encrypted="+map[bool]string{true: "yes", false: "no"}[secret != ""], func(t *testing.T) {
					a, b := &packetPeer{}, &packetPeer{}
					a.other = b
					b.other = a
					s := newPacketSession(profile, "vyandex", 1024)
					cli, err := wrapPacketTransport(s, a, "synthetic-context", secret, codec, true)
					if err != nil {
						t.Fatal(err)
					}
					var exit transport.Transport = b
					if codec != "legacy" {
						exit = transport.NewBatchedTransport(exit)
					}
					if secret != "" {
						exit, err = transport.NewEncryptedTransport(exit, secret, "synthetic-context", true)
						if err != nil {
							t.Fatal(err)
						}
					}
					if codec == "legacy" {
						exit = transport.NewCompressedTransport(exit)
					}
					got := make(chan []byte, 2)
					cli.Receive(func(p []byte) { got <- append([]byte(nil), p...) })
					exit.Receive(func(p []byte) { got <- append([]byte(nil), p...) })
					if err := exit.Start(); err != nil {
						t.Fatal(err)
					}
					defer exit.Stop()
					if err := cli.Start(); err != nil {
						t.Fatal(err)
					}
					defer cli.Stop()
					payload := bytes.Repeat([]byte{1, 2, 3, 255}, 350)
					for _, sender := range []transport.Transport{cli, exit} {
						if err := sender.Send(payload); err != nil {
							t.Fatal(err)
						}
						select {
						case p := <-got:
							if !bytes.Equal(p, payload) {
								t.Fatal("wire mismatch")
							}
						case <-time.After(time.Second):
							t.Fatal("peer did not accept packet")
						}
					}
				})
			}
		}
	}
}
func TestProfileDefaultsAndNonVolga(t *testing.T) {
	p := profileConfig("baseline")
	if p.volga != yandex.DefaultVolgaConfig() || p.outer != transport.DefaultBatchedConfig() {
		t.Fatal("baseline changed")
	}
	if normalizeProfile("unknown") != "baseline" {
		t.Fatal("unsafe profile fallback")
	}
	for _, kind := range []string{"yandex", "oneme", "mailru", "cupsonline"} {
		s := newPacketSession("throughput", kind, 1024)
		_, err := wrapPacketTransport(s, &packetPeer{}, "", "", "batched", true)
		if err != nil {
			t.Fatal(err)
		}
		if s.outer.Performance().BatchedConfig != transport.DefaultBatchedConfig() {
			t.Fatal("non-Volga batching changed")
		}
	}
}
func TestPacketLifecycle100Cycles(t *testing.T) {
	before := runtime.NumGoroutine()
	for i := 0; i < 100; i++ {
		s := newPacketSession("balanced", "vyandex", 8)
		installSession(s)
		p := &packetPeer{}
		p.other = p
		if err := finishStart(s, p); err != "" {
			t.Fatal(err)
		}
		arrived := make(chan []byte, 1)
		go func() { arrived <- ReadWait(0) }()
		if err := Send([]byte("packet")); err != "" {
			t.Fatal(err)
		}
		select {
		case data := <-arrived:
			if string(data) != "packet" {
				t.Fatal("packet changed")
			}
		case <-time.After(time.Second):
			t.Fatal("packet wakeup")
		}
		go func() { arrived <- ReadWait(0) }()
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				_ = Send([]byte("race"))
				_ = PerformanceSnapshot()
			}
		}()
		Stop()
		wg.Wait()
		select {
		case <-arrived:
		case <-time.After(time.Second):
			t.Fatal("stop did not wake reader")
		}
		if Read() != nil || ReadWait(1) != nil {
			t.Fatal("stopped session retained packets")
		}
	}
	if runtime.NumGoroutine() > before+2 {
		t.Fatal("goroutine leak")
	}
}
func TestReceiveBoundedAndNoBusyLoop(t *testing.T) {
	s := newPacketSession("balanced", "vyandex", 2)
	installSession(s)
	for _, v := range []string{"old", "two", "three"} {
		s.enqueue([]byte(v))
	}
	if string(Read()) != "two" || string(Read()) != "three" || s.drops != 1 {
		t.Fatal("drop-oldest policy changed")
	}
	started := time.Now()
	if ReadWait(30) != nil {
		t.Fatal("unexpected data")
	}
	if time.Since(started) < 25*time.Millisecond {
		t.Fatal("did not block")
	}
	s.mu.Lock()
	reads := s.reads
	s.mu.Unlock()
	if reads > 5 {
		t.Fatal("busy polling")
	}
	Stop()
}
func TestSnapshotSanitized(t *testing.T) {
	s := newPacketSession("baseline", "https://synthetic.invalid/secret-token", 2)
	installSession(s)
	data := PerformanceSnapshot()
	if strings.Contains(data, "synthetic") || strings.Contains(data, "secret-token") {
		t.Fatal("secret in diagnostics")
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(data), &decoded); err != nil {
		t.Fatal(err)
	}
	Stop()
}

// Five minutes, no network traffic. Run polling and blocking separately so
// process CPU/heap can be compared without another receiver in the process.
func TestPerfIdle(t *testing.T) {
	mode := os.Getenv("OPENFLUX_PERF_IDLE")
	if mode == "" {
		t.Skip("opt-in 300 second idle measurement")
	}
	s := newPacketSession("baseline", "vyandex", 1024)
	installSession(s)
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	start := time.Now()
	cpuBefore := perfProcessCPUSeconds()
	done := make(chan struct{})
	go func() {
		defer close(done)
		if mode == "blocking" {
			ReadWait(0)
			return
		}
		for {
			select {
			case <-s.done:
				return
			default:
				Read()
				time.Sleep(2 * time.Millisecond)
			}
		}
	}()
	time.Sleep(300 * time.Second)
	Stop()
	<-done
	runtime.GC()
	runtime.ReadMemStats(&after)
	cpuAfter := perfProcessCPUSeconds()
	t.Logf("process_cpu_seconds=%.6f supported=%v", cpuAfter-cpuBefore, cpuBefore >= 0 && cpuAfter >= 0)
	data, _ := json.Marshal(map[string]any{"mode": mode, "seconds": time.Since(start).Seconds(), "read_calls": s.reads, "wait_calls": s.waits, "before_heap": before.HeapAlloc, "after_heap": after.HeapAlloc, "total_alloc_bytes": after.TotalAlloc - before.TotalAlloc, "gc_count": after.NumGC - before.NumGC, "goroutines": runtime.NumGoroutine()})
	t.Log(string(data))
}
