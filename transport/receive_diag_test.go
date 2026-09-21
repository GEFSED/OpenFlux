package transport

import (
	"bytes"
	"testing"

	d "universal-bypass-tool/internal/receivediag"
)

// The production v0.6.0 CLI constructs Compressed(Encrypted(Volga)).
// Keep that order here: receive is Volga -> AES -> Legacy -> callback.
func TestProductionReceiveBoundaries(t *testing.T) {
	const secret = "synthetic diagnostic secret only"
	for _, name := range []string{"compressed", "raw_marker", "legacy_fallback", "aes_short", "aes_header", "aes_direction", "aes_decrypt", "aes_replay", "reversed_wrappers"} {
		t.Run(name, func(t *testing.T) {
			clientWire, exitWire := &testTransport{}, &testTransport{}
			client, err := NewEncryptedTransport(clientWire, secret, "synthetic-context", false)
			if err != nil {
				t.Fatal(err)
			}
			exit, err := NewEncryptedTransport(exitWire, secret, "synthetic-context", true)
			if err != nil {
				t.Fatal(err)
			}
			packet := bytes.Repeat([]byte{0x45}, 600)
			if name == "raw_marker" {
				packet = packet[:158]
			}
			legacy := compress(packet)
			if name == "compressed" && legacy[0] != CompressionMarker {
				t.Fatal("fixture did not compress")
			}
			if name == "raw_marker" && legacy[0] != 0 {
				t.Fatal("fixture is not raw marker")
			}
			if name == "legacy_fallback" {
				legacy = []byte{0x1f, 1, 2, 3, 4, 5}
				packet = legacy
			}
			if err := client.Send(legacy); err != nil {
				t.Fatal(err)
			}
			frame := append([]byte(nil), clientWire.sent...)
			want := map[d.Metric]uint64{d.AESPackets: 1}
			accepted := false
			switch name {
			case "aes_short":
				frame = []byte{0}
				want[d.AESTooShort] = 1
			case "aes_header":
				frame[0] ^= 1
				want[d.AESBadMagic] = 1
			case "aes_direction":
				frame[4] = 1
				want[d.AESWrongDirection] = 1
			case "aes_decrypt":
				frame[len(frame)-1] ^= 1
				want[d.AESDecryptFail] = 1
			case "reversed_wrappers":
				// Reproduce the opposite client ordering without changing the exit.
				reversed, err := NewEncryptedTransport(NewCompressedTransport(clientWire), secret, "synthetic-context", false)
				if err != nil {
					t.Fatal(err)
				}
				if err := reversed.Send(packet); err != nil {
					t.Fatal(err)
				}
				frame = clientWire.sent
				want[d.AESBadMagic] = 1
			default:
				accepted = true
				want[d.AESDecryptSuccess], want[d.AESPlaintextBytes] = 1, uint64(len(legacy))
				want[d.LegacyFrames], want[d.LegacyBytes] = 1, uint64(len(packet))
				if name == "legacy_fallback" {
					want[d.LegacyErrors], want[d.LegacyFallback] = 1, 1
				} else {
					want[d.LegacySuccess] = 1
				}
				if name == "aes_replay" {
					want[d.AESPackets], want[d.AESReplayDrop] = 2, 1
				}
			}
			var got []byte
			calls := 0
			NewCompressedTransport(exit).Receive(func(p []byte) { calls++; got = append([]byte(nil), p...) })
			before := d.Default.Snapshot()
			exitWire.deliver(frame)
			if name == "aes_replay" {
				exitWire.deliver(frame)
			}
			after := d.Default.Snapshot()
			for i, n := range after.Values {
				if delta := n - before.Values[i]; delta != want[d.Metric(i)] {
					t.Errorf("%s delta=%d want=%d", d.Names[i], delta, want[d.Metric(i)])
				}
			}
			if accepted {
				if calls != 1 || !bytes.Equal(got, packet) {
					t.Fatal("accepted frame changed or callback count wrong")
				}
			} else if calls != 0 {
				t.Fatal("rejected frame reached callback")
			}
		})
	}
}
