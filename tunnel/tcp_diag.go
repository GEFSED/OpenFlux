package tunnel

import (
	"fmt"

	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"
)

// tcpDiagnosticLine reads existing counters only. Inbound/outbound counts are
// link-endpoint observations, not application bytes or proof of Internet access.
// Recovery counters overlap; they must not be summed into a loss count.
func (t *TCPTunnel) tcpDiagnosticLine() (string, error) {
	var recovery tcpip.TCPRecovery
	if err := t.gvisorStack.TransportProtocolOption(tcp.ProtocolNumber, &recovery); err != nil {
		return "", fmt.Errorf("TCP recovery option unavailable")
	}
	s := t.gvisorStack.Stats().TCP
	return fmt.Sprintf("[TCP-DIAG] recovery_mode=%d tcp_current_connected=%d tcp_current_established=%d tcp_retransmits=%d tcp_timeouts=%d tcp_fast_recovery=%d tcp_sack_recovery=%d tcp_tlp_recovery=%d tcp_spurious_recovery=%d tcp_spurious_rto_recovery=%d tcp_fast_retransmit=%d tcp_slow_start_retransmits=%d tcp_dsack_segments=%d tcp_segments_sent=%d tcp_valid_segments_received=%d tcp_invalid_segments_received=%d tcp_segment_send_errors=%d tcp_failed_connection_attempts=%d tunnel_packets=%d tunnel_packets_out=%d",
		recovery,
		s.CurrentConnected.Value(), s.CurrentEstablished.Value(),
		s.Retransmits.Value(), s.Timeouts.Value(),
		s.FastRecovery.Value(), s.SACKRecovery.Value(), s.TLPRecovery.Value(),
		s.SpuriousRecovery.Value(), s.SpuriousRTORecovery.Value(),
		s.FastRetransmit.Value(), s.SlowStartRetransmits.Value(),
		s.SegmentsAckedWithDSACK.Value(), s.SegmentsSent.Value(),
		s.ValidSegmentsReceived.Value(), s.InvalidSegmentsReceived.Value(),
		s.SegmentSendErrors.Value(), s.FailedConnectionAttempts.Value(),
		t.tunnelEP.packetIn.Load(), t.tunnelEP.packetOut.Load()), nil
}
