package yandex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Real relay workers and sendBatch paths; the RoundTripper never opens a socket.
// Both implementations must keep the same success policy and one attempt only.
func TestRelayHTTPFailureClassification(t *testing.T) {
	cases := []struct {
		name   string
		status int
		err    error
		want   VolgaHTTPFailureDiagnostics
	}{
		{"ok", 200, nil, VolgaHTTPFailureDiagnostics{}},
		{"no_content", 204, nil, VolgaHTTPFailureDiagnostics{}},
		{"network", 0, errors.New("synthetic-private-error"), VolgaHTTPFailureDiagnostics{Total: 1, Network: 1}},
		{"timeout", 0, context.DeadlineExceeded, VolgaHTTPFailureDiagnostics{Total: 1, Timeout: 1}},
		{"cancelled", 0, context.Canceled, VolgaHTTPFailureDiagnostics{Total: 1, Network: 1}},
		{"unauthorized", 401, nil, VolgaHTTPFailureDiagnostics{Total: 1, ClientError: 1}},
		{"rate_limited", 429, nil, VolgaHTTPFailureDiagnostics{Total: 1, ClientError: 1, RateLimited: 1}},
		{"server_error", 503, nil, VolgaHTTPFailureDiagnostics{Total: 1, ServerError: 1}},
		{"redirect_without_location", 302, nil, VolgaHTTPFailureDiagnostics{Total: 1, OtherStatus: 1}},
		{"unaccepted_2xx", 201, nil, VolgaHTTPFailureDiagnostics{Total: 1, OtherStatus: 1}},
	}
	for _, baseline := range []bool{false, true} {
		label := "optimized"
		if baseline {
			label = "baseline"
		}
		for _, tc := range cases {
			t.Run(label+"/"+tc.name, func(t *testing.T) {
				cfg := DefaultVolgaConfig()
				cfg.WorkerCount, cfg.QueueSize, cfg.BatchSize = 1, 4, 1
				stats := &VolgaStats{}
				auth := &volgaAuth{Session: &http.Client{}, Token: "synthetic-private-token", RequestPath: "synthetic-private-path"}
				var attempts atomic.Int64
				rt := labRoundTripper(func(req *http.Request) (*http.Response, error) {
					attempts.Add(1)
					req.Body.Close()
					if tc.err != nil {
						return nil, tc.err
					}
					return &http.Response{StatusCode: tc.status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("synthetic-private-response")), Request: req}, nil
				})
				var start, stop func()
				var send func([]byte) error
				if baseline {
					r := newBaselineRelayClient(auth, cfg, stats)
					r.httpClient.Transport = rt
					start, stop, send = r.Start, r.Stop, r.Send
				} else {
					r := newRelayClient(auth, cfg, stats)
					r.httpClient.Transport = rt
					start, stop, send = r.Start, r.Stop, r.Send
				}
				start()
				stopped := false
				t.Cleanup(func() {
					if !stopped {
						stop()
					}
				})
				if err := send([]byte("synthetic-packet")); err != nil {
					t.Fatal(err)
				}
				deadline := time.Now().Add(2 * time.Second)
				for stats.HTTPReqsSent.Load()+stats.HTTPReqsFailed.Load() == 0 && time.Now().Before(deadline) {
					time.Sleep(time.Millisecond)
				}
				stop()
				stopped = true
				if attempts.Load() != 1 {
					t.Fatal("relay must attempt the batch once, without retries")
				}
				got := volgaPerformance(cfg, stats, 0, false)
				if got.VolgaHTTPFailureDiagnostics != tc.want || got.HTTPFailures != tc.want.Total || got.HTTPRequests != 1 {
					t.Fatalf("unexpected sanitized counters: %+v", got)
				}
				wantSent := uint64(1) - tc.want.Total
				if stats.PacketsSent.Load() != wantSent || got.Batches != wantSent {
					t.Fatal("success/failure behavior changed")
				}
				data, err := json.Marshal(got)
				if err != nil {
					t.Fatal(err)
				}
				for _, secret := range []string{"synthetic-", "volga.yandex", "Bearer", "Cookie"} {
					if bytes.Contains(data, []byte(secret)) {
						t.Fatal("diagnostics leaked request/response or error data")
					}
				}
			})
		}
	}
}
