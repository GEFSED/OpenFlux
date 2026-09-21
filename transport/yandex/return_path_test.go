package yandex

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"openflux/testsupport/basev100peer"
	"openflux/transport"
)

type localCarrier struct {
	start func() error
	stop  func() error
	send  func([]byte) error
	mu    sync.Mutex
	cb    func([]byte)
}

func (c *localCarrier) Start() error {
	if c.start != nil {
		return c.start()
	}
	return nil
}
func (c *localCarrier) Stop() error {
	if c.stop != nil {
		return c.stop()
	}
	return nil
}
func (c *localCarrier) Send(p []byte) error             { return c.send(p) }
func (c *localCarrier) Receive(cb func([]byte))         { c.mu.Lock(); c.cb = cb; c.mu.Unlock() }
func (c *localCarrier) IsConnected() bool               { return true }
func (c *localCarrier) Stats() transport.TransportStats { return transport.TransportStats{} }
func (c *localCarrier) deliver(p []byte) {
	c.mu.Lock()
	cb := c.cb
	c.mu.Unlock()
	if cb != nil {
		cb(p)
	}
}

// The only HTTP substitution rewrites the provider address to a local server.
// Production request construction, body, pool workers and WS parsing still run.
type localRelayHTTP struct {
	target *url.URL
	next   http.RoundTripper
}

func (l localRelayHTTP) RoundTrip(req *http.Request) (*http.Response, error) {
	r := req.Clone(req.Context())
	u := *r.URL
	u.Scheme = l.target.Scheme
	u.Host = l.target.Host
	u.Path = "/relay"
	r.URL = &u
	return l.next.RoundTrip(r)
}
func volgaEnvelope(operation, kind string, user int, bundle any) []byte {
	inner := map[string]any{"t": kind, "userId": user}
	if kind == "relay" {
		inner["message"] = map[string]any{"bundle": bundle}
	} else {
		inner["bundle"] = bundle
	}
	in, _ := json.Marshal(inner)
	out, _ := json.Marshal(map[string]any{"operation": operation, "message": string(in)})
	return out
}
func volgaRecord(p []byte) string {
	b := make([]byte, 2, len(p)+2)
	binary.BigEndian.PutUint16(b, uint16(len(p)))
	b = append(b, p...)
	return base64.StdEncoding.EncodeToString(b)
}
func awaitCounter(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("local carrier condition timed out")
		}
		time.Sleep(time.Millisecond)
	}
}

// Exercise original Dial and new DialContext against a REAL local WebSocket,
// not connectWSFn returning nil or a direct injection past handleMessage.
func TestVolgaWebSocketReceiveBoundaries(t *testing.T) {
	for _, baseline := range []bool{true, false} {
		t.Run(map[bool]string{true: "original-Dial", false: "DialContext"}[baseline], func(t *testing.T) {
			messages := make(chan []byte, 16)
			done := make(chan struct{})
			accepted := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, err := (&websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return r.Header.Get("Origin") == "https://volga.yandex.ru" }}).Upgrade(w, r, nil)
				if err != nil {
					return
				}
				defer conn.Close()
				close(accepted)
				for {
					select {
					case p := <-messages:
						if conn.WriteMessage(websocket.TextMessage, p) != nil {
							return
						}
					case <-done:
						return
					}
				}
			}))
			defer server.Close()
			defer close(done)
			stats := &VolgaStats{}
			cfg := DefaultVolgaConfig()
			cfg.WSReadTimeout = time.Second
			auth := &volgaAuth{UserID: 7, Session: &http.Client{}}
			got := make(chan []byte, 8)
			cb := func(p []byte) { got <- append([]byte(nil), p...) }
			finished := make(chan error, 1)
			if baseline {
				ws := newBaselineWSListener(auth, cfg, stats, &baselineRelayClient{}, cb)
				ws.testWSURL = "ws" + strings.TrimPrefix(server.URL, "http")
				defer ws.Stop()
				go func() { finished <- ws.connect() }()
			} else {
				ws := newWSListener(auth, cfg, stats, &relayClient{}, cb)
				ws.testWSURL = "ws" + strings.TrimPrefix(server.URL, "http")
				defer ws.Stop()
				go func() { finished <- ws.connect() }()
			}
			select {
			case <-accepted:
			case err := <-finished:
				t.Fatalf("handshake failed: %v", err)
			case <-time.After(3 * time.Second):
				t.Fatal("local handshake timed out")
			}
			awaitCounter(t, func() bool { return stats.wsConnected.Load() })
			if s := stats.volgaReceiveCounters.snapshot(); s.WSConnectSuccess != 1 || s.WSRawMessages != 0 || s.InnerPackets != 0 {
				t.Fatalf("no WS frames misdiagnosed: %+v", s)
			}
			messages <- []byte(`{"operation":"ping"}`)
			messages <- []byte(`not-json`)
			messages <- volgaEnvelope("SESSION", "relay", 7, []any{volgaRecord([]byte("self"))})
			messages <- volgaEnvelope("SESSION", "relay", 8, []any{"invalid-base64!"})
			messages <- volgaEnvelope("WORKER", "exchange", 8, map[string]any{"value": []any{volgaRecord([]byte("reply"))}})
			messages <- volgaEnvelope("SESSION", "relay", 8, []any{volgaRecord([]byte("reply2"))})
			awaitCounter(t, func() bool { return stats.innerPackets.Load() == 2 })
			want := VolgaReceiveDiagnostics{WSConnected: true, WSConnectSuccess: 1, WSRawMessages: 6, WSPingMessages: 1, WSSessionMessages: 3, WSWorkerMessages: 1, WSSelfIgnored: 1, WSRelayMessages: 2, WSExchangeMessages: 1, WSJSONErrors: 1, WSBase64Errors: 1, InnerPackets: 2, InnerBytes: 11}
			if actual := stats.volgaReceiveCounters.snapshot(); actual != want {
				t.Fatalf("receive boundaries: %+v", actual)
			}
			if !bytes.Equal(<-got, []byte("reply")) || !bytes.Equal(<-got, []byte("reply2")) {
				t.Fatal("WS payload changed")
			}
			// Close server side to end original blocking ReadMessage as well.
			messages <- nil // a valid empty frame; count is not used after this point
			select {
			case <-finished:
			case <-time.After(2 * time.Second):
				t.Fatal("reader did not terminate on deadline")
			}
			if stats.wsConnected.Load() {
				t.Fatal("WS remains connected after read exits")
			}
		})
	}
}

func TestVolgaWebSocketHandshakeFailureCounter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusForbidden) }))
	defer server.Close()
	for _, baseline := range []bool{true, false} {
		s := &VolgaStats{}
		auth := &volgaAuth{}
		cfg := DefaultVolgaConfig()
		var err error
		if baseline {
			w := newBaselineWSListener(auth, cfg, s, nil, nil)
			w.testWSURL = "ws" + strings.TrimPrefix(server.URL, "http")
			err = w.connect()
			w.Stop()
		} else {
			w := newWSListener(auth, cfg, s, nil, nil)
			w.testWSURL = "ws" + strings.TrimPrefix(server.URL, "http")
			err = w.connect()
			w.Stop()
		}
		if err == nil || s.wsConnectFailures.Load() != 1 || s.wsConnected.Load() || s.wsRawMessages.Load() != 0 {
			t.Fatal("failed handshake reported as connected")
		}
	}
}

func TestLocalRelayToWebSocketAgainstFrozenBase(t *testing.T) {
	for _, baseline := range []bool{true, false} {
		for _, codec := range []string{"batched", "legacy"} {
			for _, secret := range []string{"", "synthetic-diagnostic-secret"} {
				t.Run(map[bool]string{true: "original", false: "candidate"}[baseline]+"/"+codec+map[bool]string{true: "/AES", false: "/plain"}[secret != ""], func(t *testing.T) {
					messages := make(chan []byte, 8)
					done := make(chan struct{})
					peerRaw := &localCarrier{}
					peerRaw.send = func(p []byte) error {
						messages <- volgaEnvelope("SESSION", "relay", 9, []any{volgaRecord(p)})
						return nil
					}
					peer, err := basev100peer.Wrap(peerRaw, codec, secret, "synthetic-context", true)
					if err != nil {
						t.Fatal(err)
					}
					upload := make(chan []byte, 2)
					peer.Receive(func(p []byte) { upload <- append([]byte(nil), p...); _ = peer.Send(p) })
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						if r.URL.Path == "/ws" {
							conn, e := (&websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return r.Header.Get("Origin") == "https://volga.yandex.ru" }}).Upgrade(w, r, nil)
							if e != nil {
								return
							}
							defer conn.Close()
							for {
								select {
								case p := <-messages:
									if conn.WriteMessage(websocket.TextMessage, p) != nil {
										return
									}
								case <-done:
									return
								}
							}
						}
						var body struct {
							Message struct {
								Bundle []json.RawMessage `json:"bundle"`
							} `json:"message"`
						}
						if json.NewDecoder(r.Body).Decode(&body) != nil || len(body.Message.Bundle) != 3 {
							w.WriteHeader(400)
							return
						}
						var encoded string
						if json.Unmarshal(body.Message.Bundle[2], &encoded) != nil {
							w.WriteHeader(400)
							return
						}
						blob, e := base64.StdEncoding.DecodeString(encoded)
						if e != nil {
							w.WriteHeader(400)
							return
						}
						// Independent exact uint16 record framing at the fake exit.
						for len(blob) >= 2 {
							n := int(binary.BigEndian.Uint16(blob))
							blob = blob[2:]
							if n == 0 || n > len(blob) {
								w.WriteHeader(400)
								return
							}
							peerRaw.deliver(blob[:n])
							blob = blob[n:]
						}
						w.WriteHeader(204)
					}))
					defer server.Close()
					defer close(done)
					serverURL, _ := url.Parse(server.URL)
					httpTransport := &http.Transport{}
					defer httpTransport.CloseIdleConnections()
					cfg := DefaultVolgaConfig()
					cfg.WorkerCount = 2
					cfg.QueueSize = 16
					cfg.WSReadTimeout = time.Second // test resources only, not APK defaults
					auth := &volgaAuth{UserID: 7, Session: &http.Client{}}
					stats := &VolgaStats{}
					raw := &localCarrier{}
					if baseline {
						r := newBaselineRelayClient(auth, cfg, stats)
						r.httpClient.Transport = localRelayHTTP{serverURL, httpTransport}
						w := newBaselineWSListener(auth, cfg, stats, r, raw.deliver)
						w.testWSURL = "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"
						raw.start = func() error { r.Start(); w.Start(); return nil }
						raw.stop = func() error { w.Stop(); r.Stop(); return nil }
						raw.send = r.Send
					} else {
						r := newRelayClient(auth, cfg, stats)
						r.httpClient.Transport = localRelayHTTP{serverURL, httpTransport}
						w := newWSListener(auth, cfg, stats, r, raw.deliver)
						w.testWSURL = "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"
						raw.start = func() error { r.Start(); w.Start(); return nil }
						raw.stop = func() error { w.Stop(); r.Stop(); return nil }
						raw.send = r.Send
					}
					var client transport.Transport = raw
					if codec == "legacy" {
						client = transport.NewCompressedTransport(client)
					} else if baseline {
						client = transport.NewBaselineV100BatchedTransport(client)
					} else {
						client = transport.NewBatchedTransport(client)
					}
					if secret != "" {
						client, err = transport.NewEncryptedTransport(client, secret, "synthetic-context", false)
						if err != nil {
							t.Fatal(err)
						}
					}
					returned := make(chan []byte, 2)
					client.Receive(func(p []byte) { returned <- append([]byte(nil), p...) })
					if err = peer.Start(); err != nil {
						t.Fatal(err)
					}
					defer peer.Stop()
					if err = client.Start(); err != nil {
						t.Fatal(err)
					}
					defer client.Stop()
					awaitCounter(t, func() bool { return stats.wsConnected.Load() })
					payload := bytes.Repeat([]byte{0x45, 1, 2, 3}, 350)
					if err = client.Send(payload); err != nil {
						t.Fatal(err)
					}
					select {
					case p := <-upload:
						if !bytes.Equal(p, payload) {
							t.Fatal("HTTP outbound changed")
						}
					case <-time.After(3 * time.Second):
						t.Fatal("no HTTP outbound")
					}
					select {
					case p := <-returned:
						if !bytes.Equal(p, payload) {
							t.Fatal("WS return changed")
						}
					case <-time.After(3 * time.Second):
						t.Fatal("no WS return")
					}
					awaitCounter(t, func() bool { return stats.HTTPReqsSent.Load() == 1 })
					if stats.HTTPReqsFailed.Load() != 0 || stats.innerPackets.Load() != 1 || stats.wsRelayMessages.Load() != 1 {
						t.Fatal("carrier counters disagree")
					}
				})
			}
		}
	}
}

func TestBaselineConstructorKeepsOriginalResources(t *testing.T) {
	b := NewBaselineV100VolgaTransport("synthetic-context", transport.DefaultConfig())
	if b.config != DefaultVolgaConfig() {
		t.Fatal("baseline config changed")
	}
	buf := baselineB64BufPool.Get().([]byte)
	defer baselineB64BufPool.Put(buf)
	if cap(buf) < 16<<20 {
		t.Fatal("baseline pool is bounded candidate pool")
	}
	// The two encoders must copy their result before returning pooled memory.
	for _, n := range []int{1, 4096, 300000} {
		p := bytes.Repeat([]byte{23}, n)
		a, b := baselineBase64Encode(p), base64Encode(p)
		if a != b {
			t.Fatal("base64 pool changes bytes")
		}
		decoded, err := base64.StdEncoding.DecodeString(a)
		if err != nil || !bytes.Equal(p, decoded) {
			t.Fatal("base64 ownership")
		}
	}
}
