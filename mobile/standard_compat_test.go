package mobile

import (
	"bytes"
	"openflux/testsupport/basev100peer"
	"openflux/transport"
	"testing"
	"time"
)

// Frozen 8566f727 peer, independent of the preserved Standard implementation.
func TestStandardAndBatchedCompatibility(t *testing.T) {
	for _, mode := range []string{"standard", "speed", "optimized"} {
		for _, codec := range []string{"legacy", "batched"} {
			if mode != "standard" && codec == "legacy" {
				continue
			} // tested against 081d214 separately
			for _, secret := range []string{"", "synthetic-standard-key"} {
				t.Run(mode+"/"+codec+"/"+secret, func(t *testing.T) {
					a, b := &packetPeer{}, &packetPeer{}
					a.other, b.other = b, a
					var tr transport.Transport
					var err error
					if mode == "standard" {
						tr, err = wrapStandardTransport(a, "vyandex", "synthetic-context", secret, codec)
					} else {
						s := newPacketSession(mode, "vyandex", 8)
						tr, err = wrapModeTransport(s, a, "synthetic-context", secret, codec)
					}
					if err != nil {
						t.Fatal(err)
					}
					peer, err := basev100peer.Wrap(b, codec, secret, "synthetic-context", true)
					if err != nil {
						t.Fatal(err)
					}
					arrived := make(chan []byte, 4)
					tr.Receive(func(p []byte) { arrived <- append([]byte(nil), p...) })
					peer.Receive(func(p []byte) { arrived <- append([]byte(nil), p...) })
					if err := peer.Start(); err != nil {
						t.Fatal(err)
					}
					defer peer.Stop()
					if err := tr.Start(); err != nil {
						t.Fatal(err)
					}
					defer tr.Stop()
					payload := bytes.Repeat([]byte{0x45, 1, 2, 3}, 350)
					for _, sender := range []transport.Transport{tr, peer} {
						if err := sender.Send(payload); err != nil {
							t.Fatal(err)
						}
						select {
						case got := <-arrived:
							if !bytes.Equal(got, payload) {
								t.Fatal("ordinary base compatibility changed")
							}
						case <-time.After(2 * time.Second):
							t.Fatal("peer rejected frame")
						}
					}
				})
			}
		}
	}
}
