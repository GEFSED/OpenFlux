package transport

import "sync/atomic"

type batchReceiveCounters struct{ frames, success, errors, packets atomic.Uint64 }

// BatchReceiveDiagnostics counts codec boundaries; it never records content.
type BatchReceiveDiagnostics struct {
	Frames  uint64 `json:"outer_receive_frames"`
	Success uint64 `json:"outer_decode_success"`
	Errors  uint64 `json:"outer_decode_errors"`
	Packets uint64 `json:"outer_packets_decoded"`
}

func (c *batchReceiveCounters) snapshot() BatchReceiveDiagnostics {
	return BatchReceiveDiagnostics{c.frames.Load(), c.success.Load(), c.errors.Load(), c.packets.Load()}
}
