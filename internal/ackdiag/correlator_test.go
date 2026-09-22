package ackdiag

import (
	"encoding/binary"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"
)

// Synthetic headers only. No real endpoints, credentials, network or TCP payload.
func ip(seq,ack uint32,size int,flags byte,reverse bool) []byte {
	p:=make([]byte,40+size);p[0]=0x45;p[9]=6;p[8]=64
	binary.BigEndian.PutUint16(p[2:4],uint16(len(p)))
	p[15]=1;p[19]=2;p[21]=3;p[23]=4
	if reverse{p[15],p[19]=p[19],p[15];p[21],p[23]=p[23],p[21]}
	binary.BigEndian.PutUint32(p[24:28],seq);binary.BigEndian.PutUint32(p[28:32],ack)
	p[32]=0x50;p[33]=flags;return p
}
type clockTest struct { e *Engine; at time.Time }
func testEngine() *clockTest {
	c:=&clockTest{at:time.Unix(1,0)};c.e=New(func()time.Time{return c.at});c.e.Outgoing(ip(1000,0,0,2,false));return c
}
func(c *clockTest) advance(d time.Duration){c.at=c.at.Add(d)}
func(c *clockTest) send(seq uint32,n int,complete bool) *HTTPObservation {
	p:=ip(seq,0,n,16,false);c.e.Outgoing(p);c.advance(time.Millisecond);c.e.Enqueue(p)
	c.advance(time.Millisecond);o:=c.e.HTTPStart([][]byte{p});if complete{c.advance(10*time.Millisecond);c.e.HTTPDone(o,true,204,false)};return o
}
func(c *clockTest) ack(seq uint32) {
	p:=ip(0,seq,0,16,true);c.e.Inbound(p,c.at.Add(-time.Millisecond));decoded:=c.e.Incoming(p);c.advance(time.Microsecond);c.e.BeforeInject(decoded)
}
func check(t *testing.T,e *Engine,unique,acked,out uint64) map[string]uint64 {
	t.Helper();m:=e.Snapshot()
	if m["unique_downlink_tcp_bytes"]!=unique||m["acked_unique_downlink_tcp_bytes"]!=acked||m["outstanding_unique_bytes_current"]!=out||m["counter_consistency"]!=1{t.Fatal("byte conservation")}
	return m
}

func TestCumulativeACKMultipleSegments(t *testing.T) {
	c:=testEngine();c.send(1001,100,true);c.send(1101,100,true);c.send(1201,100,true)
	c.advance(100*time.Millisecond);c.ack(1301)
	m:=check(t,c.e,300,300,0)
	if m["downlink_tcp_segments_acked"]!=3||m["tunnel_out_to_ack_count"]!=3||m["http_start_to_ack_count"]!=3||m["http_end_to_ack_count"]!=3{t.Fatal("cumulative coverage")}
	if m["ws_to_ack_decode_count"]!=1||m["ws_to_ack_decode_sum_ns"]!=uint64(time.Millisecond)||m["ack_decode_to_inject_sum_ns"]!=uint64(time.Microsecond){t.Fatal("WS/inject timing")}
}
func TestRetransmissionOverlapAndPreviouslyACKedBytes(t *testing.T) {
	c:=testEngine();c.send(1001,100,true);c.send(1051,100,true)
	m:=check(t,c.e,150,0,150);if m["retransmitted_bytes"]!=50||m["retransmitted_segments"]!=1{t.Fatal("overlap counted as new bytes")}
	c.advance(time.Millisecond);c.ack(1151)
	m=check(t,c.e,150,150,0)
	if m["http_latency_ambiguous_retransmit_bytes"]!=100||m["http_end_to_ack_count"]!=1{t.Fatal("ambiguous attempt incorrectly attributed")}
	c.send(1001,150,true);m=check(t,c.e,150,150,0)
	if m["retransmitted_bytes"]!=200{t.Fatal("ACKed history lost")}
}
func TestDuplicateAndOldACK(t *testing.T) {
	c:=testEngine();c.send(1001,100,true);c.ack(1101);c.ack(1101);c.ack(1051)
	m:=check(t,c.e,100,100,0)
	if m["duplicate_ack_events"]!=1||m["old_ack_events"]!=1||m["tunnel_out_to_ack_count"]!=1{t.Fatal("duplicate ACK redelivered bytes")}
}
func TestPartiallyCoveredSegment(t *testing.T) {
	c:=testEngine();c.send(1001,100,true);c.ack(1041)
	m:=check(t,c.e,100,40,60);if m["downlink_tcp_segments_acked"]!=0||m["outstanding_segments_current"]!=1{t.Fatal("partial ACK retired segment")}
	c.advance(time.Millisecond);c.ack(1101);m=check(t,c.e,100,100,0)
	if m["downlink_tcp_segments_acked"]!=1||m["tunnel_out_to_ack_count"]!=2{t.Fatal("partial progress samples")}
}
func TestACKBeforeHTTPCompletion(t *testing.T) {
	c:=testEngine();o:=c.send(1001,100,false);c.advance(10*time.Millisecond);c.ack(1101)
	c.advance(90*time.Millisecond);c.e.HTTPDone(o,true,204,false)
	m:=check(t,c.e,100,100,0)
	if m["ack_before_http_complete"]!=1||m["ack_before_http_complete_bytes"]!=100||m["http_end_to_ack_count"]!=0||m["http_start_to_ack_count"]!=1{t.Fatal("ACK-before-complete or negative latency")}
}
func TestTTLAndBoundedMaps(t *testing.T) {
	c:=testEngine();c.send(1001,100,true);c.advance(RecordTTL);m:=check(t,c.e,100,0,0)
	if m["correlator_evictions"]!=1||m["evicted_unique_bytes"]!=100{t.Fatal("TTL must explicitly invalidate measurement")}
	c=testEngine();c.e.capRecords=1;c.send(1001,100,true);c.send(1101,100,true)
	m=c.e.Snapshot();if m["records_current"]>1||m["diagnostic_capacity_drops"]!=1{t.Fatal("record cap")}
	c.e.capFlows=1;p:=ip(2,0,0,2,false);p[15]=9;c.e.Outgoing(p)
	if len(c.e.flows)!=1{t.Fatal("flow cap")}
	c.e.capTags=1;c.e.Inbound([]byte{1},c.at);c.e.Inbound([]byte{2},c.at)
	if len(c.e.tags)!=1{t.Fatal("tag cap")}
}
func TestWraparoundAndSYNFINSequenceSpace(t *testing.T) {
	c:=testEngine();c.e=New(func()time.Time{return c.at})
	c.e.Outgoing(ip(0xfffffff0,0,0,2,false))
	c.send(0xfffffff1,40,true);c.ack(0x19);check(t,c.e,40,40,0)
	c.send(0x19,50,true);c.e.Outgoing(ip(0x4b,0,0,17,false));c.ack(0x4c)
	m:=check(t,c.e,90,90,0)
	if m["ack_beyond_observed"]!=0||m["sequence_constraint_violations"]!=0{t.Fatal("wraparound or FIN consumed data bytes")}
}
func TestUnknownFutureACKDoesNotDeliver(t *testing.T) {
	c:=testEngine();c.send(1001,100,true);c.ack(1102)
	m:=check(t,c.e,100,0,100);if m["ack_beyond_observed"]!=1{t.Fatal("unobserved ACK silently accepted")}
}
func TestBufferIdentityPropagationAndHTTPStates(t *testing.T) {
	c:=testEngine();p:=ip(1001,0,100,16,false);c.e.Outgoing(p)
	a:=append([]byte{9},p...);b:=append([]byte{8},a...);cp:=append([]byte(nil),b...)
	c.e.Move(p,a);c.e.Move(a,b);c.e.Move(b,cp);c.e.Enqueue(cp)
	m:=check(t,c.e,100,0,100);if m["http_not_started_bytes"]!=100||len(c.e.tags)!=1{t.Fatal("identity move")}
	o:=c.e.HTTPStart([][]byte{cp});m=c.e.Snapshot();if m["http_inflight_bytes"]!=100{t.Fatal("HTTP inflight state")}
	c.advance(time.Millisecond);c.e.HTTPDone(o,true,200,false);m=c.e.Snapshot()
	if m["http_success_not_acked_bytes"]!=100||m["tags_current"]!=0{t.Fatal("HTTP success state")}
	c.ack(1101);check(t,c.e,100,100,0)
}
func TestHTTPFailureDoesNotReplayOrAcknowledge(t *testing.T) {
	c:=testEngine();o:=c.send(1001,100,false);c.e.HTTPDone(o,false,429,false)
	m:=check(t,c.e,100,0,100)
	if m["http_requests"]!=1||m["http_failures"]!=1||m["http_failed_not_acked_bytes"]!=100{t.Fatal("HTTP rejection semantics")}
}
func TestConcurrentSnapshots(t *testing.T) {
	e:=New(time.Now);var wg sync.WaitGroup
	for n:=0;n<16;n++{wg.Add(1);go func(id int){defer wg.Done();syn:=ip(0,0,0,2,false);syn[15]=byte(id+1);e.Outgoing(syn);for i:=0;i<100;i++{
		p:=ip(uint32(i*100+1),0,100,16,false);p[15]=byte(id+1)
		e.Outgoing(p);e.Enqueue(p);o:=e.HTTPStart([][]byte{p});e.HTTPDone(o,true,204,false)
		a:=ip(0,uint32(i*100+101),0,16,true);a[19]=byte(id+1);e.Inbound(a,time.Now());e.BeforeInject(e.Incoming(a))
	}}(n)}
	for i:=0;i<100;i++{e.Snapshot()};wg.Wait();m:=e.Snapshot()
	if m["correlation_valid"]!=1||m["acked_unique_downlink_tcp_bytes"]!=160000||m["outstanding_unique_bytes_current"]!=0{t.Fatal("concurrent accounting")}
}

func TestIdleACKedHistoryPreservedAndUnanchoredFlagged(t *testing.T) {
	c:=testEngine();c.send(1001,100,true);c.ack(1101);c.advance(2*RecordTTL);c.e.Snapshot()
	c.send(1001,100,true);m:=check(t,c.e,100,100,0)
	if m["retransmitted_bytes"]!=100||m["correlation_valid"]!=1{t.Fatal("idle history lost")}
	e:=New(time.Now);e.Outgoing(ip(200,0,10,16,false))
	if e.Snapshot()["correlation_valid"]!=0{t.Fatal("unanchored flow silently treated as complete observation")}
}
func TestHistogramAndSecretSafeSchema(t *testing.T) {
	var h Histogram
	limits:=[]time.Duration{50*time.Millisecond,100*time.Millisecond,250*time.Millisecond,500*time.Millisecond,time.Second,2*time.Second,5*time.Second,10*time.Second}
	for _,d:=range limits{h.observe(d-1);h.observe(d)};h.observe(-time.Nanosecond)
	if h.count!=16||h.buckets[0]!=1||h.buckets[8]!=1{t.Fatal("bucket edges or negative latency")}
	c:=testEngine();c.send(1001,100,true);m:=c.e.Snapshot();data,_:=json.Marshal(m)
	for _,forbidden:=range []string{"flow_key","src","dst","port\"","payload\"","seq\"","ack\"","cookie","token","url","header"}{
		if strings.Contains(string(data),forbidden){t.Fatal("unsafe snapshot field")}
	}
}
