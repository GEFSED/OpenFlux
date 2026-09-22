// Diagnostic control copied from Android v1.0.0 8566f727c8238436728758f139130cef433147b7.
// Scheduling, queues, Dial and Stop deliberately retain original behavior.
// See docs/perf/RETURN_PATH_AUDIT.md before changing this file.
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

var baselineB64BufPool = sync.Pool{New: func() interface{} { return make([]byte, 0, 16*1024*1024) }}

func baselineBase64Encode(data []byte) string {
	buf := baselineB64BufPool.Get().([]byte)
	need := base64.StdEncoding.EncodedLen(len(data))
	if cap(buf) < need {
		buf = make([]byte, need)
	} else {
		buf = buf[:need]
	}
	base64.StdEncoding.Encode(buf, data)
	out := string(buf)
	baselineB64BufPool.Put(buf[:0])
	return out
}

type baselineRelayClient struct {
	auth   *volgaAuth
	config VolgaConfig
	stats  *VolgaStats

	httpClient *http.Client
	workers    int
	queue      chan []byte
	batchQueue chan []byte
	wg         sync.WaitGroup
	ctx        context.Context
	cancel     context.CancelFunc

	bundleID atomic.Uint64
	seq      atomic.Uint64
	localID  atomic.Uint64

	mu       sync.Mutex
	frontier string
}

func newBaselineRelayClient(auth *volgaAuth, cfg VolgaConfig, stats *VolgaStats) *baselineRelayClient {
	tr := &http.Transport{
		MaxIdleConns:        cfg.MaxIdleConns,
		MaxIdleConnsPerHost: cfg.MaxIdleConnsPerHost,
		IdleConnTimeout:     cfg.IdleConnTimeout,
		DisableCompression:  true,
		ForceAttemptHTTP2:   true,
	}

	ctx, cancel := context.WithCancel(context.Background())

	return &baselineRelayClient{
		auth:   auth,
		config: cfg,
		stats:  stats,
		httpClient: &http.Client{
			Transport: tr,
			Timeout:   cfg.RelayTimeout,
			Jar:       auth.Session.Jar,
		},
		workers:    cfg.WorkerCount,
		queue:      make(chan []byte, cfg.QueueSize),
		batchQueue: make(chan []byte, cfg.QueueSize),
		ctx:        ctx,
		cancel:     cancel,
	}
}

func (r *baselineRelayClient) Start() {
	for i := 0; i < r.workers; i++ {
		r.wg.Add(1)
		go r.worker(i)
	}
	utils.Debugf("[VOLGA] relay pool started: %d workers, batch=%d timeout=%v",
		r.workers, r.config.BatchSize, r.config.BatchTimeout)
}

func (r *baselineRelayClient) Stop() {
	r.cancel()
	close(r.queue)
	close(r.batchQueue)
	r.wg.Wait()
}

func (r *baselineRelayClient) Send(data []byte) error {
	if len(data) == 0 {
		return nil
	}
	if len(data) > r.config.MaxPayloadBytes {
		return fmt.Errorf("packet too large: %d > %d", len(data), r.config.MaxPayloadBytes)
	}

	cp := make([]byte, len(data))
	copy(cp, data)

	select {
	case r.batchQueue <- cp:
		return nil
	default:
		r.stats.QueueDrops.Add(1)
		return fmt.Errorf("queue full")
	}
}

func (r *baselineRelayClient) worker(id int) {
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

func (r *baselineRelayClient) sendBatch(batch [][]byte) error {
	blob := blobBufPool.Get().(*bytes.Buffer)
	blob.Reset()

	var lenBuf [2]byte
	var totalBytes int
	for _, p := range batch {
		binary.BigEndian.PutUint16(lenBuf[:], uint16(len(p)))
		blob.Write(lenBuf[:])
		blob.Write(p)
		totalBytes += len(p)
	}

	encoded := baselineBase64Encode(blob.Bytes())
	blobBufPool.Put(blob)

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

	buf := jsonBufPool.Get().(*bytes.Buffer)
	buf.Reset()
	enc := json.NewEncoder(buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(payload); err != nil {
		jsonBufPool.Put(buf)
		return err
	}
	bodyCopy := make([]byte, buf.Len())
	copy(bodyCopy, buf.Bytes())
	jsonBufPool.Put(buf)

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

	resp, err := r.httpClient.Do(req)
	if err != nil {
		r.stats.httpFailures.recordNetwork(err)
		return err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode != 204 && resp.StatusCode != 200 {
		r.stats.httpFailures.recordStatus(resp.StatusCode)
		return fmt.Errorf("status %d", resp.StatusCode)
	}

	r.stats.PacketsSent.Add(uint64(len(batch)))
	r.stats.PacketsBatched.Add(uint64(len(batch)))
	r.stats.BytesSent.Add(uint64(totalBytes))
	return nil
}

func (r *baselineRelayClient) SetFrontier(opID string) {
	r.mu.Lock()
	r.frontier = opID
	r.mu.Unlock()
}

func (r *baselineRelayClient) getFrontier() []interface{} {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.frontier == "" {
		return []interface{}{}
	}
	return []interface{}{r.frontier}
}

type baselineWSListener struct {
	// Per-instance local-server seams; unset in production.
	testWSURL string
	auth      *volgaAuth
	config    VolgaConfig
	stats     *VolgaStats
	relay     *baselineRelayClient
	onData    func([]byte)

	ctx    context.Context
	cancel context.CancelFunc
}

func newBaselineWSListener(auth *volgaAuth, cfg VolgaConfig, stats *VolgaStats,
	relay *baselineRelayClient, onData func([]byte)) *baselineWSListener {

	ctx, cancel := context.WithCancel(context.Background())
	return &baselineWSListener{
		auth:   auth,
		config: cfg,
		stats:  stats,
		relay:  relay,
		onData: onData,
		ctx:    ctx,
		cancel: cancel,
	}
}

func (w *baselineWSListener) Start() {
	go w.run()
}

func (w *baselineWSListener) Stop() {
	w.cancel()
}

func (w *baselineWSListener) run() {
	delay := w.config.ReconnectMinDelay

	for {
		select {
		case <-w.ctx.Done():
			return
		default:
		}

		if err := w.connect(); err != nil {
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

func (w *baselineWSListener) connect() error {
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

	conn, _, err := dialer.Dial(wsURL, header)
	if err != nil {
		w.stats.wsConnectFailures.Add(1)
		return fmt.Errorf("dial: %w", err)
	}
	defer conn.Close()
	w.stats.wsConnectSuccess.Add(1)
	w.stats.wsConnected.Store(true)
	defer w.stats.wsConnected.Store(false)

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

func (w *baselineWSListener) handleMessage(raw []byte) {
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

func (w *baselineWSListener) handleRelayMessage(raw json.RawMessage) {
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

func (w *baselineWSListener) handleBundle(raw json.RawMessage) {
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

func (w *baselineWSListener) handleBundleItem(raw json.RawMessage) {
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

type BaselineV100VolgaTransport struct {
	*transport.BaseTransport
	authorizeFn func(string) (*volgaAuth, error) // local tests only; nil uses original auth

	docURL string
	config VolgaConfig
	stats  *VolgaStats

	auth  *volgaAuth
	relay *baselineRelayClient
	ws    *baselineWSListener

	onDataMu sync.RWMutex
	onData   func([]byte)

	keepAliveStop chan struct{}
}

func NewBaselineV100VolgaTransport(docURL string, cfg transport.TransportConfig) *BaselineV100VolgaTransport {
	return &BaselineV100VolgaTransport{
		BaseTransport: transport.NewBaseTransport(cfg),
		docURL:        docURL,
		config:        DefaultVolgaConfig(),
		stats:         &VolgaStats{},
		keepAliveStop: make(chan struct{}),
	}
}

func (t *BaselineV100VolgaTransport) Start() error {
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
		return fmt.Errorf("auth: %w", err)
	}
	t.auth = auth

	t.relay = newBaselineRelayClient(auth, t.config, t.stats)
	t.relay.Start()

	t.ws = newBaselineWSListener(auth, t.config, t.stats, t.relay, func(data []byte) {
		t.onDataMu.RLock()
		cb := t.onData
		t.onDataMu.RUnlock()
		if cb != nil {
			cb(data)
		}
		t.RecordReceive(len(data))
	})
	t.ws.Start()

	go t.keepAliveLoop()
	go t.statsLoop()
	t.SetConnected(true)

	utils.Debugf("[VOLGA] transport started: user=%d(%s) rp=%s",
		auth.UserID, auth.UserIDStr, auth.RequestPath)
	return nil
}

func (t *BaselineV100VolgaTransport) Stop() error {
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
	t.SetConnected(false)
	return t.BaseTransport.Stop()
}

func (t *BaselineV100VolgaTransport) Send(data []byte) error {
	if t.relay == nil {
		return fmt.Errorf("transport not started")
	}
	return t.relay.Send(data)
}

func (t *BaselineV100VolgaTransport) Receive(callback func([]byte)) {
	t.onDataMu.Lock()
	t.onData = callback
	t.onDataMu.Unlock()
}

func (t *BaselineV100VolgaTransport) IsConnected() bool {
	return t.BaseTransport.IsConnected()
}

func (t *BaselineV100VolgaTransport) Stats() transport.TransportStats {
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

func (t *BaselineV100VolgaTransport) keepAliveLoop() {
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

func (t *BaselineV100VolgaTransport) statsLoop() {
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
