package tunnel

import (
	"encoding/binary"
	"testing"

	d "universal-bypass-tool/internal/receivediag"
	"universal-bypass-tool/transport"
)

type diagWire struct {
	receiver func([]byte)
	sent     []byte
}

func (*diagWire) Start() error                    { return nil }
func (*diagWire) Stop() error                     { return nil }
func (*diagWire) IsConnected() bool               { return true }
func (*diagWire) Stats() transport.TransportStats { return transport.TransportStats{} }
func (w *diagWire) Send(p []byte) error           { w.sent = append([]byte(nil), p...); return nil }
func (w *diagWire) Receive(f func([]byte))        { w.receiver = f }

func TestAuthenticatedLegacyReachesActualProxyCallback(t *testing.T) {
	clientWire, exitWire := &diagWire{}, &diagWire{}
	const secret = "synthetic diagnostic secret only"
	clientAES, err := transport.NewEncryptedTransport(clientWire, secret, "synthetic-context", false)
	if err != nil {
		t.Fatal(err)
	}
	exitAES, err := transport.NewEncryptedTransport(exitWire, secret, "synthetic-context", true)
	if err != nil {
		t.Fatal(err)
	}
	tun := NewTCPTunnelMode(transport.NewCompressedTransport(exitAES), true, ExitModeProxy)
	defer tun.gvisorStack.Close()
	// Synthetic IPv4 ICMP, not handled by the TCP-only proxy: no external dial.
	packet := make([]byte, 28)
	packet[0], packet[8], packet[9] = 0x45, 64, 1
	binary.BigEndian.PutUint16(packet[2:4], uint16(len(packet)))
	before := d.Default.Snapshot()
	if err := transport.NewCompressedTransport(clientAES).Send(packet); err != nil {
		t.Fatal(err)
	}
	exitWire.receiver(clientWire.sent)
	after := d.Default.Snapshot()
	want := map[d.Metric]uint64{d.AESPackets: 1, d.AESDecryptSuccess: 1, d.AESPlaintextBytes: 29, d.LegacyFrames: 1, d.LegacySuccess: 1, d.LegacyBytes: 28, d.ProxyPackets: 1, d.ProxyBytes: 28}
	for i, n := range after.Values {
		if n-before.Values[i] != want[d.Metric(i)] {
			t.Errorf("unexpected %s delta", d.Names[i])
		}
	}
}
