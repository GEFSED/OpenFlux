package utils

import (
	"bytes"
	"testing"
)

func TestRelayLogDropsFreeFormData(t *testing.T) {
	var out bytes.Buffer
	input := []byte("synthetic-url secret token cookie body address Retry-After\n")
	n, err := (relayRedactedWriter{out: &out}).Write(input)
	if err != nil || n != len(input) || out.String() != "[RELAY-LOG] suppressed=1\n" {
		t.Fatal("free-form log was not completely replaced")
	}
}
