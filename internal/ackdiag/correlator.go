// Package ackdiag is observation-only. Flow keys, TCP sequence numbers and
// packet identities are private memory state and never enter snapshots.
package ackdiag

import (
	"encoding/binary"
	"sort"
	"sync"
	"time"
)

const (
	MaxRecords = 16384
	MaxFlows = 1024
	MaxTags = 32768
	MaxRangesPerFlow = 256
	RecordTTL = 30 * time.Second
	FlowHistoryTTL = 10 * time.Minute
)

type flowKey [12]byte
type interval struct{ lo, hi int64 }
type packet struct { key flowKey; seq, ack uint32; size int; syn, fin, ackFlag bool }

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
	p.size=len(tcp)-offset; p.syn=tcp[13]&2!=0; p.fin=tcp[13]&1!=0; p.ackFlag=tcp[13]&16!=0
	return p,true
}

// Serial arithmetic is unambiguous within a half sequence space. The observed
// outstanding/forward jump is additionally constrained to <2^30 bytes.
func unwrap(seq, anchor uint32, reference int64) int64 {
	return reference + int64(int32(seq-(anchor+uint32(reference))))
}

type attempt struct {
	out, enqueue, start, end time.Time
	remaining uint64
	success, ambiguous bool
}
type record struct { interval; owner *attempt }
type flow struct {
	anchor uint32
	syn bool
	high, ack int64
	ackSeen bool
	seen []interval
	open []record
	touched time.Time
}
type tag struct { owner *attempt; ws, created time.Time }
type Histogram struct { count,sum,max uint64; buckets [9]uint64 }

func (h *Histogram) observe(d time.Duration) {
	if d < 0 { return }
	n:=uint64(d);h.count++;h.sum+=n;if n>h.max{h.max=n}
	limits:=[...]time.Duration{50*time.Millisecond,100*time.Millisecond,250*time.Millisecond,500*time.Millisecond,time.Second,2*time.Second,5*time.Second,10*time.Second}
	i:=0;for i<len(limits)&&d>=limits[i]{i++};h.buckets[i]++
}
func (h *Histogram) snapshot(m map[string]uint64,prefix string) {
	m[prefix+"_count"]=h.count;m[prefix+"_sum_ns"]=h.sum;m[prefix+"_max_ns"]=h.max
	names:=[...]string{"lt_50ms","50_100ms","100_250ms","250_500ms","500ms_1s","1s_2s","2s_5s","5s_10s","ge_10s"}
	for i,n:=range names{m[prefix+"_"+n]=h.buckets[i]}
}

type Engine struct {
	mu sync.Mutex
	now func() time.Time
	flows map[flowKey]*flow
	tags map[*byte]tag
	metrics map[string]uint64
	records int
	capRecords,capFlows,capTags int
	ttl time.Duration
	tunnelAck,httpStartAck,httpEndAck,wsDecode,decodeInject,httpDuration,enqueueDelay,httpQueueDelay Histogram
}

var metricNames=[]string{
	"downlink_tcp_segments_created","unique_downlink_tcp_bytes","retransmitted_segments","retransmitted_bytes",
	"downlink_tcp_segments_acked","acked_unique_downlink_tcp_bytes","outstanding_segments_current","outstanding_segments_peak",
	"outstanding_unique_bytes_current","outstanding_unique_bytes_peak","oldest_unacked_age_max_ns",
	"duplicate_ack_events","old_ack_events","ack_advancement_count","ack_advancement_unique_bytes",
	"ack_before_http_complete","ack_before_http_complete_bytes","ack_without_http_start_bytes","acked_after_failed_http_bytes",
	"http_latency_ambiguous_retransmit_bytes","correlator_evictions","evicted_unique_bytes","diagnostic_capacity_drops",
	"sequence_constraint_violations","ack_beyond_observed","flow_epoch_resets","idle_flow_cleanup","unanchored_flows",
	"ws_timestamp_missing","http_requests","http_successes","http_failures","http_status_429","http_payload_bytes_attempted",
	"http_payload_bytes_success","http_payload_bytes_failed","http_inflight_requests","http_inflight_peak",
	"http_body_read_errors_ignored","ws_connected","ws_messages","ws_reconnects","ws_connect_failures",
}

func New(now func() time.Time) *Engine {
	e:=&Engine{now:now,flows:make(map[flowKey]*flow),tags:make(map[*byte]tag),metrics:make(map[string]uint64),capRecords:MaxRecords,capFlows:MaxFlows,capTags:MaxTags,ttl:RecordTTL}
	for _,name:=range metricNames{e.metrics[name]=0};return e
}

func (e *Engine) peak(name string,value uint64) { if value>e.metrics[name]{e.metrics[name]=value} }
func novel(r interval,seen []interval) []interval {
	var out []interval
	for _,s:=range seen {
		if s.hi<=r.lo{continue};if s.lo>=r.hi{break}
		if s.lo>r.lo{out=append(out,interval{r.lo,s.lo})}
		if s.hi>r.lo{r.lo=s.hi};if r.lo>=r.hi{return out}
	}
	if r.lo<r.hi{out=append(out,r)};return out
}
func merge(seen []interval,r interval) []interval {
	seen=append(seen,r);sort.Slice(seen,func(i,j int)bool{return seen[i].lo<seen[j].lo})
	out:=seen[:0];for _,s:=range seen{if len(out)==0||out[len(out)-1].hi<s.lo{out=append(out,s)}else if s.hi>out[len(out)-1].hi{out[len(out)-1].hi=s.hi}}
	return out
}

func (e *Engine) Outgoing(data []byte) {
	now:=e.now();p,ok:=parse(data,false);if !ok{return}
	e.mu.Lock();defer e.mu.Unlock()
	f:=e.flows[p.key]
	if f!=nil&&p.syn&&(!f.syn||f.anchor!=p.seq){e.evictFlow(f,now);delete(e.flows,p.key);f=nil;e.metrics["flow_epoch_resets"]++}
	if f==nil {
		if len(e.flows)>=e.capFlows{e.metrics["diagnostic_capacity_drops"]++;return}
		f=&flow{anchor:p.seq,syn:p.syn,touched:now};e.flows[p.key]=f
		if !p.syn{e.metrics["unanchored_flows"]++}
	}
	f.touched=now
	lo:=unwrap(p.seq,f.anchor,f.high);if p.syn{lo++};hi:=lo+int64(p.size)
	if hi-f.high>=1<<30||f.high-lo>=1<<30{e.metrics["sequence_constraint_violations"]++;return}
	end:=hi;if p.fin{end++};if end>f.high{f.high=end}
	if p.size==0{return}
	e.metrics["downlink_tcp_segments_created"]++
	parts:=novel(interval{lo,hi},f.seen);var unique uint64
	for _,s:=range parts{unique+=uint64(s.hi-s.lo)}
	retx:=uint64(p.size)-unique
	if retx>0 {
		e.metrics["retransmitted_segments"]++;e.metrics["retransmitted_bytes"]+=retx
		for _,r:=range f.open{if r.lo<hi&&lo<r.hi{r.owner.ambiguous=true}}
	}
	if unique==0{return}
	if e.records+len(parts)>e.capRecords||len(e.tags)>=e.capTags||len(f.seen)>=MaxRangesPerFlow {
		e.metrics["diagnostic_capacity_drops"]++;return
	}
	a:=&attempt{out:now,remaining:unique}
	f.seen=merge(f.seen,interval{lo,hi})
	for _,s:=range parts{f.open=append(f.open,record{s,a});e.records++}
	e.tags[&data[0]]=tag{owner:a,created:now}
	e.metrics["unique_downlink_tcp_bytes"]+=unique
	e.metrics["outstanding_unique_bytes_current"]+=unique;e.metrics["outstanding_segments_current"]++
	e.peak("outstanding_unique_bytes_peak",e.metrics["outstanding_unique_bytes_current"])
	e.peak("outstanding_segments_peak",e.metrics["outstanding_segments_current"])
}

// Move transfers only the observation identity. It never reads or changes payload.
func (e *Engine) Move(from,to []byte) {
	if len(from)==0{return};e.mu.Lock();defer e.mu.Unlock()
	t,ok:=e.tags[&from[0]];if !ok{return};delete(e.tags,&from[0])
	if len(to)>0{e.tags[&to[0]]=t}
}
func (e *Engine) Forget(data []byte) {
	if len(data)==0{return};e.mu.Lock();delete(e.tags,&data[0]);e.mu.Unlock()
}
func (e *Engine) Enqueue(data []byte) {
	if len(data)==0{return};now:=e.now();e.mu.Lock();defer e.mu.Unlock()
	if t,ok:=e.tags[&data[0]];ok&&t.owner!=nil{t.owner.enqueue=now;e.enqueueDelay.observe(now.Sub(t.owner.out))}
}
type HTTPObservation struct { start time.Time; owners []*attempt; bytes uint64 }
func (e *Engine) HTTPStart(batch [][]byte) *HTTPObservation {
	o:=&HTTPObservation{start:e.now()};e.mu.Lock();defer e.mu.Unlock()
	for _,data:=range batch {
		o.bytes+=uint64(len(data));if len(data)==0{continue}
		if t,ok:=e.tags[&data[0]];ok&&t.owner!=nil {
			t.owner.start=o.start;o.owners=append(o.owners,t.owner)
			if !t.owner.enqueue.IsZero(){e.httpQueueDelay.observe(o.start.Sub(t.owner.enqueue))}
			delete(e.tags,&data[0])
		}
	}
	e.metrics["http_requests"]++;e.metrics["http_payload_bytes_attempted"]+=o.bytes
	e.metrics["http_inflight_requests"]++;e.peak("http_inflight_peak",e.metrics["http_inflight_requests"])
	return o
}
func (e *Engine) HTTPDone(o *HTTPObservation,success bool,status int,bodyReadError bool) {
	if o==nil{return};now:=e.now();e.mu.Lock();defer e.mu.Unlock()
	e.httpDuration.observe(now.Sub(o.start));e.metrics["http_inflight_requests"]--
	if success{e.metrics["http_successes"]++;e.metrics["http_payload_bytes_success"]+=o.bytes}else{e.metrics["http_failures"]++;e.metrics["http_payload_bytes_failed"]+=o.bytes}
	if status==429{e.metrics["http_status_429"]++};if bodyReadError{e.metrics["http_body_read_errors_ignored"]++}
	for _,a:=range o.owners{a.end=now;a.success=success}
}

func (e *Engine) Inbound(data []byte,ws time.Time) {
	if len(data)==0{return};now:=e.now();e.mu.Lock();defer e.mu.Unlock()
	if len(e.tags)>=e.capTags{e.metrics["diagnostic_capacity_drops"]++;return}
	e.tags[&data[0]]=tag{ws:ws,created:now}
}

func (e *Engine) Incoming(data []byte) time.Time {
	p,ok:=parse(data,true);now:=e.now()
	e.mu.Lock();defer e.mu.Unlock()
	var t tag;if len(data)>0{t=e.tags[&data[0]];delete(e.tags,&data[0])}
	if !ok||!p.ackFlag{return time.Time{}}
	if t.ws.IsZero(){e.metrics["ws_timestamp_missing"]++}else{e.wsDecode.observe(now.Sub(t.ws))}
	f:=e.flows[p.key];if f==nil{return now};f.touched=now
	ack:=unwrap(p.ack,f.anchor,f.high)
	if ack>f.high{e.metrics["ack_beyond_observed"]++;return now}
	if f.ackSeen&&ack<=f.ack {if ack==f.ack{e.metrics["duplicate_ack_events"]++}else{e.metrics["old_ack_events"]++};return now}
	f.ackSeen=true;f.ack=ack;e.metrics["ack_advancement_count"]++
	kept:=f.open[:0]
	for _,r:=range f.open {
		if r.lo>=ack{kept=append(kept,r);continue}
		hi:=r.hi;if ack<hi{hi=ack};n:=uint64(hi-r.lo)
		a:=r.owner;e.tunnelAck.observe(now.Sub(a.out));e.peak("oldest_unacked_age_max_ns",uint64(now.Sub(a.out)))
		if a.ambiguous{e.metrics["http_latency_ambiguous_retransmit_bytes"]+=n}else if a.start.IsZero(){e.metrics["ack_without_http_start_bytes"]+=n}else{
			e.httpStartAck.observe(now.Sub(a.start))
			if a.end.IsZero()||now.Before(a.end){e.metrics["ack_before_http_complete"]++;e.metrics["ack_before_http_complete_bytes"]+=n}else if a.success{e.httpEndAck.observe(now.Sub(a.end))}else{e.metrics["acked_after_failed_http_bytes"]+=n}
		}
		e.metrics["acked_unique_downlink_tcp_bytes"]+=n;e.metrics["ack_advancement_unique_bytes"]+=n
		e.metrics["outstanding_unique_bytes_current"]-=n;a.remaining-=n
		if a.remaining==0{e.metrics["outstanding_segments_current"]--;e.metrics["downlink_tcp_segments_acked"]++}
		r.lo=hi;if r.lo<r.hi{kept=append(kept,r)}else{e.records--}
	}
	f.open=kept;return now
}
func (e *Engine) BeforeInject(decoded time.Time) {
	if decoded.IsZero(){return};now:=e.now();e.mu.Lock();e.decodeInject.observe(now.Sub(decoded));e.mu.Unlock()
}
func (e *Engine) Event(name string) {
	e.mu.Lock();defer e.mu.Unlock()
	switch name{case "ws_messages","ws_reconnects","ws_connect_failures":e.metrics[name]++}
}
func (e *Engine) WSConnected(on bool) {e.mu.Lock();defer e.mu.Unlock();e.metrics["ws_connected"]=0;if on{e.metrics["ws_connected"]=1}}

func (e *Engine) evictRecord(r record,now time.Time) {
	n:=uint64(r.hi-r.lo);a:=r.owner
	e.peak("oldest_unacked_age_max_ns",uint64(now.Sub(a.out)))
	e.metrics["evicted_unique_bytes"]+=n;e.metrics["correlator_evictions"]++
	e.metrics["outstanding_unique_bytes_current"]-=n;a.remaining-=n;e.records--
	if a.remaining==0{e.metrics["outstanding_segments_current"]--}
}
func (e *Engine) evictFlow(f *flow,now time.Time) {for _,r:=range f.open{e.evictRecord(r,now)}}
func (e *Engine) expire(now time.Time) {
	for key,f:=range e.flows {
		kept:=f.open[:0];for _,r:=range f.open{if now.Sub(r.owner.out)>=e.ttl{e.evictRecord(r,now)}else{kept=append(kept,r)}};f.open=kept
		if len(f.open)==0&&now.Sub(f.touched)>=FlowHistoryTTL{delete(e.flows,key);e.metrics["idle_flow_cleanup"]++}
	}
	for key,t:=range e.tags{if now.Sub(t.created)>=e.ttl{delete(e.tags,key);e.metrics["correlator_evictions"]++}}
}
func (e *Engine) Snapshot() map[string]uint64 {
	now:=e.now();e.mu.Lock();defer e.mu.Unlock();e.expire(now)
	m:=make(map[string]uint64,len(e.metrics)+120);for k,v:=range e.metrics{m[k]=v}
	m["schema"]=1;m["records_current"]=uint64(e.records);m["flows_current"]=uint64(len(e.flows));m["tags_current"]=uint64(len(e.tags))
	m["records_cap"]=uint64(e.capRecords);m["flows_cap"]=uint64(e.capFlows);m["tags_cap"]=uint64(e.capTags);m["ttl_ns"]=uint64(e.ttl)
	m["http_not_started_bytes"]=0;m["http_inflight_bytes"]=0;m["http_success_not_acked_bytes"]=0;m["http_failed_not_acked_bytes"]=0
	m["oldest_unacked_age_current_ns"]=0
	for _,f:=range e.flows{for _,r:=range f.open{
		a:=r.owner;n:=uint64(r.hi-r.lo);age:=uint64(now.Sub(a.out));if age>m["oldest_unacked_age_current_ns"]{m["oldest_unacked_age_current_ns"]=age}
		switch{case a.start.IsZero():m["http_not_started_bytes"]+=n;case a.end.IsZero():m["http_inflight_bytes"]+=n;case a.success:m["http_success_not_acked_bytes"]+=n;default:m["http_failed_not_acked_bytes"]+=n}
	}}
	e.peak("oldest_unacked_age_max_ns",m["oldest_unacked_age_current_ns"]);m["oldest_unacked_age_max_ns"]=e.metrics["oldest_unacked_age_max_ns"]
	m["counter_consistency"]=0
	if m["unique_downlink_tcp_bytes"]==m["acked_unique_downlink_tcp_bytes"]+m["outstanding_unique_bytes_current"]+m["evicted_unique_bytes"]&&m["outstanding_unique_bytes_current"]==m["http_not_started_bytes"]+m["http_inflight_bytes"]+m["http_success_not_acked_bytes"]+m["http_failed_not_acked_bytes"]{m["counter_consistency"]=1}
	m["correlation_valid"]=m["counter_consistency"]
	for _,k:=range []string{"correlator_evictions","diagnostic_capacity_drops","sequence_constraint_violations","ack_beyond_observed","unanchored_flows","ack_without_http_start_bytes","ws_timestamp_missing"}{if m[k]!=0{m["correlation_valid"]=0}}
	e.tunnelAck.snapshot(m,"tunnel_out_to_ack");e.httpStartAck.snapshot(m,"http_start_to_ack");e.httpEndAck.snapshot(m,"http_end_to_ack")
	e.wsDecode.snapshot(m,"ws_to_ack_decode");e.decodeInject.snapshot(m,"ack_decode_to_inject");e.httpDuration.snapshot(m,"http_duration")
	e.enqueueDelay.snapshot(m,"tunnel_to_enqueue");e.httpQueueDelay.snapshot(m,"enqueue_to_http_start")
	return m
}
