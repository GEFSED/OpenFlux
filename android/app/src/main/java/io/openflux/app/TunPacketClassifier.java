package io.openflux.app;

/** Pure IPv4-only TUN shape check. Never retains or exposes packet data. */
final class TunPacketClassifier {
    // One definition for Builder.addAddress and the boolean destination check.
    static final String CLIENT_ADDRESS = "10.10.10.2";

    static final class Shape {
        final int bufferLen, ipVersion, ipv4Ihl, ipv4TotalLength, protocol;
        final boolean fragmented, dstIsTunnelClient, tooShort, badIhl, validIpv4;
        final String rejection;

        private Shape(int len, int version, int ihl, int total, int protocol,
                      boolean fragmented, boolean dst, boolean shortIP, boolean badIhl,
                      String rejection) {
            this.bufferLen = len; this.ipVersion = version; this.ipv4Ihl = ihl;
            this.ipv4TotalLength = total; this.protocol = protocol;
            this.fragmented = fragmented; this.dstIsTunnelClient = dst;
            this.tooShort = shortIP; this.badIhl = badIhl;
            this.rejection = rejection; this.validIpv4 = "none".equals(rejection);
        }
    }

    static Shape classifyPacketForTun(byte[] packet) {
        int len = packet == null ? 0 : packet.length;
        int version = len == 0 ? -1 : (packet[0] & 0xff) >>> 4;
        boolean v4 = version == 4;
        int ihl = v4 ? (packet[0] & 15) * 4 : -1;
        int total = v4 && len >= 4 ? u16(packet, 2) : -1;
        int protocol = v4 && len >= 10 ? packet[9] & 0xff : -1;
        boolean fragmented = v4 && len >= 8 && (u16(packet, 6) & 0x3fff) != 0;
        boolean dst = v4 && len >= 20 && packet[16] == 10 && packet[17] == 10
                && packet[18] == 10 && packet[19] == 2;
        boolean shortIP = len < (version == 6 ? 40 : 20);
        boolean badIhl = v4 && (ihl < 20 || ihl > len);
        String rejection = "none";
        if (shortIP) rejection = "too_short_ip";
        else if (!v4) rejection = "not_ipv4";
        else if (badIhl) rejection = "ipv4_bad_ihl";
        else if (total == 0) rejection = "ipv4_total_length_zero";
        else if (total < ihl) rejection = "ipv4_total_length_lt_ihl";
        else if (total > len) rejection = "ipv4_total_length_gt_buffer";
        // total < len is recorded, NOT repaired/truncated or called malformed.
        // The existing carrier preserves complete frames; it promises no padding.
        return new Shape(len, version, ihl, total, protocol, fragmented, dst,
                shortIP, badIhl, rejection);
    }

    private static int u16(byte[] p, int at) {
        return ((p[at] & 0xff) << 8) | (p[at + 1] & 0xff);
    }
}
