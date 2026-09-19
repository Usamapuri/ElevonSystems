package database_test

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"

	"elevon-backend/internal/database"
	"elevon-backend/migrations"
)

// Spec §5.8: every migration must be re-runnable. This pins the forms we
// accept; anything else fails the build rather than a deploy.
func TestMigrations_UseIdempotentDDL(t *testing.T) {
	names, err := database.MigrationNames(migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	addConstraint := regexp.MustCompile(`(?i)ADD CONSTRAINT\s+(\w+)`)
	for _, name := range names {
		body, _ := fs.ReadFile(migrations.FS, name)
		src := string(body)
		upper := strings.ToUpper(src)
		for i, raw := range strings.Split(src, "\n") {
			line := strings.ToUpper(strings.TrimSpace(raw))
			switch {
			case strings.HasPrefix(line, "CREATE TABLE") && !strings.Contains(line, "IF NOT EXISTS"):
				t.Errorf("%s:%d CREATE TABLE without IF NOT EXISTS", name, i+1)
			case (strings.HasPrefix(line, "CREATE INDEX") || strings.HasPrefix(line, "CREATE UNIQUE INDEX")) && !strings.Contains(line, "IF NOT EXISTS"):
				t.Errorf("%s:%d CREATE INDEX without IF NOT EXISTS", name, i+1)
			case strings.Contains(line, "ADD COLUMN") && !strings.Contains(line, "IF NOT EXISTS"):
				t.Errorf("%s:%d ADD COLUMN without IF NOT EXISTS", name, i+1)
			case strings.HasPrefix(line, "INSERT INTO") && !strings.Contains(upper, "ON CONFLICT"):
				t.Errorf("%s:%d INSERT without ON CONFLICT", name, i+1)
			}
		}
		for _, m := range addConstraint.FindAllStringSubmatch(src, -1) {
			if !strings.Contains(upper, "DROP CONSTRAINT IF EXISTS "+strings.ToUpper(m[1])) {
				t.Errorf("%s: ADD CONSTRAINT %s without a preceding DROP CONSTRAINT IF EXISTS", name, m[1])
			}
		}
	}
}
