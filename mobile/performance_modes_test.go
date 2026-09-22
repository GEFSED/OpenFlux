package mobile

import (
 "reflect"
 "strings"
 "testing"
 "time"
 "openflux/transport"
 "openflux/transport/yandex"
 "openflux/utils"
)

func TestModeExactConfigs(t *testing.T) {
 speed, optimized := modeConfig("speed"), modeConfig("optimized")
 for _, tc := range []struct{name, lab string; cfg performanceConfig}{
  {"speed", "throughput_w32", speed}, {"optimized", "throughput_w32_429guard", optimized},
 } {
  frozen := referenceProfileConfig(tc.lab)
  if !reflect.DeepEqual(tc.cfg.volga,frozen.volga) || !reflect.DeepEqual(tc.cfg.outer,frozen.outer) {
   t.Fatalf("%s differs from verified Perf Lab 5386251",tc.name)
  }
  v:=tc.cfg.volga
  if v.WorkerCount!=32 || v.QueueSize!=4096 || v.BatchSize!=32 || v.BatchTimeout!=time.Millisecond || v.MaxIdleConnsPerHost!=32 || v.MaxIdleConns!=64 {
   t.Fatal("explicit candidate configuration changed")
  }
 }
 if speed.volga.RateLimit429GuardEnabled || !optimized.volga.RateLimit429GuardEnabled { t.Fatal("guard enable") }
 optimized.volga.RateLimit429GuardEnabled=false
 if !reflect.DeepEqual(speed,optimized) { t.Fatal("more than the guard enable differs") }
 standard:=modeConfig("standard")
 if standard.volga!=yandex.DefaultVolgaConfig() || standard.outer!=transport.DefaultBatchedConfig() { t.Fatal("Standard default changed") }
 for _, name := range []string{"","standard","unknown","throughput_w32","throughput_w32_429guard"} {
  if normalizeMode(name)!="standard" || modeConfig(name)!=standard { t.Fatal("default/fallback changed") }
 }
 for _, name := range []string{"standard","speed","optimized"} {
  if normalizeMode(name)!=name { t.Fatal("mode name changed") }
  for _, kind := range []string{"","yandex","oneme","mailru","cupsonline"} {
   if effectiveMode(kind,name)!="standard" { t.Fatal("non-Volga scheduling changed") }
  }
 }
}

func TestModeDiagnosticsAreSafe(t *testing.T) {
 configureLogging()
 if utils.IsVerbose() { t.Fatal("provider debug logging enabled") }
 ReadLogs()
 for _, mode := range []string{"speed","optimized"} {
  s:=newPacketSession(mode,"vyandex",8)
  v,err:=yandex.NewYandexVolgaTransportWithConfig("synthetic-private-url",transport.DefaultConfig(),modeConfig(mode).volga)
  if err!=nil { t.Fatal(err) }
  s.volga,s.ready=v,true
  installSession(s)
  got:=ReadLogs()
  for _, required:=range []string{"mode="+mode,"workers=32","queue_drops=0","http_requests=0","http_429=0","guard_waits=0","reconnects=0"} {
   if !strings.Contains(got,required) { t.Fatalf("missing %s",required) }
  }
  for _, denied:=range []string{"synthetic-private","http://","https://","Cookie","Bearer"} {
   if strings.Contains(got,denied) { t.Fatal("private data in summary") }
  }
  s.stop()
 }
 installSession(nil)
}

type pendingStartPeer struct { packetPeer; entered, release chan struct{}; stopped chan struct{} }
func(p *pendingStartPeer) Start() error { close(p.entered); <-p.release; return nil }
func(p *pendingStartPeer) Stop() error { close(p.stopped); return nil }
func TestStopDuringStart(t *testing.T) {
 s:=newPacketSession("optimized","vyandex",8)
 installSession(s)
 p:=&pendingStartPeer{entered:make(chan struct{}),release:make(chan struct{}),stopped:make(chan struct{})}
 finished:=make(chan string,1)
 go func(){finished<-finishStart(s,p)}()
 <-p.entered
 waiting:=make(chan []byte,1)
 go func(){waiting<-ReadWait(0)}()
 Stop()
 select { case <-waiting: case <-time.After(time.Second):t.Fatal("reader not cancelled") }
 close(p.release)
 if <-finished=="" {t.Fatal("cancelled startup published")}
 <-p.stopped
 if IsConnected() {t.Fatal("stopped session became connected")}
 installSession(nil)
}
