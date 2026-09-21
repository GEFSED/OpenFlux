package io.openflux.app;

import java.io.IOException;
import java.util.HashSet;
import org.json.JSONObject;
import org.junit.Test;
import static org.junit.Assert.*;

public class TunWriteDiagnosticsTest {
    @Test public void classification_counter_categories() throws Exception {
        TunWriteDiagnostics d=new TunWriteDiagnostics();java.util.ArrayList<byte[]> packets=new java.util.ArrayList<>();
        for(int protocol:new int[]{6,17,1,99})packets.add(TunPacketClassifierTest.packet(40,protocol));
        byte[] p=new byte[40];p[0]=0x60;packets.add(p);
        p=new byte[20];p[0]=0x10;packets.add(p);packets.add(null);
        p=TunPacketClassifierTest.packet(20,6);p[0]=0x44;packets.add(p);
        p=TunPacketClassifierTest.packet(20,6);p[0]=0x4f;packets.add(p);
        for(int total:new int[]{0,19,21}) {p=TunPacketClassifierTest.packet(20,6);TunPacketClassifierTest.putShort(p,2,total);packets.add(p);}
        p=TunPacketClassifierTest.packet(40,6);TunPacketClassifierTest.putShort(p,2,20);packets.add(p);
        p=TunPacketClassifierTest.packet(40,6);TunPacketClassifierTest.putShort(p,6,0x2001);packets.add(p);
        for(byte[] bytes:packets)d.write(bytes,b -> {},e -> -1,false);
        JSONObject c=d.snapshot().getJSONObject("packet_classification");
        assertEquals(11,c.getLong("ipv4_packets"));assertEquals(1,c.getLong("ipv6_packets"));
        assertEquals(2,c.getLong("unknown_ip_version"));assertEquals(1,c.getLong("too_short_ip"));
        assertEquals(2,c.getLong("ipv4_bad_ihl"));assertEquals(1,c.getLong("ipv4_total_length_zero"));
        assertEquals(3,c.getLong("ipv4_total_length_lt_ihl"));assertEquals(1,c.getLong("ipv4_total_length_gt_buffer"));
        assertEquals(3,c.getLong("ipv4_total_length_lt_buffer"));assertEquals(7,c.getLong("ipv4_total_length_exact"));
        assertEquals(8,c.getLong("ipv4_tcp"));assertEquals(1,c.getLong("ipv4_udp"));assertEquals(1,c.getLong("ipv4_icmp"));
        assertEquals(1,c.getLong("ipv4_other_protocol"));assertEquals(1,c.getLong("ipv4_fragmented"));
        assertEquals(11,c.getLong("dst_is_tunnel_client"));assertEquals(6,d.snapshot().getLong("tun_write_success"));
        assertEquals(8,d.snapshot().getLong("tun_invalid_packet_drops"));
    }
    @Test public void malformed_drops_then_valid_packet_writes() throws Exception {
        TunWriteDiagnostics d=new TunWriteDiagnostics();
        assertEquals(TunWriteDiagnostics.Outcome.DROPPED,d.write(new byte[158],p -> fail("invalid packet written"),e -> 22,false));
        assertEquals(TunWriteDiagnostics.Outcome.WRITTEN,d.write(TunPacketClassifierTest.packet(158,6),p -> {},e -> -1,false));
        JSONObject s=d.snapshot();assertEquals(1,s.getLong("tun_invalid_packet_drops"));
        assertEquals(1,s.getLong("tun_write_attempts"));assertEquals(1,s.getLong("tun_write_success"));
        assertEquals(0,s.getLong("tun_write_einval"));assertEquals(0,s.getLong("tun_write_failures"));
        assertEquals(158,s.getInt("tun_last_invalid_buffer_len"));assertEquals(0,s.getInt("tun_last_invalid_ip_version"));
    }
    @Test public void valid_einval_is_distinct_and_bounded_at_ten_even_with_intervening_successes() throws Exception {
        TunWriteDiagnostics d=new TunWriteDiagnostics();byte[] p=TunPacketClassifierTest.packet(158,6);
        IOException einval=new IOException("write failed: EINVAL (Invalid argument)");
        for(int i=1;i<=10;i++) {
            try {
                assertEquals(TunWriteDiagnostics.Outcome.DROPPED,d.write(p,b -> {throw einval;},e -> 22,false));
                if(i==10)fail("tenth valid EINVAL must end session");
            } catch(IOException e) {assertEquals(10,i);assertSame(einval,e);}
            if(i<10)assertEquals(TunWriteDiagnostics.Outcome.WRITTEN,d.write(p,b -> {},e -> -1,false));
        }
        assertTrue(d.atEinvalLimit());JSONObject s=d.snapshot();
        assertEquals(10,s.getLong("tun_valid_ipv4_write_einval"));assertEquals(10,s.getLong("tun_write_einval"));
        assertEquals(19,s.getLong("tun_write_attempts"));assertEquals(9,s.getLong("tun_write_success"));
        assertEquals(10,s.getLong("tun_write_failures"));assertEquals(0,s.getLong("tun_invalid_packet_drops"));
        assertEquals(22,s.getInt("last_tun_failure_errno"));assertEquals(158,s.getInt("last_tun_failure_buffer_len"));
        assertEquals(4,s.getInt("last_tun_failure_ip_version"));assertEquals(20,s.getInt("last_tun_failure_ipv4_ihl"));
        assertEquals(158,s.getInt("last_tun_failure_ipv4_total_length"));assertEquals(6,s.getInt("last_tun_failure_protocol"));
        assertFalse(s.getBoolean("last_tun_failure_fragmented"));assertTrue(s.getBoolean("last_tun_failure_dst_is_tunnel_client"));
        try {d.write(p,b -> fail("write beyond threshold"),e -> -1,false);fail("threshold not latched");}catch(IOException expected){}
    }
    @Test public void trailing_bytes_are_written_unchanged_and_valid_einval_not_malformed() throws Exception {
        byte[] p=TunPacketClassifierTest.packet(158,6);TunPacketClassifierTest.putShort(p,2,100);p[157]=(byte)0xa5;
        TunWriteDiagnostics d=new TunWriteDiagnostics();
        d.write(p,b -> {assertSame(p,b);assertEquals(158,b.length);assertEquals((byte)0xa5,b[157]);throw new IOException();},e -> 22,false);
        assertEquals(1,d.snapshot().getLong("tun_valid_ipv4_write_einval"));
        assertEquals(0,d.snapshot().getLong("tun_invalid_packet_drops"));
    }
    @Test public void other_io_error_propagates_and_failure_survives_later_success() throws Exception {
        TunWriteDiagnostics d=new TunWriteDiagnostics();IOException broken=new IOException("synthetic-private-text");
        try {d.write(TunPacketClassifierTest.packet(158,17),p -> {throw broken;},e -> 5,true);fail();}catch(IOException e){assertSame(broken,e);}
        d.write(TunPacketClassifierTest.packet(40,6),p -> {},e -> -1,false);
        JSONObject s=d.snapshot();assertEquals(5,s.getInt("last_tun_failure_errno"));assertEquals(17,s.getInt("last_tun_failure_protocol"));
        assertEquals(1,s.getLong("tun_local_dns_packets_classified"));assertEquals(1,s.getLong("tun_mobile_packets_classified"));
        assertFalse(s.toString().contains("synthetic-private-text"));
        // Failure metadata has an exact allow-list; no packet/addresses/ports.
        HashSet<String> actual=new HashSet<>();s.keys().forEachRemaining(k -> {if(k.startsWith("last_tun_failure_"))actual.add(k);});
        assertEquals(java.util.Set.of("last_tun_failure_errno","last_tun_failure_buffer_len","last_tun_failure_ip_version","last_tun_failure_ipv4_ihl","last_tun_failure_ipv4_total_length","last_tun_failure_protocol","last_tun_failure_fragmented","last_tun_failure_dst_is_tunnel_client"),actual);
        assertEquals(0,new TunWriteDiagnostics().snapshot().getLong("tun_write_failures"));
    }
    @Test public void errno_token_fallback_is_precise() {
        assertEquals(22,TunWriteErrno.fromMessage("write failed: EINVAL (Invalid argument)"));
        assertEquals(-1,TunWriteErrno.fromMessage("EINVAL_FAKE"));
        assertEquals(-1,TunWriteErrno.fromMessage("Invalid argument"));
        assertEquals(-1,TunWriteErrno.fromMessage(null));
    }
}
