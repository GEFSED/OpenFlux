package mobile

import (
	"bytes"
	"encoding/binary"
	"testing"

	"openflux/testsupport/productionv060peer"
	"openflux/transport"
)

const productionTestSecret = "synthetic-production-compat-secret"
const productionTestContext = "synthetic-production-compat-context"

type productionCarrier struct {
	packetPeer
	beforeSend func([]byte)
}

func (p *productionCarrier) Send(data []byte) error {
	if p.beforeSend != nil {
		p.beforeSend(data) // Inspect the actual wire before delivering it to the peer.
	}
	return p.packetPeer.Send(data)
}

func syntheticProductionPacket(size int) []byte {
	p := make([]byte, size)
	p[0], p[8], p[9] = 0x45, 64, 6
	binary.BigEndian.PutUint16(p[2:4], uint16(size))
	return p
}

// Both modes run the real Android wrapper and finishStart, replacing only Volga.
// The peer has independent frozen production AES/Legacy code, not our helper.
func TestProductionV060LegacyCompatibility(t *testing.T) {
	for _, profile := range []string{"speed", "optimized"} {
		for _, encrypted := range []bool{true, false} {
			label := "plain"
			secret := ""
			if encrypted {
				label, secret = "AES", productionTestSecret
			}
			t.Run(profile+"/"+label, func(t *testing.T) {
				a, b := &productionCarrier{}, &productionCarrier{}
				a.other, b.other = &b.packetPeer, &a.packetPeer
				var clientWire, exitWire [][]byte
				a.beforeSend = func(frame []byte) {
					if encrypted && !bytes.HasPrefix(frame, []byte{'O', 'F', 'X', 1, 0}) {
						t.Fatal("client wire frame must begin with OFX/version before the production exit")
					}
					clientWire = append(clientWire, append([]byte(nil), frame...))
				}
				b.beforeSend = func(frame []byte) {
					if encrypted && !bytes.HasPrefix(frame, []byte{'O', 'F', 'X', 1, 1}) {
						t.Fatal("production return must begin with OFX/version")
					}
					exitWire = append(exitWire, append([]byte(nil), frame...))
				}
				var authenticated, decoded, callbacks int
				var expected []byte
				peer, err := productionv060peer.WrapLegacyObserved(b, secret, productionTestContext, true, func(legacy []byte) {
					// This tap is AFTER frozen AEAD.Open succeeded when AES is on.
					authenticated++
					plain, err := productionv060peer.DecodeLegacy(legacy)
					if err != nil || !bytes.Equal(plain, expected) {
						t.Fatal("production Legacy decode failed; fallback is not success")
					}
					marker := byte(0)
					if len(expected) > 200 {
						marker = 0x1f
					}
					if len(legacy) == 0 || legacy[0] != marker {
						t.Fatal("expected raw/LZ4 Legacy marker was not exercised")
					}
					decoded++
				})
				if err != nil {
					t.Fatal(err)
				}
				peer.Receive(func(p []byte) {
					if !bytes.Equal(p, expected) {
						t.Fatal("production callback payload differs")
					}
					callbacks++
				})
				if err := peer.Start(); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = peer.Stop() })
				session := newPacketSession(profile, "vyandex", 8)
				installSession(session)
				t.Cleanup(Stop)
				tr, err := wrapModeTransport(session, a, productionTestContext, secret, "legacy")
				if err != nil {
					t.Fatal(err)
				}
				if err := finishStart(session, tr); err != "" {
					t.Fatal(err)
				}
				// Exercise raw Legacy marker and actual LZ4 compression.
				for _, size := range []int{158, 1400} {
					expected = syntheticProductionPacket(size)
					before := callbacks
					if err := Send(expected); err != "" {
						t.Fatal(err)
					}
					if callbacks != before+1 || authenticated != callbacks || decoded != callbacks {
						t.Fatal("production did not accept AES -> Legacy -> callback")
					}
					if err := peer.Send(expected); err != nil {
						t.Fatal(err)
					}
					if got := Read(); !bytes.Equal(got, expected) {
						t.Fatal("production return did not reach actual mobile callback/queue")
					}
				}
				if len(clientWire) != 2 || len(exitWire) != 2 {
					t.Fatal("expected two frames in each direction")
				}
				if session.callbackPackets != 2 || session.enqueued != 2 {
					t.Fatal("profile callback/enqueue counts")
				}
			})
		}
	}
}

func TestProductionV060RejectsOldLegacyAESOrder(t *testing.T) {
	a, b := &productionCarrier{}, &packetPeer{}
	a.other, b.other = b, &a.packetPeer
	var wire []byte
	a.beforeSend = func(p []byte) { wire = append([]byte(nil), p...) }
	// Negative fixture: the actual broken 36aee530 construction.
	broken, err := transport.NewEncryptedTransport(transport.NewCompressedTransport(a), productionTestSecret, productionTestContext, false)
	if err != nil {
		t.Fatal(err)
	}
	var postAES, callbacks int
	peer, err := productionv060peer.WrapLegacyObserved(b, productionTestSecret, productionTestContext, true, func([]byte) { postAES++ })
	if err != nil {
		t.Fatal(err)
	}
	peer.Receive(func([]byte) { callbacks++ })
	packet := syntheticProductionPacket(158)
	if err := broken.Send(packet); err != nil {
		t.Fatal(err)
	}
	if len(wire) < 33 || (wire[0] != 0 && wire[0] != 0x1f) || bytes.HasPrefix(wire, []byte{'O', 'F', 'X', 1}) {
		t.Fatal("negative fixture does not reproduce Legacy outside AES")
	}
	if postAES != 0 || callbacks != 0 {
		t.Fatal("production accepted the broken header")
	}
	// Show it is the outer Legacy framing, not the secret or AEAD: after
	// removing that framing, independent production AES accepts the SAME frame.
	unwrapped, err := productionv060peer.DecodeLegacy(wire)
	if err != nil || !bytes.HasPrefix(unwrapped, []byte{'O', 'F', 'X', 1, 0}) {
		t.Fatal("negative fixture did not hide OFX inside Legacy")
	}
	probeRaw := &packetPeer{}
	probe, err := productionv060peer.NewEncryptedTransport(probeRaw, productionTestSecret, productionTestContext, true)
	if err != nil {
		t.Fatal(err)
	}
	var recovered []byte
	probe.Receive(func(p []byte) { recovered = append([]byte(nil), p...) })
	probeRaw.cb(unwrapped)
	if !bytes.Equal(recovered, packet) {
		t.Fatal("negative fixture had an unrelated AES/key failure")
	}
}
