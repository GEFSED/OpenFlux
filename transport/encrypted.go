package transport

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"golang.org/x/crypto/scrypt"

	"universal-bypass-tool/utils"
)

// maxCountryBytes bounds the optional country name piggybacked on ping-pong
// frames. It is cosmetic data, but the length is still capped defensively
// since it arrives over the wire (authenticated, not otherwise validated).
const maxCountryBytes = 48

const (
	encryptedVersion  = byte(1)
	encryptedHeader   = 5
	maxSeenNonces     = 4096
	frameData         = byte(0)
	framePingRequest  = byte(1)
	framePingResponse = byte(2)
	frameDNSRequest   = byte(3)
	frameDNSResponse  = byte(4)
)

// dnsRequestTimeout bounds how long ResolveDNS waits for the peer's answer
// before giving up; a slow or unresponsive upstream resolver on the exit node
// must never hang the caller (a SOCKS5 CONNECT or a captured TUN DNS packet)
// forever.
const dnsRequestTimeout = 5 * time.Second

// maxDNSMessage bounds a relayed DNS message. Real UDP DNS is capped at 512
// bytes without EDNS0 and rarely exceeds a few KB even with it; this is a
// defensive ceiling against a hostile or broken peer, not a protocol limit.
const maxDNSMessage = 4096

var encryptedMagic = [3]byte{'O', 'F', 'X'}

// EncryptedTransport provides end-to-end authenticated encryption over an
// untrusted transport. The document URL is used only as a public, per-document
// KDF salt; secrecy comes exclusively from the shared secret.
type EncryptedTransport struct {
	Transport
	sendAEAD      cipher.AEAD
	receiveAEAD   cipher.AEAD
	sendDirection byte
	recvDirection byte
	seenMu        sync.Mutex
	seen          map[string]struct{}
	seenOrder     []string
	pingMu        sync.Mutex
	pendingPings  map[uint64]time.Time
	lastPingMs    atomic.Int64
	pingSequence  atomic.Int64
	country       atomic.Value // string
	dnsMu         sync.Mutex
	pendingDNS    map[uint64]chan []byte
}

// NewEncryptedTransport creates a directional AES-256-GCM transport. Both
// peers must use the same secret and context, and exactly one peer must set
// exitNode=true.
func NewEncryptedTransport(inner Transport, secret, context string, exitNode bool) (*EncryptedTransport, error) {
	if inner == nil {
		return nil, errors.New("inner transport is nil")
	}
	if len(secret) < 16 {
		return nil, errors.New("encryption secret must contain at least 16 characters")
	}

	salt := sha256.Sum256([]byte("OpenFlux encrypted transport v1\x00" + context))
	master, err := scrypt.Key([]byte(secret), salt[:], 32768, 8, 1, 32)
	if err != nil {
		return nil, fmt.Errorf("derive encryption key: %w", err)
	}
	clientToExit := deriveDirectionalKey(master, "client-to-exit")
	exitToClient := deriveDirectionalKey(master, "exit-to-client")

	sendKey, receiveKey := clientToExit, exitToClient
	sendDirection, receiveDirection := byte(0), byte(1)
	if exitNode {
		sendKey, receiveKey = exitToClient, clientToExit
		sendDirection, receiveDirection = 1, 0
	}
	sendAEAD, err := newGCM(sendKey)
	if err != nil {
		return nil, err
	}
	receiveAEAD, err := newGCM(receiveKey)
	if err != nil {
		return nil, err
	}
	return &EncryptedTransport{
		Transport:     inner,
		sendAEAD:      sendAEAD,
		receiveAEAD:   receiveAEAD,
		sendDirection: sendDirection,
		recvDirection: receiveDirection,
		seen:          make(map[string]struct{}),
		pendingPings:  make(map[uint64]time.Time),
		pendingDNS:    make(map[uint64]chan []byte),
	}, nil
}

func deriveDirectionalKey(master []byte, label string) []byte {
	mac := hmac.New(sha256.New, master)
	_, _ = mac.Write([]byte("OpenFlux direction v1\x00" + label))
	return mac.Sum(nil)
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create AES cipher: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create AES-GCM: %w", err)
	}
	return aead, nil
}

func (e *EncryptedTransport) Send(data []byte) error {
	frame := make([]byte, 1, len(data)+1)
	frame[0] = frameData
	frame = append(frame, data...)
	return e.sendFrame(frame)
}

func (e *EncryptedTransport) sendFrame(data []byte) error {
	header := []byte{encryptedMagic[0], encryptedMagic[1], encryptedMagic[2], encryptedVersion, e.sendDirection}
	nonce := make([]byte, e.sendAEAD.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return fmt.Errorf("create packet nonce: %w", err)
	}
	packet := make([]byte, 0, len(header)+len(nonce)+len(data)+e.sendAEAD.Overhead())
	packet = append(packet, header...)
	packet = append(packet, nonce...)
	packet = e.sendAEAD.Seal(packet, nonce, data, header)
	return e.Transport.Send(packet)
}

func (e *EncryptedTransport) Receive(callback func([]byte)) {
	e.Transport.Receive(func(packet []byte) {
		if len(packet) < encryptedHeader+e.receiveAEAD.NonceSize()+e.receiveAEAD.Overhead() {
			return
		}
		header := packet[:encryptedHeader]
		if header[0] != encryptedMagic[0] || header[1] != encryptedMagic[1] ||
			header[2] != encryptedMagic[2] || header[3] != encryptedVersion ||
			header[4] != e.recvDirection {
			return
		}
		nonceEnd := encryptedHeader + e.receiveAEAD.NonceSize()
		nonce := packet[encryptedHeader:nonceEnd]
		plaintext, err := e.receiveAEAD.Open(nil, nonce, packet[nonceEnd:], header)
		if err != nil || !e.rememberNonce(nonce) {
			return
		}
		if len(plaintext) == 0 {
			return
		}
		switch plaintext[0] {
		case frameData:
			callback(plaintext[1:])
		case framePingRequest:
			if len(plaintext) == 9 {
				response := append([]byte{framePingResponse}, plaintext[1:]...)
				if country, ok := e.country.Load().(string); ok && country != "" {
					response = append(response, country...)
				}
				_ = e.sendFrame(response)
			}
		case framePingResponse:
			if len(plaintext) >= 9 {
				e.completePing(binary.BigEndian.Uint64(plaintext[1:9]))
				if extra := plaintext[9:]; len(extra) > 0 {
					if len(extra) > maxCountryBytes {
						extra = extra[:maxCountryBytes]
					}
					if utf8.Valid(extra) {
						e.country.Store(string(extra))
					}
				}
			}
		case frameDNSRequest:
			e.handleDNSRequest(plaintext)
		case frameDNSResponse:
			e.handleDNSResponse(plaintext)
		}
	})
}

// ResolveDNS relays a raw DNS message to whoever is on the other end of this
// encrypted transport (in practice, the exit node), which forwards it
// verbatim to upstream over UDP and returns the raw answer. Both VPN mode
// (a real query captured from the TUN device) and Proxy mode (a synthetic
// A-record query for a SOCKS5 CONNECT hostname) use this so DNS resolution
// happens on the exit node's network instead of leaking to the client's own,
// possibly censored or monitored, network.
func (e *EncryptedTransport) ResolveDNS(upstream string, query []byte) ([]byte, error) {
	if len(upstream) > 255 {
		return nil, errors.New("upstream address too long")
	}
	if len(query) == 0 || len(query) > maxDNSMessage {
		return nil, errors.New("dns query size out of range")
	}

	var tokenBytes [8]byte
	if _, err := rand.Read(tokenBytes[:]); err != nil {
		return nil, fmt.Errorf("create dns token: %w", err)
	}
	token := binary.BigEndian.Uint64(tokenBytes[:])
	reply := make(chan []byte, 1)
	e.dnsMu.Lock()
	e.pendingDNS[token] = reply
	e.dnsMu.Unlock()
	defer func() {
		e.dnsMu.Lock()
		delete(e.pendingDNS, token)
		e.dnsMu.Unlock()
	}()

	frame := make([]byte, 0, 1+8+1+len(upstream)+len(query))
	frame = append(frame, frameDNSRequest)
	frame = append(frame, tokenBytes[:]...)
	frame = append(frame, byte(len(upstream)))
	frame = append(frame, upstream...)
	frame = append(frame, query...)
	if err := e.sendFrame(frame); err != nil {
		return nil, err
	}

	select {
	case answer := <-reply:
		return answer, nil
	case <-time.After(dnsRequestTimeout):
		return nil, errors.New("dns resolution timed out (is the exit node up to date?)")
	}
}

// handleDNSRequest answers a DNS relay request. It always runs, symmetric to
// how ping requests are answered: in practice only the exit node ever
// receives one, since only ResolveDNS's caller (the client) originates them.
// The upstream UDP round trip runs in its own goroutine so a slow resolver
// cannot stall this transport's shared receive loop.
func (e *EncryptedTransport) handleDNSRequest(plaintext []byte) {
	if len(plaintext) < 10 {
		return
	}
	tokenBytes := append([]byte(nil), plaintext[1:9]...)
	upstreamLen := int(plaintext[9])
	if 10+upstreamLen > len(plaintext) {
		return
	}
	upstream := string(plaintext[10 : 10+upstreamLen])
	query := append([]byte(nil), plaintext[10+upstreamLen:]...)

	utils.SafeGo("encrypted.answerDNS", func() {
		answer, err := relayDNSQuery(upstream, query)
		if err != nil {
			utils.Debugf("[DNS] relay to %s failed: %v", upstream, err)
			return
		}
		response := make([]byte, 0, 9+len(answer))
		response = append(response, frameDNSResponse)
		response = append(response, tokenBytes...)
		response = append(response, answer...)
		if err := e.sendFrame(response); err != nil {
			utils.Debugf("[DNS] send answer failed: %v", err)
		}
	})
}

func (e *EncryptedTransport) handleDNSResponse(plaintext []byte) {
	if len(plaintext) < 9 {
		return
	}
	token := binary.BigEndian.Uint64(plaintext[1:9])
	e.dnsMu.Lock()
	reply, ok := e.pendingDNS[token]
	e.dnsMu.Unlock()
	if !ok {
		return
	}
	answer := append([]byte(nil), plaintext[9:]...)
	select {
	case reply <- answer:
	default:
	}
}

// relayDNSQuery forwards a raw DNS message to upstream (host or host:port;
// port defaults to 53) over plain UDP and returns the raw response. This is
// a byte-transparent relay with no DNS parsing, so it faithfully answers
// whatever record type the original caller asked for.
func relayDNSQuery(upstream string, query []byte) ([]byte, error) {
	if upstream == "" {
		upstream = "1.1.1.1"
	}
	addr := upstream
	if _, _, err := net.SplitHostPort(upstream); err != nil {
		addr = net.JoinHostPort(upstream, "53")
	}
	conn, err := net.DialTimeout("udp", addr, dnsRequestTimeout)
	if err != nil {
		return nil, fmt.Errorf("dial upstream: %w", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(dnsRequestTimeout))
	if _, err := conn.Write(query); err != nil {
		return nil, fmt.Errorf("write query: %w", err)
	}
	buf := make([]byte, maxDNSMessage)
	n, err := conn.Read(buf)
	if err != nil {
		return nil, fmt.Errorf("read answer: %w", err)
	}
	return buf[:n], nil
}

// Ping measures a round trip through the encrypted document transport and the
// remote OpenFlux peer. It does not use ICMP or bypass the VPN protocol.
func (e *EncryptedTransport) Ping() error {
	var tokenBytes [8]byte
	if _, err := rand.Read(tokenBytes[:]); err != nil {
		return fmt.Errorf("create ping token: %w", err)
	}
	token := binary.BigEndian.Uint64(tokenBytes[:])
	now := time.Now()
	e.pingMu.Lock()
	for pending, started := range e.pendingPings {
		if now.Sub(started) > 30*time.Second {
			delete(e.pendingPings, pending)
		}
	}
	e.pendingPings[token] = now
	e.pingMu.Unlock()
	frame := append([]byte{framePingRequest}, tokenBytes[:]...)
	if err := e.sendFrame(frame); err != nil {
		e.pingMu.Lock()
		delete(e.pendingPings, token)
		e.pingMu.Unlock()
		return err
	}
	return nil
}

func (e *EncryptedTransport) completePing(token uint64) {
	e.pingMu.Lock()
	started, ok := e.pendingPings[token]
	delete(e.pendingPings, token)
	e.pingMu.Unlock()
	if !ok {
		return
	}
	milliseconds := time.Since(started).Milliseconds()
	if milliseconds < 1 {
		milliseconds = 1
	}
	e.lastPingMs.Store(milliseconds)
	e.pingSequence.Add(1)
}

func (e *EncryptedTransport) LastPingMillis() int64 {
	if e.pingSequence.Load() == 0 {
		return -1
	}
	return e.lastPingMs.Load()
}

func (e *EncryptedTransport) PingSequence() int64 {
	return e.pingSequence.Load()
}

// SetCountry publishes this peer's own country name so it rides along on
// future ping responses. Intended for the exit node; a client-side value is
// simply never read since only the exit node answers ping requests.
func (e *EncryptedTransport) SetCountry(name string) {
	if len(name) > maxCountryBytes {
		name = name[:maxCountryBytes]
	}
	e.country.Store(name)
}

// LastCountry returns the remote peer's country name as learned from the
// most recent ping response, or "" if it hasn't arrived yet.
func (e *EncryptedTransport) LastCountry() string {
	if country, ok := e.country.Load().(string); ok {
		return country
	}
	return ""
}

func (e *EncryptedTransport) rememberNonce(nonce []byte) bool {
	key := string(nonce)
	e.seenMu.Lock()
	defer e.seenMu.Unlock()
	if _, exists := e.seen[key]; exists {
		return false
	}
	e.seen[key] = struct{}{}
	e.seenOrder = append(e.seenOrder, key)
	if len(e.seenOrder) > maxSeenNonces {
		oldest := e.seenOrder[0]
		e.seenOrder = e.seenOrder[1:]
		delete(e.seen, oldest)
	}
	return true
}
