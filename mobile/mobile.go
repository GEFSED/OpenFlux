// Package mobile exposes the OpenFlux packet transport to Android through
// gomobile. Android owns the TUN file descriptor; this package only transports
// complete IPv4 packets through the configured Yandex document.
package mobile

import (
	"fmt"
	"strings"
	"sync"

	"universal-bypass-tool/transport"
	"universal-bypass-tool/transport/yandex"
	"universal-bypass-tool/utils"
)

var client = packetClient{}

type packetClient struct {
	mu        sync.Mutex
	running   bool
	transport transport.Transport
	encrypted *transport.EncryptedTransport
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

// Start connects the packet transport. It returns an empty string on success
// and a user-readable error on failure.
func Start(documentURL, encryptionSecret string) string {
	if documentURL == "" {
		return "Ссылка на документ не указана"
	}
	if len(encryptionSecret) < 16 {
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
	client.encrypted = nil
	client.mu.Unlock()

	utils.EnableDebug()
	utils.SetLogSink(appendLog)
	appendLog("[ANDROID] Запуск транспорта Yandex Docs")

	config := transport.DefaultConfig()
	encrypted, err := transport.NewEncryptedTransport(
		yandex.NewYandexDocsTransport(documentURL, config), encryptionSecret, documentURL, false,
	)
	if err != nil {
		client.mu.Lock()
		client.running = false
		client.mu.Unlock()
		return err.Error()
	}
	trans := transport.NewCompressedTransport(encrypted)
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
		appendLog(fmt.Sprintf("[ANDROID] Ошибка запуска: %v", err))
		client.mu.Lock()
		client.running = false
		client.mu.Unlock()
		return err.Error()
	}

	client.mu.Lock()
	client.transport = trans
	client.encrypted = encrypted
	client.mu.Unlock()
	return ""
}

func Stop() {
	client.mu.Lock()
	trans := client.transport
	client.running = false
	client.transport = nil
	client.encrypted = nil
	client.packets = nil
	client.mu.Unlock()
	appendLog("[ANDROID] Остановка транспорта")
	if trans != nil {
		_ = trans.Stop()
	}
}

func Ping() string {
	client.mu.Lock()
	encrypted := client.encrypted
	running := client.running
	client.mu.Unlock()
	if !running || encrypted == nil {
		return "Транспорт не запущен"
	}
	if err := encrypted.Ping(); err != nil {
		return err.Error()
	}
	return ""
}

func PingMillis() int64 {
	client.mu.Lock()
	encrypted := client.encrypted
	client.mu.Unlock()
	if encrypted == nil {
		return -1
	}
	return encrypted.LastPingMillis()
}

func PingSequence() int64 {
	client.mu.Lock()
	encrypted := client.encrypted
	client.mu.Unlock()
	if encrypted == nil {
		return 0
	}
	return encrypted.PingSequence()
}

// ServerCountry returns the exit node's country name as learned from the
// encrypted ping protocol, or "" if it hasn't arrived yet.
func ServerCountry() string {
	client.mu.Lock()
	encrypted := client.encrypted
	client.mu.Unlock()
	if encrypted == nil {
		return ""
	}
	return encrypted.LastCountry()
}

// ResolveDNS relays a raw DNS query (as captured from the TUN device's
// outgoing UDP packets) through the encrypted transport to the exit node,
// which forwards it to dnsServer over UDP and returns the raw answer. This
// keeps DNS resolution off the client's own network entirely, matching what
// Proxy mode already does. Returns nil if the transport isn't running or the
// exit node didn't answer in time (e.g. it hasn't been updated yet).
func ResolveDNS(query []byte, dnsServer string) []byte {
	client.mu.Lock()
	encrypted := client.encrypted
	running := client.running
	client.mu.Unlock()
	if !running || encrypted == nil {
		return nil
	}
	answer, err := encrypted.ResolveDNS(dnsServer, query)
	if err != nil {
		appendLog(fmt.Sprintf("[ANDROID] DNS через туннель: %v", err))
		return nil
	}
	return answer
}

func IsConnected() bool {
	client.mu.Lock()
	trans := client.transport
	client.mu.Unlock()
	return trans != nil && trans.IsConnected()
}

func Send(packet []byte) string {
	client.mu.Lock()
	trans := client.transport
	running := client.running
	client.mu.Unlock()
	if !running || trans == nil {
		return "Транспорт не запущен"
	}
	if err := trans.Send(packet); err != nil {
		return err.Error()
	}
	return ""
}

// Read returns one received packet, or nil when the queue is empty.
func Read() []byte {
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
	client.mu.Lock()
	defer client.mu.Unlock()
	logs := strings.Join(client.logs, "\n")
	client.logs = nil
	return logs
}
