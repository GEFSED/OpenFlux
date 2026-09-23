// Package ackdiag observes TCP metadata only. Private flow keys and sequence
// numbers must never be serialized. It cannot change packet or HTTP behavior.
package ackdiag

import (
    "encoding/binary"
    "sort"
    "sync"
    "time"

    "gvisor.dev/gvisor/pkg/tcpip/seqnum"
)

const (
    MaxRecords = 32768
    MaxFlows = 4096 // Includes retained connection generations.
    MaxAttempts = 65536
    MaxReferences = 262144
    MaxACKs = 65536
    MaxTags = 4096
    MaxTagBytes = 16 << 20
    RecordTTL = 120 * time.Second
)

type flowKey [12]byte // Protocol is always IPv4/TCP.
type interval struct{ lo, hi int64 }
type packet struct {
    key flowKey
    seq, ack uint32
    size int
    syn, fin, rst, ackFlag bool
}

func parse(data []byte, reverse bool) (packet, bool) {
    var p packet
    if len(data) < 40 || data[0]>>4 != 4 || data[9] != 6 { return p, false }
    ihl := int(data[0]&15)*4
    total := int(binary.BigEndian.Uint16(data[2:4]))
    if ihl < 20 || total > len(data) || total < ihl+20 || binary.BigEndian.Uint16(data[6:8])&0x3fff != 0 { return p, false }
    tcp := data[ihl:total]
    offset := int(tcp[12]>>4)*4
    if offset < 20 || offset > len(tcp) { return p, false }
    if reverse {
        copy(p.key[0:4],data[16:20]); copy(p.key[4:8],data[12:16])
        copy(p.key[8:10],tcp[2:4]); copy(p.key[10:12],tcp[0:2])
    } else {
        copy(p.key[0:4],data[12:16]); copy(p.key[4:8],data[16:20])
        copy(p.key[8:10],tcp[0:2]); copy(p.key[10:12],tcp[2:4])
    }
    p.seq=binary.BigEndian.Uint32(tcp[4:8]); p.ack=binary.BigEndian.Uint32(tcp[8:12])
    p.size=len(tcp)-offset; p.syn=tcp[13]&2!=0; p.fin=tcp[13]&1!=0
    p.rst=tcp[13]&4!=0; p.ackFlag=tcp[13]&16!=0
    return p,true
}

// A diagnostic connection epoch covers less than half the TCP sequence space
// starting at its observed SYN. gVisor's serial comparison handles a numeric
// wrap at 2^32. Exactly half a space, pre-SYN data and epochs >=2^31 are explicitly
// unsupported, never guessed. At 10 Mbps, half a space takes about 29 minutes.
func offset(seq, anchor uint32) (int64, bool) {
    v, a := seqnum.Value(seq), seqnum.Value(anchor)
    d := uint32(a.Size(v))
    if d == 1<<31 || v.LessThan(a) { return 0, false }
    return int64(d), true
}

type attempt struct {
    interval
    out, enqueue, start, end time.Time
    success bool
}
type record struct {
    interval
    attempts []*attempt // Each attempt covers this complete atomic intersection.
    ackAt time.Time
    ackCover int64
    invalid bool
    loss *lossEvidence
}
type ackEvent struct { end int64; at time.Time; expired bool }
type flow struct {
    id uint64 // Synthetic generation ID; never derived from a tuple or ISN.
    anchor uint32
    born time.Time
    resetAt, replacedAt time.Time
    high, ackHigh, finEnd int64
    ackSeen, peerFIN, closed bool
    ranges []record // Sorted, disjoint; delivered history is retained until stop.
    attempts []*attempt
    acks []ackEvent // Strict ACK advancements, including deferred observations.
    outstanding, outstandingRanges, created uint64
}
type tag struct { owner *attempt; ws, created time.Time; retained int }
type Histogram struct { count, sum, max, bytes uint64; buckets [9]uint64 }
func (h *Histogram) observe(d time.Duration) { h.observeBytes(d,0) }
func (h *Histogram) observeBytes(d time.Duration,n uint64) {
    if d<0{return};v:=uint64(d);h.count++;h.sum+=v;h.bytes+=n;if v>h.max{h.max=v}
    limits:=[...]time.Duration{50*time.Millisecond,100*time.Millisecond,250*time.Millisecond,500*time.Millisecond,time.Second,2*time.Second,5*time.Second,10*time.Second}
    i:=0;for i<len(limits)&&d>=limits[i]{i++};h.buckets[i]++
}
func (h *Histogram) snapshot(m map[string]uint64,prefix string) {
    m[prefix+"_count"]=h.count;m[prefix+"_sum_ns"]=h.sum;m[prefix+"_max_ns"]=h.max;m[prefix+"_bytes"]=h.bytes
    names:=[...]string{"lt_50ms","50_100ms","100_250ms","250_500ms","500ms_1s","1s_2s","2_5s","5_10s","ge_10s"}
    for i,n:=range names{m[prefix+"_"+n]=h.buckets[i]}
}

type Engine struct {
    mu sync.Mutex
    now func() time.Time
    flows map[flowKey][]*flow // Generations, not just the latest tuple occupant.
    tags map[*byte]tag
    metrics map[string]uint64
    records, attempts, references, generations, acks, tagBytes int
    capRecords, capAttempts, capReferences, capFlows, capTags, capACKs int
    ttl time.Duration
    outstanding, outstandingRanges uint64
    wsDecode,decodeInject,httpDuration,enqueueDelay,httpQueueDelay Histogram
    provenance []generationEvidence
}
var metricNames=[]string{
    "downlink_tcp_segments_created","unique_downlink_tcp_bytes","retransmitted_segments","retransmitted_bytes","retransmission_attempts",
    "duplicate_ack_events","old_ack_events","ack_advancement_count","ack_observations",
    "correlator_evictions","evicted_unique_bytes","diagnostic_capacity_drops","sequence_constraint_violations",
    "unsupported_serial_range","expired_observer_ack_events","ambiguous_flow_generation","unanchored_flows","unresolved_ack_events",
    "flow_epoch_resets","flow_reset_events","flow_close_events","ignored_control_sequences","invariant_failures",
    "ws_timestamp_missing","http_requests","http_successes","http_failures","http_status_429","http_payload_bytes_attempted",
    "http_payload_bytes_success","http_payload_bytes_failed","http_inflight_requests","http_inflight_peak",
    "http_body_read_errors_ignored","ws_connected","ws_messages","ws_reconnects","ws_connect_failures",
    "outstanding_segments_peak","outstanding_unique_bytes_peak","oldest_unacked_age_max_ns",
}
func New(now func()time.Time)*Engine {
    e:=&Engine{now:now,flows:make(map[flowKey][]*flow),tags:make(map[*byte]tag),metrics:make(map[string]uint64),
        capRecords:MaxRecords,capAttempts:MaxAttempts,capReferences:MaxReferences,capFlows:MaxFlows,capTags:MaxTags,capACKs:MaxACKs,ttl:RecordTTL}
    for _,n:=range metricNames{e.metrics[n]=0};return e
}
func(e *Engine) peak(name string,v uint64){if v>e.metrics[name]{e.metrics[name]=v}}
func(e *Engine) invalid(name string){e.metrics[name]++;if name=="unsupported_serial_range"{e.metrics["sequence_constraint_violations"]++}}
func(e *Engine) room(records,attempts,refs,acks int)bool {
    if e.records+records>e.capRecords||e.attempts+attempts>e.capAttempts||e.references+refs>e.capReferences||e.acks+acks>e.capACKs{
        e.invalid("diagnostic_capacity_drops");return false
    };return true
}
func firstOut(r record)time.Time {
    t:=r.attempts[0].out;for _,a:=range r.attempts[1:]{if a.out.Before(t){t=a.out}};return t
}
// outgoing and acknowledge are also the event-level test interface. Callers hold
// mu. Event times are observation times, allowing deterministic reordered hooks.
func(e *Engine) outgoing(p packet,at time.Time)*attempt {
    gs:=e.flows[p.key]
    var f *flow;if len(gs)>0{f=gs[len(gs)-1]}
    // RST and pure ACK SEQ fields do not describe new downlink data. In particular
    // a legal RST may have SEQ=0, far from the SYN. Never unwrap that as data.
    if p.rst {
        e.metrics["ignored_control_sequences"]++
        e.observeReset(p,at,false)
        return nil
    }
    if p.size==0&&!p.syn&&!p.fin{e.metrics["ignored_control_sequences"]++;return nil}
    if p.syn {
        if f!=nil&&at.Before(f.born){e.ambiguous("before_syn",eventSYN,at,gs);return nil}
        if f!=nil&&f.anchor==p.seq&&(f.closed||f.created>0){e.ambiguous("same_isn_syn",eventSYN,at,gs);return nil}
        if f==nil||f.anchor!=p.seq {
            if e.generations>=e.capFlows{e.invalid("diagnostic_capacity_drops");return nil}
            if f!=nil{f.replacedAt=at;e.metrics["flow_epoch_resets"]++;e.checkFlow(f)}
            f=&flow{id:uint64(e.generations+1),anchor:p.seq,born:at,high:1};e.flows[p.key]=append(gs,f);e.generations++
        }
    }
    if f==nil{e.invalid("unanchored_flows");return nil}
    // A late observer event belongs to the generation alive at its timestamp.
    if at.Before(f.born){
        f=nil;for _,g:=range gs{if !at.Before(g.born){f=g}}
        if f==nil{e.ambiguous("before_syn",eventDATA,at,gs);return nil}
    }
    // Reordered old-generation DATA can be selected by a disjoint observed
    // serial arc. If it could also extend the current epoch, fail closed.
    if p.size>0&&!p.syn{
        var owner *flow
        for _,g:=range gs{if at.Before(g.born){continue};x,ok:=offset(p.seq,g.anchor);if !ok||x<1||x>=g.high{continue}
            if owner!=nil{e.ambiguous("data_overlap",eventDATA,at,[]*flow{owner,g});return nil};owner=g
        }
        if owner!=nil&&owner!=f{
            if x,ok:=offset(p.seq,f.anchor);ok&&x>=1{e.ambiguous("data_overlap",eventDATA,at,[]*flow{f,owner});return nil}
            f=owner
        }
    }
    lo,ok:=offset(p.seq,f.anchor);if !ok{e.invalid("unsupported_serial_range");return nil}
    if p.syn{lo++}
    hi:=lo+int64(p.size);end:=hi;if p.fin{end++}
    if lo<1||end>=1<<31{e.invalid("unsupported_serial_range");return nil}
    // Observed RST/FIN is not proof of receiver acceptance. Subsequent or
    // reordered data observations remain correlatable, never implicitly ACKed.
    // Numeric sequence arcs of retained epochs may not overlap. Without a
    // connection ID, delayed old-generation bytes cannot safely be distinguished.
    if p.size>0{for _,g:=range e.flows[p.key]{if g==f||at.Before(g.born){continue};if intersectsGeneration(p.seq,p.size,g){e.ambiguous("data_overlap",eventDATA,at,[]*flow{f,g});return nil}}}
    if end>f.high{f.high=end};if p.fin{f.finEnd=end}
    if p.size==0{e.applyACKs(f);e.checkFlow(f);return nil}
    e.metrics["downlink_tcp_segments_created"]++
    // Split existing atoms only at new attempt boundaries; insert gaps as unique.
    a:=&attempt{interval:interval{lo,hi},out:at}
    next:=make([]record,0,len(f.ranges)+3);cursor:=lo;var unique uint64
    for _,r:=range f.ranges{
        if r.hi<=lo||r.lo>=hi{
            if r.lo>=hi&&cursor<hi{next=append(next,record{interval:interval{cursor,hi},attempts:[]*attempt{a}});unique+=uint64(hi-cursor);cursor=hi}
            next=append(next,r);continue
        }
        if r.lo<lo{left:=r;left.hi=lo;next=append(next,left);r.lo=lo}
        if cursor<r.lo{next=append(next,record{interval:interval{cursor,r.lo},attempts:[]*attempt{a}});unique+=uint64(r.lo-cursor)}
        right:=r;right.lo=hi
        if r.hi>hi{r.hi=hi}
        r.attempts=append(append([]*attempt(nil),r.attempts...),a);next=append(next,r);cursor=r.hi
        if right.hi>hi{next=append(next,right)}
    }
    if cursor<hi{next=append(next,record{interval:interval{cursor,hi},attempts:[]*attempt{a}});unique+=uint64(hi-cursor)}
    sort.Slice(next,func(i,j int)bool{return next[i].lo<next[j].lo})
    mapped:=int64(0);for _,r:=range next{for _,owner:=range r.attempts{if owner==a{mapped+=r.hi-r.lo}}};if mapped!=hi-lo{e.invalid("invariant_failures");return nil}
    oldRefs,newRefs:=0,0;for _,r:=range f.ranges{oldRefs+=len(r.attempts)};for _,r:=range next{newRefs+=len(r.attempts)}
    if !e.room(len(next)-len(f.ranges),1,newRefs-oldRefs,0){return nil}
    // Splitting invalid atoms must not share mutable late-ACK provenance:
    // a later partial ACK may cover only one of the resulting intersections.
    for i:=range next{if next[i].loss!=nil{copyLoss:=*next[i].loss;next[i].loss=&copyLoss}}
    e.records+=len(next)-len(f.ranges);e.references+=newRefs-oldRefs;e.attempts++
    f.ranges=next;f.attempts=append(f.attempts,a)
    e.metrics["unique_downlink_tcp_bytes"]+=unique;f.created+=unique
    retx:=uint64(p.size)-unique
    if retx>0{e.metrics["retransmitted_segments"]++;e.metrics["retransmission_attempts"]++;e.metrics["retransmitted_bytes"]+=retx}
    e.applyACKs(f);e.checkFlow(f);return a
}
func(e *Engine) Outgoing(data []byte){
    p,ok:=parse(data,false);if !ok{return};e.mu.Lock();defer e.mu.Unlock();at:=e.now()
    a:=e.outgoing(p,at);if a!=nil{
        e.putTag(data,tag{owner:a,created:at})
    }
}

func(e *Engine) acknowledge(p packet,at time.Time){
    if p.rst{e.observeReset(p,at,true);return}
    gs:=e.flows[p.key];if len(gs)==0{return}
    f:=gs[len(gs)-1]
    if !p.ackFlag{if p.fin{if len(gs)>1{e.ambiguous("fin_without_ack",eventFIN,at,gs);return};f.peerFIN=true;e.updateClosed(f)};return};e.metrics["ack_observations"]++
    // ACKs which fit an old epoch are routed to that epoch only. If two epochs
    // overlap, fail closed instead of letting old traffic acknowledge new data.
    var match *flow;var ack int64
    for _,g:=range gs{
        if at.Before(g.born){continue}
        x,ok:=offset(p.ack,g.anchor);if !ok||x<1||x>g.high{continue}
        if match!=nil{e.ambiguous("ack_overlap",eventACK,at,[]*flow{match,g});return};match=g;ack=x
    }
    if match!=nil{f=match}else{
        // Defer a future ACK only when there is exactly one possible owner.
        if len(gs)>1{e.ambiguous("ack_no_owner",eventACK,at,gs);return}
        if at.Before(f.born){e.ambiguous("before_syn",eventACK,at,gs);return}
        var ok bool;ack,ok=offset(p.ack,f.anchor);if !ok||ack<1{e.invalid("unsupported_serial_range");return}
    }
    if p.fin{f.peerFIN=true}
    if f.ackSeen&&ack<=f.ackHigh{
        if ack==f.ackHigh{e.metrics["duplicate_ack_events"]++}else{e.metrics["old_ack_events"]++}
        // Earlier event times can precede an already-observed cumulative ACK.
        earlier:=false;for _,a:=range f.acks{if a.end>=ack&&!at.Before(a.at){earlier=true;break}}
        if earlier{
            needsDelivery:=false;for _,r:=range f.ranges{if !r.invalid&&r.ackAt.IsZero()&&r.lo<ack&&!firstOut(r).After(at){needsDelivery=true;break}}
            if !needsDelivery{e.updateClosed(f);e.checkFlow(f);return}
        }
    }else{f.ackSeen=true;f.ackHigh=ack;e.metrics["ack_advancement_count"]++}
    if !e.room(0,0,0,1){return};f.acks=append(f.acks,ackEvent{end:ack,at:at});e.acks++
    e.applyACKEvent(f,f.acks[len(f.acks)-1]);e.updateClosed(f);e.checkFlow(f)
}
func(e *Engine) applyACKs(f *flow){for _,ev:=range f.acks{e.applyACKEvent(f,ev)};e.updateClosed(f)}
func(e *Engine) applyACKEvent(f *flow,ev ackEvent){
    if ev.expired||ev.end>f.high{return}
    for i:=0;i<len(f.ranges);i++{
        r:=&f.ranges[i]
        if r.invalid{if r.loss!=nil&&ev.end>=r.hi&&!ev.at.Before(r.loss.at){r.loss.lateACK=true};continue}
        if r.lo>=ev.end||firstOut(*r).After(ev.at){continue}
        if !r.ackAt.IsZero()&&!ev.at.Before(r.ackAt){continue}
        if ev.end<r.hi{
            if !e.room(1,0,len(r.attempts),0){return}
            right:=*r;right.lo=ev.end;r.hi=ev.end
            f.ranges=append(f.ranges,record{});copy(f.ranges[i+2:],f.ranges[i+1:]);f.ranges[i+1]=right
            e.records++;e.references+=len(right.attempts);r=&f.ranges[i]
        }
        r.ackAt=ev.at;r.ackCover=ev.end
    }
}
func(e *Engine) updateClosed(f *flow){
    if f.finEnd>0&&f.peerFIN&&f.ackHigh>=f.finEnd&&!f.closed{f.closed=true;e.metrics["flow_close_events"]++}
}

// Invariants are checked per changed flow after each TCP event, and globally at
// every snapshot. ACKed data is retained, so later overlap cannot become unique.
func(e *Engine) checkFlow(f *flow){
    var prev int64;var created uint64
    for i,r:=range f.ranges{
        if r.lo>=r.hi||(i>0&&r.lo<prev)||len(r.attempts)==0{e.invalid("invariant_failures");return}
        prev=r.hi;created+=uint64(r.hi-r.lo)
        for _,a:=range r.attempts{if a.lo>r.lo||a.hi<r.hi{e.invalid("invariant_failures");return}}
        if !r.ackAt.IsZero(){
            if r.ackCover<r.hi||r.ackCover>f.high||firstOut(r).After(r.ackAt){e.invalid("invariant_failures");return}
        }
    }
    if created!=f.created{e.invalid("invariant_failures");return}
    var outstanding,segments uint64
    for _,r:=range f.ranges{if !r.invalid&&r.ackAt.IsZero(){outstanding+=uint64(r.hi-r.lo);segments++}}
    e.outstanding=e.outstanding-f.outstanding+outstanding
    e.outstandingRanges=e.outstandingRanges-f.outstandingRanges+segments
    f.outstanding=outstanding;f.outstandingRanges=segments
    e.peak("outstanding_unique_bytes_peak",e.outstanding);e.peak("outstanding_segments_peak",e.outstandingRanges)
}

// Buffer pointers keep identity stable across existing wrapper allocations.
// Budget their slice capacity as well as tag count; exhaustion loses diagnostics,
// never production packets. No packet content is copied into observation state.
func(e *Engine) takeTag(data []byte)(tag,bool){
    if len(data)==0{return tag{},false};t,ok:=e.tags[&data[0]];if ok{delete(e.tags,&data[0]);e.tagBytes-=t.retained};return t,ok
}
func(e *Engine) putTag(data []byte,t tag){
    if len(data)==0{return};e.takeTag(data)
    if len(e.tags)>=e.capTags||e.tagBytes+cap(data)>MaxTagBytes{e.invalid("diagnostic_capacity_drops");return}
    t.retained=cap(data);e.tags[&data[0]]=t;e.tagBytes+=t.retained
}
// Move transfers only observation identity, never wire data.
func(e *Engine) Move(from,to []byte){e.mu.Lock();defer e.mu.Unlock();if t,ok:=e.takeTag(from);ok{e.putTag(to,t)}}
func(e *Engine) Forget(data []byte){e.mu.Lock();defer e.mu.Unlock();e.takeTag(data)}
func(e *Engine) Enqueue(data []byte){
    if len(data)==0{return};e.mu.Lock();defer e.mu.Unlock();now:=e.now()
    if t,ok:=e.tags[&data[0]];ok&&t.owner!=nil{t.owner.enqueue=now;e.enqueueDelay.observe(now.Sub(t.owner.out))}
}
type HTTPObservation struct{start time.Time;owners []*attempt;bytes uint64;done bool}
func(e *Engine) HTTPStart(batch [][]byte)*HTTPObservation{
    e.mu.Lock();defer e.mu.Unlock();o:=&HTTPObservation{start:e.now()}
    for _,data:=range batch{
        o.bytes+=uint64(len(data));if len(data)==0{continue}
        if t,ok:=e.tags[&data[0]];ok&&t.owner!=nil{
            t.owner.start=o.start;o.owners=append(o.owners,t.owner)
            if !t.owner.enqueue.IsZero(){e.httpQueueDelay.observe(o.start.Sub(t.owner.enqueue))};e.takeTag(data)
        }
    }
    e.metrics["http_requests"]++;e.metrics["http_payload_bytes_attempted"]+=o.bytes;e.metrics["http_inflight_requests"]++;e.peak("http_inflight_peak",e.metrics["http_inflight_requests"])
    return o
}
func(e *Engine) HTTPDone(o *HTTPObservation,success bool,status int,bodyReadError bool){
    if o==nil{return};e.mu.Lock();defer e.mu.Unlock();if o.done{e.invalid("invariant_failures");return};o.done=true;now:=e.now()
    e.httpDuration.observe(now.Sub(o.start));e.metrics["http_inflight_requests"]--
    if success{e.metrics["http_successes"]++;e.metrics["http_payload_bytes_success"]+=o.bytes}else{e.metrics["http_failures"]++;e.metrics["http_payload_bytes_failed"]+=o.bytes}
    if status==429{e.metrics["http_status_429"]++};if bodyReadError{e.metrics["http_body_read_errors_ignored"]++}
    for _,a:=range o.owners{a.end=now;a.success=success}
}
func(e *Engine) Inbound(data []byte,ws time.Time){
    if len(data)==0{return};e.mu.Lock();defer e.mu.Unlock();e.putTag(data,tag{ws:ws,created:e.now()})
}
func(e *Engine) Incoming(data []byte)time.Time{
    p,ok:=parse(data,true);e.mu.Lock();defer e.mu.Unlock();now:=e.now()
    t,_:=e.takeTag(data);if !ok{return time.Time{}}
    if p.ackFlag{if t.ws.IsZero(){e.invalid("ws_timestamp_missing")}else{e.wsDecode.observe(now.Sub(t.ws))}}
    e.acknowledge(p,now);if !p.ackFlag{return time.Time{}};return now
}
func(e *Engine) BeforeInject(t time.Time){if t.IsZero(){return};e.mu.Lock();defer e.mu.Unlock();e.decodeInject.observe(e.now().Sub(t))}
func(e *Engine) Event(n string){e.mu.Lock();defer e.mu.Unlock();switch n{case "ws_messages","ws_reconnects","ws_connect_failures":e.metrics[n]++}}
func(e *Engine) WSConnected(on bool){e.mu.Lock();defer e.mu.Unlock();e.metrics["ws_connected"]=0;if on{e.metrics["ws_connected"]=1}}

func(e *Engine) expire(now time.Time){
    // Unique ranges, attempts, generations and ACK history are session-retained
    // under hard caps. Only transient buffer identity tags have an idle TTL.
    for k,t:=range e.tags{if now.Sub(t.created)>=e.ttl{
        delete(e.tags,k);e.tagBytes-=t.retained;e.metrics["correlator_evictions"]++
        e.metrics["expired_identity_tags"]++
        if t.owner!=nil{e.invalidateAttempt(t.owner,lossTTL,eventTTL,now)}
    }}
}

var latencyNames=[]string{"tunnel_first_out_to_ack","tunnel_last_out_to_ack","http_first_start_to_ack","http_last_start_to_ack","http_first_success_end_to_ack","http_last_success_end_to_ack"}
func addBounds(t time.Time,first,last *time.Time){if t.IsZero(){return};if first.IsZero()||t.Before(*first){*first=t};if last.IsZero()||t.After(*last){*last=t}}
func(e *Engine) Snapshot()map[string]uint64{
    e.mu.Lock();defer e.mu.Unlock();now:=e.now();e.expire(now)
    m:=make(map[string]uint64);for k,v:=range e.metrics{m[k]=v}
    m["schema"]=2;m["retained_tag_capacity_bytes"]=uint64(e.tagBytes);m["retained_tag_capacity_cap"]=MaxTagBytes;m["records_current"]=uint64(e.records);m["attempts_current"]=uint64(e.attempts);m["references_current"]=uint64(e.references);m["flows_current"]=uint64(e.generations);m["tags_current"]=uint64(len(e.tags));m["ack_history_current"]=uint64(e.acks)
    m["records_cap"]=uint64(e.capRecords);m["attempts_cap"]=uint64(e.capAttempts);m["references_cap"]=uint64(e.capReferences);m["flows_cap"]=uint64(e.capFlows);m["tags_cap"]=uint64(e.capTags);m["ack_history_cap"]=uint64(e.capACKs);m["ttl_ns"]=uint64(e.ttl)
    for _,k:=range []string{"acked_unique_downlink_tcp_bytes","outstanding_unique_bytes_current","invalidated_unique_bytes","retransmitted_unique_bytes","multi_attempt_acked_bytes","outstanding_segments_current","downlink_tcp_segments_acked","http_not_started_bytes","http_inflight_bytes","http_success_not_acked_bytes","http_failed_not_acked_bytes","oldest_unacked_age_current_ns","ack_before_first_http_complete","ack_before_last_http_complete","ack_before_first_http_complete_bytes","ack_before_last_http_complete_bytes","ack_without_http_start_bytes","pending_observer_ack_events"}{m[k]=0}
    var hist [6]Histogram;var total uint64;nr,na,nref,nack,ng:=0,0,0,0,0
    for _,gs:=range e.flows{for _,f:=range gs{
        ng++;na+=len(f.attempts);nack+=len(f.acks);nr+=len(f.ranges)
        for _,ev:=range f.acks{if ev.end>f.high{m["pending_observer_ack_events"]++;if now.Sub(ev.at)>=e.ttl{m["unresolved_ack_events"]++}}}
        for _,r:=range f.ranges{
            n:=uint64(r.hi-r.lo);total+=n;nref+=len(r.attempts)
            if len(r.attempts)>1{m["retransmitted_unique_bytes"]+=n}
            if r.invalid{m["invalidated_unique_bytes"]+=n;continue}
            var outFirst,outLast,startFirst,startLast,endFirst,endLast time.Time
            beforeAttempts:=0;incomplete:=0;success,inflight,started:=false,false,false
            for _,a:=range r.attempts{
                success=success||(!a.end.IsZero()&&a.success);inflight=inflight||(!a.start.IsZero()&&a.end.IsZero());started=started||!a.start.IsZero()
                if !r.ackAt.IsZero()&&a.out.After(r.ackAt){continue} // Post-delivery retransmission is not an ACK cause.
                beforeAttempts++;addBounds(a.out,&outFirst,&outLast)
                if !a.start.IsZero()&&!a.start.After(r.ackAt){addBounds(a.start,&startFirst,&startLast)}
                if a.end.IsZero()||a.end.After(r.ackAt){incomplete++}else if a.success{addBounds(a.end,&endFirst,&endLast)}
            }
            if r.ackAt.IsZero(){
                m["outstanding_unique_bytes_current"]+=n;m["outstanding_segments_current"]++
                age:=now.Sub(firstOut(r));if age>=0&&uint64(age)>m["oldest_unacked_age_current_ns"]{m["oldest_unacked_age_current_ns"]=uint64(age)}
                switch{case success:m["http_success_not_acked_bytes"]+=n;case inflight:m["http_inflight_bytes"]+=n;case started:m["http_failed_not_acked_bytes"]+=n;default:m["http_not_started_bytes"]+=n};continue
            }
            m["acked_unique_downlink_tcp_bytes"]+=n;m["downlink_tcp_segments_acked"]++
            if beforeAttempts>1{m["multi_attempt_acked_bytes"]+=n}
            stamps:=[6]time.Time{outFirst,outLast,startFirst,startLast,endFirst,endLast}
            for i,t:=range stamps{if !t.IsZero(){hist[i].observeBytes(r.ackAt.Sub(t),n)}}
            if startFirst.IsZero(){m["ack_without_http_start_bytes"]+=n}
            if incomplete==beforeAttempts&&beforeAttempts>0{m["ack_before_first_http_complete"]++;m["ack_before_first_http_complete_bytes"]+=n}
            if incomplete>0{m["ack_before_last_http_complete"]++;m["ack_before_last_http_complete_bytes"]+=n}
            age:=r.ackAt.Sub(outFirst);if age>=0{e.peak("oldest_unacked_age_max_ns",uint64(age))}
        }
    }}
    e.peak("oldest_unacked_age_max_ns",m["oldest_unacked_age_current_ns"]);m["oldest_unacked_age_max_ns"]=e.metrics["oldest_unacked_age_max_ns"]
    m["ack_advancement_unique_bytes"]=m["acked_unique_downlink_tcp_bytes"]
    e.lifecycleSnapshot(m)
    m["counter_consistency"]=1
    if total!=m["unique_downlink_tcp_bytes"]||total!=m["acked_unique_downlink_tcp_bytes"]+m["outstanding_unique_bytes_current"]+m["invalidated_unique_bytes"]||m["acked_unique_downlink_tcp_bytes"]>total||m["outstanding_unique_bytes_current"]!=m["http_not_started_bytes"]+m["http_inflight_bytes"]+m["http_success_not_acked_bytes"]+m["http_failed_not_acked_bytes"]||nr!=e.records||na!=e.attempts||nref!=e.references||nack!=e.acks||ng!=e.generations{e.invalid("invariant_failures");m["invariant_failures"]=e.metrics["invariant_failures"];m["counter_consistency"]=0}
    m["correlation_valid"]=m["counter_consistency"]
    for _,k:=range []string{"invalidated_unique_bytes","correlator_evictions","diagnostic_capacity_drops","sequence_constraint_violations","ambiguous_flow_generation","unanchored_flows","unresolved_ack_events","expired_observer_ack_events","pending_observer_ack_events","ack_without_http_start_bytes","ws_timestamp_missing","invariant_failures","provenance_capacity_drops"}{if m[k]>0{m["correlation_valid"]=0}}
    for i,n:=range latencyNames{hist[i].snapshot(m,n)}
    e.wsDecode.snapshot(m,"ws_to_ack_decode");e.decodeInject.snapshot(m,"ack_decode_to_inject");e.httpDuration.snapshot(m,"http_duration");e.enqueueDelay.snapshot(m,"tunnel_to_enqueue");e.httpQueueDelay.snapshot(m,"enqueue_to_http_start")
    return m
}
