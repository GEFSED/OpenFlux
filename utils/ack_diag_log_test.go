package utils

import ("bytes";"testing")

func TestAckLogsDiscardAllFreeFormValues(t *testing.T) {
	var out bytes.Buffer
	p:=[]byte("synthetic secret URL token cookie address port seq ack payload")
	n,err:=(ackRedactedWriter{out:&out}).Write(p)
	if n!=len(p)||err!=nil||out.String()!="[ACK-LOG] suppressed=1\n"{t.Fatal("unsafe log output")}
}
