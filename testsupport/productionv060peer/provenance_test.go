package productionv060peer

import (
	"bytes"
	"crypto/sha1"
	"fmt"
	"os"
	"testing"
)

// Git blob identities were read from exact production commit 081d214.
// Reversing only the package rename must reproduce the original byte content,
// including compressor.go's missing final newline. Never gofmt these copies.
func TestFrozenProductionSources(t *testing.T) {
	for name, want := range map[string]string{
		"encrypted.go":  "967b8667ba3f19c65de42b582b620c51748afad9",
		"compressor.go": "b1ca6d4fd6fc836ba64f03eebb04d6f9c06a7e77",
	} {
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		data = bytes.Replace(data, []byte("package productionv060peer"), []byte("package transport"), 1)
		blob := append([]byte(fmt.Sprintf("blob %d\x00", len(data))), data...)
		if got := fmt.Sprintf("%x", sha1.Sum(blob)); got != want {
			t.Fatalf("frozen production source %s changed: %s", name, got)
		}
	}
}
