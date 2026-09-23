package main

import (
	"encoding/json"
	"errors"
	"log"
	"os"
	"os/exec"
	"strings"
	"testing"
	"universal-bypass-tool/internal/ackdiag"
	"universal-bypass-tool/utils"
)

func TestStartupExitHelper(t *testing.T) {
	name:=os.Getenv("OPENFLUX_STARTUP_TEST_HELPER");if name==""{return}
	ackdiag.BeginStartup();utils.ConfigureAckDiagnosticLogging();ackdiag.Enable()
	ackdiag.StartupOK(ackdiag.DiagnosticInit)
	ackdiag.StartupBegin(ackdiag.TransportStart)
	secret:="https://docs.yandex.ru/document?key=SECRET_SENTINEL"
	err:=errors.New(secret)
	if name=="generic" {ackdiag.StartupTransportFailure(err);log.Fatal(err)}
	utils.Debugf("[VOLGA] authorizing...")
	if name=="captcha" {err=errors.New("auth: client-config not found in https://docs.yandex.ru/showcaptcha?key=SECRET_SENTINEL");ackdiag.StartupTransportFailure(err);log.Fatal(err)}
	utils.Debugf("[VOLGA] auth OK: user=%d(%s) rp=%s sign=%s ts=%s",1,secret,secret,secret,secret)
	if name=="relay" {ackdiag.StartupFailure(ackdiag.RelayWorkers,ackdiag.RelayFailure);log.Fatal(err)}
	utils.Debugf("[VOLGA] relay pool started: %d workers, batch=%d timeout=%v",2000,20,secret)
	ackdiag.StartupOK(ackdiag.TransportStart);ackdiag.StartupBegin(ackdiag.ProxyInit)
	if name=="proxy" {utils.Debugf("[TUNNEL] CreateNIC tunnel error: %v",err);log.Fatal(err)}
	if name=="unknown" {ackdiag.StartupFailure(ackdiag.ProxyInit,ackdiag.UnknownFailure);log.Fatal(err)}
	ackdiag.StartupOK(ackdiag.ProxyInit)
	if name=="exit_before_loop" {os.Exit(1)}
	utils.EnableDebug();utils.Debugf("PRIVATE %s",secret) // still suppression, never raw.
	ackdiag.Emit()
	os.Exit(0)
}

func TestStartupOriginalExitBehaviorAndSecretSuppression(t *testing.T) {
	for _,name:=range []string{"generic","captcha","relay","proxy","unknown","exit_before_loop","success"}{t.Run(name,func(t *testing.T){
		cmd:=exec.Command(os.Args[0],"-test.run=^TestStartupExitHelper$")
		cmd.Env=append(os.Environ(),"OPENFLUX_STARTUP_TEST_HELPER="+name)
		out,err:=cmd.CombinedOutput()
		if name=="success"{if err!=nil{t.Fatal("success exit")}}else{
			var exit *exec.ExitError;if !errors.As(err,&exit)||exit.ExitCode()!=1{t.Fatal("original exit 1 not preserved")}
		}
		if strings.Contains(string(out),"SECRET_SENTINEL")||strings.Contains(string(out),"https://"){t.Fatal("unsafe output")}
		var last ackdiag.StartupEvent; snapshot:=false
		for _,line:=range strings.Split(strings.TrimSpace(string(out)),"\n"){
			if strings.HasPrefix(line,"[ACK-STARTUP] "){if json.Unmarshal([]byte(strings.TrimPrefix(line,"[ACK-STARTUP] ")),&last)!=nil{t.Fatal("event JSON")}}
			if strings.Contains(line,"[ACK-DIAG] "){snapshot=true}
		}
		if name=="success" {if !snapshot||!last.SnapshotLoopStarted{t.Fatal("first snapshot")}} else {
			if snapshot{t.Fatal("unexpected snapshot")}
			if name!="exit_before_loop"&&last.Result!="failure"{t.Fatal("missing classified failure")}
			if name=="captcha"&&last.Failure!=ackdiag.AuthChallenge{t.Fatal("captcha evidence")}
		}
	})}
}
