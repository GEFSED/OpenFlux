package transport

import "testing"

func TestEncryptedReceiveCountersIdentifyRejection(t *testing.T) {
	wire, recvWire := &testTransport{}, &testTransport{}
	peer, err := NewEncryptedTransport(wire, "synthetic-diagnostic-secret", "synthetic-context", true)
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewEncryptedTransport(recvWire, "synthetic-diagnostic-secret", "synthetic-context", false)
	if err != nil {
		t.Fatal(err)
	}
	delivered := 0
	client.Receive(func([]byte) { delivered++ })
	if err := peer.Send([]byte("synthetic return packet")); err != nil {
		t.Fatal(err)
	}
	valid := append([]byte(nil), wire.sent...)
	recvWire.deliver([]byte{1})
	bad := append([]byte(nil), valid...)
	bad[0] ^= 1
	recvWire.deliver(bad)
	bad = append([]byte(nil), valid...)
	bad[4] ^= 1
	recvWire.deliver(bad)
	bad = append([]byte(nil), valid...)
	bad[len(bad)-1] ^= 1
	recvWire.deliver(bad)
	recvWire.deliver(valid)
	recvWire.deliver(valid)
	want := EncryptedReceiveDiagnostics{Packets: 6, TooShort: 1, BadHeader: 1, WrongDirection: 1, DecryptFail: 1, ReplayDrop: 1, Success: 1}
	if got := client.ReceiveDiagnostics(); got != want || delivered != 1 {
		t.Fatalf("counters=%+v delivered=%d", got, delivered)
	}
}

func TestBatchReceiveCountersIdentifyDecodeFailure(t *testing.T) {
	for _, baseline := range []bool{false, true} {
		wire := &testTransport{}
		var tr interface {
			Transport
			Performance() BatchPerformance
		}
		if baseline {
			tr = NewBaselineV100BatchedTransport(wire)
		} else {
			tr = NewBatchedTransport(wire)
		}
		delivered := 0
		tr.Receive(func([]byte) { delivered++ })
		if tr.Performance().Frames != 0 {
			t.Fatal("no frame must not count as decode failure")
		}
		wire.deliver([]byte{0xff, 0})
		wire.deliver(encodeBatch([][]byte{[]byte("one"), []byte("two")}))
		if got := tr.Performance().BatchReceiveDiagnostics; got != (BatchReceiveDiagnostics{Frames: 2, Success: 1, Errors: 1, Packets: 2}) || delivered != 2 {
			t.Fatalf("baseline=%v counters=%+v delivered=%d", baseline, got, delivered)
		}
	}
}
