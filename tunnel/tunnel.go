package tunnel

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"net"
	"strconv"
	"sync/atomic"
	"time"

	"golang.org/x/net/dns/dnsmessage"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
	"gvisor.dev/gvisor/pkg/tcpip/header"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"

	"universal-bypass-tool/transport"
	"universal-bypass-tool/utils"
)

type TCPTunnel struct {
	gvisorStack *stack.Stack
	tunnelEP    *TunnelLinkEndpoint
	transport   transport.Transport
	isExitNode  bool
	rawEP       *RawSocketEndpoint
	startTime   time.Time
	packetCount atomic.Uint64
	dnsResolve  func(query []byte) ([]byte, error)
}

// SetDNSResolver makes DialTCP resolve hostnames through resolve instead of
// the local system resolver, so DNS never leaves this process's own network
// directly. Intended to be backed by EncryptedTransport.ResolveDNS, which
// relays the query to the exit node over the same tunnel. Without a resolver
// configured, DialTCP falls back to local resolution (today's behavior),
// so existing callers (the desktop CLI) are unaffected until they opt in.
func (t *TCPTunnel) SetDNSResolver(resolve func(query []byte) ([]byte, error)) {
	t.dnsResolve = resolve
}

// TCP buffer size range for gvisor stacks. Big by default (exit node on a VPS);
// the memory-constrained iOS Network Extension shrinks these before building
// its stacks (see the packet-tunnel bridge).
var (
	TCPBufMin     = 65536
	TCPBufDefault = 1048576
	TCPBufMax     = 8388608
)

// SetTCPBuffers applies the configured TCP send/receive buffer ranges to s.
func SetTCPBuffers(s *stack.Stack) {
	rcv := tcpip.TCPReceiveBufferSizeRangeOption{Min: TCPBufMin, Default: TCPBufDefault, Max: TCPBufMax}
	if err := s.SetTransportProtocolOption(tcp.ProtocolNumber, &rcv); err != nil {
		utils.Debugf("[TUNNEL] set recv buffer: %v", err)
	}
	snd := tcpip.TCPSendBufferSizeRangeOption{Min: TCPBufMin, Default: TCPBufDefault, Max: TCPBufMax}
	if err := s.SetTransportProtocolOption(tcp.ProtocolNumber, &snd); err != nil {
		utils.Debugf("[TUNNEL] set send buffer: %v", err)
	}
}

func NewTCPTunnel(trans transport.Transport, isExitNode bool) *TCPTunnel {
	t := &TCPTunnel{
		transport:  trans,
		isExitNode: isExitNode,
		startTime:  time.Now(),
	}

	utils.Debugf("[TUNNEL] Net stack init...")
	t.gvisorStack = stack.New(stack.Options{
		NetworkProtocols:   []stack.NetworkProtocolFactory{ipv4.NewProtocol},
		TransportProtocols: []stack.TransportProtocolFactory{tcp.NewProtocol},
	})

	SetTCPBuffers(t.gvisorStack)

	tunnelEP := NewTunnelLinkEndpoint()
	tunnelEP.onOutgoingPacket = func(data []byte) {
		trans.Send(data)
	}
	t.tunnelEP = tunnelEP

	tunnelNIC := tcpip.NICID(1)
	if err := t.gvisorStack.CreateNIC(tunnelNIC, tunnelEP); err != nil {
		utils.Debugf("[TUNNEL] CreateNIC tunnel error: %v", err)
	}

	if isExitNode {
		t.setupExitNode(tunnelNIC)
	} else {
		t.setupClient(tunnelNIC)
	}

	trans.Receive(func(data []byte) {
		tunnelEP.InjectInbound(data)
	})

	utils.SafeGo("tunnel.printStats", t.printStats)
	return t
}

func (t *TCPTunnel) setupExitNode(tunnelNIC tcpip.NICID) {
	localIP := getLocalIP()
	utils.Debugf("[TUNNEL] EXIT NODE - Local IP: %s", localIP)

	rawEP, err := NewRawSocketEndpoint(tcpip.NICID(2))
	if err != nil {
		utils.Debugf("[TUNNEL] Raw socket error: %v", err)
		return
	}

	t.rawEP = rawEP
	rawEP.SetTransportSender(func(data []byte) {
		t.transport.Send(data)
	})

	internetNIC := tcpip.NICID(2)
	if err := t.gvisorStack.CreateNIC(internetNIC, rawEP); err != nil {
		utils.Debugf("[TUNNEL] CreateNIC internet error: %v", err)
		return
	}

	var ipBytes [4]byte
	fmt.Sscanf(localIP, "%d.%d.%d.%d", &ipBytes[0], &ipBytes[1], &ipBytes[2], &ipBytes[3])
	internetAddr := tcpip.AddrFrom4(ipBytes)
	t.gvisorStack.AddProtocolAddress(internetNIC, tcpip.ProtocolAddress{
		Protocol: ipv4.ProtocolNumber,
		AddressWithPrefix: tcpip.AddressWithPrefix{
			Address:   internetAddr,
			PrefixLen: 24,
		},
	}, stack.AddressProperties{})

	t.gvisorStack.SetForwardingDefaultAndAllNICs(ipv4.ProtocolNumber, true)
	t.gvisorStack.AddRoute(tcpip.Route{
		Destination: header.IPv4EmptySubnet,
		NIC:         internetNIC,
	})

	tunnelSubnet := tcpip.AddressWithPrefix{
		Address:   tcpip.AddrFrom4([4]byte{10, 10, 10, 0}),
		PrefixLen: 24,
	}.Subnet()
	t.gvisorStack.AddRoute(tcpip.Route{
		Destination: tunnelSubnet,
		NIC:         tunnelNIC,
	})
}

func (t *TCPTunnel) setupClient(tunnelNIC tcpip.NICID) {
	clientAddr := tcpip.AddrFrom4([4]byte{10, 10, 10, 2})
	t.gvisorStack.AddProtocolAddress(tunnelNIC, tcpip.ProtocolAddress{
		Protocol: ipv4.ProtocolNumber,
		AddressWithPrefix: tcpip.AddressWithPrefix{
			Address:   clientAddr,
			PrefixLen: 24,
		},
	}, stack.AddressProperties{})

	t.gvisorStack.AddRoute(tcpip.Route{
		Destination: header.IPv4EmptySubnet,
		NIC:         tunnelNIC,
	})
}

func (t *TCPTunnel) DialTCP(address string) (net.Conn, error) {
	host, portStr, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("split address: %w", err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port < 0 || port > 65535 {
		return nil, fmt.Errorf("bad port in %q", address)
	}

	ip, err := t.resolveIPv4(host)
	if err != nil {
		return nil, err
	}
	utils.Debugf("[TUNNEL] DialTCP %s -> %s:%d", address, ip.String(), port)

	nic := tcpip.NICID(1)
	if t.isExitNode {
		nic = tcpip.NICID(2)
	}

	conn, err := gonet.DialTCP(t.gvisorStack, tcpip.FullAddress{
		NIC:  nic,
		Addr: tcpip.AddrFrom4([4]byte{ip[0], ip[1], ip[2], ip[3]}),
		Port: uint16(port),
	}, ipv4.ProtocolNumber)

	return conn, err
}

// resolveIPv4 resolves host to an IPv4 address. When a DNS resolver has been
// configured via SetDNSResolver, the lookup is relayed through the tunnel
// (normally to the exit node, which asks its own upstream over UDP) instead
// of using this process's local resolver; without one, it falls back to the
// system resolver exactly as before.
func (t *TCPTunnel) resolveIPv4(host string) (net.IP, error) {
	if literal := net.ParseIP(host); literal != nil {
		if ip4 := literal.To4(); ip4 != nil {
			return ip4, nil
		}
		return nil, fmt.Errorf("IPv6 not supported")
	}

	if t.dnsResolve == nil {
		tcpAddr, err := net.ResolveTCPAddr("tcp", net.JoinHostPort(host, "0"))
		if err != nil {
			return nil, fmt.Errorf("resolve: %w", err)
		}
		ip4 := tcpAddr.IP.To4()
		if ip4 == nil {
			return nil, fmt.Errorf("IPv6 not supported")
		}
		return ip4, nil
	}

	ip4, err := resolveHostnameViaTunnel(t.dnsResolve, host)
	if err != nil {
		return nil, fmt.Errorf("resolve %s via tunnel: %w", host, err)
	}
	return ip4, nil
}

// resolveHostnameViaTunnel builds a minimal A-record query, sends it through
// resolve (a relay to the exit node's own DNS resolution), and extracts the
// first IPv4 answer.
func resolveHostnameViaTunnel(resolve func([]byte) ([]byte, error), host string) (net.IP, error) {
	var idBytes [2]byte
	if _, err := rand.Read(idBytes[:]); err != nil {
		return nil, fmt.Errorf("create dns id: %w", err)
	}
	name, err := dnsmessage.NewName(host + ".")
	if err != nil {
		return nil, fmt.Errorf("invalid hostname: %w", err)
	}
	query := dnsmessage.Message{
		Header: dnsmessage.Header{
			ID:               binary.BigEndian.Uint16(idBytes[:]),
			RecursionDesired: true,
		},
		Questions: []dnsmessage.Question{{
			Name:  name,
			Type:  dnsmessage.TypeA,
			Class: dnsmessage.ClassINET,
		}},
	}
	packed, err := query.Pack()
	if err != nil {
		return nil, fmt.Errorf("build dns query: %w", err)
	}

	raw, err := resolve(packed)
	if err != nil {
		return nil, err
	}

	var response dnsmessage.Message
	if err := response.Unpack(raw); err != nil {
		return nil, fmt.Errorf("parse dns response: %w", err)
	}
	for _, answer := range response.Answers {
		if a, ok := answer.Body.(*dnsmessage.AResource); ok {
			return net.IP(a.A[:]), nil
		}
	}
	return nil, fmt.Errorf("no A record for %s", host)
}

func (t *TCPTunnel) ListenTCP(port uint16) (net.Listener, error) {
	return gonet.ListenTCP(t.gvisorStack, tcpip.FullAddress{
		NIC:  1,
		Port: port,
	}, ipv4.ProtocolNumber)
}

func (t *TCPTunnel) printStats() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		stats := t.gvisorStack.Stats()
		utils.Debugf("[STATS] uptime=%v packets=%d connected=%d established=%d retrans=%d",
			time.Since(t.startTime).Round(time.Second),
			t.packetCount.Load(),
			stats.TCP.CurrentConnected.Value(),
			stats.TCP.CurrentEstablished.Value(),
			stats.TCP.Retransmits.Value(),
		)
	}
}

// localIPOverride, when set, is the address the exit node uses as its egress
// IP (both for source rewriting and the return-packet filter). Point it at a
// dedicated alias IP so the RST-drop iptables rule can be scoped with
// `-s <ip>` instead of dropping RSTs host-wide.
var localIPOverride string

// SetLocalIP overrides the auto-detected egress IP for the exit node.
func SetLocalIP(ip string) { localIPOverride = ip }

func getLocalIP() string {
	if localIPOverride != "" {
		return localIPOverride
	}
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		return "192.168.1.100"
	}
	defer conn.Close()
	localAddr := conn.LocalAddr().(*net.UDPAddr)
	return localAddr.IP.String()
}
