package mobile

import (
 "fmt"
 "sync"
 "time"
 "openflux/transport"
 "openflux/transport/yandex"
 "openflux/utils"
)

var modeClient struct {
 mu sync.Mutex
 startMu sync.Mutex
 session *packetSession
 lastDiagnostic time.Time
}
var loggingOnce sync.Once

func configureLogging() {
 loggingOnce.Do(func() {
  // Provider debug logs can contain auth/URLs. Only controlled app summaries
  // are exposed by the Android bridge, including Standard and SOCKS5.
  utils.SetDebug(false)
 })
}

func currentSession() *packetSession {
 modeClient.mu.Lock()
 defer modeClient.mu.Unlock()
 return modeClient.session
}
func installSession(s *packetSession) {
 modeClient.mu.Lock()
 modeClient.session = s
 modeClient.lastDiagnostic = time.Time{}
 modeClient.mu.Unlock()
}

// Start preserves the ordinary app's default path.
func Start(kind, url, secret, codec, maxToken, maxUid string) string {
 return StartWithMode(kind, url, secret, codec, maxToken, maxUid, "standard")
}

// StartWithMode applies explicit modes only to the Volga packet VPN.
// Other carriers and the separate SOCKS5 API retain ordinary scheduling.
func StartWithMode(kind, url, secret, codec, maxToken, maxUid, mode string) string {
 modeClient.startMu.Lock()
 defer modeClient.startMu.Unlock()
 if old := currentSession(); old != nil {
  old.mu.Lock()
  running := !old.stopped
  old.mu.Unlock()
  if running { return "" }
 }
 client.mu.Lock()
 running := client.running
 client.mu.Unlock()
 if running { return "" }
 mode = effectiveMode(kind, mode)
 if mode == "standard" {
  installSession(nil)
  return startStandard(kind, url, secret, codec, maxToken, maxUid)
 }
 if url == "" { return "Ссылка на документ не указана" }
 if secret != "" && len(secret) < 16 { return "Ключ шифрования должен содержать не менее 16 символов" }
 configureLogging()
 s := newPacketSession(mode, kind, transport.DefaultConfig().MaxQueueSize)
 installSession(s)
 tr, err := buildModeTransport(s, url, secret, codec)
 if err != nil { s.stop(); return "Не удалось создать транспорт" }
 return finishStart(s, tr)
}

func buildModeTransport(s *packetSession, url, secret, codec string) (transport.Transport, error) {
 var err error
 s.volga, err = yandex.NewYandexVolgaTransportWithConfig(url, transport.DefaultConfig(), modeConfig(s.profile).volga)
 if err != nil { return nil, err }
 return wrapModeTransport(s, s.volga, url, secret, codec)
}

// The real Android startup and independent peer tests share this exact path.
func wrapModeTransport(s *packetSession, inner transport.Transport, context, secret, codec string) (transport.Transport, error) {
 s.codec = codec
 if codec != "legacy" {
  var err error
  s.outer, err = transport.NewBatchedTransportWithConfig(inner, modeConfig(s.profile).outer)
  if err != nil { return nil, err }
  inner = s.outer
 }
 if secret != "" {
  var err error
  s.encrypted, err = transport.NewEncryptedTransport(inner, secret, context, false)
  if err != nil { return nil, err }
  inner = s.encrypted
 }
 if codec == "legacy" {
  // Exact production 081d214 construction: raw -> AES -> Legacy.
  // Send: IP -> Legacy -> AES -> Volga; receive is the inverse.
  inner = transport.NewCompressedTransport(inner)
 }
 return inner, nil
}

func Stop() {
 if s := currentSession(); s != nil { s.stop() } else { stopStandard() }
}

func appendModeDiagnostics() {
 s := currentSession()
 if s == nil { return }
 modeClient.mu.Lock()
 if time.Since(modeClient.lastDiagnostic) < 30*time.Second {
  modeClient.mu.Unlock(); return
 }
 modeClient.lastDiagnostic = time.Now()
 modeClient.mu.Unlock()
 s.mu.Lock()
 if !s.ready || s.stopped { s.mu.Unlock(); return }
 v, drops := s.volga, s.drops
 s.mu.Unlock()
 if v == nil { return }
 d := v.Performance()
 appendLog(fmt.Sprintf("[MODE] mode=%s workers=%d queue_drops=%d receive_drops=%d http_requests=%d http_429=%d guard=%t guard_waits=%d reconnects=%d",
  normalizeMode(s.profile), d.WorkerCount, d.QueueDrops, drops, d.HTTPRequests,
  d.RateLimited, d.Enabled, d.WaitEvents, d.Reconnects))
}
type packetSession struct {
	mu                                            sync.Mutex
	transport                                     transport.Transport
	volga                                         *yandex.ConfiguredVolgaTransport
	outer                                         *transport.ConfiguredBatchedTransport
	encrypted                                     *transport.EncryptedTransport
	codec                                         string
	callbackPackets, callbackBytes, enqueued      uint64
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
	appendLog("[MODE] mode=" + normalizeMode(s.profile))
	return ""
}
func (s *packetSession) enqueue(data []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.callbackPackets++
	s.callbackBytes += uint64(len(data))
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
	s.enqueued++
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
func IsConnected() bool {
	if currentSession() == nil {
		return standardIsConnected()
	}
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
	if currentSession() == nil {
		return sendStandard(packet)
	}
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
	if currentSession() == nil {
		return readStandard()
	}
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
