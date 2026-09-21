package transport

import (
	"fmt"
	"os"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"openflux/utils"
)

// Defaults for the coalescing layer. Tunable at runtime via env vars so the
// batch size can be matched to the channel's per-message limits without a
// rebuild (OPENFLUX_BATCH_BYTES / OPENFLUX_BATCH_COUNT / OPENFLUX_BATCH_LINGER_MS).
const (
	defaultMaxBatchBytes = 8192
	defaultMaxBatchCount = 64
	defaultLingerMs      = 5
	batchQueueDepth      = 4096
)

// BatchedTransport replaces the old per-packet CompressedTransport. It queues
// outgoing tunnel packets, coalesces bursts into a single framed+zstd batch per
// inner transport message, and splits batches back into packets on receive.
//
// This is the symmetric layer: client and exit node must both use it (they do,
// because main.go wraps both the same way).
type BatchedTransport struct {
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

	running atomic.Bool

	mu     sync.RWMutex
	userCb func([]byte)
}

func envInt(name string, def int) int {
	if v := os.Getenv(name); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return def
}

func NewBatchedTransport(inner Transport) *BatchedTransport {
	cfg := DefaultBatchedConfig()
	cfg.Linger = time.Duration(envInt("OPENFLUX_BATCH_LINGER_MS", defaultLingerMs)) * time.Millisecond
	cfg.MaxBatchBytes = envInt("OPENFLUX_BATCH_BYTES", defaultMaxBatchBytes)
	cfg.MaxBatchCount = envInt("OPENFLUX_BATCH_COUNT", defaultMaxBatchCount)
	return newBatchedTransport(inner, cfg)
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

func NewBatchedTransportWithConfig(inner Transport, cfg BatchedConfig) (*BatchedTransport, error) {
	if inner == nil || cfg.MaxBatchBytes < 1 || cfg.MaxBatchBytes > 8<<20 || cfg.MaxBatchCount < 1 || cfg.MaxBatchCount > 65535 || cfg.QueueDepth < 1 || cfg.QueueDepth > 1000000 || cfg.Linger < 0 || cfg.Linger > time.Second {
		return nil, fmt.Errorf("invalid batching configuration")
	}
	return newBatchedTransport(inner, cfg), nil
}

func newBatchedTransport(inner Transport, cfg BatchedConfig) *BatchedTransport {
	return &BatchedTransport{
		Transport:     inner,
		queue:         make(chan []byte, cfg.QueueDepth),
		linger:        cfg.Linger,
		maxBatchBytes: cfg.MaxBatchBytes,
		maxBatchCount: cfg.MaxBatchCount,
	}
}

func (b *BatchedTransport) Start() error {
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

func (b *BatchedTransport) Stop() error {
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
func (b *BatchedTransport) Send(data []byte) error {
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

func (b *BatchedTransport) Receive(callback func([]byte)) {
	b.mu.Lock()
	b.userCb = callback
	b.mu.Unlock()

	b.Transport.Receive(func(data []byte) {
		pkts, err := decodeBatch(data)
		if err != nil {
			utils.Debugf("[BATCH] decode error (%d bytes): %v", len(data), err)
			return
		}
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

func (b *BatchedTransport) flushLoop() {
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
	BatchedConfig
	QueueLen   int     `json:"queue_len"`
	QueueDrops uint64  `json:"queue_drops"`
	Batches    uint64  `json:"batches"`
	Packets    uint64  `json:"packets"`
	SendErrors uint64  `json:"send_errors"`
	Average    float64 `json:"avg_packets_per_batch"`
}

func (b *BatchedTransport) Performance() BatchPerformance {
	n, p := b.batches.Load(), b.packets.Load()
	avg := float64(0)
	if n > 0 {
		avg = float64(p) / float64(n)
	}
	return BatchPerformance{BatchedConfig{b.maxBatchBytes, b.maxBatchCount, b.linger, cap(b.queue)}, len(b.queue), b.drops.Load(), n, p, b.sendErrors.Load(), avg}
}
