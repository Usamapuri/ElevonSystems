package database

import (
	"strings"
	"testing"
)

func TestInjectBusinessTimezoneOption_URLForm(t *testing.T) {
	got := injectBusinessTimezoneOption("postgres://u:p@host:5432/db?sslmode=disable")
	if !strings.Contains(got, "options=-c+TimeZone%3DAsia%2FKarachi") {
		t.Fatalf("URL DSN must carry the TimeZone option, got %s", got)
	}
}

func TestInjectBusinessTimezoneOption_KeywordForm(t *testing.T) {
	got := injectBusinessTimezoneOption("host=h user=u dbname=d")
	if !strings.Contains(got, "options='-c TimeZone=Asia/Karachi'") {
		t.Fatalf("keyword DSN must carry the TimeZone option, got %s", got)
	}
}

func TestInjectBusinessTimezoneOption_RespectsOperatorOptions(t *testing.T) {
	in := "postgres://u:p@host/db?options=-c%20TimeZone%3DUTC"
	if got := injectBusinessTimezoneOption(in); got != in {
		t.Fatalf("operator-set options must not be clobbered, got %s", got)
	}
}
