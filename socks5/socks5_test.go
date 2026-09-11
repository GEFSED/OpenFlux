package socks5

import (
	"errors"
	"net"
	"testing"
	"time"
)

type unusedDialer struct{}

func (unusedDialer) DialTCP(string) (net.Conn, error) { return nil, nil }

func TestCloseClosesListener(t *testing.T) {
	server := NewSOCKS5Server("127.0.0.1:0", unusedDialer{})
	result := make(chan error, 1)
	go func() { result <- server.Start() }()

	deadline := time.Now().Add(time.Second)
	for {
		server.mu.Lock()
		ready := server.listener != nil
		server.mu.Unlock()
		if ready {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("listener did not start")
		}
		time.Sleep(time.Millisecond)
	}

	if err := server.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	select {
	case err := <-result:
		if !errors.Is(err, net.ErrClosed) {
			t.Fatalf("Start() after Close() error = %v, want net.ErrClosed", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Start() did not return after Close()")
	}
}

func TestCloseBeforeStart(t *testing.T) {
	server := NewSOCKS5Server("127.0.0.1:0", unusedDialer{})
	if err := server.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if err := server.Start(); err == nil {
		t.Fatal("Start() after Close() unexpectedly succeeded")
	}
}
