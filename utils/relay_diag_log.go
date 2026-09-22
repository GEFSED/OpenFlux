package utils

import (
	"encoding/json"
	"io"
	"log"
	"os"
	"sync/atomic"
)

var relayDiagnosticMode atomic.Bool
var relayDiagnosticLog = log.New(os.Stderr, "", log.LstdFlags|log.Lmicroseconds)

// This diagnostic executable must never emit legacy free-form errors containing
// endpoint identifiers. It preserves error returns and Fatal's exit behavior.
func ConfigureRelayDiagnosticLogging() {
	relayDiagnosticMode.Store(true)
	log.SetOutput(relayRedactedWriter{out: os.Stderr})
}

type relayRedactedWriter struct{ out io.Writer }

func (w relayRedactedWriter) Write(p []byte) (int, error) {
	_, err := io.WriteString(w.out, "[RELAY-LOG] suppressed=1\n")
	return len(p), err
}

// Values are numeric only; call sites supply a fixed schema, never user data.
func RelayDiagnosticSnapshot(values map[string]uint64) {
	encoded, err := json.Marshal(values)
	if err == nil {
		relayDiagnosticLog.Printf("[RELAY-DIAG] %s", encoded)
	}
}
