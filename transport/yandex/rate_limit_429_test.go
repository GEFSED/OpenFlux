package yandex

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"openflux/transport"
)

var rateLimitTestEpoch = time.Date(2026, 1, 2, 12, 0, 0, 0, time.UTC)

type rateLimitTestWait struct {
	until time.Time
	done  chan struct{}
}

type rateLimitTestClock struct {
	mu      sync.Mutex
	now     time.Time
	waiters map[*rateLimitTestWait]bool
	started chan time.Duration
}

func (c *rateLimitTestClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *rateLimitTestClock) Wait(ctx context.Context, d time.Duration) error {
	c.mu.Lock()
	w := &rateLimitTestWait{until: c.now.Add(d), done: make(chan struct{})}
	c.waiters[w] = true
	c.mu.Unlock()
	defer func() { c.mu.Lock(); delete(c.waiters, w); c.mu.Unlock() }()
	c.started <- d
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-w.done:
		return nil
	}
}

func (c *rateLimitTestClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
	for w := range c.waiters {
		if !c.now.Before(w.until) {
			delete(c.waiters, w)
			close(w.done)
		}
	}
}

// Every HTTP attempt uses an actual local httptest server. Only the destination
// is redirected in this test-only RoundTripper; no real provider is contacted.
func localGuardRelay(t *testing.T, enabled bool, handler func(http.ResponseWriter, *http.Request, int64)) (*relayClient, *rateLimitTestClock, *atomic.Int64) {
	t.Helper()
	count := &atomic.Int64{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		defer req.Body.Close()
		io.Copy(io.Discard, req.Body)
		if req.Method != http.MethodPost {
			t.Error("relay changed request method")
		}
		handler(w, req, count.Add(1))
	}))
	t.Cleanup(server.Close)
	destination, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	local := &http.Transport{}
	t.Cleanup(local.CloseIdleConnections)
	cfg := DefaultVolgaConfig()
	cfg.WorkerCount, cfg.QueueSize, cfg.BatchSize = 32, 4096, 32
	cfg.BatchTimeout = time.Millisecond
	cfg.MaxIdleConnsPerHost, cfg.MaxIdleConns = 32, 64
	cfg.RateLimit429GuardEnabled = enabled
	r := newRelayClient(&volgaAuth{Session: &http.Client{}, Token: "synthetic-token", RequestPath: "synthetic-path"}, cfg, &VolgaStats{})
	t.Cleanup(r.Stop)
	r.httpClient.Transport = labRoundTripper(func(req *http.Request) (*http.Response, error) {
		copy := req.Clone(req.Context())
		u := *req.URL
		u.Scheme, u.Host = destination.Scheme, destination.Host
		copy.URL, copy.Host = &u, destination.Host
		return local.RoundTrip(copy)
	})
	clock := &rateLimitTestClock{now: rateLimitTestEpoch, waiters: make(map[*rateLimitTestWait]bool), started: make(chan time.Duration, 128)}
	if r.rateLimit != nil {
		r.rateLimit.clock = clock
	}
	return r, clock, count
}

func guardBatch(r *relayClient) error { return r.sendBatch([][]byte{[]byte("synthetic-packet")}) }

func asyncGuardBatch(r *relayClient) <-chan error {
	done := make(chan error, 1)
	go func() { done <- guardBatch(r) }()
	return done
}

func expectGuardResult(t *testing.T, err error, rejected bool) {
	t.Helper()
	if rejected {
		if err == nil || err.Error() != "status 429" {
			t.Fatal("expected the unchanged 429 batch failure")
		}
	} else if err != nil {
		t.Fatal("expected successful local relay request")
	}
}

func awaitGuardResult(t *testing.T, done <-chan error, rejected bool) {
	t.Helper()
	select {
	case err := <-done:
		expectGuardResult(t, err, rejected)
	case <-time.After(5 * time.Second):
		t.Fatal("local relay request did not finish")
	}
}

func awaitGuardWait(t *testing.T, c *rateLimitTestClock, want time.Duration) {
	t.Helper()
	select {
	case got := <-c.started:
		if got != want {
			t.Fatalf("gate wait = %v, want %v", got, want)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("request did not wait at the shared gate")
	}
}

func guardEventually(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("local worker condition did not complete")
		}
		time.Sleep(time.Millisecond)
	}
}

func Test429GuardNo429NoWait(t *testing.T) {
	r, c, count := localGuardRelay(t, true, func(w http.ResponseWriter, _ *http.Request, _ int64) { w.WriteHeader(204) })
	for i := 0; i < 4; i++ {
		expectGuardResult(t, guardBatch(r), false)
	}
	d := r.rateLimit.snapshot()
	if count.Load() != 4 || d != (VolgaRateLimitDiagnostics{Enabled: true}) || len(c.started) != 0 {
		t.Fatal("successful traffic acquired a cooldown or changed request count")
	}
}

func Test429GuardRetryAfterSeconds(t *testing.T) {
	r, c, count := localGuardRelay(t, true, func(w http.ResponseWriter, _ *http.Request, n int64) {
		if n == 1 {
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(429)
			return
		}
		w.WriteHeader(204)
	})
	expectGuardResult(t, guardBatch(r), true)
	done := asyncGuardBatch(r) // A new, explicitly submitted batch, not a retry.
	awaitGuardWait(t, c, time.Second)
	c.advance(time.Second - time.Nanosecond)
	if count.Load() != 1 {
		t.Fatal("future request started before Retry-After expired")
	}
	c.advance(time.Nanosecond)
	awaitGuardResult(t, done, false)
	d := r.rateLimit.snapshot()
	if count.Load() != 2 || d.WaitEvents != 1 || d.WaitNS != int64(time.Second) || d.Events429 != 1 || d.RetryAfterUsed != 1 || d.FallbackUsed != 0 {
		t.Fatalf("unexpected safe counters: %+v", d)
	}
}

func Test429GuardRetryAfterHTTPDate(t *testing.T) {
	r, c, _ := localGuardRelay(t, true, func(w http.ResponseWriter, _ *http.Request, n int64) {
		if n == 1 {
			w.Header().Set("Retry-After", rateLimitTestEpoch.Add(2*time.Second).Format(http.TimeFormat))
			w.WriteHeader(429)
			return
		}
		w.WriteHeader(200)
	})
	expectGuardResult(t, guardBatch(r), true)
	done := asyncGuardBatch(r)
	awaitGuardWait(t, c, 2*time.Second)
	c.advance(2 * time.Second)
	awaitGuardResult(t, done, false)
	if d := r.rateLimit.snapshot(); d.RetryAfterUsed != 1 || d.WaitNS != int64(2*time.Second) {
		t.Fatal("HTTP-date gate was not respected")
	}
}

func Test429GuardInvalidOrAbsentRetryAfter(t *testing.T) {
	for _, tc := range []struct {
		name   string
		values []string
	}{
		{"invalid_retry_after", []string{"private-header-marker"}},
		{"no_retry_after", nil},
		{"empty", []string{""}},
		{"negative", []string{"-1"}},
		{"fraction", []string{"0.5"}},
		{"multiple", []string{"1", "2"}},
		{"past_date", []string{rateLimitTestEpoch.Add(-time.Second).Format(http.TimeFormat)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, _, count := localGuardRelay(t, true, func(w http.ResponseWriter, _ *http.Request, _ int64) {
				for _, value := range tc.values {
					w.Header().Add("Retry-After", value)
				}
				w.WriteHeader(429)
			})
			expectGuardResult(t, guardBatch(r), true)
			d := r.rateLimit.snapshot()
			invalid := uint64(0)
			if len(tc.values) != 0 {
				invalid = 1
			}
			if count.Load() != 1 || d.RetryAfterInvalid != invalid || d.FallbackUsed != 1 || d.GateRemainingNS != int64(rateLimitFallbackMin) {
				t.Fatalf("incorrect fallback diagnostics: %+v", d)
			}
			data, err := json.Marshal(d)
			if err != nil || strings.Contains(string(data), "private-header-marker") || strings.Contains(string(data), "synthetic") || strings.Contains(string(data), "http:") {
				t.Fatal("guard diagnostics must contain only numeric/bool fields")
			}
		})
	}
}

func Test429GuardRetryAfterBounds(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  time.Duration
	}{
		{"0", 0},
		{" 1 ", time.Second},
		{strings.Repeat("9", 100), 5 * time.Second},
		{rateLimitTestEpoch.Add(time.Hour).Format(http.TimeFormat), 5 * time.Second},
	} {
		r, _, _ := localGuardRelay(t, true, func(w http.ResponseWriter, _ *http.Request, _ int64) {
			w.Header().Set("Retry-After", tc.value)
			w.WriteHeader(429)
		})
		expectGuardResult(t, guardBatch(r), true)
		d := r.rateLimit.snapshot()
		if d.RetryAfterUsed != 1 || d.FallbackUsed != 0 || d.GateRemainingNS != int64(tc.want) || d.MaxCooldownNS != int64(tc.want) {
			t.Fatal("Retry-After bound/zero handling changed")
		}
	}
}

func Test429GuardExponentialFallbackBounded(t *testing.T) {
	r, c, count := localGuardRelay(t, true, func(w http.ResponseWriter, _ *http.Request, _ int64) { w.WriteHeader(429) })
	for i, ms := range []int{25, 50, 100, 200, 400, 800, 1000, 1000} {
		expectGuardResult(t, guardBatch(r), true)
		d := r.rateLimit.snapshot()
		want := time.Duration(ms) * time.Millisecond
		if d.GateRemainingNS != int64(want) || d.MaxCooldownNS > int64(rateLimitFallbackMax) || d.FallbackUsed != uint64(i+1) || count.Load() != int64(i+1) {
			t.Fatalf("fallback sequence/bound differs: %+v", d)
		}
		c.advance(want)
	}
}

func holdGuardResponse(req *http.Request, release <-chan struct{}) bool {
	select {
	case <-release:
		return true
	case <-req.Context().Done():
		return false
	}
}

func awaitGuardSignal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(5 * time.Second):
		t.Fatal("local HTTP synchronization timed out")
	}
}

func Test429GuardConcurrent429Gate(t *testing.T) {
	const workers = 8
	entered := make(chan struct{}, workers)
	release := make(chan struct{})
	r, _, count := localGuardRelay(t, true, func(w http.ResponseWriter, req *http.Request, n int64) {
		entered <- struct{}{}
		if !holdGuardResponse(req, release) {
			return
		}
		w.Header().Set("Retry-After", fmt.Sprint(1+n%5))
		w.WriteHeader(429)
	})
	done := make([]<-chan error, workers)
	for i := range done {
		done[i] = asyncGuardBatch(r)
	}
	for range done {
		awaitGuardSignal(t, entered)
	}
	close(release) // All requests were already admitted before the first 429.
	var previous time.Time
	for _, result := range done {
		awaitGuardResult(t, result, true)
		r.rateLimit.mu.Lock()
		deadline := r.rateLimit.until
		r.rateLimit.mu.Unlock()
		if deadline.Before(previous) {
			t.Fatal("concurrent 429 moved the shared gate backwards")
		}
		previous = deadline
	}
	d := r.rateLimit.snapshot()
	if count.Load() != workers || d.Events429 != workers || d.RetryAfterUsed != workers || d.GateRemainingNS != int64(5*time.Second) {
		t.Fatalf("concurrent gate lost updates: %+v", d)
	}
}

func Test429GuardExtensionRechecksWait(t *testing.T) {
	firstEntered, secondEntered := make(chan struct{}), make(chan struct{})
	firstRelease, secondRelease := make(chan struct{}), make(chan struct{})
	r, c, count := localGuardRelay(t, true, func(w http.ResponseWriter, req *http.Request, n int64) {
		switch n {
		case 1:
			close(firstEntered)
			if !holdGuardResponse(req, firstRelease) {
				return
			}
			w.Header().Set("Retry-After", "1")
		case 2:
			close(secondEntered)
			if !holdGuardResponse(req, secondRelease) {
				return
			}
			w.Header().Set("Retry-After", "5")
		default:
			w.WriteHeader(204)
			return
		}
		w.WriteHeader(429)
	})
	first := asyncGuardBatch(r)
	awaitGuardSignal(t, firstEntered)
	second := asyncGuardBatch(r)
	awaitGuardSignal(t, secondEntered)
	close(firstRelease)
	awaitGuardResult(t, first, true)
	future := asyncGuardBatch(r)
	awaitGuardWait(t, c, time.Second)
	close(secondRelease)
	awaitGuardResult(t, second, true)
	c.advance(time.Second)
	awaitGuardWait(t, c, 4*time.Second)
	if count.Load() != 2 {
		t.Fatal("waiter ignored the extended deadline")
	}
	c.advance(4 * time.Second)
	awaitGuardResult(t, future, false)
	if d := r.rateLimit.snapshot(); d.WaitEvents != 1 || d.WaitNS != int64(5*time.Second) || d.GateRemainingNS != 0 {
		t.Fatalf("extension must count one waiting request: %+v", d)
	}
}

func Test429GuardCancellationDuringWait(t *testing.T) {
	r, _, count := localGuardRelay(t, true, func(w http.ResponseWriter, _ *http.Request, _ int64) {
		w.Header().Set("Retry-After", "5")
		w.WriteHeader(429)
	})
	// Exercise the production timer/select implementation as well as fake time.
	r.rateLimit.clock = realRateLimitClock{}
	expectGuardResult(t, guardBatch(r), true)
	r.Start()
	if err := r.Send([]byte("new-pending-batch")); err != nil {
		t.Fatal(err)
	}
	guardEventually(t, func() bool { return r.rateLimit.snapshot().WaitEvents == 1 })
	stopped := make(chan struct{})
	go func() { r.Stop(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop failed to cancel the gate wait promptly")
	}
	if count.Load() != 1 || r.rateLimit.snapshot().WaitNS <= 0 {
		t.Fatal("cancelled waiter issued HTTP or failed to account its wait")
	}
}

func Test429GuardSuccessfulRecovery(t *testing.T) {
	firstEntered, staleEntered := make(chan struct{}), make(chan struct{})
	firstRelease, staleRelease := make(chan struct{}), make(chan struct{})
	r, c, count := localGuardRelay(t, true, func(w http.ResponseWriter, req *http.Request, n int64) {
		switch n {
		case 1:
			close(firstEntered)
			if !holdGuardResponse(req, firstRelease) {
				return
			}
		case 2:
			close(staleEntered)
			if !holdGuardResponse(req, staleRelease) {
				return
			}
			w.WriteHeader(204)
			return
		case 4:
			w.WriteHeader(200)
			return
		}
		w.WriteHeader(429)
	})
	first := asyncGuardBatch(r)
	awaitGuardSignal(t, firstEntered)
	stale := asyncGuardBatch(r)
	awaitGuardSignal(t, staleEntered)
	close(firstRelease)
	awaitGuardResult(t, first, true)
	c.advance(25 * time.Millisecond)
	close(staleRelease) // Old success after expiry must NOT reset the new strike.
	awaitGuardResult(t, stale, false)
	expectGuardResult(t, guardBatch(r), true)
	if r.rateLimit.snapshot().GateRemainingNS != int64(50*time.Millisecond) {
		t.Fatal("stale in-flight success reset a newer 429 strike")
	}
	c.advance(50 * time.Millisecond)
	expectGuardResult(t, guardBatch(r), false) // Fresh post-cooldown success resets.
	expectGuardResult(t, guardBatch(r), true)
	d := r.rateLimit.snapshot()
	if d.GateRemainingNS != int64(25*time.Millisecond) || d.MaxCooldownNS != int64(50*time.Millisecond) || d.FallbackUsed != 3 || count.Load() != 5 {
		t.Fatal("successful recovery did not reset the bounded fallback")
	}
}

func Test429GuardNoBatchRetry(t *testing.T) {
	r, _, count := localGuardRelay(t, true, func(w http.ResponseWriter, _ *http.Request, _ int64) {
		w.Header().Set("Retry-After", "1")
		w.WriteHeader(429)
	})
	r.Start()
	if err := r.Send([]byte("one-submitted-batch")); err != nil {
		t.Fatal(err)
	}
	guardEventually(t, func() bool { return r.stats.HTTPReqsFailed.Load() == 1 })
	r.Stop()
	if count.Load() != 1 || r.stats.HTTPReqsSent.Load() != 0 || r.stats.httpFailures.snapshot(1).RateLimited != 1 || r.rateLimit.snapshot().Events429 != 1 {
		t.Fatal("one rejected batch must cause exactly one HTTP attempt/failure")
	}
}

func Test429GuardDisabledEquivalence(t *testing.T) {
	r, c, count := localGuardRelay(t, false, func(w http.ResponseWriter, _ *http.Request, n int64) {
		if n <= 2 {
			w.Header().Set("Retry-After", "5")
			w.WriteHeader(429)
			return
		}
		w.WriteHeader(204)
	})
	if r.rateLimit != nil {
		t.Fatal("disabled profile allocated a gate")
	}
	expectGuardResult(t, guardBatch(r), true)
	expectGuardResult(t, guardBatch(r), true)
	expectGuardResult(t, guardBatch(r), false)
	d := volgaPerformance(r.config, r.stats, 0, true)
	if count.Load() != 3 || len(c.started) != 0 || d.VolgaRateLimitDiagnostics != (VolgaRateLimitDiagnostics{}) || r.stats.PacketsSent.Load() != 1 || r.stats.httpFailures.snapshot(0).RateLimited != 2 {
		t.Fatal("disabled guard changed request/failure/wait behavior")
	}
}

func Test429GuardPerformanceSnapshot(t *testing.T) {
	r, _, _ := localGuardRelay(t, true, func(w http.ResponseWriter, _ *http.Request, _ int64) {
		w.Header().Set("Retry-After", "private-header-marker")
		w.WriteHeader(429)
	})
	expectGuardResult(t, guardBatch(r), true)
	tr := newYandexVolgaTransport("synthetic-document", transport.DefaultConfig(), r.config)
	tr.relay, tr.stats = r, r.stats
	d := tr.Performance()
	if d.VolgaRateLimitDiagnostics != r.rateLimit.snapshot() || !d.Enabled || d.Events429 != 1 {
		t.Fatal("transport snapshot lost guard diagnostics")
	}
	data, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	if fields["rate_limit_guard_enabled"] != true {
		t.Fatal("guard enable flag missing from JSON")
	}
	for _, key := range []string{"rate_limit_429_events", "rate_limit_wait_events", "rate_limit_wait_ns", "rate_limit_retry_after_used", "rate_limit_retry_after_invalid", "rate_limit_fallback_used", "rate_limit_current_gate_remaining_ns", "rate_limit_max_cooldown_ns"} {
		if value, ok := fields[key].(float64); !ok || value < 0 {
			t.Fatalf("missing or unsafe numeric diagnostic %s", key)
		}
	}
	for _, forbidden := range []string{"private-header-marker", "synthetic", "Retry-After", "http:", "token", "cookie"} {
		if strings.Contains(string(data), forbidden) {
			t.Fatal("private data in guard snapshot")
		}
	}
}
