package io.openflux.app;

import java.io.IOException;
import org.json.JSONException;
import org.json.JSONObject;

/** Session-local diagnostic policy, used ONLY when BuildConfig.PERF_LAB is true.
 * No packet reference, addresses, ports, exception text or payload is retained.
 * This is a structural check, not a checksum/TCP validity or delivery guarantee.
 */
final class TunWriteDiagnostics {
    static final int EINVAL = 22;
    static final int MAX_VALID_EINVAL = 10;
    interface Writer { void write(byte[] packet) throws IOException; }
    interface ErrnoResolver { int errno(IOException error); }
    enum Outcome { WRITTEN, DROPPED }

    private long attempts, success, failures, einval, validEinval, invalidDrops;
    private long ipv4, ipv6, unknownVersion, tooShort, badIhl, totalZero, totalLtIhl;
    private long totalGtBuffer, totalLtBuffer, totalExact, tcp, udp, icmp, other, fragments, dstClient;
    private long mobilePackets, dnsPackets;
    private int lastErrno = -1;
    private TunPacketClassifier.Shape lastFailure;
    private TunPacketClassifier.Shape lastInvalid;
    private String lastDropReason = "none";

    synchronized boolean atEinvalLimit() { return validEinval >= MAX_VALID_EINVAL; }

    // Called under the service output lock. The object is replaced at session
    // start and retained after stop so a failure snapshot survives teardown.
    synchronized Outcome write(byte[] packet, Writer writer, ErrnoResolver errnoResolver,
                               boolean localDns) throws IOException {
        TunPacketClassifier.Shape s = TunPacketClassifier.classifyPacketForTun(packet);
        classify(s);
        if (localDns) dnsPackets++; else mobilePackets++;
        if (!s.validIpv4) {
            invalidDrops++;
            lastDropReason = s.rejection;
            lastInvalid = s;
            return Outcome.DROPPED;
        }
        if (validEinval >= MAX_VALID_EINVAL) throw new IOException("TUN diagnostic EINVAL limit reached");
        attempts++; // Actual writes only; pre-validation drops are separate.
        try {
            writer.write(packet); // Always the original array and full length.
            success++;
            return Outcome.WRITTEN;
        } catch (IOException error) {
            failures++;
            lastErrno = errnoResolver.errno(error);
            lastFailure = s;
            if (lastErrno == EINVAL) {
                einval++;
                // Every attempted write passed the structural IPv4 check.
                validEinval++;
                if (validEinval < MAX_VALID_EINVAL) return Outcome.DROPPED;
            }
            // Preserve the serious error/10th valid EINVAL for service fail().
            throw error;
        }
    }

    private void classify(TunPacketClassifier.Shape s) {
        if (s.ipVersion == 4) ipv4++;
        else if (s.ipVersion == 6) ipv6++;
        else unknownVersion++;
        if (s.tooShort) tooShort++;
        if (s.badIhl) badIhl++;
        if (s.ipVersion != 4) return;
        if (s.ipv4TotalLength >= 0) {
            if (s.ipv4TotalLength == 0) totalZero++;
            if (s.ipv4TotalLength < s.ipv4Ihl) totalLtIhl++;
            if (s.ipv4TotalLength > s.bufferLen) totalGtBuffer++;
            else if (s.ipv4TotalLength < s.bufferLen) totalLtBuffer++;
            else totalExact++;
        }
        if (s.protocol == 6) tcp++;
        else if (s.protocol == 17) udp++;
        else if (s.protocol == 1) icmp++;
        else if (s.protocol >= 0) other++;
        if (s.fragmented) fragments++;
        if (s.dstIsTunnelClient) dstClient++;
    }

    synchronized JSONObject snapshot() throws JSONException {
        JSONObject d = new JSONObject();
        d.put("tun_write_attempts", attempts);
        d.put("tun_write_success", success);
        d.put("tun_write_failures", failures);
        d.put("tun_write_einval", einval);
        d.put("tun_valid_ipv4_write_einval", validEinval);
        d.put("tun_invalid_packet_drops", invalidDrops);
        d.put("tun_last_invalid_reason", lastDropReason);
        d.put("tun_last_invalid_buffer_len", lastInvalid == null ? -1 : lastInvalid.bufferLen);
        d.put("tun_last_invalid_ip_version", lastInvalid == null ? -1 : lastInvalid.ipVersion);
        d.put("tun_mobile_packets_classified", mobilePackets);
        d.put("tun_local_dns_packets_classified", dnsPackets);
        JSONObject c = new JSONObject();
        c.put("ipv4_packets", ipv4); c.put("ipv6_packets", ipv6);
        c.put("unknown_ip_version", unknownVersion); c.put("too_short_ip", tooShort);
        c.put("ipv4_bad_ihl", badIhl); c.put("ipv4_total_length_zero", totalZero);
        c.put("ipv4_total_length_lt_ihl", totalLtIhl);
        c.put("ipv4_total_length_gt_buffer", totalGtBuffer);
        c.put("ipv4_total_length_lt_buffer", totalLtBuffer);
        c.put("ipv4_total_length_exact", totalExact);
        c.put("ipv4_tcp", tcp); c.put("ipv4_udp", udp); c.put("ipv4_icmp", icmp);
        c.put("ipv4_other_protocol", other); c.put("ipv4_fragmented", fragments);
        c.put("dst_is_tunnel_client", dstClient);
        d.put("packet_classification", c);
        TunPacketClassifier.Shape f = lastFailure;
        d.put("last_tun_failure_errno", lastErrno);
        d.put("last_tun_failure_buffer_len", f == null ? -1 : f.bufferLen);
        d.put("last_tun_failure_ip_version", f == null ? -1 : f.ipVersion);
        d.put("last_tun_failure_ipv4_ihl", f == null ? -1 : f.ipv4Ihl);
        d.put("last_tun_failure_ipv4_total_length", f == null ? -1 : f.ipv4TotalLength);
        d.put("last_tun_failure_protocol", f == null ? -1 : f.protocol);
        d.put("last_tun_failure_fragmented", f != null && f.fragmented);
        d.put("last_tun_failure_dst_is_tunnel_client", f != null && f.dstIsTunnelClient);
        return d;
    }
}
