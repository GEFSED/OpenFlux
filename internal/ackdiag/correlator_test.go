package ackdiag

import (
    "encoding/binary"
    "encoding/json"
    "math/rand"
    "runtime"
    "strings"
    "sync"
    "testing"
    "time"
    "unsafe"
)

// Event tests use opaque synthetic IDs, not packet addresses/ports/payloads.
type simulation struct{e *Engine;at time.Time;key flowKey;anchor uint32}
func flowID(n uint32)flowKey{var k flowKey;binary.BigEndian.PutUint32(k[:4],n);return k}
func sim(anchor uint32)*simulation{
    s:=&simulation{at:time.Unix(1000,0),key:flowID(1),anchor:anchor}
    s.e=New(func()time.Time{return s.at});s.syn(anchor);return s
}
func(s *simulation)syn(n uint32){s.anchor=n;s.e.outgoing(packet{key:s.key,seq:n,syn:true},s.at)}
func(s *simulation)advance(d time.Duration){s.at=s.at.Add(d)}
func(s *simulation)sendAt(seq uint32,n int,at time.Time)*attempt{
    a:=s.e.outgoing(packet{key:s.key,seq:seq,size:n},at)
    if a!=nil{a.enqueue=at.Add(time.Millisecond);a.start=at.Add(2*time.Millisecond);a.end=at.Add(3*time.Millisecond);a.success=true}
    return a
}
func(s *simulation)send(seq uint32,n int)*attempt{s.advance(5*time.Millisecond);return s.sendAt(seq,n,s.at)}
func(s *simulation)ackAt(n uint32,at time.Time){s.e.acknowledge(packet{key:s.key,ack:n,ackFlag:true},at)}
func(s *simulation)ack(n uint32){s.advance(100*time.Millisecond);s.ackAt(n,s.at)}
func assertState(t *testing.T,e *Engine,unique,acked,out uint64,valid bool)map[string]uint64{
    t.Helper();m:=e.Snapshot()
    if m["unique_downlink_tcp_bytes"]!=unique||m["acked_unique_downlink_tcp_bytes"]!=acked||m["outstanding_unique_bytes_current"]!=out||m["counter_consistency"]!=1||unique!=acked+out+m["invalidated_unique_bytes"]{t.Fatalf("conservation: unique=%d acked=%d outstanding=%d invalid=%d consistency=%d",m["unique_downlink_tcp_bytes"],m["acked_unique_downlink_tcp_bytes"],m["outstanding_unique_bytes_current"],m["invalidated_unique_bytes"],m["counter_consistency"])}
    if valid&&(m["correlation_valid"]!=1||m["correlator_evictions"]!=0||m["sequence_constraint_violations"]!=0||m["invalidated_unique_bytes"]!=0){t.Fatalf("invalid synthetic observation: %v",m)}
    return m
}
func TestDeterministicTCPEvents(t *testing.T){
    cases:=[]struct{name string;run func(*testing.T,*simulation)}{
        {"one_segment_one_ack",func(t *testing.T,s *simulation){s.send(1001,100);s.ack(1101);assertState(t,s.e,100,100,0,true)}},
        {"cumulative_multiple",func(t *testing.T,s *simulation){s.send(1001,100);s.send(1101,100);s.send(1201,100);s.ack(1301);assertState(t,s.e,300,300,0,true)}},
        {"duplicate_ack",func(t *testing.T,s *simulation){s.send(1001,100);s.ack(1101);s.ack(1101);s.ack(1051);m:=assertState(t,s.e,100,100,0,true);if m["duplicate_ack_events"]!=1||m["old_ack_events"]!=1{t.Fatal("ACK classification")}}},
        {"exact_retransmission",func(t *testing.T,s *simulation){s.send(1001,100);s.send(1001,100);s.ack(1101);m:=assertState(t,s.e,100,100,0,true);if m["retransmitted_bytes"]!=100||m["multi_attempt_acked_bytes"]!=100{t.Fatal("attempt lost")}}},
        {"partial_overlap",func(t *testing.T,s *simulation){s.send(1001,100);s.send(1051,100);s.ack(1151);m:=assertState(t,s.e,150,150,0,true);if m["retransmitted_unique_bytes"]!=50||m["multi_attempt_acked_bytes"]!=50{t.Fatal("overlap over-attributed")}}},
        {"retx_spans_earlier_segments",func(t *testing.T,s *simulation){s.send(1001,100);s.send(1101,100);s.send(1051,200);s.ack(1251);m:=assertState(t,s.e,250,250,0,true);if m["retransmitted_bytes"]!=150||m["multi_attempt_acked_bytes"]!=150{t.Fatal("union")}}},
        {"ack_original_and_retransmission",func(t *testing.T,s *simulation){s.send(1001,100);s.send(1001,100);s.send(1101,100);s.ack(1201);m:=assertState(t,s.e,200,200,0,true);if m["http_first_success_end_to_ack_bytes"]!=200||m["http_last_success_end_to_ack_bytes"]!=200{t.Fatal("HTTP samples excluded")}}},
        {"ack_after_retransmission",func(t *testing.T,s *simulation){a:=s.send(1001,100);s.advance(time.Second);b:=s.send(1001,100);s.ack(1101);m:=assertState(t,s.e,100,100,0,true);if m["tunnel_first_out_to_ack_sum_ns"]!=uint64(s.at.Sub(a.out))||m["tunnel_last_out_to_ack_sum_ns"]!=uint64(s.at.Sub(b.out)){t.Fatal("first/last attempt attribution")}}},
        {"ack_before_http_complete",func(t *testing.T,s *simulation){a:=s.send(1001,100);a.end=time.Time{};s.ack(1101);a.end=s.at.Add(time.Second);m:=assertState(t,s.e,100,100,0,true);if m["ack_before_first_http_complete_bytes"]!=100||m["ack_before_last_http_complete_bytes"]!=100||m["http_first_success_end_to_ack_count"]!=0{t.Fatal("negative latency")}}},
        {"delayed_ack_gt30s",func(t *testing.T,s *simulation){s.send(1001,100);s.advance(45*time.Second);assertState(t,s.e,100,0,100,true);s.ack(1101);assertState(t,s.e,100,100,0,true)}},
        {"fin_sequence_byte",func(t *testing.T,s *simulation){s.send(1001,100);s.e.outgoing(packet{key:s.key,seq:1101,fin:true},s.at);s.ack(1102);s.e.acknowledge(packet{key:s.key,fin:true,ackFlag:true,ack:1102},s.at);m:=assertState(t,s.e,100,100,0,true);if m["flow_close_events"]!=1{t.Fatal("FIN close")}}},
        {"syn_with_data",func(t *testing.T,s *simulation){a:=s.e.outgoing(packet{key:s.key,seq:1000,syn:true,size:100},s.at);a.start=s.at;a.end=s.at;a.success=true;s.ack(1101);assertState(t,s.e,100,100,0,true)}},
        {"out_of_order_segments",func(t *testing.T,s *simulation){s.send(1101,100);s.send(1001,100);s.ack(1201);assertState(t,s.e,200,200,0,true)}},
        {"partial_ack",func(t *testing.T,s *simulation){s.send(1001,100);s.ack(1041);assertState(t,s.e,100,40,60,true);s.ack(1101);assertState(t,s.e,100,100,0,true)}},
        {"stale_plus_new_data",func(t *testing.T,s *simulation){s.send(1001,100);s.advance(60*time.Second);s.send(1101,100);s.ack(1201);assertState(t,s.e,200,200,0,true)}},
        {"post_ack_retransmission",func(t *testing.T,s *simulation){s.send(1001,100);s.ack(1101);m:=assertState(t,s.e,100,100,0,true);before:=m["tunnel_last_out_to_ack_sum_ns"];s.send(1001,100);s.ack(1101);m=assertState(t,s.e,100,100,0,true);if m["multi_attempt_acked_bytes"]!=0||m["tunnel_last_out_to_ack_sum_ns"]!=before{t.Fatal("post-delivery attempt attributed to earlier ACK")}}},
    }
    for _,tc:=range cases{t.Run(tc.name,func(t *testing.T){tc.run(t,sim(1000))})}
}
func TestWrapAndOldDataAcrossNumericWrap(t *testing.T){
    s:=sim(0xfffffff0);s.send(0xfffffff1,40);s.send(0x19,50);s.send(0xfffffff8,40);s.ack(0x4b)
    m:=assertState(t,s.e,90,90,0,true);if m["retransmitted_bytes"]!=40{t.Fatal("wrap overlap")}
    s.e.outgoing(packet{key:s.key,seq:0x4b,fin:true},s.at);s.ack(0x4c);assertState(t,s.e,90,90,0,true)
}
func TestReorderedObserverEvents(t *testing.T){
    s:=sim(1000);out:=s.at.Add(time.Millisecond);ack:=out.Add(time.Second)
    s.ackAt(1201,ack) // Observer callback seen before earlier output callback.
    s.sendAt(1101,100,out.Add(10*time.Millisecond));s.sendAt(1001,100,out)
    s.at=ack;assertState(t,s.e,200,200,0,true)
    // A later-observed but earlier-timestamp partial ACK must win for its prefix.
    s.ackAt(1051,out.Add(500*time.Millisecond));m:=assertState(t,s.e,200,200,0,true)
    if m["tunnel_first_out_to_ack_bytes"]!=200{t.Fatal("reordered ACK counted twice")}
}
func TestFirstLastHTTPSuccessAndPendingCompletion(t *testing.T){
    s:=sim(1000);a:=s.send(1001,100);a.success=false
    b:=s.send(1001,100);c:=s.send(1001,100);c.end=time.Time{}
    s.ack(1101);m:=assertState(t,s.e,100,100,0,true)
    if m["http_first_success_end_to_ack_sum_ns"]!=uint64(s.at.Sub(b.end))||m["http_last_success_end_to_ack_sum_ns"]!=uint64(s.at.Sub(b.end))||m["ack_before_first_http_complete"]!=0||m["ack_before_last_http_complete"]!=1{t.Fatal("attempt completion bounds")}
    c.end=s.at.Add(time.Second);c.success=true;m=assertState(t,s.e,100,100,0,true)
    if m["http_last_success_end_to_ack_sum_ns"]!=uint64(s.at.Sub(b.end)){t.Fatal("negative end sample")}
}
func TestFlowCloseReuseAndOldACK(t *testing.T){
    s:=sim(1000);s.send(1001,100);s.ack(1101)
    s.e.outgoing(packet{key:s.key,rst:true},s.at)
    s.advance(time.Second);s.syn(9000);s.send(9001,100)
    s.ack(1101);assertState(t,s.e,200,100,100,true)
    s.ack(9101);assertState(t,s.e,200,200,0,true)
    if s.e.generations!=2{t.Fatal("generation history lost")}
}
func TestAmbiguousReuseAndResetOutstandingRetained(t *testing.T){
    s:=sim(1000);s.send(1001,100);s.e.outgoing(packet{key:s.key,rst:true},s.at)
    m:=assertState(t,s.e,100,0,100,true);if m["invalidated_unique_bytes"]!=0||m["rst_unacked_unique_bytes"]!=100{t.Fatal("RST silently delivered/lost bytes")}
    s.advance(time.Second);s.syn(1000);if s.e.Snapshot()["ambiguous_flow_generation"]!=1{t.Fatal("same-ISN reuse guessed")}
    s=sim(1000);s.send(1001,100);s.ack(1101);s.syn(1050);s.send(1051,100)
    if s.e.Snapshot()["ambiguous_flow_generation"]!=1{t.Fatal("overlapping epochs guessed")}
}
func TestOldViolationRuleReproducedWithoutGuessingRealPackets(t *testing.T){
    // The old checker ran this rule before its size==0 return. Five synthetic
    // empty control packets reproduce five violations; this is NOT a claim
    // about the five unrecorded packets in the real run.
    anchor:=uint32(0x60000000);high:=int64(1);old:=0;s:=sim(anchor)
    for _,seq:=range []uint32{0,1,100,200,300}{
        lo:=high+int64(int32(seq-(anchor+uint32(high))))
        if lo-high>=1<<30||high-lo>=1<<30{old++}
        s.e.outgoing(packet{key:s.key,seq:seq,ackFlag:true},s.at)
    }
    s.e.outgoing(packet{key:s.key,seq:0,rst:true},s.at)
    m:=assertState(t,s.e,0,0,0,true)
    if old!=5||m["ignored_control_sequences"]!=6{t.Fatal("old predicate reproduction")}
}
func TestSerialUnsupportedAndDeferredACKRetention(t *testing.T){
    s:=sim(1000);s.send(1000+(1<<31),1);if s.e.Snapshot()["unsupported_serial_range"]!=1{t.Fatal("half space guessed")}
    s=sim(1000);s.send(999,1);if s.e.Snapshot()["unsupported_serial_range"]!=1{t.Fatal("pre-SYN data guessed")}
    s=sim(1000);s.ack(1201);if s.e.Snapshot()["correlation_valid"]!=0{t.Fatal("unresolved ACK valid")};s.advance(RecordTTL);m:=s.e.Snapshot()
    if m["expired_observer_ack_events"]!=0||m["correlator_evictions"]!=0||m["correlation_valid"]!=0{t.Fatal("pending observation must remain invalid, retained")}
    s.sendAt(1001,200,s.e.flows[s.key][0].born.Add(time.Millisecond));assertState(t,s.e,200,200,0,true)
}
func TestTTLAndAllHardCaps(t *testing.T){
    s:=sim(1000);s.send(1001,100);s.advance(2*RecordTTL);m:=assertState(t,s.e,100,0,100,true)
    if m["correlator_evictions"]!=0||m["invalidated_unique_bytes"]!=0{t.Fatal("ledger wall-clock expiry")}
    for _,which:=range []string{"range","attempt","reference","ack","flow"}{t.Run(which,func(t *testing.T){
        s:=sim(1000)
        switch which{case "range":s.e.capRecords=0;case "attempt":s.e.capAttempts=0;case "reference":s.e.capReferences=0;case "ack":s.e.capACKs=0;case "flow":s.e.capFlows=1}
        if which=="flow"{s.key=flowID(2);s.syn(0)}else if which=="ack"{s.ack(1001)}else{s.send(1001,100)}
        m:=s.e.Snapshot();if m["diagnostic_capacity_drops"]!=1||m["correlation_valid"]!=0||m["counter_consistency"]!=1{t.Fatal("hard cap")}
    })}
    s=sim(1000);s.e.capTags=1;s.e.Inbound([]byte{1},s.at);s.e.Inbound([]byte{2},s.at);if len(s.e.tags)!=1||s.e.Snapshot()["diagnostic_capacity_drops"]!=1{t.Fatal("tag cap")}
    s=sim(1000);s.e.Inbound(make([]byte,MaxTagBytes+1),s.at);if len(s.e.tags)!=0||s.e.tagBytes!=0{t.Fatal("retained buffer cap")}
}

// Only integration tests below use synthetic packet headers, to verify parser
// and identity propagation at the existing unchanged production hooks.
func ip(seq,ack uint32,size int,flags byte,reverse bool)[]byte{
    p:=make([]byte,40+size);p[0]=0x45;p[9]=6;p[15]=1;p[19]=2;p[21]=3;p[23]=4
    if reverse{p[15],p[19]=p[19],p[15];p[21],p[23]=p[23],p[21]}
    binary.BigEndian.PutUint16(p[2:4],uint16(len(p)));binary.BigEndian.PutUint32(p[24:28],seq);binary.BigEndian.PutUint32(p[28:32],ack);p[32]=0x50;p[33]=flags;return p
}
func TestRealHookIdentityHTTPAndReturnTiming(t *testing.T){
    at:=time.Unix(1,0);e:=New(func()time.Time{return at});e.Outgoing(ip(1000,0,0,2,false))
    p:=ip(1001,0,100,16,false);e.Outgoing(p);cp:=append([]byte(nil),p...);e.Move(p,cp);e.Enqueue(cp)
    if e.Snapshot()["http_not_started_bytes"]!=100{t.Fatal("not started")}
    at=at.Add(time.Millisecond);o:=e.HTTPStart([][]byte{cp});if e.Snapshot()["http_inflight_bytes"]!=100{t.Fatal("inflight")}
    at=at.Add(time.Millisecond);e.HTTPDone(o,true,204,false);if e.Snapshot()["http_success_not_acked_bytes"]!=100{t.Fatal("success")}
    at=at.Add(100*time.Millisecond);a:=ip(0,1101,0,16,true);e.Inbound(a,at.Add(-time.Millisecond));stamp:=e.Incoming(a);at=at.Add(time.Microsecond);e.BeforeInject(stamp)
    m:=assertState(t,e,100,100,0,true);if m["ws_to_ack_decode_sum_ns"]!=uint64(time.Millisecond)||m["ack_decode_to_inject_sum_ns"]!=uint64(time.Microsecond)||e.tagBytes!=0{t.Fatal("timing or tag leak")}
}
func TestEveryRetransmissionGetsHTTPObservation(t *testing.T){
    at:=time.Unix(1,0);e:=New(func()time.Time{return at});e.Outgoing(ip(1000,0,0,2,false))
    for i:=0;i<3;i++{p:=ip(1001,0,100,16,false);e.Outgoing(p);o:=e.HTTPStart([][]byte{p});if len(o.owners)!=1{t.Fatal("pure retransmission lost HTTP owner")};e.HTTPDone(o,i!=0,204,false);at=at.Add(time.Millisecond)}
    a:=ip(0,1101,0,16,true);e.Inbound(a,at);e.Incoming(a);m:=assertState(t,e,100,100,0,true)
    if m["retransmission_attempts"]!=2||m["http_requests"]!=3||m["multi_attempt_acked_bytes"]!=100{t.Fatal("retransmit ownership")}
}
func TestHTTPFailureNoReplayAndACKBeforeDoReturns(t *testing.T){
    at:=time.Unix(1,0);e:=New(func()time.Time{return at});e.Outgoing(ip(1000,0,0,2,false));p:=ip(1001,0,100,16,false);e.Outgoing(p);o:=e.HTTPStart([][]byte{p})
    a:=ip(0,1101,0,16,true);at=at.Add(time.Millisecond);e.Inbound(a,at);e.Incoming(a);at=at.Add(time.Second);e.HTTPDone(o,false,429,false)
    m:=assertState(t,e,100,100,0,true);if m["http_requests"]!=1||m["http_failures"]!=1||m["ack_before_first_http_complete"]!=1{t.Fatal("HTTP completion or retry")}
}
func TestConcurrentHooksSnapshots(t *testing.T){
    e:=New(time.Now);var wg sync.WaitGroup
    for id:=1;id<=16;id++{wg.Add(1);go func(id int){defer wg.Done();p:=ip(0,0,0,2,false);p[15]=byte(id);e.Outgoing(p)
        for n:=0;n<40;n++{p=ip(uint32(n*100+1),0,100,16,false);p[15]=byte(id);e.Outgoing(p);e.Enqueue(p);o:=e.HTTPStart([][]byte{p});e.HTTPDone(o,true,204,false)
            a:=ip(0,uint32(n*100+101),0,16,true);a[19]=byte(id);e.Inbound(a,time.Now());e.BeforeInject(e.Incoming(a))}
    }(id)}
    for i:=0;i<40;i++{e.Snapshot()};wg.Wait();assertState(t,e,64000,64000,0,true)
}

func TestSeededThousandsOfFlows(t *testing.T){
    for _,seed:=range []int64{7,20260923}{t.Run("fixed_seed",func(t *testing.T){
        rng:=rand.New(rand.NewSource(seed));s:=sim(1000);s.e=New(func()time.Time{return s.at});var unique uint64
        for id:=uint32(2);id<=2049;id++{
            s.key=flowID(id);anchor:=rng.Uint32();s.syn(anchor)
            seq:=anchor+1;var ends []uint32
            for n:=0;n<4;n++{size:=100+rng.Intn(1400);s.send(seq,size);if n%2==0{s.send(seq+uint32(size/3),size-size/3)};seq+=uint32(size);ends=append(ends,seq);unique+=uint64(size)}
            // Delayed ACKs well above the old TTL, without changing virtual time
            // for other flows; event timestamps use one monotonic server clock.
            outAt:=s.at;s.ackAt(ends[1],outAt.Add(33*time.Second));s.ackAt(seq,outAt.Add(34*time.Second));s.ackAt(seq,outAt.Add(35*time.Second))
            s.e.outgoing(packet{key:s.key,seq:seq,fin:true},outAt.Add(35*time.Second));s.e.acknowledge(packet{key:s.key,ack:seq+1,ackFlag:true,fin:true},outAt.Add(36*time.Second))
            // A disjoint new sequence epoch on the same synthetic tuple.
            anchor2:=anchor+(1<<20);s.at=outAt.Add(37*time.Second);s.syn(anchor2);s.send(anchor2+1,50);unique+=50;s.ack(anchor2+51)
            // Keep the number of generations within the documented cap.
        }
        s.at=s.at.Add(40*time.Second)
        m:=assertState(t,s.e,unique,unique,0,true)
        t.Logf("seed=%d flows=2048 reused=2048 unique=%d valid=%d evictions=%d sequence_violations=%d conservation=%d",seed,unique,m["correlation_valid"],m["correlator_evictions"],m["sequence_constraint_violations"],m["counter_consistency"])
    })}
}
func TestReplayScale(t *testing.T){
    s:=sim(0xffff0000);seq:=uint32(0xffff0001);starts:=make([]uint32,1246);sizes:=make([]int,1246);rng:=rand.New(rand.NewSource(1838));var unique uint64
    for i:=range starts{sizes[i]=800+rng.Intn(200);starts[i]=seq;s.send(seq,sizes[i]);seq+=uint32(sizes[i]);unique+=uint64(sizes[i])}
    for i:=0;i<592;i++{j:=(i*17)%1246;s.send(starts[j],sizes[j])}
    s.advance(40*time.Second)
    for i:=0;i<2994;i++{j:=i*1246/2994;end:=starts[j]+uint32(sizes[j]);s.ackAt(end,s.at.Add(time.Duration(i)*time.Millisecond))}
    s.at=s.at.Add(4*time.Second);m:=assertState(t,s.e,unique,unique,0,true)
    if m["downlink_tcp_segments_created"]!=1838||m["retransmission_attempts"]!=592||m["ack_observations"]!=2994||m["oldest_unacked_age_max_ns"]<=uint64(32*time.Second){t.Fatal("replay scale drift")}
    t.Logf("segments=1838 retransmissions=592 ACKs=2994 unique=%d valid=%d evictions=%d sequence_violations=%d conservation=%d",unique,m["correlation_valid"],m["correlator_evictions"],m["sequence_constraint_violations"],m["counter_consistency"])
}
func TestMemoryLayoutAndBoundedFixture(t *testing.T){
    t.Logf("linux sizes: flow=%d range=%d attempt=%d ack_event=%d tag=%d reference=%d",unsafe.Sizeof(flow{}),unsafe.Sizeof(record{}),unsafe.Sizeof(attempt{}),unsafe.Sizeof(ackEvent{}),unsafe.Sizeof(tag{}),unsafe.Sizeof((*attempt)(nil)))
    runtime.GC();var before,after runtime.MemStats;runtime.ReadMemStats(&before)
    s:=sim(0);for i:=0;i<10000;i++{s.send(uint32(1+i*100),100)};runtime.GC();runtime.ReadMemStats(&after);runtime.KeepAlive(s)
    if after.HeapAlloc>before.HeapAlloc{t.Logf("10000 one-attempt ranges retained heap delta=%d",after.HeapAlloc-before.HeapAlloc)}
}
func TestHistogramAndPrivateSchema(t *testing.T){
    var h Histogram;for _,d:=range []time.Duration{0,50*time.Millisecond,100*time.Millisecond,250*time.Millisecond,500*time.Millisecond,time.Second,2*time.Second,5*time.Second,10*time.Second}{h.observe(d)};h.observe(-time.Nanosecond)
    if h.count!=9{t.Fatal("histogram")};for _,n:=range h.buckets{if n!=1{t.Fatal("histogram edges")}}
    s:=sim(1000);s.send(1001,100);s.send(1001,100);s.ack(1101);m:=s.e.Snapshot();data,_:=json.Marshal(m)
    for _,forbidden:=range []string{"flow_key","src","dst","port\"","payload\"","\"seq\"","\"ack\"","cookie","token","url","header","ambiguous_retransmit_bytes"}{if strings.Contains(string(data),forbidden){t.Fatalf("private or obsolete schema field: %s",forbidden)}}
}

// Independent tiny per-byte oracle; it does not reuse range insertion/matching.
func TestSeededByteLedgerOracle(t *testing.T){
    for seed:=int64(1);seed<=8;seed++{
        s:=sim(0xfffffff0);rng:=rand.New(rand.NewSource(seed));var seen,delivered [256]bool
        var first [256]time.Time;high:=0
        for i:=0;i<160;i++{
            if i%3!=2{
                lo:=rng.Intn(230);n:=1+rng.Intn(26);if lo+n>256{n=256-lo}
                s.send(s.anchor+1+uint32(lo),n);if lo+n>high{high=lo+n}
                for j:=lo;j<lo+n;j++{if !seen[j]{first[j]=s.at};seen[j]=true}
            }else{
                end:=rng.Intn(high+1);s.ack(s.anchor+1+uint32(end))
                for j:=0;j<end;j++{if seen[j]&&!first[j].After(s.at){delivered[j]=true}}
            }
            var unique,acked uint64;for j:=range seen{if seen[j]{unique++};if delivered[j]{acked++}}
            assertState(t,s.e,unique,acked,unique-acked,true)
        }
        s.ack(s.anchor+1+uint32(high));var unique uint64;for _,v:=range seen{if v{unique++}}
        assertState(t,s.e,unique,unique,0,true)
    }
}
func TestInvariantViolationLatchesInvalid(t *testing.T){
    s:=sim(1000);s.send(1001,100);s.ack(1101);f:=s.e.flows[s.key][0]
    f.ranges[0].ackCover=0;s.e.checkFlow(f)
    if s.e.Snapshot()["correlation_valid"]!=0{t.Fatal("invalid ACK provenance accepted")}
}
