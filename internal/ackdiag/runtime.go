package ackdiag

import (
	"encoding/json"
	"log"
	"os"
	"sync/atomic"
	"time"
)

var Default = New(time.Now)
var enabled atomic.Bool
var logger = log.New(os.Stderr, "", log.LstdFlags|log.Lmicroseconds)

func Enable() { enabled.Store(true) }
func Outgoing(data []byte) { if enabled.Load(){Default.Outgoing(data)} }
func Move(from,to []byte) { if enabled.Load(){Default.Move(from,to)} }
func Forget(data []byte) { if enabled.Load(){Default.Forget(data)} }
func Enqueue(data []byte) { if enabled.Load(){Default.Enqueue(data)} }
func HTTPStart(batch [][]byte) *HTTPObservation {if enabled.Load(){return Default.HTTPStart(batch)};return nil}
func HTTPDone(o *HTTPObservation,success bool,status int,bodyReadError bool) {if enabled.Load(){Default.HTTPDone(o,success,status,bodyReadError)}}
func Inbound(data []byte,stamp time.Time) {if enabled.Load(){Default.Inbound(data,stamp)}}
func Incoming(data []byte) time.Time {if enabled.Load(){return Default.Incoming(data)};return time.Time{}}
func BeforeInject(stamp time.Time) {if enabled.Load(){Default.BeforeInject(stamp)}}
func Event(name string) {if enabled.Load(){Default.Event(name)}}
func SetWSConnected(on bool) {if enabled.Load(){Default.WSConnected(on)}}
func Emit() {
	if !enabled.Load(){return}
	StartupOK(SnapshotLoop)
	values:=SnapshotForLog(Default.Snapshot())
	data,err:=json.Marshal(values)
	if err!=nil{StartupFailure(SnapshotLoop,SchemaFailure);return}
	if err=logger.Output(2,"[ACK-DIAG] "+string(data));err!=nil{StartupFailure(SnapshotLoop,SchemaFailure)}
}
