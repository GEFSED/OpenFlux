package mobile

import (
	"openflux/transport"
	"runtime"
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
func TestPacketLifecycle100Cycles(t *testing.T) {
	before := runtime.NumGoroutine()
	for i := 0; i < 100; i++ {
		s := newPacketSession("speed", "vyandex", 8)
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
				appendModeDiagnostics()
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
	s := newPacketSession("speed", "vyandex", 2)
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
