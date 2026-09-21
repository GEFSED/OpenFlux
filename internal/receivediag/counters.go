// Package receivediag provides process-local, payload-free receive diagnostics.
// It is specific to the diagnostic exit build; it creates no workers or queues.
package receivediag

import (
	"strconv"
	"strings"
	"sync/atomic"
)

type Metric int

const (
	VolgaFrames Metric = iota
	VolgaBytes
	LegacyFrames
	LegacySuccess
	LegacyFallback
	LegacyErrors
	LegacyBytes
	AESPackets
	AESTooShort
	AESBadMagic
	AESWrongDirection
	AESDecryptFail
	AESDecryptSuccess
	AESPlaintextBytes
	AESReplayDrop
	ProxyPackets
	ProxyBytes
	metricCount
)

var Names = [...]string{
	"diag_volga_payload_frames", "diag_volga_payload_bytes",
	"diag_legacy_receive_frames", "diag_legacy_decode_success",
	"diag_legacy_decode_fallback", "diag_legacy_decode_errors", "diag_legacy_output_bytes",
	"diag_aes_receive_packets", "diag_aes_too_short", "diag_aes_bad_magic",
	"diag_aes_wrong_direction", "diag_aes_decrypt_fail", "diag_aes_decrypt_success",
	"diag_aes_plaintext_bytes", "diag_aes_replay_drop",
	"diag_proxy_callback_packets", "diag_proxy_callback_bytes",
}

type Layer uint32

const (
	None Layer = iota
	Volga
	Legacy
	AESTooShortLayer
	AESHeader
	AESDirection
	AESDecrypt
	Proxy
)

var layerNames = [...]string{"none", "volga", "legacy", "aes_too_short", "aes_header", "aes_direction", "aes_decrypt", "proxy"}

type Counters struct {
	values [metricCount]atomic.Uint64
	first  atomic.Uint32
}

var Default Counters

func (c *Counters) Add(metric Metric, n int) { c.values[metric].Add(uint64(n)) }
func (c *Counters) Fail(layer Layer)         { c.first.CompareAndSwap(0, uint32(layer)) }

// Snapshot is race-safe, but concurrent increments can straddle a snapshot.
// Use settled snapshots and deltas for the two phone tests. No reset is exposed.
type Snapshot struct {
	Values [metricCount]uint64
	First  Layer
}

func (c *Counters) Snapshot() Snapshot {
	var s Snapshot
	for i := range s.Values {
		s.Values[i] = c.values[i].Load()
	}
	s.First = Layer(c.first.Load())
	return s
}

// Line has a closed schema: integers and a fixed enum only, never caller data.
func (c *Counters) Line() string {
	s := c.Snapshot()
	var b strings.Builder
	for i, name := range Names {
		if i != 0 {
			b.WriteByte(' ')
		}
		b.WriteString(name)
		b.WriteByte('=')
		b.WriteString(strconv.FormatUint(s.Values[i], 10))
	}
	b.WriteString(" first_failure_layer=")
	b.WriteString(layerNames[s.First])
	return b.String()
}
