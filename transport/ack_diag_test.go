package transport

import (
	"bytes"
	"encoding/binary"
	"testing"
	"time"

	"universal-bypass-tool/internal/ackdiag"
)

type ackObservedWire struct { testTransport; observation *ackdiag.HTTPObservation }
func(w *ackObservedWire) Send(data []byte) error {
	cp:=append([]byte(nil),data...)
	ackdiag.Move(data,cp);ackdiag.Enqueue(cp)
	w.observation=ackdiag.HTTPStart([][]byte{cp});w.sent=cp;return nil
}
func ackSyntheticPacket(seq,ack uint32,size int,flags byte,reverse bool) []byte {
	p:=make([]byte,40+size);p[0]=0x45;p[9]=6;p[15]=1;p[19]=2;p[21]=3;p[23]=4
	if reverse{p[15],p[19]=p[19],p[15];p[21],p[23]=p[23],p[21]}
	binary.BigEndian.PutUint16(p[2:4],uint16(len(p)));binary.BigEndian.PutUint32(p[24:28],seq);binary.BigEndian.PutUint32(p[28:32],ack)
	p[32]=0x50;p[33]=flags;return p
}
func TestAckIdentityThroughRealLegacyAESWrappers(t *testing.T) {
	ackdiag.Enable()
	exitWire,clientWire:=&ackObservedWire{},&ackObservedWire{}
	exitAES,err:=NewEncryptedTransport(exitWire,"synthetic unit test secret only","synthetic-context",true);if err!=nil{t.Fatal(err)}
	clientAES,err:=NewEncryptedTransport(clientWire,"synthetic unit test secret only","synthetic-context",false);if err!=nil{t.Fatal(err)}
	exitStack,clientStack:=NewCompressedTransport(exitAES),NewCompressedTransport(clientAES)
	ackdiag.Outgoing(ackSyntheticPacket(123,0,0,2,false))
	packet:=ackSyntheticPacket(124,0,300,16,false);ackdiag.Outgoing(packet)
	if err:=exitStack.Send(packet);err!=nil{t.Fatal(err)}
	if len(exitWire.sent)<5||string(exitWire.sent[:3])!="OFX"{t.Fatal("Legacy/AES wire order changed")}
	ackdiag.HTTPDone(exitWire.observation,true,204,false)
	var received []byte
	clientStack.Receive(func(p []byte){received=append([]byte(nil),p...)})
	clientWire.deliver(exitWire.sent)
	if !bytes.Equal(received,packet){t.Fatal("wire payload changed")}
	ackPacket:=ackSyntheticPacket(0,424,0,16,true)
	if err:=clientStack.Send(ackPacket);err!=nil{t.Fatal(err)}
	ackdiag.HTTPDone(clientWire.observation,true,204,false)
	exitStack.Receive(func(p []byte){if !bytes.Equal(p,ackPacket){t.Fatal("return ACK changed")};ackdiag.BeforeInject(ackdiag.Incoming(p))})
	ackdiag.Inbound(clientWire.sent,time.Now());exitWire.deliver(clientWire.sent);ackdiag.Forget(clientWire.sent)
	m:=ackdiag.Default.Snapshot()
	if m["unique_downlink_tcp_bytes"]!=300||m["acked_unique_downlink_tcp_bytes"]!=300||m["http_end_to_ack_count"]!=1||m["correlation_valid"]!=1{t.Fatal("identity lost across real encryption/compression")}
}
