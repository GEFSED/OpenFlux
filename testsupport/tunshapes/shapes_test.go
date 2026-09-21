package tunshapes

import (
	"crypto/sha256"
	"fmt"
	"testing"
)

func TestSharedPacketShapes(t *testing.T) {
	for _, c := range Cases() {
		t.Run(c.Name, func(t *testing.T) {
			p := c.Packet()
			if fmt.Sprintf("%x", sha256.Sum256(p)) != c.SHA256 {
				t.Fatal("synthetic corpus differs from Java reference")
			}
			if c.Version == 4 && checksum(p[:20]) != 0 {
				t.Fatal("IPv4 header checksum")
			}
		})
	}
}
