package ackdiag

import (
    "encoding/json"
    "os"
    "path/filepath"
    "testing"
    "time"
)

// Linux CI exports genuine frozen-Engine snapshots, never copied hand-written
// schema fixtures. No production code, hook, or logger is changed.
func TestExportSchema3Fixtures(t *testing.T) {
    dir := os.Getenv("ACK_SCHEMA_FIXTURE_DIR")
    if dir == "" { t.Skip("fixture export only in Linux validator CI") }
    if err := os.MkdirAll(dir, 0700); err != nil { t.Fatal(err) }
    save := func(name string, s *simulation) {
        t.Helper()
        s.e.metrics["ws_connected"] = 1
        m := s.e.Snapshot()
        b, err := json.Marshal(m)
        if err != nil { t.Fatal(err) }
        if err = os.WriteFile(filepath.Join(dir, name+".json"), b, 0600); err != nil { t.Fatal(err) }
    }
    s := sim(1000); save("idle", s)
    s.reset(0, false); save("idle_rst", s)
    s = sim(1000); s.send(1001, 100); save("outstanding", s)
    s.ack(1051); save("partial_ack", s)
    s.ack(1101); save("normal_ack", s)
    s = sim(1000); s.send(1001,100); s.send(1051,50); save("retransmission",s)
    s.reset(1101,false); save("rst_outstanding",s)
    s.advance(150*time.Second); s.ack(1101); save("rst_late_ack",s)
    s = sim(1000); s.send(1001,100); s.reset(1101,false); s.syn(9000); s.send(9001,100)
    save("replacement_outstanding",s); s.ack(1101); s.ack(9101); save("reuse_resolved",s)
    for reason := lossTTL; reason <= lossOther; reason++ {
        s = sim(1000); s.send(1001,100); s.send(1001,100); s.ack(1051)
        s.e.outgoing(packet{key:s.key,seq:1101,fin:true},s.at)
        s.reset(1102,false); s.syn(9000); s.advance(time.Second)
        f := s.e.flows[s.key][0]
        s.e.invalidateRange(f,&f.ranges[1],reason,eventCAP,s.at); s.e.checkFlow(f)
        s.ack(1101); save("loss_"+lossNames[reason],s)
    }
    cases := []struct{name string; run func(*simulation)}{
        {"same_isn_syn",func(s *simulation){s.send(1001,100);s.reset(1101,false);s.syn(1000)}},
        {"before_syn",func(s *simulation){s.sendAt(1001,100,s.at.Add(-time.Second))}},
        {"data_overlap",func(s *simulation){s.send(1001,100);s.syn(1050);s.send(1051,100)}},
        {"ack_overlap",func(s *simulation){s.send(1001,100);s.syn(1050);s.ack(1051)}},
        {"ack_no_owner",func(s *simulation){s.send(1001,100);s.syn(9000);s.ack(9900)}},
        {"rst_no_owner",func(s *simulation){s.send(1001,100);s.syn(9000);s.reset(0,false)}},
        {"rst_multiple_owners",func(s *simulation){s.send(1001,100);s.syn(1050);s.reset(1051,true)}},
        {"fin_without_ack",func(s *simulation){s.send(1001,100);s.syn(9000);s.e.acknowledge(packet{key:s.key,fin:true},s.at)}},
    }
    for _, c := range cases { s=sim(1000);c.run(s);save("ambiguity_"+c.name,s) }
    s=sim(1000);s.send(1001,100)
    for i:=0;i<MaxProvenance+1;i++ {s.syn(1000)}
    save("explicit_provenance_overflow",s)
    // Long, structurally valid (correlation-invalid) provenance >16 KiB.
    s=sim(1000)
    for i:=0;i<200;i++ {s.send(uint32(1001+i*100),100)}
    f:=s.e.flows[s.key][0]
    for i:=range f.ranges {s.e.invalidateRange(f,&f.ranges[i],lossOther,eventCAP,s.at)}
    s.e.checkFlow(f);save("long_loss_provenance",s)
    // Real transport hook accounting, including duration histogram.
    s=sim(1000); s.e=New(func()time.Time{return s.at})
    s.e.Outgoing(ip(1000,0,0,2,false));p:=ip(1001,0,100,16,false)
    s.e.Outgoing(p);s.e.Enqueue(p);o:=s.e.HTTPStart([][]byte{p})
    s.advance(time.Millisecond);s.e.HTTPDone(o,true,204,false)
    a:=ip(0,1101,0,16,true);s.advance(100*time.Millisecond)
    s.e.Inbound(a,s.at);stamp:=s.e.Incoming(a);s.e.BeforeInject(stamp)
    save("http_hook_accounting",s)
    t.Log("Exported schema 3 fixtures from unchanged Engine/runtime data contract")
}
