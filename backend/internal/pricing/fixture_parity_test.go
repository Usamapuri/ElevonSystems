package pricing

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

// TestFixtureParity fails the build if the Go and TS suites ever load
// different bytes: a change to one fixture without the other must not slip
// through. Both paths are relative so this test runs the same from a local
// checkout or CI.
func TestFixtureParity(t *testing.T) {
	goPath := filepath.Join("testdata", "pricing_fixture.json")
	tsPath := filepath.Join("..", "..", "..", "frontend", "src", "lib", "pricing_fixture.json")

	goBytes, err := os.ReadFile(goPath)
	if err != nil {
		t.Fatalf("read %s: %v", goPath, err)
	}
	tsBytes, err := os.ReadFile(tsPath)
	if err != nil {
		t.Fatalf("read %s: %v", tsPath, err)
	}

	goSum := sha256.Sum256(goBytes)
	tsSum := sha256.Sum256(tsBytes)
	if goSum != tsSum {
		t.Fatalf(
			"pricing fixture drift: %s (sha256 %s) != %s (sha256 %s) — copy one over the other, do not edit only one side",
			goPath, hex.EncodeToString(goSum[:]), tsPath, hex.EncodeToString(tsSum[:]),
		)
	}
}
