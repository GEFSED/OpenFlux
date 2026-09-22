// Package mobile exposes the OpenFlux packet transport to Android through
// gomobile. Android owns the TUN file descriptor; this package only transports
// complete IPv4 packets through the configured Yandex document.
package mobile

import (
	"strconv"
	"strings"
	"sync"

	"openflux/transport"
	"openflux/transport/cupsonline"
	"openflux/transport/mailru"
	"openflux/transport/oneme"
	"openflux/transport/yandex"
)

var client = packetClient{}

type packetClient struct {
	mu        sync.Mutex
	running   bool
	transport transport.Transport
	packets   [][]byte
	logs      []string
}

func appendLog(message string) {
	client.mu.Lock()
	defer client.mu.Unlock()
	client.logs = append(client.logs, message)
	if len(client.logs) > 500 {
		client.logs = append([]string(nil), client.logs[len(client.logs)-500:]...)
	}
}

// Start connects the packet transport. transportType is "yandex" (default
// when empty), "vyandex", "mailru", "cupsonline" or "oneme". documentURL is
// required for all but "oneme", which instead needs maxToken (and optionally
// maxUid). codec is "batched" (default, zstd+coalescing, matches the CLI's
// --codec=batched) or "legacy" (per-packet LZ4; both peers must agree). It
// returns an empty string on success and a user-readable error on failure.
func startStandard(transportType, documentURL, encryptionSecret, codec, maxToken, maxUid string) string {
	if transportType == "" {
		transportType = "yandex"
	}
	if transportType != "oneme" && documentURL == "" {
		return "Ссылка на документ не указана"
	}
	if encryptionSecret != "" && len(encryptionSecret) < 16 {
		return "Ключ шифрования должен содержать не менее 16 символов"
	}

	client.mu.Lock()
	if client.running {
		client.mu.Unlock()
		return ""
	}
	client.running = true
	client.packets = nil
	client.logs = nil
	client.mu.Unlock()

	configureLogging()
	appendLog("[MODE] mode=standard")

	config := transport.DefaultConfig()
	var inner transport.Transport
	switch transportType {
	case "vyandex":
		inner = yandex.NewYandexVolgaTransport(documentURL, config)
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

	inner, err := wrapStandardTransport(inner, transportType, documentURL, encryptionSecret, codec)
	if err != nil {
		client.mu.Lock()
		client.running = false
		client.mu.Unlock()
		return "Ошибка создания транспорта"
	}
	trans := inner
	trans.Receive(func(data []byte) {
		packet := append([]byte(nil), data...)
		client.mu.Lock()
		if !client.running {
			client.mu.Unlock()
			return
		}
		if len(client.packets) >= config.MaxQueueSize {
			client.packets = client.packets[1:]
		}
		client.packets = append(client.packets, packet)
		client.mu.Unlock()
	})

	if err := trans.Start(); err != nil {
		appendLog("[ERROR] Ошибка запуска транспорта")
		client.mu.Lock()
		client.running = false
		client.mu.Unlock()
		return "Ошибка транспорта"
	}

	client.mu.Lock()
	client.transport = trans
	client.mu.Unlock()
	return ""
}

func stopStandard() {
	client.mu.Lock()
	trans := client.transport
	client.running = false
	client.transport = nil
	client.packets = nil
	client.mu.Unlock()
	appendLog("[ANDROID] Остановка транспорта")
	if trans != nil {
		_ = trans.Stop()
	}
}

func standardIsConnected() bool {
	client.mu.Lock()
	trans := client.transport
	client.mu.Unlock()
	return trans != nil && trans.IsConnected()
}

func sendStandard(packet []byte) string {
	client.mu.Lock()
	trans := client.transport
	running := client.running
	client.mu.Unlock()
	if !running || trans == nil {
		return "Транспорт не запущен"
	}
	if err := trans.Send(packet); err != nil {
		return "Ошибка транспорта"
	}
	return ""
}

// Read returns one received packet, or nil when the queue is empty.
func readStandard() []byte {
	client.mu.Lock()
	defer client.mu.Unlock()
	if len(client.packets) == 0 {
		return nil
	}
	packet := client.packets[0]
	client.packets = client.packets[1:]
	return packet
}

// ReadLogs returns and clears the pending log lines.
func ReadLogs() string {
	appendModeDiagnostics()
	client.mu.Lock()
	defer client.mu.Unlock()
	logs := strings.Join(client.logs, "\n")
	client.logs = nil
	return logs
}

func wrapStandardTransport(inner transport.Transport, transportType, documentURL, encryptionSecret, codec string) (transport.Transport, error) {
	// Preserve ordinary base 8566f727 construction, including its wrapper order.
	if codec == "legacy" {
		inner = transport.NewCompressedTransport(inner)
	} else {
		inner = transport.NewBatchedTransport(inner)
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
			return nil, err
		}
		inner = encrypted
	}
	return inner, nil
}
