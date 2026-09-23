package ackdiag

import (
    "encoding/json"
    "math/rand"
    "strings"
    "testing"
    "time"
    "unsafe"
)

func(s *simulation)reset(seq uint32,incoming bool){
    s.advance(time.Millisecond)
    p:=packet{key:s.key,seq:seq,rst:true}
    if incoming{p.ackFlag=true;p.ack=seq;s.e.acknowledge(p,s.at)}else{s.e.outgoing(p,s.at)}
}
func TestRSTLifecycleCases(t *testing.T){
    tests:=[]struct{name string;run func(*testing.T,*simulation)}{
        {"data_ack_rst",func(t *testing.T,s *simulation){s.send(1001,100);s.ack(1101);s.reset(1101,false);assertState(t,s.e,100,100,0,true)}},
        {"data_rst_before_ack",func(t *testing.T,s *simulation){s.send(1001,100);s.reset(1101,true);m:=assertState(t,s.e,100,0,100,true);if m["rst_unacked_unique_bytes"]!=100{t.Fatal("censoring lost")};s.ack(1101);m=assertState(t,s.e,100,100,0,true);if m["acked_after_rst_bytes"]!=100{t.Fatal("late ACK lost")}}},
        {"http_success_rst_late_ack",func(t *testing.T,s *simulation){s.send(1001,100);s.reset(1101,false);s.advance(150*time.Second);m:=assertState(t,s.e,100,0,100,true);if m["http_success_not_acked_bytes"]!=100{t.Fatal("HTTP became delivery")};s.ack(1101);m=assertState(t,s.e,100,100,0,true);if m["http_first_success_end_to_ack_bytes"]!=100{t.Fatal("attempt lost")}}},
        {"data_retransmission_rst",func(t *testing.T,s *simulation){s.send(1001,100);s.send(1051,50);s.reset(1101,false);assertState(t,s.e,100,0,100,true);s.ack(1101);assertState(t,s.e,100,100,0,true)}},
        {"partial_ack_rst",func(t *testing.T,s *simulation){s.send(1001,100);s.ack(1051);s.reset(1101,false);assertState(t,s.e,100,50,50,true);s.ack(1101);m:=assertState(t,s.e,100,100,0,true);if m["acked_after_rst_bytes"]!=50{t.Fatal("partial reset")}}},
        {"cumulative_ack_rst",func(t *testing.T,s *simulation){s.send(1001,100);s.send(1101,100);s.ack(1201);s.reset(1201,false);assertState(t,s.e,200,200,0,true)}},
        {"fin_then_rst",func(t *testing.T,s *simulation){s.send(1001,100);s.e.outgoing(packet{key:s.key,seq:1101,fin:true},s.at);s.reset(1102,false);s.ack(1102);assertState(t,s.e,100,100,0,true)}},
        {"rst_no_outstanding",func(t *testing.T,s *simulation){s.reset(0,false);s.reset(0,true);assertState(t,s.e,0,0,0,true)}},
        {"rst_unknown_flow",func(t *testing.T,s *simulation){s.key=flowID(99);s.reset(0,false);s.reset(0,true);m:=assertState(t,s.e,0,0,0,true);if m["rst_unknown_flow_events"]!=2{t.Fatal("unknown resets")}}},
        {"stale_rst_previous_generation",func(t *testing.T,s *simulation){s.send(1001,100);s.advance(time.Second);s.syn(9000);s.send(9001,100);s.reset(1101,false);m:=assertState(t,s.e,200,0,200,true);if m["rst_unacked_unique_bytes"]!=100{t.Fatal("stale reset hit latest")};s.ack(1101);s.ack(9101);assertState(t,s.e,200,200,0,true)}},
        {"new_syn_after_rst",func(t *testing.T,s *simulation){s.send(1001,100);s.reset(1101,true);s.syn(9000);s.send(9001,100);s.ack(9101);assertState(t,s.e,200,100,100,true)}},
        {"old_ack_after_new_syn",func(t *testing.T,s *simulation){s.send(1001,100);s.reset(1101,false);s.syn(9000);s.send(9001,100);s.ack(1101);assertState(t,s.e,200,100,100,true);s.ack(9101);assertState(t,s.e,200,200,0,true)}},
        {"rst_wraparound",func(t *testing.T,s *simulation){s=sim(0xfffffff0);s.send(0xfffffff1,64);s.reset(0x31,true);s.ack(0x31);assertState(t,s.e,64,64,0,true)}},
        {"many_short_reused_connections",func(t *testing.T,s *simulation){var n uint64;for i:=0;i<40;i++{if i>0{s.advance(time.Second);s.syn(uint32(1000+i*10000))};seq:=s.anchor+1;s.send(seq,100);s.reset(seq+100,i%2==0);s.ack(seq+100);n+=100;assertState(t,s.e,n,n,0,true)}}},
        {"rst_observer_reordering",func(t *testing.T,s *simulation){out:=s.at.Add(time.Millisecond);reset:=out.Add(time.Second);s.e.outgoing(packet{key:s.key,seq:1101,rst:true},reset);s.sendAt(1001,100,out);s.ackAt(1101,reset.Add(time.Second));s.at=reset.Add(2*time.Second);assertState(t,s.e,100,100,0,true)}},
        {"rst_not_delivery_even_with_ack_bit",func(t *testing.T,s *simulation){s.send(1001,100);s.reset(1101,true);assertState(t,s.e,100,0,100,true)}},
        {"reordered_old_data_after_reuse",func(t *testing.T,s *simulation){s.send(1001,100);old:=s.at;s.reset(1101,false);s.syn(9000);s.send(9001,100);s.sendAt(1001,100,old);s.ack(1101);s.ack(9101);assertState(t,s.e,200,200,0,true)}},
    }
    for _,tc:=range tests{t.Run(tc.name,func(t *testing.T){tc.run(t,sim(1000))})}
}

func TestGenerationAmbiguityReasons(t *testing.T){
    cases:=[]struct{reason string;run func(*simulation)}{
        {"same_isn_syn",func(s *simulation){s.send(1001,100);s.reset(1101,false);s.syn(1000)}},
        {"before_syn",func(s *simulation){s.sendAt(1001,100,s.at.Add(-time.Second))}},
        {"data_overlap",func(s *simulation){s.send(1001,100);s.syn(1050);s.send(1051,100)}},
        {"ack_overlap",func(s *simulation){s.send(1001,100);s.syn(1050);s.ack(1051)}},
        {"ack_no_owner",func(s *simulation){s.send(1001,100);s.syn(9000);s.ack(9900)}},
        {"rst_no_owner",func(s *simulation){s.send(1001,100);s.syn(9000);s.reset(0,false)}},
        {"rst_multiple_owners",func(s *simulation){s.send(1001,100);s.syn(1050);s.reset(1051,true)}},
        {"fin_without_ack",func(s *simulation){s.send(1001,100);s.syn(9000);s.e.acknowledge(packet{key:s.key,fin:true},s.at)}},
    }
    for _,tc:=range cases{t.Run(tc.reason,func(t *testing.T){s:=sim(1000);tc.run(s);m:=s.e.Snapshot()
        if m["ambiguous_flow_generation"]!=1||m["generation_ambiguity_"+tc.reason]!=1||m["correlation_valid"]!=0||m["counter_consistency"]!=1||m["generation_evidence_count"]!=1{t.Fatalf("specific ambiguity not preserved: %v",m)}
    })}
}

func TestRSTTimeSelectsOnlyExistingGeneration(t *testing.T){
    s:=sim(1000);s.send(1001,100);old:=s.at;s.advance(time.Second);s.syn(9000);s.send(9001,100)
    s.e.outgoing(packet{key:s.key,rst:true},old) // Opaque RST is unique BEFORE second SYN.
    m:=assertState(t,s.e,200,0,200,true);if m["rst_unacked_unique_bytes"]!=100{t.Fatal("timestamp scope")}
    s.ackAt(1101,old.Add(4*time.Millisecond));s.ack(9101);assertState(t,s.e,200,200,0,true)
}

func TestCrossEpochPartialOverlapBeforeAnchor(t *testing.T){
    for _,base:=range []uint32{1000,0xfffffff0}{
        s:=sim(base+50);s.send(base+51,100);s.syn(base);s.send(base+1,200)
        m:=s.e.Snapshot();if m["generation_ambiguity_data_overlap"]!=1||m["correlation_valid"]!=0||m["counter_consistency"]!=1{t.Fatal("interval start-only overlap check")}
    }
}

func TestProvenanceOverflowIsExplicit(t *testing.T){
    s:=sim(1000);s.send(1001,100)
    for i:=0;i<MaxProvenance+1;i++{s.syn(1000)}
    m:=s.e.Snapshot();if m["generation_evidence_count"]!=MaxProvenance||m["provenance_capacity_drops"]!=1||m["correlation_valid"]!=0{t.Fatal("silent evidence loss")}
}

func TestInvalidationReasonsAndSafeProvenance(t *testing.T){
    for reason:=lossTTL;reason<=lossOther;reason++{s:=sim(1000);s.send(1001,100);s.send(1001,100);s.ack(1051);s.advance(time.Second)
        f:=s.e.flows[s.key][0];s.e.invalidateRange(f,&f.ranges[1],reason,eventCAP,s.at);s.e.checkFlow(f)
        s.ack(1101);m:=assertState(t,s.e,100,50,0,false)
        if m["invalidated_"+lossNames[reason]+"_bytes"]!=50||m["invalidation_reason_consistency"]!=1||m["unexplained_invalidated_bytes"]!=0||m["correlation_valid"]!=0{t.Fatal("loss accounting")}
        if m["loss_0_length"]!=50||m["loss_0_relative_lo"]!=51||m["loss_0_relative_hi"]!=101||m["loss_0_http_existed"]!=1||m["loss_0_http_succeeded"]!=1||m["loss_0_retransmitted"]!=1||m["loss_0_prior_ack_observed"]!=1||m["loss_0_late_covering_ack_observed"]!=1{t.Fatal("loss provenance")}
        b,_:=json.Marshal(m);for _,bad:=range []string{"src","dst","cookie","token","url","payload\"","anchor\"","raw_seq","raw_ack"}{if strings.Contains(string(b),bad){t.Fatalf("sensitive provenance: %s",bad)}}
    }
}

func TestNoLedgerTTLAndExplicitTagLoss(t *testing.T){
    s:=sim(1000);s.send(1001,100);s.reset(1101,false);s.advance(10*time.Minute);assertState(t,s.e,100,0,100,true)
    s.ack(1101);assertState(t,s.e,100,100,0,true)
    // A lost buffer identity IS observer loss, unlike ordinary TCP close.
    s=sim(1000);a:=s.send(1001,100);b:=[]byte{1};s.e.putTag(b,tag{owner:a,created:s.at});s.advance(RecordTTL)
    m:=assertState(t,s.e,100,0,0,false)
    if m["invalidated_ttl_bytes"]!=100||m["expired_identity_tags"]!=1||m["correlator_evictions"]!=1{t.Fatal("tag loss not explicit")}
}

func TestInvalidatedRangeSplitKeepsPerRangeLateACKEvidence(t *testing.T){
    s:=sim(1000);s.send(1001,100);f:=s.e.flows[s.key][0]
    s.e.invalidateRange(f,&f.ranges[0],lossOther,eventCAP,s.at);s.e.checkFlow(f)
    s.send(1051,25);s.ack(1051)
    m:=assertState(t,s.e,100,0,0,false)
    if m["invalidation_records"]!=3||m["loss_0_late_covering_ack_observed"]!=1||m["loss_1_late_covering_ack_observed"]!=0||m["loss_2_late_covering_ack_observed"]!=0{t.Fatal("shared invalidation evidence across split atoms")}
}

func TestRSTChurnStress(t *testing.T){
    for _,seed:=range []int64{178,4309}{
        rng:=rand.New(rand.NewSource(seed));s:=sim(0);s.e=New(func()time.Time{return s.at});var total uint64
        // 1024 tuples x 3 generations; bounded by 4096 generations. All have
        // overlap, delayed/partial ACKs and reset before delivery observation.
        for id:=uint32(1);id<=1024;id++{s.key=flowID(id);base:=rng.Uint32()
            for gen:=uint32(0);gen<3;gen++{anchor:=base+gen*(1<<20);s.syn(anchor);n:=200+rng.Intn(800);out:=s.at.Add(time.Millisecond)
                s.sendAt(anchor+1,n,out);s.sendAt(anchor+uint32(n/2)+1,n-n/2,out.Add(time.Millisecond));total+=uint64(n)
                s.at=out.Add(2*time.Millisecond);s.reset(anchor+uint32(n)+1,gen%2==0)
                // ACK callback order differs from timestamp order.
                late:=s.at.Add(150*time.Second);s.ackAt(anchor+uint32(n)+1,late);s.ackAt(anchor+uint32(n/2)+1,late.Add(-time.Second));s.at=late.Add(time.Second)
                // Stale reset after a reuse is covered separately; this reset
                // must select its generation among retained disjoint epochs.
                s.reset(anchor+uint32(n)+1,false)
            }
        }
        m:=assertState(t,s.e,total,total,0,true)
        if m["diagnostic_capacity_drops"]!=0||m["unexplained_invalidated_bytes"]!=0||m["flow_reset_events"]!=6144{t.Fatal("RST stress")}
        t.Logf("seed=%d tuples=1024 generations=3072 resets=6144 unique=%d valid=%d conservation=%d evictions=%d",seed,total,m["correlation_valid"],m["counter_consistency"],m["correlator_evictions"])
    }
}

func TestLatestReplayScaleRST(t *testing.T){
    s:=sim(0);s.e=New(func()time.Time{return s.at})
    const count=1865
    type seg struct{key flowKey;seq uint32;n int;at time.Time}
    segments:=make([]seg,0,count);var unique uint64
    for i:=0;i<count;i++{id:=uint32(i%178+1);s.key=flowID(id);anchor:=uint32(0xffff0000)+id*100000
        if i<178{s.syn(anchor)}
        seq:=anchor+1+uint32(i/178)*1000;n:=1000;if i==count-1{n=983}
        s.send(seq,n);segments=append(segments,seg{s.key,seq,n,s.at});unique+=uint64(n)
    }
    // 592076 unique bytes retransmitted across exactly 866 attempts.
    for i:=0;i<866;i++{j:=i%593;p:=segments[j];n:=1000;if j==592{n=76};s.key=p.key;s.send(p.seq,n)}
    for id:=uint32(1);id<=178;id++{s.key=flowID(id);s.reset(0,false)}
    s.advance(130*time.Second);assertState(t,s.e,unique,0,unique,true)
    for i:=0;i<4309;i++{p:=segments[i*count/4309];s.key=p.key;s.ackAt(p.seq+uint32(p.n),s.at.Add(time.Duration(i)*time.Millisecond))}
    s.at=s.at.Add(5*time.Second);m:=assertState(t,s.e,unique,unique,0,true)
    if unique!=1864983||m["retransmitted_unique_bytes"]!=592076||m["retransmission_attempts"]!=866||m["ack_observations"]!=4309||m["flow_reset_events"]!=178||m["oldest_unacked_age_max_ns"]<=uint64(122*time.Second){t.Fatal("latest replay magnitude")}
    t.Logf("unique=%d retransmitted_unique=%d attempts=%d ACKs=%d RST=%d valid=%d evictions=%d sequence_violations=%d conservation=%d",unique,m["retransmitted_unique_bytes"],m["retransmission_attempts"],m["ack_observations"],m["flow_reset_events"],m["correlation_valid"],m["correlator_evictions"],m["sequence_constraint_violations"],m["counter_consistency"])
}

func TestLifecycleMemoryBudget(t *testing.T){
    payload:=uint64(MaxFlows)*uint64(unsafe.Sizeof(flow{}))+uint64(MaxRecords)*uint64(unsafe.Sizeof(record{})+unsafe.Sizeof(lossEvidence{}))+uint64(MaxAttempts)*uint64(unsafe.Sizeof(attempt{}))+uint64(MaxACKs)*uint64(unsafe.Sizeof(ackEvent{}))+uint64(MaxReferences)*8+uint64(MaxProvenance)*uint64(unsafe.Sizeof(generationEvidence{}))
    t.Logf("flow=%d range=%d attempt=%d loss=%d generation_evidence=%d bounded_struct_payload=%d",unsafe.Sizeof(flow{}),unsafe.Sizeof(record{}),unsafe.Sizeof(attempt{}),unsafe.Sizeof(lossEvidence{}),unsafe.Sizeof(generationEvidence{}),payload)
    if payload>32<<20{t.Fatal("observer structure budget drift")}
}
