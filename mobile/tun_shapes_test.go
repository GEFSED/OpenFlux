package mobile

import (
	"bytes"
	"testing"

	"openflux/testsupport/productionv060peer"
	"openflux/testsupport/tunshapes"
)

func TestAuthenticated158ByteLegacyShapesReachActualMobileCallback(t *testing.T) {
	EnablePerformanceLab()
	baselineActive.Store(true)
	defer baselineActive.Store(false)
	a, b := &packetPeer{}, &packetPeer{}
	a.other = b
	b.other = a
	if err := baselineStartWithCarrier("vyandex", "synthetic-context", "synthetic-tun-shape-secret", "legacy", "", "", a); err != "" {
		t.Fatal(err)
	}
	defer Stop()
	peer, err := productionv060peer.WrapLegacy(b, "synthetic-tun-shape-secret", "synthetic-context", true)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range tunshapes.Cases() {
		t.Run(c.Name, func(t *testing.T) {
			want := c.Packet()
			if err := peer.Send(want); err != nil {
				t.Fatal(err)
			}
			got := Read()
			if len(got) != 158 || !bytes.Equal(want, got) {
				t.Fatal("mobile altered an authenticated shape")
			}
		})
	}
	baselineClient.mu.Lock()
	defer baselineClient.mu.Unlock()
	if baselineClient.encrypted.ReceiveDiagnostics().Success != 4 || baselineClient.callbackPackets != 4 || baselineClient.callbackBytes != 632 || baselineClient.enqueued != 4 {
		t.Fatal("wrong authenticated callback counts")
	}
}
