// Diagnostic control copied from Android v1.0.0 8566f727c8238436728758f139130cef433147b7.
// Legacy wrapping matches production v0.6.0 081d214300c1067f17f6c0d02f84a8491f1a7b98.
// Other changes are sanitized logging/errors, read-only counters and a test carrier seam.
// Original global callback, slice FIFO, start publication and Stop behavior remain.
// Package mobile exposes the OpenFlux packet transport to Android through
// gomobile. Android owns the TUN file descriptor; this package only transports
// complete IPv4 packets through the configured Yandex document.
package mobile

import (
	"strconv"
	"sync"

	"openflux/transport"
	"openflux/transport/cupsonline"
	"openflux/transport/mailru"
	"openflux/transport/oneme"
	"openflux/transport/yandex"
)

var baselineClient = baselinePacketClient{}

type baselinePacketClient struct {
	mu                                                                       sync.Mutex
	running                                                                  bool
	transport                                                                transport.Transport
	packets                                                                  [][]byte
	logs                                                                     []string
	kind, codec                                                              string
	volga                                                                    *yandex.BaselineV100VolgaTransport
	outer                                                                    *transport.BaselineV100BatchedTransport
	encrypted                                                                *transport.EncryptedTransport
	callbackPackets, callbackBytes, enqueued, dropped, reads, sent, received uint64
}

// Start connects the packet transport. transportType is "yandex" (default
// when empty), "vyandex", "mailru", "cupsonline" or "oneme". documentURL is
// required for all but "oneme", which instead needs maxToken (and optionally
// maxUid). codec is "batched" (default, zstd+coalescing, matches the CLI's
// --codec=batched) or "legacy" (per-packet LZ4; both peers must agree). It
// returns an empty string on success and a user-readable error on failure.
func baselineStart(transportType, documentURL, encryptionSecret, codec, maxToken, maxUid string) string {
	return baselineStartWithCarrier(transportType, documentURL, encryptionSecret, codec, maxToken, maxUid, nil)
}

// rawOverride is per-call and used only by compatibility tests. The Android
// entry point always passes nil and selects the original Volga constructor.
func baselineStartWithCarrier(transportType, documentURL, encryptionSecret, codec, maxToken, maxUid string, rawOverride transport.Transport) string {
	if transportType == "" {
		transportType = "yandex"
	}
	if transportType != "oneme" && documentURL == "" {
		return "Ссылка на документ не указана"
	}
	if encryptionSecret != "" && len(encryptionSecret) < 16 {
		return "Ключ шифрования должен содержать не менее 16 символов"
	}

	baselineClient.mu.Lock()
	if baselineClient.running {
		baselineClient.mu.Unlock()
		return ""
	}
	baselineClient.running = true
	baselineClient.packets = nil
	baselineClient.logs = nil
	baselineClient.kind, baselineClient.codec = transportType, codec
	baselineClient.volga = nil
	baselineClient.outer = nil
	baselineClient.encrypted = nil
	baselineClient.callbackPackets = 0
	baselineClient.callbackBytes = 0
	baselineClient.enqueued = 0
	baselineClient.dropped = 0
	baselineClient.reads = 0
	baselineClient.sent = 0
	baselineClient.received = 0
	baselineClient.mu.Unlock()

	configureLogging()

	config := transport.DefaultConfig()
	var volga *yandex.BaselineV100VolgaTransport
	var outer *transport.BaselineV100BatchedTransport
	var aes *transport.EncryptedTransport
	var inner transport.Transport
	if rawOverride != nil {
		inner = rawOverride
	} else {
		switch transportType {
		case "vyandex":
			volga = yandex.NewBaselineV100VolgaTransport(documentURL, config)
			inner = volga
		case "mailru":
			inner = mailru.NewMailruDocsTransport(documentURL, config)
		case "cupsonline":
			inner = cupsonline.NewCupsonlineTransport(documentURL, config, true)
		case "oneme":
			uidint, _ := strconv.ParseInt(maxUid, 10, 64)
			inner = oneme.NewOneMeTransport(false, maxToken, uidint, config)
		default:
			inner = yandex.NewYandexDocsTransport(documentURL, config)
		}

	}
	// Legacy matches production 081d214: construct raw -> AES -> Legacy,
	// so Send is packet -> Legacy -> AES -> raw. Batched keeps its existing
	// v1.0.0 ordering; this Legacy exit establishes no Batched compatibility.
	if codec != "legacy" {
		outer = transport.NewBaselineV100BatchedTransport(inner)
		inner = outer
	}

	if encryptionSecret != "" {
		// Same fallback as the CLI: the KDF context is the document URL, or
		// the transport name when there isn't one (oneme). Both peers must
		// derive the same context or the encrypted channel just won't work.
		context := transportType
		if documentURL != "" {
			context = documentURL
		}
		encrypted, err := transport.NewEncryptedTransport(inner, encryptionSecret, context, false)
		if err != nil {
			baselineClient.mu.Lock()
			baselineClient.running = false
			baselineClient.mu.Unlock()
			return "Ошибка транспорта (подробности скрыты)"
		}
		inner = encrypted
		aes = encrypted
		appendLog("[ANDROID] Шифрование транспорта: AES-256-GCM включено")
	} else {
		appendLog("[ANDROID] Шифрование транспорта отключено (ключ не задан)")
	}
	if codec == "legacy" {
		inner = transport.NewCompressedTransport(inner)
	}
	trans := inner
	trans.Receive(func(data []byte) {
		packet := append([]byte(nil), data...)
		baselineClient.mu.Lock()
		baselineClient.callbackPackets++
		baselineClient.callbackBytes += uint64(len(data))
		if !baselineClient.running {
			baselineClient.mu.Unlock()
			return
		}
		if len(baselineClient.packets) >= config.MaxQueueSize {
			baselineClient.packets = baselineClient.packets[1:]
			baselineClient.dropped++
		}
		baselineClient.packets = append(baselineClient.packets, packet)
		baselineClient.enqueued++
		baselineClient.received += uint64(len(packet))
		baselineClient.mu.Unlock()
	})

	if err := trans.Start(); err != nil {
		appendLog("[ERROR] Ошибка запуска транспорта")
		baselineClient.mu.Lock()
		baselineClient.running = false
		baselineClient.mu.Unlock()
		return "Ошибка транспорта (подробности скрыты)"
	}

	baselineClient.mu.Lock()
	baselineClient.transport = trans
	baselineClient.volga, baselineClient.outer, baselineClient.encrypted = volga, outer, aes
	baselineClient.mu.Unlock()
	return ""
}

func baselineStop() {
	baselineClient.mu.Lock()
	trans := baselineClient.transport
	baselineClient.running = false
	baselineClient.transport = nil
	baselineClient.packets = nil
	baselineClient.mu.Unlock()
	appendLog("[ANDROID] Остановка транспорта")
	if trans != nil {
		_ = trans.Stop()
	}
}

func baselineIsConnected() bool {
	baselineClient.mu.Lock()
	trans := baselineClient.transport
	baselineClient.mu.Unlock()
	return trans != nil && trans.IsConnected()
}

func baselineSend(packet []byte) string {
	baselineClient.mu.Lock()
	trans := baselineClient.transport
	running := baselineClient.running
	baselineClient.mu.Unlock()
	if !running || trans == nil {
		return "Транспорт не запущен"
	}
	if err := trans.Send(packet); err != nil {
		return "Ошибка транспорта (подробности скрыты)"
	}
	baselineClient.mu.Lock()
	baselineClient.sent += uint64(len(packet))
	baselineClient.mu.Unlock()
	return ""
}

// Read returns one received packet, or nil when the queue is empty.
func baselineRead() []byte {
	baselineClient.mu.Lock()
	defer baselineClient.mu.Unlock()
	baselineClient.reads++
	if len(baselineClient.packets) == 0 {
		return nil
	}
	packet := baselineClient.packets[0]
	baselineClient.packets = baselineClient.packets[1:]
	return packet
}

func baselineSnapshot(result map[string]any) {
	baselineClient.mu.Lock()
	defer baselineClient.mu.Unlock()
	c := &baselineClient
	result["profile"] = "baseline"
	result["baseline_runtime"] = "android-v1.0.0-8566f727"
	result["transport"] = safeTransportName(c.kind)
	result["codec"] = safeCodecName(c.codec)
	result["receive_mode"] = "polling_2ms_original_slice"
	result["receive_queue_len"] = len(c.packets)
	result["receive_queue_cap"] = transport.DefaultConfig().MaxQueueSize
	result["receive_drops"] = c.dropped
	result["receive_ring_enqueued"] = c.enqueued
	result["receive_ring_dropped"] = c.dropped
	result["mobile_callback_packets"] = c.callbackPackets
	result["mobile_callback_bytes"] = c.callbackBytes
	result["read_calls"] = c.reads
	result["read_wait_calls"] = 0
	result["upload_bytes"] = c.sent
	result["download_bytes"] = c.received
	result["encryption_enabled"] = c.encrypted != nil
	result["connected"] = c.transport != nil && c.transport.IsConnected()
	result["transport_started"] = c.running && c.transport != nil
	if c.volga != nil {
		v := c.volga.Performance()
		result["volga"] = v
		result["ws_connected"] = v.WSConnected
	}
	if c.outer != nil {
		result["outer"] = c.outer.Performance()
	}
	if c.encrypted != nil {
		result["encrypted"] = c.encrypted.ReceiveDiagnostics()
	}
}
