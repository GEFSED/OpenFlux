package utils

import (
	"io"
	"log"
	"os"
)

// Only logging changes: preserve error returns and log.Fatal's exit semantics.
func ConfigureAckDiagnosticLogging() {
	log.SetOutput(ackRedactedWriter{out:os.Stderr})
}
type ackRedactedWriter struct{ out io.Writer }
func (w ackRedactedWriter) Write(p []byte)(int,error) {
	_,err:=io.WriteString(w.out,"[ACK-LOG] suppressed=1\n");return len(p),err
}
