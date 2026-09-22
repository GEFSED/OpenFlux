package yandex

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

	"openflux/transport"
	"openflux/utils"
)

// Explicit mobile scheduling path, ported from Perf Lab 5386251.
// The original constructor in vyandex.go remains the Standard/CLI path.

var (
	configuredB64Pool = sync.Pool{
		New: func() interface{} { return make([]byte, 0, 4*1024) },
	}
	configuredJSONPool = sync.Pool{
		New: func() interface{} { return bytes.NewBuffer(make([]byte, 0, 128*1024)) },
	}
	configuredBlobPool = sync.Pool{
		New: func() interface{} { return bytes.NewBuffer(make([]byte, 0, 64*1024)) },
	}
)

func configuredBase64Encode(data []byte) string {
	buf := configuredB64Pool.Get().([]byte)
	need := base64.StdEncoding.EncodedLen(len(data))
	if cap(buf) < need {
		buf = make([]byte, need)
	} else {
		buf = buf[:need]
	}
	base64.StdEncoding.Encode(buf, data)
	out := string(buf)
	// Measurements in docs/perf: 256 KiB retains common batches while avoiding
	// multi-MiB retention per concurrent worker. Oversized buffers are transient.
	if retainBase64Buffer(cap(buf)) {
		configuredB64Pool.Put(buf[:0])
	}
	return out
}

func retainBase64Buffer(capacity int) bool { return capacity <= 256*1024 }

type configuredVolgaStats struct {
	volgaReceiveCounters
	httpFailures   volgaHTTPFailureCounters
	PacketsSent    atomic.Uint64
	PacketsRecv    atomic.Uint64
	BytesSent      atomic.Uint64
	BytesReceived  atomic.Uint64
	HTTPReqsSent   atomic.Uint64
	HTTPReqsFailed atomic.Uint64
	WSReconnects   atomic.Uint64
	QueueDrops     atomic.Uint64
	WorkerBusy     atomic.Int64
	PeakWorkerBusy atomic.Int64
	BatchesSent    atomic.Uint64
	PacketsBatched atomic.Uint64
}

type configuredRelayClient struct {
	auth   *volgaAuth
	config VolgaConfig
	stats  *configuredVolgaStats

	httpClient *http.Client
	workers    int
	batchQueue chan []byte
	enqueueMu  sync.Mutex
	wg         sync.WaitGroup
	ctx        context.Context
	cancel     context.CancelFunc

	bundleID atomic.Uint64
	seq      atomic.Uint64
	localID  atomic.Uint64

	mu       sync.Mutex
	frontier string

	rateLimit *relay429Gate
}

func newConfiguredRelayClient(auth *volgaAuth, cfg VolgaConfig, stats *configuredVolgaStats) *configuredRelayClient {
	tr := &http.Transport{
		MaxIdleConns:        cfg.MaxIdleConns,
		MaxIdleConnsPerHost: cfg.MaxIdleConnsPerHost,
		IdleConnTimeout:     cfg.IdleConnTimeout,
		DisableCompression:  true,
		ForceAttemptHTTP2:   true,
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &configuredRelayClient{
		auth:   auth,
		config: cfg,
		stats:  stats,
		httpClient: &http.Client{
			Transport: tr,
			Timeout:   cfg.RelayTimeout,
			Jar:       auth.Session.Jar,
		},
		workers:    cfg.WorkerCount,
		batchQueue: make(chan []byte, cfg.QueueSize),
		ctx:        ctx,
		cancel:     cancel,
		rateLimit:  newRelay429Gate(cfg.RateLimit429GuardEnabled),
	}
}

func (r *configuredRelayClient) Start() {
	for i := 0; i < r.workers; i++ {
		r.wg.Add(1)
		go r.worker(i)
	}
	utils.Debugf("[VOLGA] relay pool started: %d workers, batch=%d timeout=%v",
		r.workers, r.config.BatchSize, r.config.BatchTimeout)
}

func (r *configuredRelayClient) Stop() {
	r.enqueueMu.Lock()
	r.cancel()
	r.enqueueMu.Unlock()
	r.wg.Wait()
	r.httpClient.CloseIdleConnections()
	for {
		select {
		case <-r.batchQueue:
		default:
			return
		}
	}
}

func (r *configuredRelayClient) Send(data []byte) error {
	r.enqueueMu.Lock()
	defer r.enqueueMu.Unlock()
	if err := r.ctx.Err(); err != nil {
		return err
	}
	if len(data) == 0 {
		return nil
	}
	if len(data) > r.config.MaxPayloadBytes {
		return fmt.Errorf("packet too large: %d > %d", len(data), r.config.MaxPayloadBytes)
	}

	cp := make([]byte, len(data))
	copy(cp, data)

	select {
	case <-r.ctx.Done():
		return r.ctx.Err()
	case r.batchQueue <- cp:
		return nil
	default:
		r.stats.QueueDrops.Add(1)
		return fmt.Errorf("queue full")
	}
}

func (r *configuredRelayClient) worker(id int) {
	defer r.wg.Done()

	batch := make([][]byte, 0, r.config.BatchSize)
	totalBytes := 0
	timer := time.NewTimer(r.config.BatchTimeout)
	if !timer.Stop() {
		<-timer.C
	}
	defer timer.Stop()

	flush := func() {
		if len(batch) == 0 {
			return
		}
		busy := r.stats.WorkerBusy.Add(1)
		for peak := r.stats.PeakWorkerBusy.Load(); busy > peak; peak = r.stats.PeakWorkerBusy.Load() {
			if r.stats.PeakWorkerBusy.CompareAndSwap(peak, busy) {
				break
			}
		}
		err := r.sendBatch(batch)
		if err != nil {
			r.stats.HTTPReqsFailed.Add(1)
			utils.Debugf("[VOLGA] batch send failed: %v", err)
		} else {
			r.stats.HTTPReqsSent.Add(1)
			r.stats.BatchesSent.Add(1)
		}
		r.stats.WorkerBusy.Add(-1)
		batch = batch[:0]
		totalBytes = 0
	}

	for {
		select {
		case <-r.ctx.Done():
			flush()
			return

		case pkt, ok := <-r.batchQueue:
			if !ok {
				flush()
				return
			}
			batch = append(batch, pkt)
			totalBytes += len(pkt)

			if len(batch) >= r.config.BatchSize || totalBytes >= r.config.BatchMaxBytes {
				flush()
			} else if len(batch) == 1 {
				timer.Reset(r.config.BatchTimeout)
			}

		case <-timer.C:
			flush()
		}
	}
}

func (r *configuredRelayClient) sendBatch(batch [][]byte) error {
	blob := configuredBlobPool.Get().(*bytes.Buffer)
	blob.Reset()

	var lenBuf [2]byte
	var totalBytes int
	for _, p := range batch {
		binary.BigEndian.PutUint16(lenBuf[:], uint16(len(p)))
		blob.Write(lenBuf[:])
		blob.Write(p)
		totalBytes += len(p)
	}

	encoded := configuredBase64Encode(blob.Bytes())
	configuredBlobPool.Put(blob)

	frontier := r.getFrontier()
	opID := fmt.Sprintf("1-%d.%d", r.auth.UserID, r.seq.Add(1))
	relayOpID := fmt.Sprintf("1-%d.%d", r.auth.UserID, r.seq.Add(1))

	bundle := []interface{}{
		map[string]interface{}{
			"id":         opID,
			"frontier":   frontier,
			"undoable":   true,
			"actionName": "textInsert",
			"ops":        []interface{}{[]interface{}{"it", "vyd:t/00000000000008", 0, "A"}},
			"sideEffect": false,
			"localId":    r.localID.Add(1),
		},
		map[string]interface{}{
			"id":         relayOpID,
			"frontier":   []interface{}{opID},
			"undoable":   false,
			"actionName": "setCaret",
			"ops": []interface{}{
				[]interface{}{"us", r.auth.UserID, []interface{}{
					[]interface{}{
						[]interface{}{"vyd:t/00000000000008", 0, -1},
						[]interface{}{"vyd:t/00000000000008", 0, -1},
					},
				}},
			},
			"sideEffect": true,
			"localId":    r.localID.Add(1),
		},
		encoded,
	}

	payload := map[string]interface{}{
		"message": map[string]interface{}{
			"bundleId": r.bundleID.Add(1),
			"bundle":   bundle,
		},
		"targetUserId": nil,
	}

	buf := configuredJSONPool.Get().(*bytes.Buffer)
	buf.Reset()
	enc := json.NewEncoder(buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(payload); err != nil {
		configuredJSONPool.Put(buf)
		return err
	}
	bodyCopy := make([]byte, buf.Len())
	copy(bodyCopy, buf.Bytes())
	configuredJSONPool.Put(buf)

	urlStr := fmt.Sprintf("https://volga.yandex.ru/session/main/%s/relay", r.auth.RequestPath)
	req, err := http.NewRequestWithContext(r.ctx, "POST", urlStr, bytes.NewReader(bodyCopy))
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", volgaUserAgent)
	req.Header.Set("Authorization", "Bearer "+r.auth.Token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://volga.yandex.ru")
	req.Header.Set("Referer", "https://volga.yandex.ru/document/?request-path="+r.auth.RequestPath)
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Sec-Fetch-Dest", "empty")
	req.Header.Set("Sec-Fetch-Mode", "cors")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.ContentLength = int64(len(bodyCopy))

	var cookieParts []string
	for _, c := range r.auth.Cookies {
		cookieParts = append(cookieParts, c.Name+"="+c.Value)
	}
	if len(cookieParts) > 0 {
		req.Header.Set("Cookie", strings.Join(cookieParts, "; "))
	}

	var permit uint64
	if r.rateLimit != nil {
		permit, err = r.rateLimit.wait(r.ctx)
		if err != nil {
			return err
		}
	}
	resp, err := r.httpClient.Do(req)
	if err != nil {
		r.stats.httpFailures.recordNetwork(err)
		return err
	}
	defer resp.Body.Close()
	if r.rateLimit != nil && resp.StatusCode == http.StatusTooManyRequests {
		r.rateLimit.rejected(resp.Header.Values("Retry-After"))
	}
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != 204 && resp.StatusCode != 200 {
		r.stats.httpFailures.recordStatus(resp.StatusCode)
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	if r.rateLimit != nil {
		r.rateLimit.succeeded(permit)
	}

	r.stats.PacketsSent.Add(uint64(len(batch)))
	r.stats.PacketsBatched.Add(uint64(len(batch)))
	r.stats.BytesSent.Add(uint64(totalBytes))
	return nil
}

func (r *configuredRelayClient) SetFrontier(opID string) {
	r.mu.Lock()
	r.frontier = opID
	r.mu.Unlock()
}

func (r *configuredRelayClient) getFrontier() []interface{} {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.frontier == "" {
		return []interface{}{}
	}
	return []interface{}{r.frontier}
}

type configuredWSListener struct {
	// Per-instance local-server seams; unset in production.
	testWSURL string
	// Per-instance dependency seam for local lifecycle tests (nil uses network).
	connectFn func() error
	auth      *volgaAuth
	config    VolgaConfig
	stats     *configuredVolgaStats
	relay     *configuredRelayClient
	onData    func([]byte)

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func newConfiguredWSListener(auth *volgaAuth, cfg VolgaConfig, stats *configuredVolgaStats,
	relay *configuredRelayClient, onData func([]byte)) *configuredWSListener {

	ctx, cancel := context.WithCancel(context.Background())
	return &configuredWSListener{
		auth:   auth,
		config: cfg,
		stats:  stats,
		relay:  relay,
		onData: onData,
		ctx:    ctx,
		cancel: cancel,
	}
}

func (w *configuredWSListener) Start() {
	w.wg.Add(1)
	go func() { defer w.wg.Done(); w.run() }()
}

func (w *configuredWSListener) Stop() {
	w.cancel()
	w.wg.Wait()
}

func (w *configuredWSListener) run() {
	delay := w.config.ReconnectMinDelay

	for {
		select {
		case <-w.ctx.Done():
			return
		default:
		}

		connect := w.connect
		if w.connectFn != nil {
			connect = w.connectFn
		}
		if err := connect(); err != nil {
			utils.Debugf("[VOLGA] WS error: %v", err)
		}
		if w.ctx.Err() != nil {
			return
		}

		w.stats.WSReconnects.Add(1)
		utils.Debugf("[VOLGA] WS reconnect in %v", delay)
		select {
		case <-time.After(delay):
		case <-w.ctx.Done():
			return
		}

		delay = time.Duration(float64(delay) * w.config.ReconnectMultiplier)
		if delay > w.config.ReconnectMaxDelay {
			delay = w.config.ReconnectMaxDelay
		}
	}
}

func (w *configuredWSListener) connect() error {
	wsURL := "wss://push.yandex.ru/v2/subscribe/websocket?" +
		"service=volga" +
		"&user=" + url.QueryEscape(w.auth.UserIDStr) +
		"&sign=" + w.auth.Sign +
		"&ts=" + w.auth.TS +
		"&client=web" +
		"&session=" + w.auth.SessionID +
		"&fetch_history=" + url.QueryEscape(w.auth.UserIDStr+":volga:0:1") +
		"&x_request_attempt=0"

	header := http.Header{}
	header.Set("User-Agent", volgaUserAgent)
	header.Set("Origin", "https://volga.yandex.ru")

	var cookieParts []string
	for _, c := range w.auth.Cookies {
		cookieParts = append(cookieParts, c.Name+"="+c.Value)
	}
	header.Set("Cookie", strings.Join(cookieParts, "; "))

	dialer := websocket.Dialer{
		HandshakeTimeout: w.config.WSHandshakeTimeout,
		ReadBufferSize:   4 << 20,
		WriteBufferSize:  4 << 20,
	}

	if w.testWSURL != "" {
		wsURL = w.testWSURL
	}

	conn, resp, err := dialer.DialContext(w.ctx, wsURL, header)
	if err != nil {
		w.stats.wsConnectFailures.Add(1)
		if resp != nil && resp.Body != nil {
			resp.Body.Close()
		}
		return fmt.Errorf("dial: %w", err)
	}
	defer conn.Close()
	w.stats.wsConnectSuccess.Add(1)
	w.stats.wsConnected.Store(true)
	defer w.stats.wsConnected.Store(false)
	stopClose := context.AfterFunc(w.ctx, func() { conn.Close() })
	defer stopClose()

	utils.Debugf("[VOLGA] WS connected: user=%s", w.auth.UserIDStr)

	for {
		select {
		case <-w.ctx.Done():
			return nil
		default:
		}

		conn.SetReadDeadline(time.Now().Add(w.config.WSReadTimeout))
		_, msg, err := conn.ReadMessage()
		if err != nil {
			return fmt.Errorf("read: %w", err)
		}

		w.handleMessage(msg)
	}
}

func (w *configuredWSListener) handleMessage(raw []byte) {
	w.stats.wsRawMessages.Add(1)
	var envelope struct {
		Operation string `json:"operation"`
		Message   string `json:"message"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		w.stats.wsJSONErrors.Add(1)
		return
	}

	if envelope.Operation == "ping" {
		w.stats.wsPingMessages.Add(1)
		return
	}
	if envelope.Operation != "SESSION" && envelope.Operation != "WORKER" {
		return
	}
	if envelope.Operation == "SESSION" {
		w.stats.wsSessionMessages.Add(1)
	} else {
		w.stats.wsWorkerMessages.Add(1)
	}
	if envelope.Message == "" {
		return
	}

	var inner struct {
		T       string          `json:"t"`
		UserID  int             `json:"userId"`
		Bundle  json.RawMessage `json:"bundle"`
		Message json.RawMessage `json:"message"`
	}
	if err := json.Unmarshal([]byte(envelope.Message), &inner); err != nil {
		w.stats.wsJSONErrors.Add(1)
		return
	}

	if inner.UserID == w.auth.UserID {
		w.stats.wsSelfIgnored.Add(1)
		return
	}

	switch inner.T {
	case "relay":
		w.stats.wsRelayMessages.Add(1)
		w.handleRelayMessage(inner.Message)
	case "exchange":
		w.stats.wsExchangeMessages.Add(1)
		w.handleBundle(inner.Bundle)
	}
}

func (w *configuredWSListener) handleRelayMessage(raw json.RawMessage) {
	var relay struct {
		Bundle []json.RawMessage `json:"bundle"`
	}
	if err := json.Unmarshal(raw, &relay); err != nil {
		w.stats.wsJSONErrors.Add(1)
		return
	}
	for _, item := range relay.Bundle {
		w.handleBundleItem(item)
	}
}

func (w *configuredWSListener) handleBundle(raw json.RawMessage) {
	var asArray []json.RawMessage
	if err := json.Unmarshal(raw, &asArray); err == nil {
		for _, item := range asArray {
			w.handleBundleItem(item)
		}
		return
	}

	var asObject struct {
		Value []json.RawMessage `json:"value"`
	}
	if err := json.Unmarshal(raw, &asObject); err == nil {
		for _, item := range asObject.Value {
			w.handleBundleItem(item)
		}
	} else {
		w.stats.wsJSONErrors.Add(1)
	}
}

func (w *configuredWSListener) handleBundleItem(raw json.RawMessage) {
	var asObj struct {
		ID     string `json:"id"`
		Action string `json:"actionName"`
	}
	if err := json.Unmarshal(raw, &asObj); err == nil && asObj.Action != "" {
		if asObj.ID != "" {
			w.relay.SetFrontier(asObj.ID)
		}
		return
	}

	if !json.Valid(raw) {
		w.stats.wsJSONErrors.Add(1)
	}
	var asStr string
	if err := json.Unmarshal(raw, &asStr); err == nil && asStr != "" {
		decoded, err := base64.StdEncoding.DecodeString(asStr)
		if err != nil {
			w.stats.wsBase64Errors.Add(1)
			return
		}
		packets := decodeBatch(decoded)
		w.stats.PacketsRecv.Add(uint64(len(packets)))
		w.stats.BytesReceived.Add(uint64(len(decoded)))
		for _, pkt := range packets {
			w.stats.innerPackets.Add(1)
			w.stats.innerBytes.Add(uint64(len(pkt)))
			if w.onData != nil {
				w.onData(pkt)
			}
		}
	}
}

type ConfiguredVolgaTransport struct {
	*transport.BaseTransport
	// Defaults remain the real provider; tests substitute auth/WS without
	// process-global hooks or external credentials/network traffic.
	authorizeFn func(string) (*volgaAuth, error)
	connectWSFn func(*configuredWSListener) error

	docURL string
	config VolgaConfig
	stats  *configuredVolgaStats

	auth  *volgaAuth
	relay *configuredRelayClient
	ws    *configuredWSListener

	onDataMu sync.RWMutex
	onData   func([]byte)

	keepAliveStop chan struct{}
	lifecycle     sync.Mutex
	relayMu       sync.RWMutex
	loops         sync.WaitGroup
}

func NewYandexVolgaTransportWithConfig(docURL string, cfg transport.TransportConfig, volga VolgaConfig) (*ConfiguredVolgaTransport, error) {
	if volga.WorkerCount < 1 || volga.WorkerCount > 2000 || volga.QueueSize < 1 || volga.QueueSize > 1000000 || volga.BatchSize < 1 || volga.BatchSize > 1024 || volga.BatchTimeout < 0 || volga.BatchTimeout > time.Second || volga.BatchMaxBytes < 1 || volga.MaxIdleConns < 1 || volga.MaxIdleConnsPerHost < 1 || volga.RelayTimeout <= 0 || volga.KeepAliveInterval <= 0 || volga.WSReadTimeout <= 0 {
		return nil, fmt.Errorf("invalid Volga configuration")
	}
	return newConfiguredVolgaTransport(docURL, cfg, volga), nil
}
func newConfiguredVolgaTransport(docURL string, cfg transport.TransportConfig, volga VolgaConfig) *ConfiguredVolgaTransport {
	return &ConfiguredVolgaTransport{
		BaseTransport: transport.NewBaseTransport(cfg),
		docURL:        docURL,
		config:        volga,
		stats:         &configuredVolgaStats{},
		keepAliveStop: make(chan struct{}),
	}
}

func (t *ConfiguredVolgaTransport) Start() error {
	t.lifecycle.Lock()
	defer t.lifecycle.Unlock()
	if t.IsRunning() {
		return nil
	}
	t.keepAliveStop = make(chan struct{})
	if err := t.BaseTransport.Start(); err != nil {
		return err
	}

	utils.Debugf("[VOLGA] authorizing...")
	authorizer := authorize
	if t.authorizeFn != nil {
		authorizer = t.authorizeFn
	}
	auth, err := authorizer(t.docURL)
	if err != nil {
		t.BaseTransport.Stop()
		return fmt.Errorf("auth: %w", err)
	}
	t.auth = auth

	t.relayMu.Lock()
	t.relay = newConfiguredRelayClient(auth, t.config, t.stats)
	t.relayMu.Unlock()
	t.relay.Start()

	t.ws = newConfiguredWSListener(auth, t.config, t.stats, t.relay, func(data []byte) {
		t.onDataMu.RLock()
		cb := t.onData
		t.onDataMu.RUnlock()
		if cb != nil {
			cb(data)
		}
		t.RecordReceive(len(data))
	})
	if t.connectWSFn != nil {
		listener := t.ws
		t.ws.connectFn = func() error { return t.connectWSFn(listener) }
	}
	t.ws.Start()

	t.loops.Add(2)
	go func() { defer t.loops.Done(); t.keepAliveLoop() }()
	go func() { defer t.loops.Done(); t.statsLoop() }()
	t.SetConnected(true)

	utils.Debugf("[VOLGA] transport started: user=%d(%s) rp=%s",
		auth.UserID, auth.UserIDStr, auth.RequestPath)
	return nil
}

func (t *ConfiguredVolgaTransport) Stop() error {
	t.lifecycle.Lock()
	defer t.lifecycle.Unlock()
	select {
	case <-t.keepAliveStop:
	default:
		close(t.keepAliveStop)
	}
	if t.ws != nil {
		t.ws.Stop()
	}
	if t.relay != nil {
		t.relay.Stop()
	}
	t.loops.Wait()
	if t.auth != nil && t.auth.Session != nil {
		t.auth.Session.CloseIdleConnections()
	}
	t.SetConnected(false)
	return t.BaseTransport.Stop()
}

func (t *ConfiguredVolgaTransport) Send(data []byte) error {
	t.relayMu.RLock()
	r := t.relay
	t.relayMu.RUnlock()
	if r == nil {
		return fmt.Errorf("transport not started")
	}
	return r.Send(data)
}

func (t *ConfiguredVolgaTransport) Receive(callback func([]byte)) {
	t.onDataMu.Lock()
	t.onData = callback
	t.onDataMu.Unlock()
}

func (t *ConfiguredVolgaTransport) IsConnected() bool {
	return t.BaseTransport.IsConnected()
}

func (t *ConfiguredVolgaTransport) Stats() transport.TransportStats {
	base := t.BaseTransport.Stats()
	return transport.TransportStats{
		BytesSent:     t.stats.BytesSent.Load(),
		BytesReceived: t.stats.BytesReceived.Load(),
		PacketsSent:   t.stats.PacketsSent.Load(),
		PacketsRecv:   t.stats.PacketsRecv.Load(),
		Reconnects:    t.stats.WSReconnects.Load(),
		Connected:     t.IsConnected(),
		Uptime:        base.Uptime,
	}
}

func (t *ConfiguredVolgaTransport) keepAliveLoop() {
	ticker := time.NewTicker(t.config.KeepAliveInterval)
	defer ticker.Stop()

	for {
		select {
		case <-t.keepAliveStop:
			return
		case <-ticker.C:
			if !t.IsRunning() {
				return
			}
			_ = t.relay.Send([]byte{0x00})
		}
	}
}

func (t *ConfiguredVolgaTransport) statsLoop() {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	var lastSent, lastBytes, lastHTTP, lastFailed, lastRecv, lastRecvBytes, lastBatches, lastBatched uint64

	for {
		select {
		case <-t.keepAliveStop:
			return
		case <-ticker.C:
			sent := t.stats.PacketsSent.Load()
			bytes := t.stats.BytesSent.Load()
			httpReqs := t.stats.HTTPReqsSent.Load()
			failed := t.stats.HTTPReqsFailed.Load()
			recv := t.stats.PacketsRecv.Load()
			recvBytes := t.stats.BytesReceived.Load()
			batches := t.stats.BatchesSent.Load()
			batched := t.stats.PacketsBatched.Load()

			utils.Debugf("[VOLGA-STATS] send %d pkt/s (%d KB/s) | http %d req/s fail %d | batch %d (avg %.1f pkt) | recv %d pkt/s (%d KB/s) | busy %d/%d",
				(sent-lastSent)/5, (bytes-lastBytes)/5/1024,
				(httpReqs-lastHTTP)/5, failed-lastFailed,
				(batches-lastBatches)/5,
				float64(batched-lastBatched)/float64(maxU64(batches-lastBatches, 1)),
				(recv-lastRecv)/5, (recvBytes-lastRecvBytes)/5/1024,
				t.stats.WorkerBusy.Load(), t.config.WorkerCount)

			lastSent, lastBytes = sent, bytes
			lastHTTP, lastFailed = httpReqs, failed
			lastRecv, lastRecvBytes = recv, recvBytes
			lastBatches, lastBatched = batches, batched
		}
	}
}
