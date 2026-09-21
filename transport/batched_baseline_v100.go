// Diagnostic control from Android v1.0.0 8566f727c8238436728758f139130cef433147b7.
// Original idle Stop behavior is intentionally retained; see RETURN_PATH_AUDIT.md.
package transport

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"openflux/utils"
)

// Defaults for the coalescing layer. Tunable at runtime via env vars so the
// batch size can be matched to the channel's per-message limits without a
// rebuild (OPENFLUX_BATCH_BYTES / OPENFLUX_BATCH_COUNT / OPENFLUX_BATCH_LINGER_MS).
// BaselineV100BatchedTransport replaces the old per-packet CompressedTransport. It queues
// outgoing tunnel packets, coalesces bursts into a single framed+zstd batch per
// inner transport message, and splits batches back into packets on receive.
//
// This is the symmetric layer: client and exit node must both use it (they do,
// because main.go wraps both the same way).
type BaselineV100BatchedTransport struct {
	Transport

	queue         chan []byte
	lingerMs      int
	maxBatchBytes int
	maxBatchCount int

	running                             atomic.Bool
	receiveCounters                     batchReceiveCounters
	packets, batches, drops, sendErrors atomic.Uint64

	mu     sync.RWMutex
	userCb func([]byte)
}

func NewBaselineV100BatchedTransport(inner Transport) *BaselineV100BatchedTransport {
	return &BaselineV100BatchedTransport{
		Transport:     inner,
		queue:         make(chan []byte, batchQueueDepth),
		lingerMs:      envInt("OPENFLUX_BATCH_LINGER_MS", defaultLingerMs),
		maxBatchBytes: envInt("OPENFLUX_BATCH_BYTES", defaultMaxBatchBytes),
		maxBatchCount: envInt("OPENFLUX_BATCH_COUNT", defaultMaxBatchCount),
	}
}

func (b *BaselineV100BatchedTransport) Start() error {
	if err := b.Transport.Start(); err != nil {
		return err
	}
	b.running.Store(true)
	go b.flushLoop()
	return nil
}

func (b *BaselineV100BatchedTransport) Stop() error {
	b.running.Store(false)
	return b.Transport.Stop()
}

// Send copies the packet (the caller's buffer is reused by gVisor) and enqueues
// it for batching. A full queue drops the packet; the tunnel's TCP will
// retransmit, same as the old "write queue full" behavior.
func (b *BaselineV100BatchedTransport) Send(data []byte) error {
	p := make([]byte, len(data))
	copy(p, data)
	select {
	case b.queue <- p:
		return nil
	default:
		b.drops.Add(1)
		return fmt.Errorf("batch queue full")
	}
}

func (b *BaselineV100BatchedTransport) Receive(callback func([]byte)) {
	b.mu.Lock()
	b.userCb = callback
	b.mu.Unlock()

	b.Transport.Receive(func(data []byte) {
		b.receiveCounters.frames.Add(1)
		pkts, err := decodeBatch(data)
		if err != nil {
			b.receiveCounters.errors.Add(1)
			utils.Debugf("[BATCH] decode error (%d bytes): %v", len(data), err)
			return
		}
		b.receiveCounters.success.Add(1)
		b.receiveCounters.packets.Add(uint64(len(pkts)))
		b.mu.RLock()
		cb := b.userCb
		b.mu.RUnlock()
		if cb == nil {
			return
		}
		for _, p := range pkts {
			cb(p)
		}
	})
}

func (b *BaselineV100BatchedTransport) flushLoop() {
	for b.running.Load() {
		first, ok := <-b.queue
		if !ok {
			return
		}
		batch := [][]byte{first}
		size := 2 + len(first)

		// Phase 1: absorb everything already queued (burst coalescing). This
		// alone collapses a window's worth of segments into one message.
	drainNow:
		for size < b.maxBatchBytes && len(batch) < b.maxBatchCount {
			select {
			case p, ok := <-b.queue:
				if !ok {
					b.recordBaselineSend(batch)
					return
				}
				batch = append(batch, p)
				size += 2 + len(p)
			default:
				break drainNow
			}
		}

		// Phase 2: brief linger to catch stragglers arriving just after the
		// burst. Negligible next to the channel RTT, but it fills batches
		// during steady bulk transfer.
		if b.lingerMs > 0 && size < b.maxBatchBytes && len(batch) < b.maxBatchCount {
			timer := time.NewTimer(time.Duration(b.lingerMs) * time.Millisecond)
		linger:
			for size < b.maxBatchBytes && len(batch) < b.maxBatchCount {
				select {
				case p, ok := <-b.queue:
					if !ok {
						timer.Stop()
						b.recordBaselineSend(batch)
						return
					}
					batch = append(batch, p)
					size += 2 + len(p)
				case <-timer.C:
					break linger
				}
			}
			timer.Stop()
		}

		b.recordBaselineSend(batch)
	}
}

func (b *BaselineV100BatchedTransport) recordBaselineSend(batch [][]byte) {
	if err := b.Transport.Send(encodeBatch(batch)); err != nil {
		b.sendErrors.Add(1)
	} else {
		b.batches.Add(1)
		b.packets.Add(uint64(len(batch)))
	}
}
func (b *BaselineV100BatchedTransport) Performance() BatchPerformance {
	n, p := b.batches.Load(), b.packets.Load()
	avg := float64(0)
	if n > 0 {
		avg = float64(p) / float64(n)
	}
	return BatchPerformance{BatchedConfig: BatchedConfig{b.maxBatchBytes, b.maxBatchCount, time.Duration(b.lingerMs) * time.Millisecond, cap(b.queue)}, QueueLen: len(b.queue), QueueDrops: b.drops.Load(), Batches: n, Packets: p, SendErrors: b.sendErrors.Load(), Average: avg, BatchReceiveDiagnostics: b.receiveCounters.snapshot()}
}
