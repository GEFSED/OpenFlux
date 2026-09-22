package yandex

import (
	"bytes"
	"testing"

	"openflux/testsupport/productionv060peer"
	"openflux/testsupport/tunshapes"
	"openflux/transport"
)

// Real Volga inner parser -> AES -> Legacy -> callback, using authenticated
// frames made by the frozen production v0.6.0 exit wrapper. AES authenticates bytes, not IP.
func TestAuthenticated158ByteVolgaLegacyReturnShapes(t *testing.T) {
	raw := &localCarrier{}
	aes, err := transport.NewEncryptedTransport(raw, "synthetic-tun-shape-secret", "synthetic-context", false)
	if err != nil {
		t.Fatal(err)
	}
	client := transport.NewCompressedTransport(aes)
	stats := &VolgaStats{}
	w := newBaselineWSListener(&volgaAuth{UserID: 7}, DefaultVolgaConfig(), stats, &baselineRelayClient{}, raw.deliver)
	defer w.Stop()
	peerRaw := &localCarrier{send: func(frame []byte) error {
		w.handleMessage(volgaEnvelope("SESSION", "relay", 9, []any{volgaRecord(frame)}))
		return nil
	}}
	peer, err := productionv060peer.WrapLegacy(peerRaw, "synthetic-tun-shape-secret", "synthetic-context", true)
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
	if stats.innerPackets.Load() != 4 || aes.ReceiveDiagnostics().Success != 4 {
		t.Fatal("all four shapes must authenticate, including non-IP")
	}
	// Raw Volga keepalive is emitted below AES; it is not a legal authenticated
	// application control frame. AES rejects it before Legacy is reached.
	w.handleMessage(volgaEnvelope("SESSION", "relay", 9, []any{volgaRecord([]byte{0})}))
	if aes.ReceiveDiagnostics().TooShort != 1 || aes.ReceiveDiagnostics().Success != 4 {
		t.Fatal("raw keepalive reached authenticated callback")
	}
}
