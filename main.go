package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	godebug "runtime/debug"
	"strconv"
	"strings"
	"time"

	_ "github.com/wlynxg/anet"
	"universal-bypass-tool/socks5"
	"universal-bypass-tool/transport"
	"universal-bypass-tool/transport/oneme"
	"universal-bypass-tool/transport/yandex"
	"universal-bypass-tool/tunnel"
	"universal-bypass-tool/utils"
)

var (
	globalDocUrl string
	maxToken     string
	maxUid       string
	localIP      string
)

func main() {
	//os.Setenv("GODEBUG", "netdns=go")
	fmt.Print("written by p1neappleXpress\n")

	exitNode := flag.Bool("exit-node", false, "Run as exit node (needs root)")
	client := flag.Bool("client", false, "Run as client")
	debug := flag.Bool("debug", false, "Enable verbose debug logging")
	socksAddr := flag.String("socks5", ":1080", "SOCKS5 address")
	transportType := flag.String("transport", "yandex", "Transport type (yandex, google, custom)")
	flag.StringVar(&globalDocUrl, "url", "", "Document URL. Required for Yandex.Docs transport")
	urlFile := flag.String("url-file", "", "Read the document URL from a file")
	encryptionKeyFile := flag.String("encryption-key-file", "", "Read the shared encryption secret from a file")
	flag.StringVar(&maxToken, "maxToken", "", "MAX Web token. If u use MAX transport")
	flag.StringVar(&maxUid, "maxUid", "", "MAX call user id. If u use MAX transport")
	flag.StringVar(&localIP, "local-ip", "", "Egress IP for exit node (scoped RST drop)")
	flag.Parse()

	if localIP != "" {
		tunnel.SetLocalIP(localIP)
	}

	// The exit node often runs on a tiny VPS; keep the heap tight under load
	// (GC aggressively). Set GOMEMLIMIT in the environment for a hard soft-cap.
	if *exitNode {
		godebug.SetGCPercent(20)
	}

	if !*exitNode && !*client {
		flag.Usage()
		os.Exit(1)
	}

	if *debug {
		utils.EnableDebug()
	}

	log.Printf("=== Universal Bypass Tool ===")
	log.Printf("Mode: %s", map[bool]string{true: "EXIT NODE", false: "CLIENT"}[*exitNode])
	log.Printf("Transport: %s", *transportType)

	config := transport.DefaultConfig()
	var trans transport.Transport

	switch *transportType {
	case "yandex":
		var err error
		globalDocUrl, err = readRequiredOption(globalDocUrl, *urlFile, "document URL")
		if err != nil {
			log.Fatal(err)
		}
		secret, err := readRequiredOption("", *encryptionKeyFile, "encryption key")
		if err != nil {
			log.Fatal(err)
		}
		encrypted, err := transport.NewEncryptedTransport(
			yandex.NewYandexDocsTransport(globalDocUrl, config), secret, globalDocUrl, *exitNode,
		)
		if err != nil {
			log.Fatalf("Configure encrypted transport: %v", err)
		}
		if *exitNode {
			go detectAndPublishCountry(encrypted)
		}
		trans = transport.NewCompressedTransport(encrypted)
	case "oneme":
		uidint, _ := strconv.ParseInt(maxUid, 10, 64)
		trans = transport.NewCompressedTransport(oneme.NewOneMeTransport(*exitNode, maxToken, uidint, config))
	default:
		log.Fatalf("Unknown transport type: %s", *transportType)
	}

	if err := trans.Start(); err != nil {
		log.Fatalf("Failed to start transport: %v", err)
	}

	tun := tunnel.NewTCPTunnel(trans, *exitNode)

	if *exitNode {
		log.Printf("Running as EXIT NODE (needs root for raw socket)")
		if localIP != "" {
			// Scoped: only drop kernel RSTs originating from the tunnel's
			// egress IP, leaving the host's other services (and their
			// closed-port RSTs) untouched.
			log.Printf("! Run: sudo iptables -A OUTPUT -p tcp --tcp-flags RST RST -s %s -j DROP", localIP)
		} else {
			log.Printf("! Kernel RSTs would tear down tunnel connections. Prefer a scoped rule:")
			log.Printf("!   assign a dedicated alias IP, run with --local-ip <ip>, then:")
			log.Printf("!   sudo iptables -A OUTPUT -p tcp --tcp-flags RST RST -s <ip> -j DROP")
			log.Printf("! Host-wide fallback (drops ALL outbound RST; makes closed ports look filtered):")
			log.Printf("!   sudo iptables -A OUTPUT -p tcp --tcp-flags RST RST -j DROP")
		}
		select {}
	} else {
		log.Printf("Running as CLIENT (SOCKS5 on %s)", *socksAddr)
		socks5Server := socks5.NewSOCKS5Server(*socksAddr, tun)
		log.Fatal(socks5Server.Start())
	}
}

// detectAndPublishCountry looks up this exit node's own public IP country and
// publishes it via encrypted.SetCountry, so it starts riding along on ping
// responses to the client. Best-effort: the client just shows no country if
// this never succeeds.
func detectAndPublishCountry(encrypted *transport.EncryptedTransport) {
	client := &http.Client{Timeout: 5 * time.Second}
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * 5 * time.Second)
		}
		country, err := lookupCountry(client)
		if err != nil {
			utils.Debugf("[GEOIP] lookup failed: %v", err)
			continue
		}
		encrypted.SetCountry(country)
		utils.Debugf("[GEOIP] exit node country: %s", country)
		return
	}
	utils.Debugf("[GEOIP] country lookup gave up after retries")
}

func lookupCountry(client *http.Client) (string, error) {
	// ip-api.com's free tier is HTTP-only (HTTPS requires a paid plan). The
	// request only reveals this exit node's own public IP, which is already
	// inherently visible to anyone it connects to, so plain HTTP is fine here.
	resp, err := client.Get("http://ip-api.com/line/?fields=country")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("geoip lookup returned HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 128))
	if err != nil {
		return "", err
	}
	country := strings.TrimSpace(string(body))
	if country == "" {
		return "", fmt.Errorf("geoip lookup returned no usable country")
	}
	return country, nil
}

func readRequiredOption(value, filename, label string) (string, error) {
	if value != "" && filename != "" {
		return "", fmt.Errorf("use only one of the inline or file options for %s", label)
	}
	if filename != "" {
		data, err := os.ReadFile(filename)
		if err != nil {
			return "", fmt.Errorf("read %s file: %w", label, err)
		}
		value = string(data)
	}
	value = strings.TrimSpace(value)
	if value == "" {
		return "", fmt.Errorf("%s is required", label)
	}
	return value, nil
}
