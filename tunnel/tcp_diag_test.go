package tunnel

import (
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"

	"universal-bypass-tool/transport"
)

type rackDiagnosticTransport struct{ *transport.BaseTransport }

func (t *rackDiagnosticTransport) Send([]byte) error { return nil }

func TestDiagnosticConstructorRecovery(t *testing.T) {
	want := int(tcpip.TCPRACKLossDetection)
	if value := os.Getenv("RACK_EXPECT_RECOVERY"); value != "" {
		var err error
		want, err = strconv.Atoi(value)
		if err != nil {
			t.Fatal(err)
		}
	}
	trans := &rackDiagnosticTransport{transport.NewBaseTransport(transport.DefaultConfig())}
	tun := NewTCPTunnelMode(trans, true, ExitModeProxy)
	defer tun.gvisorStack.Destroy()
	var got tcpip.TCPRecovery
	if err := tun.gvisorStack.TransportProtocolOption(tcp.ProtocolNumber, &got); err != nil {
		t.Fatal(err)
	}
	if int(got) != want {
		t.Fatalf("TCP recovery = %d, want %d", got, want)
	}
	var send tcpip.TCPSendBufferSizeRangeOption
	var receive tcpip.TCPReceiveBufferSizeRangeOption
	if err := tun.gvisorStack.TransportProtocolOption(tcp.ProtocolNumber, &send); err != nil {
		t.Fatal(err)
	}
	if err := tun.gvisorStack.TransportProtocolOption(tcp.ProtocolNumber, &receive); err != nil {
		t.Fatal(err)
	}
	if send.Min != TCPBufMin || send.Default != TCPBufDefault || send.Max != TCPBufMax ||
		receive.Min != TCPBufMin || receive.Default != TCPBufDefault || receive.Max != TCPBufMax {
		t.Fatal("production TCP buffer settings changed")
	}
}

func TestTCPDiagnosticSchemaAndReadOnlyCounters(t *testing.T) {
	s := stack.New(stack.Options{
		NetworkProtocols:   []stack.NetworkProtocolFactory{ipv4.NewProtocol},
		TransportProtocols: []stack.TransportProtocolFactory{tcp.NewProtocol},
	})
	defer s.Destroy()
	tun := &TCPTunnel{gvisorStack: s, tunnelEP: NewTunnelLinkEndpoint()}
	counters := s.Stats().TCP
	fields := []struct {
		name    string
		counter *tcpip.StatCounter
	}{
		{"tcp_current_connected", counters.CurrentConnected},
		{"tcp_current_established", counters.CurrentEstablished},
		{"tcp_retransmits", counters.Retransmits},
		{"tcp_timeouts", counters.Timeouts},
		{"tcp_fast_recovery", counters.FastRecovery},
		{"tcp_sack_recovery", counters.SACKRecovery},
		{"tcp_tlp_recovery", counters.TLPRecovery},
		{"tcp_spurious_recovery", counters.SpuriousRecovery},
		{"tcp_spurious_rto_recovery", counters.SpuriousRTORecovery},
		{"tcp_fast_retransmit", counters.FastRetransmit},
		{"tcp_slow_start_retransmits", counters.SlowStartRetransmits},
		{"tcp_dsack_segments", counters.SegmentsAckedWithDSACK},
		{"tcp_segments_sent", counters.SegmentsSent},
		{"tcp_valid_segments_received", counters.ValidSegmentsReceived},
		{"tcp_invalid_segments_received", counters.InvalidSegmentsReceived},
		{"tcp_segment_send_errors", counters.SegmentSendErrors},
		{"tcp_failed_connection_attempts", counters.FailedConnectionAttempts},
	}
	for i, field := range fields {
		field.counter.IncrementBy(uint64(i + 2))
	}
	tun.tunnelEP.packetIn.Add(101)
	tun.tunnelEP.packetOut.Add(103)
	line, err := tun.tcpDiagnosticLine()
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^\[TCP-DIAG\]( [a-z_]+=[0-9]+){20}$`).MatchString(line) {
		t.Fatalf("unexpected diagnostic schema: %q", line)
	}
	values := make(map[string]uint64)
	for _, field := range strings.Fields(line)[1:] {
		pair := strings.SplitN(field, "=", 2)
		value, err := strconv.ParseUint(pair[1], 10, 64)
		if err != nil {
			t.Fatal(err)
		}
		if _, duplicate := values[pair[0]]; duplicate {
			t.Fatalf("duplicate diagnostic field %q", pair[0])
		}
		values[pair[0]] = value
	}
	for i, field := range fields {
		if values[field.name] != uint64(i+2) {
			t.Fatalf("%s mapped to wrong counter", field.name)
		}
	}
	if values["recovery_mode"] != uint64(tcpip.TCPRACKLossDetection) ||
		values["tunnel_packets"] != 101 || values["tunnel_packets_out"] != 103 {
		t.Fatal("incorrect recovery mode or packet counters")
	}
	again, err := tun.tcpDiagnosticLine()
	if err != nil || again != line {
		t.Fatal("snapshot mutated counters")
	}
}
