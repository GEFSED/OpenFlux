package yandex

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type relayTestRoundTripper func(*http.Request) (*http.Response, error)

func (f relayTestRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func diagnosticTestRelay(t *testing.T, server *httptest.Server) *relayClient {
	t.Helper()
	cfg := DefaultVolgaConfig()
	cfg.WorkerCount, cfg.QueueSize = 2, 8
	auth := &volgaAuth{Session: &http.Client{}, UserID: 7, UserIDStr: "7", RequestPath: "synthetic"}
	r := newRelayClient(auth, cfg, &VolgaStats{})
	target, _ := url.Parse(server.URL)
	base := server.Client().Transport
	r.httpClient.Transport = relayTestRoundTripper(func(req *http.Request) (*http.Response, error) {
		copy := req.Clone(req.Context())
		copy.URL.Scheme, copy.URL.Host, copy.Host = target.Scheme, target.Host, target.Host
		return base.RoundTrip(copy)
	})
	t.Cleanup(r.cancel)
	return r
}

func TestRelayDiagnosticHTTPAccountingAndFrozenWire(t *testing.T) {
	for _, status := range []int{200, 204, 201, 302, 400, 401, 403, 404, 409, 418, 429, 503} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var bodies [][]byte
			var mu sync.Mutex
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				body, _ := io.ReadAll(req.Body)
				mu.Lock()
				bodies = append(bodies, body)
				mu.Unlock()
				w.WriteHeader(status)
			}))
			defer server.Close()
			r := diagnosticTestRelay(t, server)
			frozen := diagnosticTestRelay(t, server)
			batch := [][]byte{[]byte("synthetic-one"), []byte("synthetic-two")}
			err := r.sendBatch(batch)
			oldErr := frozen.sendBatchProductionReference(batch)
			ok := status == 200 || status == 204
			if (err == nil) != ok || (oldErr == nil) != ok {
				t.Fatal("production acceptance changed")
			}
			mu.Lock()
			defer mu.Unlock()
			if len(bodies) != 2 || !bytes.Equal(bodies[0], bodies[1]) {
				t.Fatal("wire body differs from independent exact-source sendBatch, or replay occurred")
			}
			d := &r.stats.diag
			if d.requests.Load() != 1 || d.durationCount.Load() != 1 || d.inflight.Load() != 0 || d.batches.Load() != 1 || d.packets.Load() != 2 {
				t.Fatal("attempt/batch accounting")
			}
			if d.bodyAttempted.Load() != uint64(len(bodies[0])) || d.payloadAttempted.Load() != 26 {
				t.Fatal("body/payload accounting")
			}
			if d.successes.Load()+d.failures.Load() != 1 || d.bodySuccess.Load()+d.bodyFailed.Load() != d.bodyAttempted.Load() || d.payloadSuccess.Load()+d.payloadFailed.Load() != 26 {
				t.Fatal("completed accounting identity")
			}
			var buckets uint64
			for i := range d.durationBuckets { buckets += d.durationBuckets[i].Load() }
			if buckets != 1 || d.durationSum.Load() == 0 || d.durationMax.Load() != d.durationSum.Load() {
				t.Fatal("latency accounting")
			}
			if d.failures.Load() != d.network.Load()+d.timeout.Load()+d.http4xx.Load()+d.http5xx.Load()+d.otherStatus.Load() {
				t.Fatal("failure hierarchy double count")
			}
			exact := map[int]*atomic.Uint64{400: &d.status400, 401: &d.status401, 403: &d.status403, 404: &d.status404, 409: &d.status409, 429: &d.status429}
			for code, counter := range exact {
				want := uint64(0)
				if code == status { want = 1 }
				if counter.Load() != want { t.Fatal("wrong exact status category") }
			}
			if status == 429 && d.payload429.Load() != 26 { t.Fatal("429 bytes") }
		})
	}
}

func TestRelayDiagnosticNetworkTimeoutAndIgnoredBodyReadError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer server.Close()
	for _, tc := range []struct{name string; failure error; timeout bool}{
		{"network", errors.New("synthetic network failure"), false},
		{"timeout", context.DeadlineExceeded, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := diagnosticTestRelay(t, server)
			var calls int
			r.httpClient.Transport = relayTestRoundTripper(func(*http.Request) (*http.Response, error) { calls++; return nil, tc.failure })
			if r.sendBatch([][]byte{{1, 2, 3}}) == nil || calls != 1 { t.Fatal("failure/retry behavior") }
			d := &r.stats.diag
			if d.failures.Load() != 1 || (d.timeout.Load() == 1) != tc.timeout || d.timeout.Load()+d.network.Load() != 1 { t.Fatal("error category") }
		})
	}
	r := diagnosticTestRelay(t, server)
	r.httpClient.Transport = relayTestRoundTripper(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: diagnosticBrokenBody{}}, nil
	})
	if err := r.sendBatch([][]byte{{1}}); err != nil { t.Fatal("production ignores body read error on accepted status") }
	if r.stats.diag.bodyReadErrors.Load() != 1 || r.stats.diag.successes.Load() != 1 { t.Fatal("ignored read error observability") }
}

type diagnosticBrokenBody struct{}
func (diagnosticBrokenBody) Read([]byte) (int, error) { return 0, errors.New("synthetic read error") }
func (diagnosticBrokenBody) Close() error { return nil }

func TestRelayDiagnosticWorkerNoRetryAndQueueDrop(t *testing.T) {
	var calls atomic.Uint64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); w.WriteHeader(429) }))
	defer server.Close()
	r := diagnosticTestRelay(t, server)
	r.config.BatchSize = 1
	r.Start()
	if err := r.Send([]byte{1, 2, 3}); err != nil { t.Fatal(err) }
	deadline := time.Now().Add(2*time.Second)
	for r.stats.HTTPReqsFailed.Load() != 1 && time.Now().Before(deadline) { time.Sleep(time.Millisecond) }
	r.Stop()
	if calls.Load() != 1 || r.stats.HTTPReqsFailed.Load() != 1 || r.stats.diag.status429.Load() != 1 { t.Fatal("one rejected batch must not replay") }
	q := diagnosticTestRelay(t, server)
	q.batchQueue = make(chan []byte, 1)
	if q.Send([]byte{1}) != nil || q.Send([]byte{2}) == nil { t.Fatal("queue policy changed") }
	if q.stats.QueueDrops.Load() != 1 || q.stats.diag.requests.Load() != 0 || q.stats.diag.failures.Load() != 0 { t.Fatal("queue drop classified as HTTP error") }
}

func TestRelayDiagnosticConcurrentCountersAndSnapshot(t *testing.T) {
	var stats VolgaStats
	d := &stats.diag
	r := &relayClient{workers: 16, batchQueue: make(chan []byte, 8)}
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := 0; n < 100; n++ {
				d.batch(2, 3)
				d.observeWorkers(16)
				d.observeQueue(1)
				s := d.begin(20, 3)
				d.finish(s, 20, 3, 429, nil, false)
			}
		}()
	}
	for n := 0; n < 100; n++ { d.snapshot(&stats, r, true) }
	wg.Wait()
	if d.requests.Load() != 1600 || d.failures.Load() != 1600 || d.payload429.Load() != 4800 || d.inflight.Load() != 0 { t.Fatal("lost counters") }
	values := d.snapshot(&stats, r, true)
	encoded, _ := json.Marshal(values)
	for _, forbidden := range []string{"synthetic", "cookie", "token", "request_path", "retry_after", "url"} {
		if strings.Contains(string(encoded), forbidden) { t.Fatal("non-numeric diagnostic schema") }
	}
}

func TestRelayDiagnosticLatencyBoundaries(t *testing.T) {
	limits := []time.Duration{50*time.Millisecond,100*time.Millisecond,250*time.Millisecond,500*time.Millisecond,time.Second,2*time.Second,5*time.Second}
	for i, value := range limits {
		if relayDurationBucket(value-1) != i || relayDurationBucket(value) != i+1 { t.Fatal("histogram boundary") }
	}
}

func TestRelayDiagnosticWebSocketCounters(t *testing.T) {
	stats := &VolgaStats{}
	var got []byte
	w := &wsListener{auth: &volgaAuth{UserID: 7}, stats: stats, relay: &relayClient{}, onData: func(b []byte) { got = append(got, b...) }}
	w.handleMessage([]byte("{"))
	inner, _ := json.Marshal(map[string]interface{}{"t":"relay", "userId":8, "message":map[string]interface{}{"bundle":[]interface{}{base64.StdEncoding.EncodeToString([]byte{0, 3, 1, 2, 3}), "!invalid-base64!"}}})
	outer, _ := json.Marshal(map[string]interface{}{"operation":"SESSION", "message":string(inner)})
	w.handleMessage(outer)
	if !bytes.Equal(got, []byte{1,2,3}) || stats.diag.wsMessages.Load() != 2 || stats.diag.wsPayloadBytes.Load() != 3 || stats.diag.wsJSONErrors.Load() != 1 || stats.diag.wsBase64Errors.Load() != 1 { t.Fatal("WS observability changed delivery") }
}
