package utils

import (
	"io"
	"log"
	"os"
	"sync/atomic"
)

var ackLogging atomic.Bool

// Only logging changes: preserve error returns and log.Fatal's exit semantics.
func ConfigureAckDiagnosticLogging() {
	ackLogging.Store(true)
	log.SetOutput(ackRedactedWriter{out:os.Stderr})
}
func ackDebugOutput() io.Writer {
	if ackLogging.Load(){return ackRedactedWriter{out:os.Stderr}}
	return os.Stderr
}
type ackRedactedWriter struct{ out io.Writer }
func (w ackRedactedWriter) Write(p []byte)(int,error) {
	_,err:=io.WriteString(w.out,"[ACK-LOG] suppressed=1\n");return len(p),err
}
