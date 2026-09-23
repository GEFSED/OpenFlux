package ackdiag

import (
	"encoding/json"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
)

// LogSchema versions the external log contract, not Engine's frozen schema-3
// accounting. Schema 3 has no startup-event envelope.
const LogSchema = 6

type StartupStage string
type FailureClass string

const (
	ProcessStart StartupStage = "process_start"
	DiagnosticInit StartupStage = "diagnostic_init"
	TransportStart StartupStage = "transport_start"
	Authorization StartupStage = "authorization"
	RelayWorkers StartupStage = "relay_workers"
	ProxyInit StartupStage = "proxy_init"
	SnapshotLoop StartupStage = "snapshot_loop"
	NoFailure FailureClass = "none"
	AuthMissing FailureClass = "auth_client_config_missing"
	AuthChallenge FailureClass = "auth_challenge_or_captcha_classified"
	AuthOther FailureClass = "auth_other"
	TransportOther FailureClass = "transport_start_other"
	RelayFailure FailureClass = "relay_workers_failure"
	ProxyFailure FailureClass = "proxy_init_failure"
	DiagnosticFailure FailureClass = "diagnostic_init_failure"
	SchemaFailure FailureClass = "schema_emit_failure"
	UnknownFailure FailureClass = "unknown_startup_failure"
)

type StartupEvent struct {
	Schema int `json:"schema"`
	Event string `json:"event"`
	Ordinal uint64 `json:"ordinal"`
	Stage StartupStage `json:"startup_stage"`
	Result string `json:"startup_result"`
	Failure FailureClass `json:"failure_class"`
	TransportStarted bool `json:"transport_started"`
	AuthorizationCompleted bool `json:"authorization_completed"`
	RelayWorkersStarted bool `json:"relay_workers_started"`
	ProxyInitialized bool `json:"proxy_initialized"`
	SnapshotLoopStarted bool `json:"diagnostic_snapshot_loop_started"`
}

// StartupRecorder observes calls only. It cannot return an error to the network
// path, exit a process, launch workers, recover a panic, retry, or change a timer.
type StartupRecorder struct {
	mu sync.Mutex
	out, fallback io.Writer
	state StartupEvent
	failed bool
	bootstrapOrdinal uint64
	bootstrapChallenge bool
}

func NewStartupRecorder(out, fallback io.Writer) *StartupRecorder {
	return &StartupRecorder{out:out, fallback:fallback,
		state:StartupEvent{Schema:LogSchema, Event:"startup"}}
}

func safeStage(s StartupStage) bool {
	switch s {case ProcessStart,DiagnosticInit,TransportStart,Authorization,RelayWorkers,ProxyInit,SnapshotLoop:return true};return false
}
func safeFailure(f FailureClass) bool {
	switch f {case NoFailure,AuthMissing,AuthChallenge,AuthOther,TransportOther,RelayFailure,ProxyFailure,DiagnosticFailure,SchemaFailure,UnknownFailure:return true};return false
}

func writeStartup(w io.Writer, e StartupEvent) bool {
	if w==nil{return false}
	b,err:=json.Marshal(e);if err!=nil{return false}
	line:=append(append([]byte("[ACK-STARTUP] "),b...), '\n')
	n,err:=w.Write(line);return err==nil && n==len(line)
}

func (r *StartupRecorder) Record(stage StartupStage, result string, failure FailureClass) {
	r.mu.Lock();defer r.mu.Unlock()
	if r.failed{return}
	if stage==SnapshotLoop && result=="ok" && r.state.SnapshotLoopStarted{return}
	if !safeStage(stage)||!safeFailure(failure)||(result!="begin"&&result!="ok"&&result!="failure") {
		stage=ProcessStart;result="failure";failure=UnknownFailure
	}
	if result!="failure" {failure=NoFailure} else if failure==NoFailure {failure=UnknownFailure}
	r.state.Ordinal++;r.state.Stage=stage;r.state.Result=result;r.state.Failure=failure
	if result=="ok" {
		switch stage {
		case TransportStart:r.state.TransportStarted=true
		case Authorization:r.state.AuthorizationCompleted=true
		case RelayWorkers:r.state.RelayWorkersStarted=true
		case ProxyInit:r.state.ProxyInitialized=true
		case SnapshotLoop:r.state.SnapshotLoopStarted=true
		}
	}
	r.failed=result=="failure"
	if !writeStartup(r.out,r.state) {
		// A broken sink cannot prove absence of loss. A best-effort separate sink
		// reports the enum only; partial/absent records still fail closed in CI/harness.
		r.failed=true;r.state.Result="failure";r.state.Failure=SchemaFailure
		if stage==DiagnosticInit {r.state.Failure=DiagnosticFailure}
		writeStartup(r.fallback,r.state)
	}
}

// Error strings alone never establish a challenge. Only the bootstrap body
// structural observer can upgrade AuthMissing in StartupTransportFailure.
func ClassifyTransportFailure(err error) (StartupStage, FailureClass) {
	if err==nil{return TransportStart,UnknownFailure}
	s:=err.Error()
	if !strings.HasPrefix(s,"auth: "){return TransportStart,TransportOther}
	s=strings.TrimPrefix(s,"auth: ")
	const missing="client-config not found in "
	if !strings.HasPrefix(s,missing){return Authorization,AuthOther}
	return Authorization,AuthMissing
}

var startupRecorder atomic.Pointer[StartupRecorder]

func BeginStartup() {
	r:=NewStartupRecorder(os.Stderr,os.Stdout);startupRecorder.Store(r)
	r.Record(ProcessStart,"ok",NoFailure);r.Record(DiagnosticInit,"begin",NoFailure)
}
func StartupBegin(s StartupStage){if r:=startupRecorder.Load();r!=nil{r.Record(s,"begin",NoFailure)}}
func StartupOK(s StartupStage){if r:=startupRecorder.Load();r!=nil{r.Record(s,"ok",NoFailure)}}
func StartupFailure(s StartupStage,f FailureClass){if r:=startupRecorder.Load();r!=nil{r.Record(s,"failure",f)}}
func StartupTransportFailure(err error){
	s,f:=ClassifyTransportFailure(err)
	if r:=startupRecorder.Load();r!=nil&&f==AuthMissing {
		r.mu.Lock();challenge:=r.bootstrapChallenge;r.mu.Unlock()
		if challenge {f=AuthChallenge}
	}
	StartupFailure(s,f)
}

// Observe ONLY compile-time format strings at existing log call sites, before
// the verbose flag gate. No arguments, formatted errors, or packet data enter
// this observer. The actual authorize/relay/proxy functions remain unchanged.
func ObserveStartupFormat(format string) {
	r:=startupRecorder.Load();if r==nil{return}
	switch format {
	case "[VOLGA] authorizing...":r.Record(Authorization,"begin",NoFailure)
	case "[VOLGA] auth OK: user=%d(%s) rp=%s sign=%s ts=%s":
		r.Record(Authorization,"ok",NoFailure);r.Record(RelayWorkers,"begin",NoFailure)
	case "[VOLGA] relay pool started: %d workers, batch=%d timeout=%v":r.Record(RelayWorkers,"ok",NoFailure)
	case "[TUNNEL] CreateNIC tunnel error: %v":r.Record(ProxyInit,"failure",ProxyFailure)
	}
}

func SnapshotForLog(values map[string]uint64) map[string]uint64 {
	values["schema"]=LogSchema
	return values
}
