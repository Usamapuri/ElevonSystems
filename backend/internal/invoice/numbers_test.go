package invoice

import (
	"sync"
	"testing"
	"time"

	"elevon-backend/internal/testdb"
	"elevon-backend/internal/util"
)

func date(t *testing.T, s string) time.Time {
	t.Helper()
	d, err := time.ParseInLocation(DateLayout, s, util.BusinessLocation())
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// The format is spec §6.6 and FBR error rule 0173: alphanumerics with a dash
// in between. Receipts carry the R- prefix and their own counter.
func TestFormat(t *testing.T) {
	day := date(t, "2026-09-20")
	cases := []struct {
		prefix string
		seq    int
		want   string
	}{
		{"", 1, "20260920-001"},
		{"", 42, "20260920-042"},
		{"", 999, "20260920-999"},
		{"", 1000, "20260920-1000"}, // grows a digit rather than wrapping
		{ReceiptPrefix, 1, "R-20260920-001"},
		{ReceiptPrefix, 7, "R-20260920-007"},
	}
	for _, c := range cases {
		if got := Format(c.prefix, day, c.seq); got != c.want {
			t.Errorf("Format(%q, %d) = %q, want %q", c.prefix, c.seq, got, c.want)
		}
	}

	// A number is keyed to the business date it is handed, not to today.
	if got := Format("", date(t, "2027-01-05"), 3); got != "20270105-003" {
		t.Errorf("other date: %q", got)
	}
}

// Sequences are per business date and per document kind: two dates and the
// two counters never interfere.
func TestAllocate_PerDateAndPerKind(t *testing.T) {
	db := testdb.Fresh(t)
	monday, tuesday := date(t, "2026-09-21"), date(t, "2026-09-22")

	for _, want := range []string{"20260921-001", "20260921-002", "20260921-003"} {
		got, err := AllocateInvoiceNumber(db, monday)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("invoice number = %q, want %q", got, want)
		}
	}
	// A new date starts at 001 again.
	if got, err := AllocateInvoiceNumber(db, tuesday); err != nil || got != "20260922-001" {
		t.Fatalf("next day: %q %v", got, err)
	}
	// Receipts have their own counter, so Monday's first receipt is 001 even
	// though three invoices were issued that day.
	if got, err := AllocateReceiptNumber(db, monday); err != nil || got != "R-20260921-001" {
		t.Fatalf("receipt: %q %v", got, err)
	}
	if got, err := AllocateReceiptNumber(db, monday); err != nil || got != "R-20260921-002" {
		t.Fatalf("second receipt: %q %v", got, err)
	}
}

// The property the whole of numbering rests on: 50 allocations racing on one
// counter produce 50 distinct, dense numbers. A SELECT-then-UPDATE allocator
// fails this; the single upsert cannot.
func TestAllocate_ConcurrentIsUniqueAndDense(t *testing.T) {
	db := testdb.Fresh(t)
	day := date(t, "2026-09-23")

	const n = 50
	numbers := make([]string, n)
	errs := make([]error, n)
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			numbers[i], errs[i] = AllocateInvoiceNumber(db, day)
		}(i)
	}
	close(start)
	wg.Wait()

	seen := map[string]bool{}
	for i, err := range errs {
		if err != nil {
			t.Fatalf("allocation %d: %v", i, err)
		}
		if seen[numbers[i]] {
			t.Fatalf("number %s handed out twice", numbers[i])
		}
		seen[numbers[i]] = true
	}
	// Dense: exactly 001…050, no gaps.
	for seq := 1; seq <= n; seq++ {
		want := Format("", day, seq)
		if !seen[want] {
			t.Fatalf("missing %s from a dense run of %d", want, n)
		}
	}

	var last int
	if err := db.QueryRow(`SELECT last_value FROM invoice_number_counters WHERE business_date = $1::date`,
		day.Format(DateLayout)).Scan(&last); err != nil {
		t.Fatal(err)
	}
	if last != n {
		t.Fatalf("counter = %d, want %d", last, n)
	}
}
