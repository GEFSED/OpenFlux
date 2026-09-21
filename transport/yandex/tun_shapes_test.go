package yandex

import (
	"bytes"
	"testing"

	"openflux/testsupport/basev100peer"
	"openflux/testsupport/tunshapes"
	"openflux/transport"
)

// Real Volga inner parser -> Legacy -> AES -> callback, using authenticated
// frames made by the frozen v1.0.0 exit wrapper. AES authenticates bytes, not IP.
func TestAuthenticated158ByteVolgaLegacyReturnShapes(t *testing.T) {
	raw := &localCarrier{}
	client, err := transport.NewEncryptedTransport(transport.NewCompressedTransport(raw), "synthetic-tun-shape-secret", "synthetic-context", false)
	if err != nil {
		t.Fatal(err)
	}
	stats := &VolgaStats{}
	w := newBaselineWSListener(&volgaAuth{UserID: 7}, DefaultVolgaConfig(), stats, &baselineRelayClient{}, raw.deliver)
	defer w.Stop()
	peerRaw := &localCarrier{send: func(frame []byte) error {
		w.handleMessage(volgaEnvelope("SESSION", "relay", 9, []any{volgaRecord(frame)}))
		return nil
	}}
	peer, err := basev100peer.Wrap(peerRaw, "legacy", "synthetic-tun-shape-secret", "synthetic-context", true)
	if err != nil {
		t.Fatal(err)
	}
	var got []byte
	client.Receive(func(p []byte) { got = append([]byte(nil), p...) })
	for _, c := range tunshapes.Cases() {
		t.Run(c.Name, func(t *testing.T) {
			want := c.Packet()
			got = nil
			if err := peer.Send(want); err != nil {
				t.Fatal(err)
			}
			if len(got) != 158 || !bytes.Equal(want, got) {
				t.Fatal("authenticated shape changed before callback")
			}
		})
	}
	if stats.innerPackets.Load() != 4 || client.ReceiveDiagnostics().Success != 4 {
		t.Fatal("all four shapes must authenticate, including non-IP")
	}
	// Raw Volga keepalive is emitted below AES; it is not a legal authenticated
	// application control frame. The Legacy decoder removes its zero marker.
	w.handleMessage(volgaEnvelope("SESSION", "relay", 9, []any{volgaRecord([]byte{0})}))
	if client.ReceiveDiagnostics().TooShort != 1 || client.ReceiveDiagnostics().Success != 4 {
		t.Fatal("raw keepalive reached authenticated callback")
	}
}
