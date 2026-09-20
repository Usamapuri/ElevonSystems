package handlers

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Spec §6.9: no err.Error() reaches a client. Any handler line that builds a
// response and mentions .Error() fails the build. Log the error, send a code.
func TestHandlers_NeverSendRawErrors(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(src), "\n") {
			if (strings.Contains(line, "c.JSON(") || strings.Contains(line, "models.Fail(")) && strings.Contains(line, ".Error()") {
				t.Errorf("%s:%d sends a raw error to the client: %s", f, i+1, strings.TrimSpace(line))
			}
		}
	}
}
