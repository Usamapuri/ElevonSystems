package dayops

import (
	_ "embed"
	"strings"
	"testing"
)

//go:embed service.go
var serviceSource string

// funcRange returns the [start, end) line range of the named top-level
// function's declaration+body: from its `func` line to the next line that
// starts a new top-level declaration.
func funcRange(t *testing.T, name string) (int, int) {
	t.Helper()
	lines := strings.Split(serviceSource, "\n")
	start := -1
	for i, line := range lines {
		if strings.HasPrefix(line, "func "+name+"(") {
			start = i
			break
		}
	}
	if start < 0 {
		t.Fatalf("service.go no longer declares func %s — the no-auto-open contract has nothing to guard", name)
	}
	for j := start + 1; j < len(lines); j++ {
		if strings.HasPrefix(lines[j], "func ") {
			return start, j
		}
	}
	return start, len(lines)
}

func sourceLines() []string { return strings.Split(serviceSource, "\n") }

// Spec §6.7 / CLAUDE.md invariant 6: the invoice path never opens a business
// day. Auto-opening has to invent an opening float, and every cash figure for
// the following 24 hours is measured against that invented number, so the
// shortage it produces at close is indistinguishable from a real one. The
// honest answer is to block the sale until a human declares the float.
func TestEnsureOpenDayForInvoice_NeverInsertsABusinessDay(t *testing.T) {
	start, end := funcRange(t, "EnsureOpenDayForInvoice")
	lines := sourceLines()
	for i := start; i < end; i++ {
		if strings.Contains(strings.ToUpper(lines[i]), "INSERT INTO BUSINESS_DAYS") {
			t.Fatalf("service.go:%d — EnsureOpenDayForInvoice must never create a business day: %s",
				i+1, strings.TrimSpace(lines[i]))
		}
	}
}

// The same rule from the other side: Open is the only function in the package
// that may create a business day. A helper that inserted one and was called
// from the invoice path would slip past the body scan above.
func TestOpen_IsTheOnlyInsertOfABusinessDay(t *testing.T) {
	openStart, openEnd := funcRange(t, "Open")
	for i, line := range sourceLines() {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "//") {
			continue // prose about the rule, not code
		}
		if !strings.Contains(strings.ToUpper(trimmed), "INSERT INTO BUSINESS_DAYS") {
			continue
		}
		if i < openStart || i >= openEnd {
			t.Errorf("service.go:%d inserts a business day outside Open: %s", i+1, trimmed)
		}
	}
}

// The four branches of §6.7, pinned in source so a refactor cannot quietly
// drop one. Branch 3 in particular must stay an UPDATE of the existing row:
// a late sale reattaches to the day it belongs to, it does not spawn a second
// row for the same date.
func TestEnsureOpenDayForInvoice_KeepsTheFourBranches(t *testing.T) {
	start, end := funcRange(t, "EnsureOpenDayForInvoice")
	body := strings.Join(sourceLines()[start:end], "\n")
	for _, want := range []string{
		"ErrPreviousDayOpen",   // branch 2
		"ErrDayNotOpen",        // branch 4
		"UPDATE business_days", // branch 3 — reopen for late sale
		"reopen_for_sale",      // ...with its audit row
	} {
		if !strings.Contains(body, want) {
			t.Errorf("EnsureOpenDayForInvoice no longer mentions %q — one of the four §6.7 branches is missing", want)
		}
	}
}
