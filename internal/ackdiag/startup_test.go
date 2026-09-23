package ackdiag

import (
	"bytes"
	"encoding/json"
	"errors"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

const syntheticSecret = "SECRET_SENTINEL"
type failingWriter struct{}
func (failingWriter) Write([]byte)(int,error){return 0,errors.New(syntheticSecret)}

// Explicit fault injection into observer stages, not network fault injection.
// relay.Start and NewTCPTunnelMode have no error return in the frozen source.
func startupScenario(name string) []string {
	var out bytes.Buffer
	r:=NewStartupRecorder(&out,&out)
	r.Record(ProcessStart,"ok",NoFailure)
	r.Record(DiagnosticInit,"begin",NoFailure)
	if name=="diagnostic_failure"{r.Record(DiagnosticInit,"failure",DiagnosticFailure);return splitStartup(out.String())}
	if name=="banner_failure"{r.Record(DiagnosticInit,"failure",SchemaFailure);return splitStartup(out.String())}
	r.Record(DiagnosticInit,"ok",NoFailure)
	r.Record(TransportStart,"begin",NoFailure)
	if name=="transport_failure"{s,f:=ClassifyTransportFailure(errors.New("generic "+syntheticSecret));r.Record(s,"failure",f);return splitStartup(out.String())}
	r.Record(Authorization,"begin",NoFailure)
	message:=""
	switch name {
	case "missing":message="auth: client-config not found in https://docs.yandex.ru/document?token="+syntheticSecret
	case "captcha":message="auth: client-config not found in https://docs.yandex.ru/showcaptcha?key="+syntheticSecret
	case "auth_other":message="auth: POST auth/initial: "+syntheticSecret
	case "long_error":message="auth: client-config not found in https://docs.yandex.ru/document?token="+strings.Repeat(syntheticSecret,4096)
	}
	if message!=""{s,f:=ClassifyTransportFailure(errors.New(message));r.Record(s,"failure",f);return splitStartup(out.String())}
	r.Record(Authorization,"ok",NoFailure);r.Record(RelayWorkers,"begin",NoFailure)
	if name=="relay_failure"{r.Record(RelayWorkers,"failure",RelayFailure);return splitStartup(out.String())}
	r.Record(RelayWorkers,"ok",NoFailure);r.Record(TransportStart,"ok",NoFailure)
	r.Record(ProxyInit,"begin",NoFailure)
	if name=="proxy_failure"{r.Record(ProxyInit,"failure",ProxyFailure);return splitStartup(out.String())}
	if name=="unknown"{r.Record(ProxyInit,"failure",UnknownFailure);return splitStartup(out.String())}
	if name=="exit_before_loop"{return splitStartup(out.String())}
	r.Record(ProxyInit,"ok",NoFailure)
	r.Record(SnapshotLoop,"ok",NoFailure)
	if name=="snapshot_failure"{r.Record(SnapshotLoop,"failure",SchemaFailure)}
	return splitStartup(out.String())
}
func splitStartup(s string) []string{return strings.Split(strings.TrimSuffix(s,"\n"),"\n")}
func decodeStartup(t *testing.T,line string) StartupEvent {
	t.Helper();var e StartupEvent
	if !strings.HasPrefix(line,"[ACK-STARTUP] "){t.Fatal("prefix")}
	if err:=json.Unmarshal([]byte(strings.TrimPrefix(line,"[ACK-STARTUP] ")),&e);err!=nil{t.Fatal(err)}
	return e
}
func TestStartupScenariosAndSecrets(t *testing.T){
	cases:=map[string]FailureClass{"success":NoFailure,"transport_failure":TransportOther,"missing":AuthMissing,"captcha":AuthMissing,"auth_other":AuthOther,"relay_failure":RelayFailure,"proxy_failure":ProxyFailure,"diagnostic_failure":DiagnosticFailure,"banner_failure":SchemaFailure,"snapshot_failure":SchemaFailure,"unknown":UnknownFailure,"long_error":AuthMissing}
	for name,want:=range cases {t.Run(name,func(t *testing.T){
		lines:=startupScenario(name)
		for i,l:=range lines {e:=decodeStartup(t,l);if e.Schema!=LogSchema||e.Ordinal!=uint64(i+1){t.Fatal("contract")};if strings.Contains(l,syntheticSecret)||strings.Contains(l,"https://"){t.Fatal("secret leak")}}
		last:=decodeStartup(t,lines[len(lines)-1]);if last.Failure!=want{t.Fatal("failure class")}
		if name=="success"&&(!last.TransportStarted||!last.AuthorizationCompleted||!last.RelayWorkersStarted||!last.ProxyInitialized||!last.SnapshotLoopStarted){t.Fatal("completion flags")}
		if name!="success"&&last.Result!="failure"{t.Fatal("missing failure")}
	})}
}
func TestStartupCAPTCHARequiresExactAuthEvidence(t *testing.T){
	for _,text:=range []string{"exit 1 captcha","auth: generic captcha","auth: client-config not found in https://evil.example/showcaptcha","auth: client-config not found in https://docs.yandex.ru/document?captcha=1","auth: client-config not found in https://yandex.ru.evil.example/showcaptcha"} {
		_,c:=ClassifyTransportFailure(errors.New(text));if c==AuthChallenge{t.Fatal("false challenge inference")}
	}
}
func TestStartupSinkFailuresRemainSafe(t *testing.T){
	var fallback bytes.Buffer
	r:=NewStartupRecorder(failingWriter{},&fallback);r.Record(DiagnosticInit,"begin",NoFailure)
	e:=decodeStartup(t,strings.TrimSpace(fallback.String()));if e.Failure!=DiagnosticFailure{t.Fatal("init sink failure")}
	fallback.Reset();r=NewStartupRecorder(failingWriter{},&fallback);r.Record(SnapshotLoop,"ok",NoFailure)
	e=decodeStartup(t,strings.TrimSpace(fallback.String()));if e.Failure!=SchemaFailure{t.Fatal("schema sink failure")}
	// Both sinks failing must not panic or change application control flow.
	r=NewStartupRecorder(failingWriter{},failingWriter{});r.Record(ProcessStart,"ok",NoFailure)
}
func TestStartupUnknownFieldsNeverSerialized(t *testing.T){
	var out bytes.Buffer;r:=NewStartupRecorder(&out,&out)
	r.Record(StartupStage(syntheticSecret),"bad",FailureClass(syntheticSecret))
	if strings.Contains(out.String(),syntheticSecret){t.Fatal("unsafe enum")}
	e:=decodeStartup(t,strings.TrimSpace(out.String()));if e.Failure!=UnknownFailure{t.Fatal("unknown")}
}
func TestStartupConcurrentSnapshotStage(t *testing.T){
	var out bytes.Buffer;r:=NewStartupRecorder(&out,&out);var wg sync.WaitGroup
	for i:=0;i<32;i++{wg.Add(1);go func(){defer wg.Done();r.Record(SnapshotLoop,"ok",NoFailure)}()};wg.Wait()
	if len(splitStartup(out.String()))!=1{t.Fatal("duplicate loop event")}
}
func TestActualFirstSnapshotWriteFailure(t *testing.T){
	var out bytes.Buffer
	previousRecorder:=startupRecorder.Load();previousLogger:=logger;previousEnabled:=enabled.Load()
	defer func(){startupRecorder.Store(previousRecorder);logger=previousLogger;enabled.Store(previousEnabled)}()
	startupRecorder.Store(NewStartupRecorder(&out,&out));logger=log.New(failingWriter{},"",0);enabled.Store(true)
	Emit()
	lines:=splitStartup(out.String());last:=decodeStartup(t,lines[len(lines)-1])
	if last.Stage!=SnapshotLoop||last.Failure!=SchemaFailure{t.Fatal("snapshot write failure not classified")}
	if strings.Contains(out.String(),syntheticSecret){t.Fatal("underlying writer error leaked")}
}
func TestExportStartupFixtures(t *testing.T){
	dir:=os.Getenv("ACK_STARTUP_FIXTURE_DIR");if dir==""{t.Skip("Linux CI export")}
	if err:=os.MkdirAll(dir,0700);err!=nil{t.Fatal(err)}
	for _,name:=range []string{"success","transport_failure","missing","captcha","auth_other","relay_failure","proxy_failure","diagnostic_failure","banner_failure","snapshot_failure","exit_before_loop","unknown","long_error"}{
		lines:=startupScenario(name)
		if name=="success" {
			lines=append(lines,"written by p1neappleXpress")
			values:=SnapshotForLog(New(time.Now).Snapshot());values["ws_connected"]=1
			b,err:=json.Marshal(values);if err!=nil{t.Fatal(err)}
			lines=append(lines,"2026/09/23 14:00:00.000000 [ACK-DIAG] "+string(b))
		}
		b,err:=json.Marshal(lines);if err!=nil{t.Fatal(err)}
		if err=os.WriteFile(filepath.Join(dir,name+".json"),b,0600);err!=nil{t.Fatal(err)}
	}
}
