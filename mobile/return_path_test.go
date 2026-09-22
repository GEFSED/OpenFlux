package mobile

import (
	"bytes"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"openflux/testsupport/basev100peer"
	"openflux/transport"
)

// Unlike the old symmetric test, the peer's codec/framing/AES implementation
// is frozen at 8566f727, not the current Perf Lab implementation.
type observedCarrier struct {
	packetPeer
	muFrames            sync.Mutex
	frames              [][]byte
	startedWithCallback bool
}

func (p *observedCarrier) Start() error {
	p.mu.Lock()
	p.startedWithCallback = p.cb != nil
	p.mu.Unlock()
	return nil
}
func (p *observedCarrier) Send(data []byte) error {
	p.muFrames.Lock()
	p.frames = append(p.frames, append([]byte(nil), data...))
	p.muFrames.Unlock()
	return p.packetPeer.Send(data)
}

// Legacy compatibility is tested separately against exact production 081d214.
func TestV100BatchedPeerVersusBaselineAndroidPath(t *testing.T) {
	EnablePerformanceLab()
	for _, codec := range []string{"batched"} {
		for _, secret := range []string{"", "synthetic-diagnostic-secret"} {
			t.Run(codec+map[bool]string{true: "/AES", false: "/plain"}[secret != ""], func(t *testing.T) {
				baselineActive.Store(true)
				defer baselineActive.Store(false)
				a, b := &observedCarrier{}, &packetPeer{}
				a.other = b
				b.other = &a.packetPeer
				peer, err := basev100peer.Wrap(b, codec, secret, "synthetic-context", true)
				if err != nil {
					t.Fatal(err)
				}
				got := make(chan []byte, 8)
				peer.Receive(func(p []byte) { got <- append([]byte(nil), p...) })
				if err := peer.Start(); err != nil {
					t.Fatal(err)
				}
				defer peer.Stop()
				// This is the actual Baseline startup path, with only the raw
				// carrier replaced; callback installation, codec, KDF context,
				// exit=false, global slice queue and public Send/Read are real.
				if err := baselineStartWithCarrier("vyandex", "synthetic-context", secret, codec, "", "", a); err != "" {
					t.Fatal(err)
				}
				defer Stop()
				if !a.startedWithCallback {
					t.Fatal("callback registered after carrier Start")
				}
				tr := baselineClient.transport
				if secret != "" {
					e, ok := tr.(*transport.EncryptedTransport)
					if !ok {
						t.Fatal("AES is not outermost")
					}
					tr = e.Transport
				}
				if codec == "batched" {
					b, ok := tr.(*transport.BaselineV100BatchedTransport)
					if !ok {
						t.Fatal("not original batched lifecycle")
					}
					tr = b.Transport
				} else {
					c, ok := tr.(*transport.CompressedTransport)
					if !ok {
						t.Fatal("legacy codec missing")
					}
					tr = c.Transport
				}
				if tr != a {
					t.Fatal("carrier wrapping order changed")
				}
				for _, packet := range [][]byte{[]byte{0x45, 0, 0, 20}, bytes.Repeat([]byte("return-traffic"), 100)} {
					if err := Send(packet); err != "" {
						t.Fatal(err)
					}
					select {
					case p := <-got:
						if !bytes.Equal(packet, p) {
							t.Fatal("base exit rejected upload")
						}
					case <-time.After(2 * time.Second):
						t.Fatal("no upload")
					}
					if err := peer.Send(packet); err != nil {
						t.Fatal(err)
					}
					deadline := time.Now().Add(2 * time.Second)
					for {
						p := Read()
						if p != nil {
							if !bytes.Equal(packet, p) {
								t.Fatal("baseline rejected base reply")
							}
							break
						}
						if time.Now().After(deadline) {
							t.Fatal("no return packet")
						}
						time.Sleep(time.Millisecond)
					}
				}
				a.muFrames.Lock()
				frames := append([][]byte(nil), a.frames...)
				a.muFrames.Unlock()
				for _, frame := range frames {
					pkts, err := basev100peer.DecodeCodec(frame, codec)
					if err != nil || len(pkts) == 0 {
						t.Fatal("base codec did not accept wire frame")
					}
					if secret != "" && !bytes.HasPrefix(pkts[0], []byte{'O', 'F', 'X', 1, 0}) {
						t.Fatal("wire order must be codec(AES(packet))")
					}
				}
				var diag map[string]any
				if err := json.Unmarshal([]byte(PerformanceSnapshot()), &diag); err != nil {
					t.Fatal(err)
				}
				if diag["mobile_callback_packets"] != float64(2) || diag["receive_ring_enqueued"] != float64(2) || diag["receive_ring_dropped"] != float64(0) {
					t.Fatalf("callback boundary: %v", diag)
				}
				for _, forbidden := range []string{"synthetic-context", "synthetic-diagnostic-secret", "return-traffic"} {
					if bytes.Contains([]byte(PerformanceSnapshot()), []byte(forbidden)) {
						t.Fatal("diagnostics leak data")
					}
				}
			})
		}
	}
}

func TestMobileCallbackCountersAndQueueControl(t *testing.T) {
	s := newPacketSession("balanced", "vyandex", 2)
	for _, p := range []string{"one", "two", "three"} {
		s.enqueue([]byte(p))
	}
	if s.callbackPackets != 3 || s.callbackBytes != 11 || s.enqueued != 3 || s.drops != 1 {
		t.Fatal("ring counters")
	}
	if string(s.read()) != "two" || string(s.read()) != "three" {
		t.Fatal("ring delivery")
	}
	s.stop()
	s.enqueue([]byte("late"))
	if s.callbackPackets != 4 || s.enqueued != 3 {
		t.Fatal("late callback not distinguished from enqueue")
	}
}

func TestBaselineNegativeReceiveBoundaries(t *testing.T) {
	EnablePerformanceLab()
	baselineActive.Store(true)
	defer baselineActive.Store(false)
	a, b := &packetPeer{}, &packetPeer{}
	a.other = b
	b.other = a
	if err := baselineStartWithCarrier("vyandex", "synthetic-context", "synthetic-diagnostic-secret", "batched", "", "", a); err != "" {
		t.Fatal(err)
	}
	defer Stop()
	outer, aes := baselineClient.outer, baselineClient.encrypted
	if outer.Performance().Frames != 0 || aes.ReceiveDiagnostics().Packets != 0 || baselineClient.callbackPackets != 0 {
		t.Fatal("no frames must be distinguishable from decode rejection")
	}
	a.mu.Lock()
	deliver := a.cb
	a.mu.Unlock()
	deliver([]byte{0xff, 0})
	if outer.Performance().Errors != 1 || aes.ReceiveDiagnostics().Packets != 0 {
		t.Fatal("bad codec frame reached AES")
	}
	for i, secret := range []string{"", "synthetic-wrong-secret", "synthetic-diagnostic-secret"} {
		peer, err := basev100peer.Wrap(b, "batched", secret, "synthetic-context", true)
		if err != nil {
			t.Fatal(err)
		}
		if err = peer.Start(); err != nil {
			t.Fatal(err)
		}
		if err = peer.Send(bytes.Repeat([]byte{0x45}, 64)); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(2 * time.Second)
		for aes.ReceiveDiagnostics().Packets < uint64(i+1) {
			if time.Now().After(deadline) {
				t.Fatal("missing encrypted boundary")
			}
			time.Sleep(time.Millisecond)
		}
		// Packets is recorded before its disposition, so wait on the specific
		// outcome rather than racing a partially updated snapshot.
		for {
			d := aes.ReceiveDiagnostics()
			if d.BadHeader+d.DecryptFail+d.Success == uint64(i+1) {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("missing disposition")
			}
			time.Sleep(time.Millisecond)
		}
		_ = peer.Stop()
	}
	deadline := time.Now().Add(time.Second)
	for Read() == nil {
		if time.Now().After(deadline) {
			t.Fatal("successful return did not reach mobile queue")
		}
		time.Sleep(time.Millisecond)
	}
	if got := aes.ReceiveDiagnostics(); got.Packets != 3 || got.BadHeader != 1 || got.DecryptFail != 1 || got.Success != 1 {
		t.Fatalf("AES boundary %+v", got)
	}
	if got := outer.Performance(); got.Frames != 4 || got.Errors != 1 || got.Success != 3 || got.BatchReceiveDiagnostics.Packets != 3 {
		t.Fatalf("codec boundary %+v", got)
	}
	baselineClient.mu.Lock()
	callbacks, enqueued := baselineClient.callbackPackets, baselineClient.enqueued
	baselineClient.mu.Unlock()
	if callbacks != 1 || enqueued != 1 {
		t.Fatal("rejected frames leaked into mobile callback")
	}
}

func TestOriginalSliceAndCandidateRingDeliverSameOverflowSequence(t *testing.T) {
	EnablePerformanceLab()
	a, b := &packetPeer{}, &packetPeer{}
	a.other = b
	b.other = a
	if err := baselineStartWithCarrier("vyandex", "synthetic-context", "", "legacy", "", "", a); err != "" {
		t.Fatal(err)
	}
	defer baselineStop()
	peer, err := basev100peer.Wrap(b, "legacy", "", "synthetic-context", true)
	if err != nil {
		t.Fatal(err)
	}
	s := newPacketSession("balanced", "vyandex", 1024)
	for i := 0; i < 1026; i++ {
		packet := []byte{byte(i >> 8), byte(i)}
		s.enqueue(packet)
		if err := peer.Send(packet); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 1024; i++ {
		if !bytes.Equal(baselineRead(), s.read()) {
			t.Fatal("slice/ring queue behavior differs")
		}
	}
	if baselineRead() != nil || s.read() != nil || baselineClient.dropped != 2 || s.drops != 2 {
		t.Fatal("overflow accounting differs")
	}
}
