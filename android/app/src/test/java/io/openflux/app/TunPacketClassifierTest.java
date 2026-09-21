package io.openflux.app;

import java.nio.charset.StandardCharsets;
import java.util.Arrays;
import org.json.JSONArray;
import org.json.JSONObject;
import org.junit.Test;
import static org.junit.Assert.*;

public class TunPacketClassifierTest {
    static byte[] packet(int len, int protocol) {
        byte[] p = new byte[len];
        p[0] = 0x45; p[8] = 64; p[9] = (byte) protocol;
        putShort(p, 2, len);
        p[16] = 10; p[17] = 10; p[18] = 10; p[19] = 2;
        return p;
    }
    static void putShort(byte[] p, int at, int n) { p[at]=(byte)(n>>>8);p[at+1]=(byte)n; }

    @Test public void valid_ipv4_tcp_udp_icmp() {
        for (int protocol : new int[]{6,17,1}) {
            TunPacketClassifier.Shape s = TunPacketClassifier.classifyPacketForTun(packet(158,protocol));
            assertTrue(s.validIpv4);assertEquals(4,s.ipVersion);assertEquals(20,s.ipv4Ihl);
            assertEquals(158,s.ipv4TotalLength);assertEquals(protocol,s.protocol);
            assertTrue(s.dstIsTunnelClient);assertFalse(s.fragmented);
            assertEquals("10.10.10.2",TunPacketClassifier.CLIENT_ADDRESS);
        }
    }
    @Test public void ipv6_unknown_and_short() {
        byte[] v6 = new byte[40];v6[0]=0x60;
        assertEquals(6,TunPacketClassifier.classifyPacketForTun(v6).ipVersion);
        assertFalse(TunPacketClassifier.classifyPacketForTun(v6).validIpv4);
        byte[] unknown=packet(158,6);unknown[0]=0x75;
        assertEquals(7,TunPacketClassifier.classifyPacketForTun(unknown).ipVersion);
        assertFalse(TunPacketClassifier.classifyPacketForTun(unknown).validIpv4);
        for (byte[] p : new byte[][]{null,new byte[0],new byte[]{0x45},Arrays.copyOf(packet(20,6),19)}) {
            TunPacketClassifier.Shape s=TunPacketClassifier.classifyPacketForTun(p);
            assertFalse(s.validIpv4);assertTrue(s.tooShort);
        }
    }
    @Test public void bad_ihl_and_impossible_total_lengths() {
        byte[] p=packet(20,6);p[0]=0x44;
        assertTrue(TunPacketClassifier.classifyPacketForTun(p).badIhl);
        p[0]=0x4f;assertTrue(TunPacketClassifier.classifyPacketForTun(p).badIhl);
        p[0]=0x45;
        for (int total : new int[]{0,19,21}) {
            putShort(p,2,total);assertFalse(TunPacketClassifier.classifyPacketForTun(p).validIpv4);
        }
        putShort(p,2,0);assertEquals("ipv4_total_length_zero",TunPacketClassifier.classifyPacketForTun(p).rejection);
        putShort(p,2,19);assertEquals("ipv4_total_length_lt_ihl",TunPacketClassifier.classifyPacketForTun(p).rejection);
        putShort(p,2,21);assertEquals("ipv4_total_length_gt_buffer",TunPacketClassifier.classifyPacketForTun(p).rejection);
    }
    @Test public void total_length_lt_buffer_is_not_repaired() {
        byte[] p=packet(158,6);putShort(p,2,100);p[157]=(byte)0xa5;byte[] before=p.clone();
        TunPacketClassifier.Shape s=TunPacketClassifier.classifyPacketForTun(p);
        assertTrue(s.validIpv4);assertEquals(100,s.ipv4TotalLength);assertEquals(158,s.bufferLen);
        assertArrayEquals(before,p);
    }
    @Test public void fragmented_ipv4_and_options() {
        byte[] p=packet(80,6);p[0]=0x46;
        assertEquals(24,TunPacketClassifier.classifyPacketForTun(p).ipv4Ihl);
        for(int bits:new int[]{0x2000,1,0x2001}) {
            putShort(p,6,bits);assertTrue(TunPacketClassifier.classifyPacketForTun(p).fragmented);
            assertTrue(TunPacketClassifier.classifyPacketForTun(p).validIpv4);
        }
        putShort(p,6,0x4000);assertFalse(TunPacketClassifier.classifyPacketForTun(p).fragmented);
        p[19]=3;assertFalse(TunPacketClassifier.classifyPacketForTun(p).dstIsTunnelClient);
    }

    static JSONArray sharedCases() throws Exception {
        try (java.io.InputStream in=TunPacketClassifierTest.class.getResourceAsStream("/cases.json")) {
            assertNotNull(in);return new JSONArray(new String(in.readAllBytes(),StandardCharsets.UTF_8));
        }
    }
    static byte[] sharedPacket(JSONObject c) throws Exception {
        byte[] p=packet(c.getInt("buffer_len"),6);
        if(c.getInt("version")!=4) {Arrays.fill(p,(byte)0x31);return p;}
        int total=c.getInt("total_length");putShort(p,2,total);
        p[12]=(byte)192;p[13]=0;p[14]=2;p[15]=1;
        putShort(p,20,1);putShort(p,22,2);p[32]=0x50;p[33]=0x10;
        if(total<=p.length) {
            byte[] pseudo=new byte[12+total-20];System.arraycopy(p,12,pseudo,0,8);
            pseudo[9]=6;putShort(pseudo,10,total-20);System.arraycopy(p,20,pseudo,12,total-20);
            putShort(p,36,checksum(pseudo));Arrays.fill(p,total,p.length,(byte)0xa5);
        }
        putShort(p,10,checksum(Arrays.copyOf(p,20)));return p;
    }
    private static int checksum(byte[] p) {
        long sum=0;for(int i=0;i<p.length;i+=2){sum+=(p[i]&255)<<8;if(i+1<p.length)sum+=p[i+1]&255;}
        while((sum>>>16)!=0)sum=(sum&65535)+(sum>>>16);return (~(int)sum)&65535;
    }
    @Test public void four_authenticated_return_shapes_are_distinct() throws Exception {
        JSONArray cases=sharedCases();assertEquals(4,cases.length());
        for(int i=0;i<cases.length();i++) {
            JSONObject c=cases.getJSONObject(i);byte[] p=sharedPacket(c);
            byte[] digest=java.security.MessageDigest.getInstance("SHA-256").digest(p);
            StringBuilder hex=new StringBuilder();for(byte b:digest)hex.append(String.format(java.util.Locale.ROOT,"%02x",b&255));
            assertEquals("Go and Java must classify identical synthetic bytes",c.getString("sha256"),hex.toString());
            TunPacketClassifier.Shape s=TunPacketClassifier.classifyPacketForTun(p);
            assertEquals(c.getBoolean("valid_ipv4"),s.validIpv4);
            assertEquals(c.getInt("version"),s.ipVersion);
            assertEquals(c.getInt("buffer_len"),s.bufferLen);
            assertEquals(c.getInt("total_length"),s.ipv4TotalLength);
            TunWriteDiagnostics d=new TunWriteDiagnostics();
            d.write(p, bytes -> assertSame(p,bytes),e -> -1,false);
            JSONObject counts=d.snapshot().getJSONObject("packet_classification");
            if(i==1)assertEquals(1,counts.getLong("unknown_ip_version"));
            else assertEquals(1,counts.getLong("ipv4_total_length_"+c.getString("length_relation")));
        }
    }
}
