package reports

import (
	"testing"
	"time"

	"elevon-backend/internal/testdb"
)

func TestLoadPeriodSummaryTotals(t *testing.T) {
	db := testdb.Fresh(t)
	seedPeriod(t, db)

	s, err := LoadPeriodSummary(ctx(), db, testRange())
	if err != nil {
		t.Fatalf("LoadPeriodSummary: %v", err)
	}

	if s.From != day1 || s.To != day3 {
		t.Errorf("window = %s…%s, want %s…%s", s.From, s.To, day1, day3)
	}
	eqInt(t, "invoices", s.Invoices, 5)
	eqInt(t, "voids", s.Voids, 1)
	eq(t, "kg", s.KgSold, 16.001)
	eq(t, "gross", s.Gross, 4020.25)
	eq(t, "discount", s.Discount, 20)
	eq(t, "taxable", s.Taxable, 4000.25)
	eq(t, "tax", s.Tax, 702)
	eq(t, "further tax", s.FurtherTax, 5.01)
	eq(t, "rounding", s.Rounding, -0.26)
	eq(t, "net", s.Net, 4707)

	eq(t, "tender cash", s.Tenders.Cash, 1652)
	eq(t, "tender card", s.Tenders.Card, 590)
	eq(t, "tender online", s.Tenders.Online, 105)
	eq(t, "tender on-account", s.Tenders.OnAccount, 2360)

	eq(t, "receipts cash", s.Receipts.Cash, 500)
	eq(t, "receipts card", s.Receipts.Card, 1000)
	eq(t, "receipts online", s.Receipts.Online, 0)
	eq(t, "receipts total", s.ReceiptsTotal, 1500)

	// The identity that makes the vocabulary coherent: the rounded rupee
	// figure the customer paid is the taxable base plus both taxes plus the
	// rounding step. If this ever fails, one of the six sums is defined
	// wrong, not just reported wrong.
	eq(t, "taxable+tax+further+rounding", s.Taxable+s.Tax+s.FurtherTax+s.Rounding, s.Net)
	// And the tender split is a partition of net, on-account included.
	eq(t, "tender split", s.Tenders.Cash+s.Tenders.Card+s.Tenders.Online+s.Tenders.OnAccount, s.Net)
}

// A voided invoice contributes to the void count and to nothing else; a
// voided receipt contributes to nothing at all.
func TestLoadPeriodSummaryExcludesVoids(t *testing.T) {
	db := testdb.Fresh(t)
	seedPeriod(t, db)

	only := Range{From: mustDate(day1), To: mustDate(day1)}
	s, err := LoadPeriodSummary(ctx(), db, only)
	if err != nil {
		t.Fatalf("LoadPeriodSummary: %v", err)
	}
	eqInt(t, "invoices", s.Invoices, 2)
	eqInt(t, "voids", s.Voids, 1)
	// INV-0003 would have added 300 gross, 54 tax, 354 net and 1.200 kg.
	eq(t, "gross", s.Gross, 1520)
	eq(t, "tax", s.Tax, 270)
	eq(t, "net", s.Net, 1770)
	eq(t, "kg", s.KgSold, 6.000)
	// REC-0002 (online 250) is voided, so the online receipts column is bare.
	eq(t, "receipts online", s.Receipts.Online, 0)
	eq(t, "receipts total", s.ReceiptsTotal, 500)
}

func TestLoadPeriodSummaryEmptyWindow(t *testing.T) {
	db := testdb.Fresh(t)
	seedPeriod(t, db)

	quiet := Range{From: mustDate("2026-04-01"), To: mustDate("2026-04-07")}
	s, err := LoadPeriodSummary(ctx(), db, quiet)
	if err != nil {
		t.Fatalf("LoadPeriodSummary: %v", err)
	}
	if s.From != "2026-04-01" || s.To != "2026-04-07" {
		t.Errorf("window = %s…%s", s.From, s.To)
	}
	eqInt(t, "invoices", s.Invoices, 0)
	eq(t, "net", s.Net, 0)
	eq(t, "receipts total", s.ReceiptsTotal, 0)
}

func TestDailyRowsAndUnionOfActivity(t *testing.T) {
	db := testdb.Fresh(t)
	seedPeriod(t, db)

	rows, err := Daily(ctx(), db, wideRange())
	if err != nil {
		t.Fatalf("Daily: %v", err)
	}
	// 2026-03-08, 03-13 and 03-14 had no activity at all and must not appear.
	if len(rows) != 4 {
		t.Fatalf("Daily returned %d rows, want 4: %+v", len(rows), rows)
	}
	want := []string{dayReceiptsOnly, day1, day2, day3}
	for i, w := range want {
		if rows[i].BusinessDate != w {
			t.Fatalf("row %d is %s, want %s", i, rows[i].BusinessDate, w)
		}
	}
	if rows[1].Label != "10-03-2026" {
		t.Errorf("label = %q, want 10-03-2026", rows[1].Label)
	}

	// A date whose only activity was a payment against account still gets a
	// row, with no invoices on it.
	eqInt(t, "receipts-only invoices", rows[0].Invoices, 0)
	eq(t, "receipts-only net", rows[0].Net, 0)
	eq(t, "receipts-only receipts", rows[0].ReceiptsTotal, 300)

	eq(t, day2+" net", rows[2].Net, 2465)
	eq(t, day2+" further tax", rows[2].FurtherTax, 5.01)
	eq(t, day2+" rounding", rows[2].Rounding, -0.26)
	eq(t, day3+" net", rows[3].Net, 472)

	// The daily rows sum to the period summary over the same window.
	s, err := LoadPeriodSummary(ctx(), db, wideRange())
	if err != nil {
		t.Fatalf("LoadPeriodSummary: %v", err)
	}
	var net, gross, receipts float64
	var invoices, voids int
	for _, r := range rows {
		net += r.Net
		gross += r.Gross
		receipts += r.ReceiptsTotal
		invoices += r.Invoices
		voids += r.Voids
	}
	eq(t, "Σ daily net", net, s.Net)
	eq(t, "Σ daily gross", gross, s.Gross)
	eq(t, "Σ daily receipts", receipts, s.ReceiptsTotal)
	eqInt(t, "Σ daily invoices", invoices, s.Invoices)
	eqInt(t, "Σ daily voids", voids, s.Voids)
}

// The invariant this package exists to protect: the figures a closed day
// sealed onto its business_days row — the ones the Z-report printed and
// handed to the owner — are the same figures the daily report shows for
// that date. If reports and dayops ever disagree, the owner is holding two
// pieces of paper that contradict each other.
func TestSealedDayEqualsDailyRow(t *testing.T) {
	db := testdb.Fresh(t)
	f := seedPeriod(t, db)
	sealDay(t, db, f.day1, f.cashier1, -50)
	sealDay(t, db, f.day2, f.cashier1, 0)

	rows, err := Daily(ctx(), db, testRange())
	if err != nil {
		t.Fatalf("Daily: %v", err)
	}
	byDate := map[string]DailyRow{}
	for _, r := range rows {
		byDate[r.BusinessDate] = r
	}

	for _, dateKey := range []string{day1, day2} {
		row, ok := byDate[dateKey]
		if !ok {
			t.Fatalf("no daily row for %s", dateKey)
		}
		var (
			gross, discounts, taxCollected, net float64
			onAccount, receipts                 float64
			invoiceCount, voidCount             int
		)
		if err := db.QueryRow(`
			SELECT gross_sales::float8, discounts::float8, tax_collected::float8, net_sales::float8,
			       on_account_sales::float8, receipts_collected::float8, invoice_count, void_count
			FROM business_days WHERE business_date = $1::date`, dateKey).Scan(
			&gross, &discounts, &taxCollected, &net, &onAccount, &receipts, &invoiceCount, &voidCount); err != nil {
			t.Fatalf("read sealed %s: %v", dateKey, err)
		}

		eq(t, dateKey+" sealed gross", gross, row.Gross)
		eq(t, dateKey+" sealed discounts", discounts, row.Discount)
		// dayops seals sales tax and further tax as one figure; this package
		// keeps them apart, so the comparison adds them back together.
		eq(t, dateKey+" sealed tax", taxCollected, row.Tax+row.FurtherTax)
		eq(t, dateKey+" sealed net", net, row.Net)
		eq(t, dateKey+" sealed on-account", onAccount, row.Tenders.OnAccount)
		eq(t, dateKey+" sealed receipts", receipts, row.ReceiptsTotal)
		eqInt(t, dateKey+" sealed invoice count", invoiceCount, row.Invoices)
		eqInt(t, dateKey+" sealed void count", voidCount, row.Voids)
	}
}

// A range whose To precedes its From is empty, not an error and not a
// silently swapped window.
func TestReversedRangeIsEmpty(t *testing.T) {
	db := testdb.Fresh(t)
	seedPeriod(t, db)

	rows, err := Daily(ctx(), db, Range{From: mustDate(day3), To: mustDate(day1)})
	if err != nil {
		t.Fatalf("Daily: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("reversed range returned %d rows", len(rows))
	}
}

// Range.Label is the DD-MM-YYYY form §6.8 asks responses to carry.
func TestRangeLabel(t *testing.T) {
	r := Range{From: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), To: time.Date(2026, 3, 31, 0, 0, 0, 0, time.UTC)}
	if got := r.Label(); got != "01-03-2026 — 31-03-2026" {
		t.Fatalf("Label() = %q", got)
	}
}
