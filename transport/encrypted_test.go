package transport

import (
	"bytes"
	"net"
	"sync"
	"testing"
	"time"
)

type testTransport struct {
	mu       sync.Mutex
	receiver func([]byte)
	sent     []byte
}

func (t *testTransport) Start() error                  { return nil }
func (t *testTransport) Stop() error                   { return nil }
func (t *testTransport) IsConnected() bool             { return true }
func (t *testTransport) Stats() TransportStats         { return TransportStats{} }
func (t *testTransport) Receive(callback func([]byte)) { t.receiver = callback }
func (t *testTransport) Send(data []byte) error {
	t.mu.Lock()
	t.sent = append([]byte(nil), data...)
	t.mu.Unlock()
	return nil
}
func (t *testTransport) deliver(data []byte) {
	if t.receiver != nil {
		t.receiver(data)
	}
}

// lastSent reads sent under the lock, for use from tests that (unlike the
// others in this file) touch it concurrently with a goroutine still calling
// Send, e.g. while an in-flight ResolveDNS request is pending.
func (t *testTransport) lastSent() []byte {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.sent
}

func waitFor(t *testing.T, timeout time.Duration, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("timed out waiting for condition")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestEncryptedTransportRoundTrip(t *testing.T) {
	clientWire := &testTransport{}
	exitWire := &testTransport{}
	client, err := NewEncryptedTransport(clientWire, "a sufficiently long shared secret", "document", false)
	if err != nil {
		t.Fatal(err)
	}
	exitNode, err := NewEncryptedTransport(exitWire, "a sufficiently long shared secret", "document", true)
	if err != nil {
		t.Fatal(err)
	}

	want := []byte("private IPv4 packet")
	var got []byte
	exitNode.Receive(func(data []byte) { got = append([]byte(nil), data...) })
	if err := client.Send(want); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(clientWire.sent, want) {
		t.Fatal("ciphertext contains plaintext")
	}
	exitWire.deliver(clientWire.sent)
	if !bytes.Equal(got, want) {
		t.Fatalf("received %q, want %q", got, want)
	}

	reply := []byte("private response")
	got = nil
	client.Receive(func(data []byte) { got = append([]byte(nil), data...) })
	if err := exitNode.Send(reply); err != nil {
		t.Fatal(err)
	}
	clientWire.deliver(exitWire.sent)
	if !bytes.Equal(got, reply) {
		t.Fatalf("received %q, want %q", got, reply)
	}
}

func TestEncryptedTransportRejectsWrongKeyTamperingAndReplay(t *testing.T) {
	wire := &testTransport{}
	client, err := NewEncryptedTransport(wire, "first sufficiently long secret", "document", false)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Send([]byte("packet")); err != nil {
		t.Fatal(err)
	}

	wrongWire := &testTransport{}
	wrongExit, err := NewEncryptedTransport(wrongWire, "other sufficiently long secret", "document", true)
	if err != nil {
		t.Fatal(err)
	}
	called := 0
	wrongExit.Receive(func([]byte) { called++ })
	wrongWire.deliver(wire.sent)
	if called != 0 {
		t.Fatal("wrong key was accepted")
	}

	rightWire := &testTransport{}
	rightExit, err := NewEncryptedTransport(rightWire, "first sufficiently long secret", "document", true)
	if err != nil {
		t.Fatal(err)
	}
	rightExit.Receive(func([]byte) { called++ })
	tampered := append([]byte(nil), wire.sent...)
	tampered[len(tampered)-1] ^= 1
	rightWire.deliver(tampered)
	if called != 0 {
		t.Fatal("tampered packet was accepted")
	}
	rightWire.deliver(wire.sent)
	rightWire.deliver(wire.sent)
	if called != 1 {
		t.Fatalf("replayed packet delivered %d times, want 1", called)
	}
}

func TestEncryptedTransportRequiresStrongSecret(t *testing.T) {
	if _, err := NewEncryptedTransport(&testTransport{}, "too short", "document", false); err == nil {
		t.Fatal("short secret was accepted")
	}
}

func TestEncryptedTransportPingRoundTrip(t *testing.T) {
	clientWire := &testTransport{}
	exitWire := &testTransport{}
	client, err := NewEncryptedTransport(clientWire, "a sufficiently long shared secret", "document", false)
	if err != nil {
		t.Fatal(err)
	}
	exitNode, err := NewEncryptedTransport(exitWire, "a sufficiently long shared secret", "document", true)
	if err != nil {
		t.Fatal(err)
	}
	client.Receive(func([]byte) {})
	exitNode.Receive(func([]byte) {})
	if err := client.Ping(); err != nil {
		t.Fatal(err)
	}
	exitWire.deliver(clientWire.sent)
	clientWire.deliver(exitWire.sent)
	if client.PingSequence() != 1 {
		t.Fatalf("ping sequence = %d, want 1", client.PingSequence())
	}
	if client.LastPingMillis() < 1 {
		t.Fatalf("ping = %d ms, want positive value", client.LastPingMillis())
	}
}

func TestEncryptedTransportPingCarriesCountry(t *testing.T) {
	clientWire := &testTransport{}
	exitWire := &testTransport{}
	client, err := NewEncryptedTransport(clientWire, "a sufficiently long shared secret", "document", false)
	if err != nil {
		t.Fatal(err)
	}
	exitNode, err := NewEncryptedTransport(exitWire, "a sufficiently long shared secret", "document", true)
	if err != nil {
		t.Fatal(err)
	}
	client.Receive(func([]byte) {})
	exitNode.Receive(func([]byte) {})

	if client.LastCountry() != "" {
		t.Fatalf("client country = %q before any ping, want empty", client.LastCountry())
	}

	exitNode.SetCountry("Germany")
	if err := client.Ping(); err != nil {
		t.Fatal(err)
	}
	exitWire.deliver(clientWire.sent)
	clientWire.deliver(exitWire.sent)

	if client.PingSequence() != 1 {
		t.Fatalf("ping sequence = %d, want 1", client.PingSequence())
	}
	if got := client.LastCountry(); got != "Germany" {
		t.Fatalf("client country = %q, want %q", got, "Germany")
	}
}

// TestEncryptedTransportResolveDNSRoundTrip exercises the client -> exit node
// -> upstream UDP -> exit node -> client relay end to end: ResolveDNS on the
// client blocks in a goroutine while a fake "upstream" UDP server answers,
// exactly mirroring how the exit node relays a real resolver.
func TestEncryptedTransportResolveDNSRoundTrip(t *testing.T) {
	upstream, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer upstream.Close()
	canned := []byte("fake dns answer bytes")
	go func() {
		buf := make([]byte, 512)
		n, addr, err := upstream.ReadFrom(buf)
		if err != nil || n == 0 {
			return
		}
		_, _ = upstream.WriteTo(canned, addr)
	}()

	clientWire := &testTransport{}
	exitWire := &testTransport{}
	client, err := NewEncryptedTransport(clientWire, "a sufficiently long shared secret", "document", false)
	if err != nil {
		t.Fatal(err)
	}
	exitNode, err := NewEncryptedTransport(exitWire, "a sufficiently long shared secret", "document", true)
	if err != nil {
		t.Fatal(err)
	}
	client.Receive(func([]byte) {})
	exitNode.Receive(func([]byte) {})

	type result struct {
		answer []byte
		err    error
	}
	resultCh := make(chan result, 1)
	go func() {
		answer, err := client.ResolveDNS(upstream.LocalAddr().String(), []byte("dns query bytes"))
		resultCh <- result{answer, err}
	}()

	waitFor(t, 2*time.Second, func() bool { return clientWire.lastSent() != nil })
	exitWire.deliver(clientWire.lastSent())

	waitFor(t, 2*time.Second, func() bool { return exitWire.lastSent() != nil })
	clientWire.deliver(exitWire.lastSent())

	select {
	case res := <-resultCh:
		if res.err != nil {
			t.Fatal(res.err)
		}
		if !bytes.Equal(res.answer, canned) {
			t.Fatalf("answer = %q, want %q", res.answer, canned)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ResolveDNS did not return")
	}
}

func TestEncryptedTransportResolveDNSTimesOutWithoutAnswer(t *testing.T) {
	clientWire := &testTransport{}
	client, err := NewEncryptedTransport(clientWire, "a sufficiently long shared secret", "document", false)
	if err != nil {
		t.Fatal(err)
	}
	client.Receive(func([]byte) {})

	if _, err := client.ResolveDNS("127.0.0.1:1", []byte("query")); err == nil {
		t.Fatal("expected a timeout error when nobody answers")
	}
}
