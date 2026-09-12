// Proxy mode exposes a local SOCKS5 listener backed by the same encrypted
// document transport as the VPN packet mode, but routed through an in-process
// gVisor TCP/IP stack (tunnel.TCPTunnel) instead of an Android VpnService TUN.
// This mirrors exactly what the desktop CLI's client mode already does in
// main.go, so it needs no changes on the exit node / VDS side.
package mobile

import (
	"fmt"
	"sync"

	"universal-bypass-tool/socks5"
	"universal-bypass-tool/transport"
	"universal-bypass-tool/transport/yandex"
	"universal-bypass-tool/tunnel"
	"universal-bypass-tool/utils"
)

var proxy = proxyState{}

type proxyState struct {
	mu        sync.Mutex
	running   bool
	transport transport.Transport
	tun       *tunnel.TCPTunnel
	server    *socks5.SOCKS5Server
}

// StartProxy launches the local SOCKS5 proxy. Returns "" once the listener is
// bound and the transport handshake has started, or a user-readable error.
// Call ProxyIsConnected to learn when the tunnel itself is actually up.
// Hostname lookups are resolved locally by TCPTunnel (the same as the
// desktop CLI client), so no exit-node changes are required. When username
// is non-empty, the SOCKS5 server requires that username/password (e.g. for
// a proxy bound to 0.0.0.0 and reachable from the local network); an empty
// username leaves it open, as appropriate for a loopback-only bind.
func StartProxy(documentURL, encryptionSecret, listenAddr, username, password string) string {
	if documentURL == "" {
		return "Ссылка на документ не указана"
	}
	if encryptionSecret != "" && len(encryptionSecret) < 16 {
		return "Ключ шифрования должен содержать не менее 16 символов"
	}

	proxy.mu.Lock()
	if proxy.running {
		proxy.mu.Unlock()
		return ""
	}
	proxy.mu.Unlock()

	utils.EnableDebug()
	utils.SetLogSink(appendLog)
	appendLog("[ANDROID] Запуск прокси-транспорта Yandex Docs")

	config := transport.DefaultConfig()
	var inner transport.Transport = yandex.NewYandexDocsTransport(documentURL, config)
	if encryptionSecret != "" {
		encrypted, err := transport.NewEncryptedTransport(inner, encryptionSecret, documentURL, false)
		if err != nil {
			return err.Error()
		}
		inner = encrypted
		appendLog("[ANDROID] Шифрование прокси-транспорта: AES-256-GCM включено")
	} else {
		appendLog("[ANDROID] Шифрование прокси-транспорта отключено (ключ не задан)")
	}
	trans := transport.NewCompressedTransport(inner)
	if err := trans.Start(); err != nil {
		appendLog(fmt.Sprintf("[ANDROID] Ошибка запуска прокси: %v", err))
		return err.Error()
	}

	tun := tunnel.NewTCPTunnel(trans, false)
	server := socks5.NewSOCKS5Server(listenAddr, tun)
	if username != "" {
		server.SetAuth(username, password)
	}
	if err := server.Bind(); err != nil {
		_ = trans.Stop()
		appendLog(fmt.Sprintf("[ANDROID] Не удалось занять %s: %v", listenAddr, err))
		return fmt.Sprintf("Порт %s уже занят", listenAddr)
	}

	proxy.mu.Lock()
	proxy.running = true
	proxy.transport = trans
	proxy.tun = tun
	proxy.server = server
	proxy.mu.Unlock()

	utils.SafeGo("mobile.proxyServe", func() {
		err := server.Start()
		proxy.mu.Lock()
		stillRunning := proxy.running
		proxy.mu.Unlock()
		if stillRunning && err != nil {
			appendLog(fmt.Sprintf("[ANDROID] Прокси остановлен: %v", err))
		}
	})

	appendLog(fmt.Sprintf("[ANDROID] SOCKS5-прокси слушает %s", listenAddr))
	return ""
}

func StopProxy() {
	proxy.mu.Lock()
	server := proxy.server
	trans := proxy.transport
	proxy.running = false
	proxy.transport = nil
	proxy.tun = nil
	proxy.server = nil
	proxy.mu.Unlock()
	appendLog("[ANDROID] Остановка прокси-транспорта")
	if server != nil {
		_ = server.Close()
	}
	if trans != nil {
		_ = trans.Stop()
	}
}

func ProxyIsRunning() bool {
	proxy.mu.Lock()
	defer proxy.mu.Unlock()
	return proxy.running
}

func ProxyIsConnected() bool {
	proxy.mu.Lock()
	trans := proxy.transport
	proxy.mu.Unlock()
	return trans != nil && trans.IsConnected()
}

// ProxyBytesSent and ProxyBytesReceived return running totals relayed
// through the local SOCKS5 server (client -> internet and internet ->
// client respectively) across every connection since StartProxy, for a live
// speed indicator. Both are 0 if the proxy isn't running.
func ProxyBytesSent() int64 {
	proxy.mu.Lock()
	server := proxy.server
	proxy.mu.Unlock()
	if server == nil {
		return 0
	}
	return server.BytesSent()
}

func ProxyBytesReceived() int64 {
	proxy.mu.Lock()
	server := proxy.server
	proxy.mu.Unlock()
	if server == nil {
		return 0
	}
	return server.BytesReceived()
}
