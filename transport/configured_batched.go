package transport

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"openflux/utils"
)

// ConfiguredBatchedTransport replaces the old per-packet CompressedTransport. It queues
// outgoing tunnel packets, coalesces bursts into a single framed+zstd batch per
// inner transport message, and splits batches back into packets on receive.
//
// This is the symmetric layer: client and exit node must both use it (they do,
// because main.go wraps both the same way).
type ConfiguredBatchedTransport struct {
	Transport

	queue         chan []byte
	linger        time.Duration
	maxBatchBytes int
	maxBatchCount int
	done          chan struct{}
	wg            sync.WaitGroup
	lifecycle     sync.Mutex
	enqueueMu     sync.Mutex
	packets       atomic.Uint64
	batches       atomic.Uint64
	drops         atomic.Uint64
	sendErrors    atomic.Uint64

	running         atomic.Bool
	receiveCounters batchReceiveCounters

	mu     sync.RWMutex
	userCb func([]byte)
}

// BatchedConfig controls scheduling only; framing and compression are unchanged.
type BatchedConfig struct {
	MaxBatchBytes int           `json:"max_batch_bytes"`
	MaxBatchCount int           `json:"max_batch_count"`
	Linger        time.Duration `json:"linger_ns"`
	QueueDepth    int           `json:"queue_cap"`
}

// DefaultBatchedConfig is independent of environment variables for mobile use.
func DefaultBatchedConfig() BatchedConfig {
	return BatchedConfig{defaultMaxBatchBytes, defaultMaxBatchCount, defaultLingerMs * time.Millisecond, batchQueueDepth}
}

func NewBatchedTransportWithConfig(inner Transport, cfg BatchedConfig) (*ConfiguredBatchedTransport, error) {
	if inner == nil || cfg.MaxBatchBytes < 1 || cfg.MaxBatchBytes > 8<<20 || cfg.MaxBatchCount < 1 || cfg.MaxBatchCount > 65535 || cfg.QueueDepth < 1 || cfg.QueueDepth > 1000000 || cfg.Linger < 0 || cfg.Linger > time.Second {
		return nil, fmt.Errorf("invalid batching configuration")
	}
	return newConfiguredBatchedTransport(inner, cfg), nil
}

func newConfiguredBatchedTransport(inner Transport, cfg BatchedConfig) *ConfiguredBatchedTransport {
	return &ConfiguredBatchedTransport{
		Transport:     inner,
		queue:         make(chan []byte, cfg.QueueDepth),
		linger:        cfg.Linger,
		maxBatchBytes: cfg.MaxBatchBytes,
		maxBatchCount: cfg.MaxBatchCount,
	}
}

func (b *ConfiguredBatchedTransport) Start() error {
	b.lifecycle.Lock()
	defer b.lifecycle.Unlock()
	if b.running.Load() {
		return nil
	}
	if err := b.Transport.Start(); err != nil {
		return err
	}
	b.done = make(chan struct{})
	b.running.Store(true)
	b.wg.Add(1)
	go func() { defer b.wg.Done(); b.flushLoop() }()
	return nil
}

func (b *ConfiguredBatchedTransport) Stop() error {
	b.lifecycle.Lock()
	defer b.lifecycle.Unlock()
	b.enqueueMu.Lock()
	wasRunning := b.running.Swap(false)
	b.enqueueMu.Unlock()
	if !wasRunning {
		return nil
	}
	close(b.done)
	err := b.Transport.Stop() // unblock an inner Send before joining
	b.wg.Wait()
	for {
		select {
		case <-b.queue:
		default:
			return err
		}
	}
}

// Send copies the packet (the caller's buffer is reused by gVisor) and enqueues
// it for batching. A full queue drops the packet; the tunnel's TCP will
// retransmit, same as the old "write queue full" behavior.
func (b *ConfiguredBatchedTransport) Send(data []byte) error {
	b.enqueueMu.Lock()
	defer b.enqueueMu.Unlock()
	if !b.running.Load() {
		return fmt.Errorf("batch transport stopped")
	}
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

func (b *ConfiguredBatchedTransport) Receive(callback func([]byte)) {
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

func (b *ConfiguredBatchedTransport) flushLoop() {
	for b.running.Load() {
		var first []byte
		select {
		case <-b.done:
			return
		case first = <-b.queue:
		}
		batch := [][]byte{first}
		size := 2 + len(first)

		// Phase 1: absorb everything already queued (burst coalescing). This
		// alone collapses a window's worth of segments into one message.
	drainNow:
		for size < b.maxBatchBytes && len(batch) < b.maxBatchCount {
			select {
			case <-b.done:
				return
			case p, ok := <-b.queue:
				if !ok {
					b.Transport.Send(encodeBatch(batch))
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
		if b.linger > 0 && size < b.maxBatchBytes && len(batch) < b.maxBatchCount {
			timer := time.NewTimer(b.linger)
		linger:
			for size < b.maxBatchBytes && len(batch) < b.maxBatchCount {
				select {
				case <-b.done:
					timer.Stop()
					return
				case p, ok := <-b.queue:
					if !ok {
						timer.Stop()
						b.Transport.Send(encodeBatch(batch))
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

		if err := b.Transport.Send(encodeBatch(batch)); err != nil {
			b.sendErrors.Add(1)
		} else {
			b.batches.Add(1)
			b.packets.Add(uint64(len(batch)))
		}
	}
}

// BatchPerformance contains counters only, never payloads or credentials.
type BatchPerformance struct {
	BatchReceiveDiagnostics
	BatchedConfig
	QueueLen   int     `json:"queue_len"`
	QueueDrops uint64  `json:"queue_drops"`
	Batches    uint64  `json:"batches"`
	Packets    uint64  `json:"packets"`
	SendErrors uint64  `json:"send_errors"`
	Average    float64 `json:"avg_packets_per_batch"`
}

func (b *ConfiguredBatchedTransport) Performance() BatchPerformance {
	n, p := b.batches.Load(), b.packets.Load()
	avg := float64(0)
	if n > 0 {
		avg = float64(p) / float64(n)
	}
	return BatchPerformance{BatchedConfig: BatchedConfig{b.maxBatchBytes, b.maxBatchCount, b.linger, cap(b.queue)}, QueueLen: len(b.queue), QueueDrops: b.drops.Load(), Batches: n, Packets: p, SendErrors: b.sendErrors.Load(), Average: avg, BatchReceiveDiagnostics: b.receiveCounters.snapshot()}
}
