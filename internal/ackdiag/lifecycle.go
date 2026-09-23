package ackdiag

import (
    "strconv"
    "time"
)

// Schema 3. These enums are closed; none is derived from an error or a packet.
type lossReason uint64
const (
    lossTTL lossReason = iota + 1
    lossRST
    lossEpoch
    lossAmbiguous
    lossCapacity
    lossOther
)
var lossNames = [...]string{"", "ttl", "rst", "epoch_replacement", "ambiguous_generation", "capacity", "other"}
type eventKind uint64
const (
    eventDATA eventKind = iota + 1
    eventACK
    eventSYN
    eventFIN
    eventRST
    eventTTL
    eventCAP
)
const MaxProvenance = 256
// Check BOTH endpoints of an arc. A DATA start before another generation's
// SYN can still overlap that generation. Signed modular distance is safe here
// within the documented half-space and IPv4 payload bound; no seq is exported.
func intersectsGeneration(seq uint32, size int, g *flow)bool{
    lo:=int64(int32(seq-g.anchor))
    return lo<g.high&&lo+int64(size)>1
}
var ambiguityNames = [...]string{
    "same_isn_syn", "before_syn", "data_overlap", "ack_overlap",
    "ack_no_owner", "rst_no_owner", "rst_multiple_owners", "fin_without_ack",
}
type lossEvidence struct {
    reason lossReason
    kind eventKind
    at time.Time
    age time.Duration
    httpExisted, httpSuccess, retransmitted, ackBefore, lateACK bool
    rstBefore, finObserved, epochBefore bool
}
type generationEvidence struct {
    reason uint64
    kind eventKind
    firstID, secondID, candidates uint64
    at time.Time
}

// A reset is a control observation, not evidence of delivery or non-delivery.
// Its ACK field is used ONLY as generation evidence, never as a delivery ACK.
// A single observed generation can retain an unmatched reset (e.g. SEQ=0).
// With reuse, require one sequence-compatible generation; never guess latest.
func(e *Engine) observeReset(p packet, at time.Time, incoming bool) {
    e.metrics["flow_reset_events"]++
    gs:=e.flows[p.key]
    eligible:=make([]*flow,0,len(gs))
    for _,g:=range gs{if !at.Before(g.born){eligible=append(eligible,g)}}
    if len(gs)==0{e.metrics["rst_unknown_flow_events"]++;return}
    if len(eligible)==0{e.ambiguous("before_syn",eventRST,at,gs);return}
    var f *flow
    if len(eligible)==1{f=eligible[0]}else{
        for _,g:=range eligible{
            value:=p.seq
            if incoming{if !p.ackFlag{continue};value=p.ack}
            x,ok:=offset(value,g.anchor)
            if !ok||x>g.high{continue}
            if f!=nil{e.ambiguous("rst_multiple_owners",eventRST,at,[]*flow{f,g});return}
            f=g
        }
        if f==nil{e.ambiguous("rst_no_owner",eventRST,at,eligible);return}
    }
    if f.resetAt.IsZero()||at.Before(f.resetAt){f.resetAt=at}
    f.closed=true
    e.metrics["rst_attributed_events"]++
    e.checkFlow(f)
}

func(e *Engine) ambiguous(reason string, kind eventKind, at time.Time, candidates []*flow) {
    var code uint64
    for i,n:=range ambiguityNames{if n==reason{code=uint64(i+1);break}}
    if code==0{e.invalid("invariant_failures");return}
    e.invalid("ambiguous_flow_generation")
    e.metrics["generation_ambiguity_"+reason]++
    ev:=generationEvidence{reason:code,kind:kind,at:at,candidates:uint64(len(candidates))}
    if len(candidates)>0{ev.firstID=candidates[0].id}
    if len(candidates)>1{ev.secondID=candidates[1].id}
    if len(e.provenance)>=MaxProvenance{e.metrics["provenance_capacity_drops"]++}else{e.provenance=append(e.provenance,ev)}
    // Do not erase deterministically known ranges just because ownership of
    // another observation is unknown. The reason counter latches run invalid.
}

// Forced observation loss is irreversible. A late ACK is recorded as evidence
// but cannot erase invalidity or silently turn invalidated bytes into delivered.
func(e *Engine) invalidateRange(f *flow, r *record, reason lossReason, kind eventKind, at time.Time) {
    if r.invalid||!r.ackAt.IsZero(){return}
    if reason<lossTTL||reason>lossOther{e.invalid("invariant_failures");reason=lossOther}
    x:=&lossEvidence{reason:reason,kind:kind,at:at,age:at.Sub(firstOut(*r)),retransmitted:len(r.attempts)>1,ackBefore:f.ackSeen}
    x.rstBefore=!f.resetAt.IsZero()&&!f.resetAt.After(at)
    x.epochBefore=!f.replacedAt.IsZero()&&!f.replacedAt.After(at)
    x.finObserved=f.peerFIN||f.finEnd>0
    for _,a:=range r.attempts{
        x.httpExisted=x.httpExisted||(!a.start.IsZero()&&!a.start.After(at))
        x.httpSuccess=x.httpSuccess||(!a.end.IsZero()&&!a.end.After(at)&&a.success)
    }
    r.invalid=true;r.loss=x
    if reason==lossTTL{e.metrics["evicted_unique_bytes"]+=uint64(r.hi-r.lo)}
}
func(e *Engine) invalidateAttempt(a *attempt, reason lossReason, kind eventKind, at time.Time) {
    for _,gs:=range e.flows{for _,f:=range gs{
        for i:=range f.ranges{r:=&f.ranges[i];for _,owner:=range r.attempts{if owner==a{e.invalidateRange(f,r,reason,kind,at);break}}}
        e.checkFlow(f)
    }}
}
func bit(v bool)uint64{if v{return 1};return 0}
func elapsed(a,b time.Time)uint64{if a.IsZero()||b.IsZero()||a.Before(b){return 0};return uint64(a.Sub(b))}

func(e *Engine) lifecycleSnapshot(m map[string]uint64) {
    m["schema"]=3
    m["range_ttl_ns"]=0;m["generation_ttl_ns"]=0;m["attempt_ttl_ns"]=0;m["ack_history_ttl_ns"]=0
    m["identity_tag_ttl_ns"]=uint64(e.ttl)
    for _,n:=range lossNames[1:]{m["invalidated_"+n+"_bytes"]=0}
    for _,n:=range ambiguityNames{m["generation_ambiguity_"+n]=e.metrics["generation_ambiguity_"+n]}
    for _,n:=range []string{"rst_unknown_flow_events","rst_attributed_events","expired_identity_tags","provenance_capacity_drops"}{m[n]=e.metrics[n]}
    m["rst_unacked_unique_bytes"]=0;m["acked_after_rst_bytes"]=0;m["replaced_unacked_unique_bytes"]=0
    m["delivery_observation_complete"]=bit(m["outstanding_unique_bytes_current"]==0&&m["invalidated_unique_bytes"]==0)
    m["invalidation_records"]=0
    // Every invalid atom is retained and serialized with relative coordinates,
    // so no sampling/overwriting can hide a forced byte loss. Bound: MaxRecords.
    for _,gs:=range e.flows{for _,f:=range gs{for _,r:=range f.ranges{
        n:=uint64(r.hi-r.lo)
        if !r.invalid{
            if !f.resetAt.IsZero(){if r.ackAt.IsZero(){m["rst_unacked_unique_bytes"]+=n}else if !r.ackAt.Before(f.resetAt){m["acked_after_rst_bytes"]+=n}}
            if !f.replacedAt.IsZero()&&r.ackAt.IsZero(){m["replaced_unacked_unique_bytes"]+=n}
            continue
        }
        if r.loss==nil{m["invalidated_other_bytes"]+=n;e.invalid("invariant_failures");continue}
        x:=r.loss;m["invalidated_"+lossNames[x.reason]+"_bytes"]+=n
        prefix:="loss_"+strconv.FormatUint(m["invalidation_records"],10)+"_";m["invalidation_records"]++
        m[prefix+"generation"]=f.id;m[prefix+"reason"]=uint64(x.reason);m[prefix+"event"]=uint64(x.kind)
        m[prefix+"length"]=n;m[prefix+"relative_lo"]=uint64(r.lo);m[prefix+"relative_hi"]=uint64(r.hi)
        m[prefix+"age_ns"]=uint64(max(x.age,0));m[prefix+"since_syn_ns"]=elapsed(x.at,f.born)
        m[prefix+"http_existed"]=bit(x.httpExisted);m[prefix+"http_succeeded"]=bit(x.httpSuccess)
        m[prefix+"retransmitted"]=bit(x.retransmitted);m[prefix+"prior_ack_observed"]=bit(x.ackBefore);m[prefix+"late_covering_ack_observed"]=bit(x.lateACK)
        m[prefix+"rst_before_loss"]=bit(x.rstBefore);m[prefix+"fin_observed_at_loss"]=bit(x.finObserved);m[prefix+"epoch_before_loss"]=bit(x.epochBefore)
    }}}
    var sum uint64;for _,n:=range lossNames[1:]{sum+=m["invalidated_"+n+"_bytes"]}
    m["invalidation_reason_consistency"]=bit(sum==m["invalidated_unique_bytes"])
    m["unexplained_invalidated_bytes"]=0
    if sum!=m["invalidated_unique_bytes"]{m["unexplained_invalidated_bytes"]=m["invalidated_unique_bytes"];e.invalid("invariant_failures")}
    m["generation_evidence_count"]=uint64(len(e.provenance));m["generation_evidence_cap"]=MaxProvenance
    for i,ev:=range e.provenance{
        p:="generation_event_"+strconv.Itoa(i)+"_"
        m[p+"reason"]=ev.reason;m[p+"event"]=uint64(ev.kind);m[p+"first_id"]=ev.firstID;m[p+"second_id"]=ev.secondID;m[p+"candidates"]=ev.candidates
        // Relative ordering only, no packet seq/ack/address or wall-clock value.
        for _,gs:=range e.flows{for _,f:=range gs{if f.id==ev.firstID{m[p+"since_first_syn_ns"]=elapsed(ev.at,f.born);m[p+"before_first_syn"]=bit(ev.at.Before(f.born))}}}
    }
    m["invariant_failures"]=e.metrics["invariant_failures"]
}
