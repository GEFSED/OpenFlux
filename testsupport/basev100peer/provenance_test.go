package basev100peer

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"
)

func TestFrozenBaseSources(t *testing.T) {
	data, err := os.ReadFile("PROVENANCE.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Commit string            `json:"commit"`
		Files  map[string]string `json:"sha256_before_package_rename"`
	}
	if err = json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Commit != "8566f727c8238436728758f139130cef433147b7" {
		t.Fatal("wrong reference commit")
	}
	for name, want := range manifest.Files {
		b, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		b = bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n"))
		b = bytes.Replace(b, []byte("package basev100peer"), []byte("package transport"), 1)
		h := sha256.Sum256(b)
		if hex.EncodeToString(h[:]) != want {
			t.Fatalf("frozen reference %s changed", name)
		}
	}
}
