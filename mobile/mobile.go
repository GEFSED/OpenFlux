// Package mobile exposes complete IP packets to Android through gomobile.
package mobile

import (
	"encoding/json"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"openflux/transport"
	"openflux/transport/cupsonline"
	"openflux/transport/mailru"
	"openflux/transport/oneme"
	"openflux/transport/yandex"
	"openflux/utils"
)

var client = packetClient{}
var perfLab atomic.Bool
var loggingOnce sync.Once

// EnablePerformanceLab must be called before starting either Android service.
// Lab builds suppress underlying debug logs (which can include provider auth).
func EnablePerformanceLab() { perfLab.Store(true) }
func configureLogging() {
	loggingOnce.Do(func() {
		if perfLab.Load() {
			utils.SetDebug(false)
		} else {
			utils.EnableDebug()
		}
		utils.SetLogSink(appendLog)
	})
}

type packetClient struct {
	mu      sync.Mutex
	startMu sync.Mutex
	session *packetSession
	logs    []string
}
type packetSession struct {
	mu                                            sync.Mutex
	transport                                     transport.Transport
	volga                                         *yandex.YandexVolgaTransport
	outer                                         *transport.BatchedTransport
	profile, transportType                        string
	ready, stopped                                bool
	packets                                       [][]byte
	head, count                                   int
	notify, done                                  chan struct{}
	reads, waits, timeouts, drops, sent, received uint64
}

func newPacketSession(profile, kind string, depth int) *packetSession {
	return &packetSession{profile: profile, transportType: kind, packets: make([][]byte, depth), notify: make(chan struct{}, 1), done: make(chan struct{})}
}
func appendLog(message string) {
	// Performance exports never use the transport log buffer. Also keep the lab
	// log viewer safe even if a caller returns an error containing a provider URL.
	if perfLab.Load() && !strings.HasPrefix(message, "[PERF]") {
		message = "[ANDROID] Событие транспорта (подробности скрыты в Perf Lab)"
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	client.logs = append(client.logs, message)
	if len(client.logs) > 500 {
		client.logs = append([]string(nil), client.logs[len(client.logs)-500:]...)
	}
}
func currentSession() *packetSession {
	client.mu.Lock()
	defer client.mu.Unlock()
	return client.session
}

// Start is the compatibility entry point; CLI/old Android scheduling is kept.
func Start(kind, url, secret, codec, maxToken, maxUid string) string {
	return startClient(kind, url, secret, codec, maxToken, maxUid, "baseline", false)
}

// StartWithProfile is used only by the experimental Android APK.
func StartWithProfile(kind, url, secret, codec, maxToken, maxUid, profile string) string {
	EnablePerformanceLab()
	return startClient(kind, url, secret, codec, maxToken, maxUid, profile, true)
}
func startClient(kind, url, secret, codec, maxToken, maxUid, profile string, lab bool) string {
	if kind == "" {
		kind = "yandex"
	}
	if kind != "oneme" && url == "" {
		return "Ссылка на документ не указана"
	}
	if secret != "" && len(secret) < 16 {
		return "Ключ шифрования должен содержать не менее 16 символов"
	}
	profile = normalizeProfile(profile)
	client.startMu.Lock()
	defer client.startMu.Unlock()
	if old := currentSession(); old != nil {
		old.mu.Lock()
		running := !old.stopped
		old.mu.Unlock()
		if running {
			return ""
		}
	}
	configureLogging()
	s := newPacketSession(profile, kind, transport.DefaultConfig().MaxQueueSize)
	client.mu.Lock()
	client.session = s
	client.logs = nil
	client.mu.Unlock()
	trans, err := buildPacketTransport(s, url, secret, codec, maxToken, maxUid, lab)
	if err != nil {
		s.stop()
		return "Не удалось создать транспорт"
	}
	return finishStart(s, trans)
}

// Both production startup and lifecycle tests use this transaction. Stop while
// Start is pending wakes readers immediately; the starting caller owns cleanup.
func finishStart(s *packetSession, trans transport.Transport) string {
	trans.Receive(s.enqueue)
	if err := trans.Start(); err != nil {
		s.stop()
		_ = trans.Stop()
		appendLog("[ERROR] Ошибка запуска транспорта")
		return "Ошибка запуска транспорта"
	}
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		_ = trans.Stop()
		return "Запуск отменён"
	}
	s.transport = trans
	s.ready = true
	s.mu.Unlock()
	appendLog("[PERF] profile=" + s.profile + " transport=" + safeTransportName(s.transportType))
	return ""
}
func buildPacketTransport(s *packetSession, url, secret, codec, maxToken, maxUid string, lab bool) (transport.Transport, error) {
	cfg := transport.DefaultConfig()
	var inner transport.Transport
	switch s.transportType {
	case "vyandex":
		var err error
		s.volga, err = yandex.NewYandexVolgaTransportWithConfig(url, cfg, profileConfig(s.profile).volga)
		if err != nil {
			return nil, err
		}
		inner = s.volga
	case "mailru":
		inner = mailru.NewMailruDocsTransport(url, cfg)
	case "cupsonline":
		inner = cupsonline.NewCupsonlineTransport(url, cfg, true)
	case "oneme":
		uid, _ := strconv.ParseInt(maxUid, 10, 64)
		inner = oneme.NewOneMeTransport(false, maxToken, uid, cfg)
	default:
		inner = yandex.NewYandexDocsTransport(url, cfg)
	}
	return wrapPacketTransport(s, inner, url, secret, codec, lab)
}
func wrapPacketTransport(s *packetSession, inner transport.Transport, url, secret, codec string, lab bool) (transport.Transport, error) {
	if codec == "legacy" {
		inner = transport.NewCompressedTransport(inner)
	} else {
		if lab && s.transportType == "vyandex" {
			var err error
			s.outer, err = transport.NewBatchedTransportWithConfig(inner, profileConfig(s.profile).outer)
			if err != nil {
				return nil, err
			}
		} else {
			s.outer = transport.NewBatchedTransport(inner)
		}
		inner = s.outer
	}
	if secret != "" {
		context := s.transportType
		if url != "" {
			context = url
		}
		return transport.NewEncryptedTransport(inner, secret, context, false)
	}
	return inner, nil
}
func (s *packetSession) enqueue(data []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return
	}
	packet := append([]byte(nil), data...)
	if s.count == len(s.packets) {
		s.packets[s.head] = nil
		s.head = (s.head + 1) % len(s.packets)
		s.count--
		s.drops++
	}
	s.packets[(s.head+s.count)%len(s.packets)] = packet
	s.count++
	s.received += uint64(len(packet))
	select {
	case s.notify <- struct{}{}:
	default:
	}
}
func (s *packetSession) stop() {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	s.stopped = true
	close(s.done)
	clear(s.packets)
	s.count = 0
	s.head = 0
	trans := s.transport
	s.mu.Unlock()
	if trans != nil {
		_ = trans.Stop()
	}
}
func Stop() {
	if s := currentSession(); s != nil {
		s.stop()
	}
}
func IsConnected() bool {
	s := currentSession()
	if s == nil {
		return false
	}
	s.mu.Lock()
	tr, ready := s.transport, s.ready && !s.stopped
	s.mu.Unlock()
	return ready && tr != nil && tr.IsConnected()
}
func Send(packet []byte) string {
	s := currentSession()
	if s == nil {
		return "Транспорт не запущен"
	}
	s.mu.Lock()
	tr, ready := s.transport, s.ready && !s.stopped
	s.mu.Unlock()
	if !ready || tr == nil {
		return "Транспорт не запущен"
	}
	if err := tr.Send(packet); err != nil {
		return "Отправка не выполнена"
	}
	s.mu.Lock()
	s.sent += uint64(len(packet))
	s.mu.Unlock()
	return ""
}
func (s *packetSession) read() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reads++
	if s.stopped || s.count == 0 {
		return nil
	}
	p := s.packets[s.head]
	s.packets[s.head] = nil
	s.head = (s.head + 1) % len(s.packets)
	s.count--
	return p
}

// Read retains its nonblocking compatibility semantics.
func Read() []byte {
	if s := currentSession(); s != nil {
		return s.read()
	}
	return nil
}

// ReadWait waits for packet/Stop/deadline without per-packet goroutines. A
// nonpositive timeout waits indefinitely. Stop always wakes an indefinite wait.
func ReadWait(timeoutMs int) []byte {
	s := currentSession()
	if s == nil {
		return nil
	}
	return s.readWait(timeoutMs)
}
func (s *packetSession) readWait(timeoutMs int) []byte {
	s.mu.Lock()
	s.waits++
	s.mu.Unlock()
	var deadline <-chan time.Time
	if timeoutMs > 0 {
		if timeoutMs > 60000 {
			timeoutMs = 60000
		}
		timer := time.NewTimer(time.Duration(timeoutMs) * time.Millisecond)
		defer timer.Stop()
		deadline = timer.C
	}
	for {
		if p := s.read(); p != nil {
			return p
		}
		select {
		case <-s.done:
			return nil
		case <-s.notify:
		case <-deadline:
			s.mu.Lock()
			s.timeouts++
			s.mu.Unlock()
			return nil
		}
	}
}
func ReadLogs() string {
	client.mu.Lock()
	defer client.mu.Unlock()
	s := strings.Join(client.logs, "\n")
	client.logs = nil
	return s
}

// PerformanceSnapshot exports only whitelisted numeric counters and enum names.
// URL, token, cookie, keys, packet bytes and log buffers are never serialized.
func PerformanceSnapshot() string {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	result := map[string]any{"profile": "baseline", "transport": "none", "connected": false, "go_heap_alloc": m.HeapAlloc, "go_heap_sys": m.HeapSys, "go_heap_inuse": m.HeapInuse, "go_stack_inuse": m.StackInuse, "goroutines": runtime.NumGoroutine(), "gc_count": m.NumGC, "gc_pause_ns": m.PauseTotalNs, "total_alloc_bytes": m.TotalAlloc}
	if s := currentSession(); s != nil {
		s.mu.Lock()
		result["profile"] = s.profile
		result["transport"] = safeTransportName(s.transportType)
		result["receive_mode"] = "polling_2ms"
		if s.profile != "baseline" {
			result["receive_mode"] = "blocking"
		}
		result["receive_queue_len"] = s.count
		result["receive_queue_cap"] = len(s.packets)
		result["receive_drops"] = s.drops
		result["read_calls"] = s.reads
		result["read_wait_calls"] = s.waits
		result["read_timeouts"] = s.timeouts
		result["upload_bytes"] = s.sent
		result["download_bytes"] = s.received
		ready := s.ready && !s.stopped
		var volga *yandex.YandexVolgaTransport
		var outer *transport.BatchedTransport
		var tr transport.Transport
		if ready {
			volga, outer, tr = s.volga, s.outer, s.transport
		}
		s.mu.Unlock()
		if ready && tr != nil {
			result["connected"] = tr.IsConnected()
			if volga != nil {
				result["volga"] = volga.Performance()
			}
			if outer != nil {
				result["outer"] = outer.Performance()
			}
		}
	}
	data, err := json.Marshal(result)
	if err != nil {
		return "{}"
	}
	return string(data)
}
func safeTransportName(s string) string {
	switch s {
	case "yandex", "vyandex", "mailru", "cupsonline", "oneme":
		return s
	}
	return "unknown"
}
