// Package tunshapes defines synthetic packet shapes shared by Go carrier tests
// and Java pre-TUN classification tests. No real traffic or credentials.
package tunshapes

import (
	_ "embed"
	"encoding/binary"
	"encoding/json"
)

//go:embed cases.json
var casesJSON []byte

type Case struct {
	SHA256         string `json:"sha256"`
	Name           string `json:"name"`
	Version        int    `json:"version"`
	BufferLen      int    `json:"buffer_len"`
	TotalLength    int    `json:"total_length"`
	ValidIPv4      bool   `json:"valid_ipv4"`
	LengthRelation string `json:"length_relation"`
}

func Cases() []Case {
	var cases []Case
	if err := json.Unmarshal(casesJSON, &cases); err != nil {
		panic(err)
	}
	return cases
}

// Packet returns an owned array. A/C include complete IPv4 and TCP headers with
// valid checksums; C intentionally has extra backing bytes. D is truncated.
func (c Case) Packet() []byte {
	p := make([]byte, c.BufferLen)
	if c.Version != 4 {
		for i := range p {
			p[i] = 0x31
		}
		return p
	}
	p[0], p[8], p[9] = 0x45, 64, 6
	binary.BigEndian.PutUint16(p[2:4], uint16(c.TotalLength))
	copy(p[12:16], []byte{192, 0, 2, 1}) // TEST-NET-1, never a real packet capture
	copy(p[16:20], []byte{10, 10, 10, 2})
	binary.BigEndian.PutUint16(p[20:22], 1)
	binary.BigEndian.PutUint16(p[22:24], 2)
	p[32], p[33] = 0x50, 0x10
	if c.TotalLength <= len(p) {
		pseudo := make([]byte, 12+c.TotalLength-20)
		copy(pseudo[:8], p[12:20])
		pseudo[9] = 6
		binary.BigEndian.PutUint16(pseudo[10:12], uint16(c.TotalLength-20))
		copy(pseudo[12:], p[20:c.TotalLength])
		binary.BigEndian.PutUint16(p[36:38], checksum(pseudo))
		for i := c.TotalLength; i < len(p); i++ {
			p[i] = 0xa5
		}
	}
	binary.BigEndian.PutUint16(p[10:12], checksum(p[:20]))
	return p
}

func checksum(p []byte) uint16 {
	var sum uint32
	for i := 0; i < len(p); i += 2 {
		sum += uint32(p[i]) << 8
		if i+1 < len(p) {
			sum += uint32(p[i+1])
		}
	}
	for sum>>16 != 0 {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	return ^uint16(sum)
}
