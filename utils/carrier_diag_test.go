package utils

import (
	"bytes"
	"log"
	"regexp"
	"strings"
	"testing"
)

func TestCarrierDiagnosticsNeverFormatArguments(t *testing.T) {
	var output bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&output)
	defer log.SetOutput(previous)
	formats := []string{
		"[VOLGA] WS connected: user=%s",
		"[VOLGA] WS error: %v",
		"[VOLGA] WS reconnect in %v",
		"[VOLGA] transport started: user=%d(%s) rp=%s",
		"[VOLGA] batch send failed: %v",
		"[PANIC] recovered in %s: %v",
	}
	for _, format := range formats {
		output.Reset()
		Debugf(format, "synthetic-secret-sentinel", "synthetic-document-sentinel", "synthetic-cookie-sentinel")
		line := strings.TrimSpace(output.String())
		if !regexp.MustCompile(`\[CARRIER-DIAG\] [a-z_]+=1$`).MatchString(line) {
			t.Fatalf("unexpected carrier schema: %q", line)
		}
		if strings.Contains(line, "sentinel") {
			t.Fatal("format argument leaked")
		}
	}
	output.Reset()
	Debugf("[VOLGA] authorize(%s)", "synthetic-secret-sentinel")
	Debugf("[VOLGA] auth OK: user=%d(%s) rp=%s sign=%s ts=%s", 1, "sentinel", "sentinel", "sentinel", "sentinel")
	if output.Len() != 0 {
		t.Fatal("non-allowlisted debug output was enabled")
	}
}
