package transport

import (
	"sync"
	"testing"
	"time"
)

func TestBatchedConfigCompatibility(t *testing.T) {
	want := BatchedConfig{8192, 64, 5 * time.Millisecond, 4096}
	if DefaultBatchedConfig() != want {
		t.Fatal("defaults changed")
	}
	t.Setenv("OPENFLUX_BATCH_BYTES", "16384")
	if NewBatchedTransport(&fakeTransport{}).maxBatchBytes != 16384 {
		t.Fatal("legacy env broken")
	}
	explicit, err := NewBatchedTransportWithConfig(&fakeTransport{}, want)
	if err != nil {
		t.Fatal(err)
	}
	if explicit.maxBatchBytes != 8192 {
		t.Fatal("typed config depends on environment")
	}
	bad := want
	bad.QueueDepth = 0
	if _, err := NewBatchedTransportWithConfig(&fakeTransport{}, bad); err == nil {
		t.Fatal("invalid config accepted")
	}
}
func TestBatchSendStop100Cycles(t *testing.T) {
	b, err := NewBatchedTransportWithConfig(&fakeTransport{}, DefaultBatchedConfig())
 if err != nil { t.Fatal(err) }
	for i := 0; i < 100; i++ {
		if err := b.Start(); err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = b.Send([]byte("synthetic"))
			}
		}()
		if err := b.Stop(); err != nil {
			t.Fatal(err)
		}
		wg.Wait()
		if len(b.queue) != 0 {
			t.Fatal("stopped queue retained a packet")
		}
	}
}
