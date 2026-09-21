package yandex

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/http/cookiejar"
	"os"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"openflux/transport"
)

type labRoundTripper func(*http.Request) (*http.Response, error)

func (f labRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type labLink struct {
	relay *relayClient
	cb    func([]byte)
}

func (l *labLink) Start() error                    { l.relay.Start(); return nil }
func (l *labLink) Stop() error                     { l.relay.Stop(); return nil }
func (l *labLink) Send(p []byte) error             { return l.relay.Send(p) }
func (l *labLink) Receive(cb func([]byte))         { l.cb = cb }
func (l *labLink) IsConnected() bool               { return true }
func (l *labLink) Stats() transport.TransportStats { return transport.TransportStats{} }

type labConfig struct {
	Name  string
	Volga VolgaConfig
	Outer transport.BatchedConfig
}
type labResult struct {
	Offered, Undelivered                                                     uint64
	Name, Workload                                                           string
	RTTMS, StallMS                                                           int
	Config                                                                   labConfig
	Packets, Bytes, Attempts, QueueDrops, PeakBusy                           uint64
	Seconds, PacketsPerSecond, MBPerSecond, HTTPPerSecond, AverageVolgaBatch float64
	P50MS, P95MS, P99MS                                                      float64
	Outer                                                                    transport.BatchPerformance
	Before, Started, Peak, Stopped                                           memoryPoint
	AllocBytes, Allocs, GCPauseNS                                            uint64
	GCCount                                                                  uint32
}

func runLab(t *testing.T, c labConfig, workload string, rtt, stall time.Duration, duration time.Duration) labResult {
	t.Helper()
	runtime.GC()
	result := labResult{Name: c.Name, Workload: workload, Config: c, RTTMS: int(rtt / time.Millisecond), StallMS: int(stall / time.Millisecond), Before: memoryAt("before")}
	jar, _ := cookiejar.New(nil)
	stats := &VolgaStats{}
	r := newRelayClient(&volgaAuth{Session: &http.Client{Jar: jar}, Token: "synthetic-token", RequestPath: "synthetic-path", UserID: 1}, c.Volga, stats)
	link := &labLink{relay: r}
	outer, err := transport.NewBatchedTransportWithConfig(link, c.Outer)
	if err != nil {
		t.Fatal(err)
	}
	// Exercise the unchanged receiver codec on a default-config exit peer.
	decoder := transport.NewBatchedTransport(&labLink{relay: r})
	var mu sync.Mutex
	times := map[uint64]time.Time{}
	latencies := make([]float64, 0, 10000)
	var delivered, bytesDelivered, requests atomic.Uint64
	decoder.Receive(func(p []byte) {
		id := binary.BigEndian.Uint64(p[:8])
		mu.Lock()
		sent, ok := times[id]
		if ok {
			latencies = append(latencies, float64(time.Since(sent))/float64(time.Millisecond))
			delete(times, id)
		}
		mu.Unlock()
		if !ok {
			t.Error("unexpected or duplicate synthetic packet")
			return
		}
		delivered.Add(1)
		bytesDelivered.Add(uint64(len(p)))
	})
	receive := decoder.Transport.(*labLink).cb
	r.httpClient.Transport = labRoundTripper(func(req *http.Request) (*http.Response, error) {
		defer req.Body.Close()
		var payload struct {
			Message struct {
				Bundle []json.RawMessage `json:"bundle"`
			} `json:"message"`
		}
		if err := json.NewDecoder(req.Body).Decode(&payload); err != nil {
			return nil, err
		}
		var encoded string
		if err := json.Unmarshal(payload.Message.Bundle[len(payload.Message.Bundle)-1], &encoded); err != nil {
			return nil, err
		}
		blob, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return nil, err
		}
		n := requests.Add(1)
		delay := rtt
		if stall > 0 && n%23 == 0 {
			delay += stall
		}
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-req.Context().Done():
			return nil, req.Context().Err()
		}
		// Independent simultaneous requests model multiplexed service delay, not a
		// serialized HTTP/1 server. No sockets or external destinations are contacted.
		for _, p := range decodeBatch(blob) {
			receive(p)
		}
		return &http.Response{StatusCode: 204, Header: make(http.Header), Body: io.NopCloser(bytes.NewReader(nil)), Request: req}, nil
	})
	if err := outer.Start(); err != nil {
		t.Fatal(err)
	}
	result.Started = memoryAt("started")
	result.Peak = result.Started
	var memBefore runtime.MemStats
	runtime.ReadMemStats(&memBefore)
	var sampleWG sync.WaitGroup
	sampleStop := make(chan struct{})
	sampleWG.Add(1)
	go func() {
		defer sampleWG.Done()
		tick := time.NewTicker(20 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-sampleStop:
				return
			case <-tick.C:
				p := memoryAt("peak")
				if p.HeapAlloc > result.Peak.HeapAlloc {
					result.Peak = p
				}
			}
		}
	}()
	payload := make([]byte, 1400)
	rand.New(rand.NewSource(79)).Read(payload)
	start := time.Now()
	var id, sendDrops uint64
	tick := time.NewTicker(10 * time.Millisecond)
	for time.Since(start) < duration {
		<-tick.C
		count := 64
		switch workload {
		case "burst":
			count = 100
		case "interactive":
			count = 3
		}
		for j := 0; j < count; j++ {
			size := 1400
			if workload == "mixed" {
				size = []int{64, 256, 512, 1400}[id%4]
			}
			if workload == "interactive" {
				size = 64
			}
			id++
			binary.BigEndian.PutUint64(payload[:8], id)
			mu.Lock()
			times[id] = time.Now()
			mu.Unlock()
			if err := outer.Send(payload[:size]); err != nil {
				sendDrops++
				mu.Lock()
				delete(times, id)
				mu.Unlock()
			}
		}
		if workload == "burst" {
			time.Sleep(40 * time.Millisecond)
		}
		if workload == "interactive" {
			time.Sleep(90 * time.Millisecond)
		}
	}
	tick.Stop()
	deadline := time.Now().Add(3*time.Second + stall)
	for delivered.Load() < id-sendDrops && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
		if stats.QueueDrops.Load() > 0 && len(r.batchQueue) == 0 && stats.WorkerBusy.Load() == 0 && outer.Performance().QueueLen == 0 {
			break
		}
	}
	result.Seconds = time.Since(start).Seconds()
	result.Outer = outer.Performance()
	if err := outer.Stop(); err != nil {
		t.Fatal(err)
	}
	close(sampleStop)
	sampleWG.Wait()
	result.Packets = delivered.Load()
	result.Offered = id
	result.Undelivered = id - delivered.Load()
	result.Bytes = bytesDelivered.Load()
	result.Attempts = requests.Load()
	result.QueueDrops = sendDrops + stats.QueueDrops.Load()
	result.PeakBusy = uint64(stats.PeakWorkerBusy.Load())
	result.PacketsPerSecond = float64(result.Packets) / result.Seconds
	result.MBPerSecond = float64(result.Bytes) / 1e6 / result.Seconds
	result.HTTPPerSecond = float64(result.Attempts) / result.Seconds
	if result.Attempts > 0 {
		result.AverageVolgaBatch = float64(stats.PacketsBatched.Load()) / float64(result.Attempts)
	}
	mu.Lock()
	sort.Float64s(latencies)
	if len(latencies) > 0 {
		result.P50MS = latencies[(len(latencies)-1)*50/100]
		result.P95MS = latencies[(len(latencies)-1)*95/100]
		result.P99MS = latencies[(len(latencies)-1)*99/100]
	}
	mu.Unlock()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	result.AllocBytes = m.TotalAlloc - memBefore.TotalAlloc
	result.Allocs = m.Mallocs - memBefore.Mallocs
	result.GCCount = m.NumGC - memBefore.NumGC
	result.GCPauseNS = m.PauseTotalNs - memBefore.PauseTotalNs
	runtime.GC()
	result.Stopped = memoryAt("stopped")
	return result
}

// Reproducible opt-in harness. See docs/perf; it never calls authorize or Yandex.
func TestPerfLab(t *testing.T) {
	path := os.Getenv("OPENFLUX_PERF_OUTPUT")
	if path == "" {
		t.Skip("opt-in local performance lab")
	}
	base := labConfig{"baseline", DefaultVolgaConfig(), transport.DefaultBatchedConfig()}
	candidate := base
	candidate.Name = "candidate"
	candidate.Volga.WorkerCount = 64
	candidate.Volga.QueueSize = 4096
	candidate.Volga.MaxIdleConns = 128
	candidate.Volga.MaxIdleConnsPerHost = 64
	candidate.Outer.Linger = time.Millisecond
	cases := []labConfig{base}
	if os.Getenv("OPENFLUX_PERF_SEARCH") != "" {
		for _, v := range []int{16, 32, 64, 128, 256} {
			c := candidate
			c.Name = fmt.Sprintf("workers-%d", v)
			c.Volga.WorkerCount = v
			cases = append(cases, c)
		}
		for _, v := range []int{512, 2048, 4096, 8192, 16384, 32768} {
			c := candidate
			c.Name = fmt.Sprintf("queue-%d", v)
			c.Volga.QueueSize = v
			cases = append(cases, c)
		}
		for _, v := range []int{8192, 16384, 32768} {
			for _, ms := range []int{0, 1, 2, 5} {
				c := candidate
				c.Name = fmt.Sprintf("outer-%d-%dms", v, ms)
				c.Outer.MaxBatchBytes = v
				c.Outer.Linger = time.Duration(ms) * time.Millisecond
				cases = append(cases, c)
			}
		}
		for _, v := range []int{8, 16, 20, 32, 64} {
			for _, us := range []int{0, 500, 1000, 2000} {
				c := candidate
				c.Name = fmt.Sprintf("inner-%d-%dus", v, us)
				c.Volga.BatchSize = v
				c.Volga.BatchTimeout = time.Duration(us) * time.Microsecond
				cases = append(cases, c)
			}
		}
	}
	results := make([]labResult, 0, len(cases))
	if os.Getenv("OPENFLUX_PERF_SEARCH") != "" {
		for _, c := range cases {
			results = append(results, runLab(t, c, "bulk", 60*time.Millisecond, 0, time.Second))
			t.Log(c.Name, "done")
		}
	} else {
		low := candidate
		low.Name = "low_latency"
		low.Volga.QueueSize = 512
		low.Volga.BatchSize = 8
		low.Volga.BatchTimeout = 0
		low.Outer.Linger = 0
		low.Outer.QueueDepth = 512
		balanced := candidate
		balanced.Name = "balanced"
		balanced.Volga.QueueSize = 2048
		balanced.Volga.BatchTimeout = 500 * time.Microsecond
		balanced.Outer.MaxBatchBytes = 16 << 10
		balanced.Outer.QueueDepth = 2048
		throughput := candidate
		throughput.Name = "throughput"
		throughput.Volga.BatchSize = 32
		throughput.Volga.BatchTimeout = time.Millisecond
		throughput.Outer.MaxBatchBytes = 32 << 10
		throughput.Outer.Linger = 2 * time.Millisecond
		for _, c := range []labConfig{base, balanced, low, throughput} {
			for _, workload := range []string{"bulk", "mixed", "burst", "interactive"} {
				for _, link := range [][2]int{{20, 0}, {60, 0}, {120, 0}, {60, 300}, {60, 1000}} {
					results = append(results, runLab(t, c, workload, time.Duration(link[0])*time.Millisecond, time.Duration(link[1])*time.Millisecond, time.Second))
				}
				t.Log(c.Name, workload, "done")
			}
		}
	}
	data, _ := json.MarshalIndent(results, "", "  ")
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}
