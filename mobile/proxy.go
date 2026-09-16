// Proxy mode exposes a local SOCKS5 listener backed by the same encrypted
// document transport as the VPN packet mode, but routed through an in-process
// gVisor TCP/IP stack (tunnel.TCPTunnel) instead of an Android VpnService TUN.
// This mirrors exactly what the desktop CLI's client mode already does in
// main.go, so it needs no changes on the exit node / VDS side.
package mobile

import (
	"fmt"
	"strconv"
	"sync"

	"openflux/socks5"
	"openflux/transport"
	"openflux/transport/cupsonline"
	"openflux/transport/mailru"
	"openflux/transport/oneme"
	"openflux/transport/yandex"
	"openflux/tunnel"
	"openflux/utils"
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
func StartProxy(transportType, documentURL, encryptionSecret, codec, maxToken, maxUid, listenAddr, username, password string) string {
	if transportType == "" {
		transportType = "yandex"
	}
	if transportType != "oneme" && documentURL == "" {
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
	appendLog(fmt.Sprintf("[ANDROID] Запуск прокси-транспорта %s", transportType))

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

	// App-layer codec, same as the CLI's --codec flag. Both peers must use
	// the same one. Applied before encryption so it compresses plaintext
	// rather than ciphertext.
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
			return err.Error()
		}
		inner = encrypted
		appendLog("[ANDROID] Шифрование прокси-транспорта: AES-256-GCM включено")
	} else {
		appendLog("[ANDROID] Шифрование прокси-транспорта отключено (ключ не задан)")
	}
	trans := inner
	if err := trans.Start(); err != nil {
		appendLog(fmt.Sprintf("[ERROR] Ошибка запуска прокси: %v", err))
		return err.Error()
	}

	tun := tunnel.NewTCPTunnel(trans, false)
	server := socks5.NewSOCKS5Server(listenAddr, tun)
	if username != "" {
		server.SetAuth(username, password)
	}
	if err := server.Bind(); err != nil {
		_ = trans.Stop()
		appendLog(fmt.Sprintf("[ERROR] Не удалось занять %s: %v", listenAddr, err))
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
			appendLog(fmt.Sprintf("[ERROR] Прокси остановлен: %v", err))
		}
	})

	appendLog(fmt.Sprintf("[SUCCESS] SOCKS5-прокси слушает %s", listenAddr))
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
