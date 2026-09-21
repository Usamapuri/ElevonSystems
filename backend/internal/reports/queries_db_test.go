package reports

import (
	"math"
	"strconv"
	"testing"
	"time"

	"elevon-backend/internal/dayops"
	"elevon-backend/internal/pricing"
	"elevon-backend/internal/testdb"
	"elevon-backend/internal/util"

	"github.com/google/uuid"
)

func TestProductsSharesSumTo100(t *testing.T) {
	db := testdb.Fresh(t)
	f := seedPeriod(t, db)

	rows, err := Products(ctx(), db, testRange())
	if err != nil {
		t.Fatalf("Products: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d product rows, want 2: %+v", len(rows), rows)
	}

	// Biggest gross first.
	if rows[0].Name != "LPG Domestic" || rows[1].Name != "LPG Commercial" {
		t.Fatalf("order = %q, %q", rows[0].Name, rows[1].Name)
	}
	if rows[0].ProductID == nil || *rows[0].ProductID != f.product1 {
		t.Errorf("row 0 product id = %v, want %v", rows[0].ProductID, f.product1)
	}
	// INV-0001 4.000 + INV-0004 8.000 + INV-0005 0.401 + INV-0006 1.600;
	// the voided INV-0003's 1.200 kg is not there.
	eq(t, "domestic kg", rows[0].Kg, 14.001)
	eqInt(t, "domestic invoices", rows[0].Invoices, 4)
	eq(t, "domestic gross", rows[0].Gross, 3500.25)
	eq(t, "commercial kg", rows[1].Kg, 2.000)
	eqInt(t, "commercial invoices", rows[1].Invoices, 1)
	eq(t, "commercial gross", rows[1].Gross, 520)

	var share, gross float64
	for _, r := range rows {
		share += r.Share
		gross += r.Gross
	}
	eq(t, "Σ share", share, 100)

	// Product gross partitions the period's gross, which is what makes the
	// share column mean anything.
	s, err := LoadPeriodSummary(ctx(), db, testRange())
	if err != nil {
		t.Fatalf("LoadPeriodSummary: %v", err)
	}
	eq(t, "Σ product gross", gross, s.Gross)
	eq(t, "Σ product kg", rows[0].Kg+rows[1].Kg, s.KgSold)
}

func TestTaxBandsReconcile(t *testing.T) {
	db := testdb.Fresh(t)
	seedPeriod(t, db)

	bands, err := TaxBands(ctx(), db, testRange())
	if err != nil {
		t.Fatalf("TaxBands: %v", err)
	}
	if len(bands) != 2 {
		t.Fatalf("got %d bands, want 2: %+v", len(bands), bands)
	}
	// Ascending by rate, and the rate is the invoice's own snapshot as a
	// fraction.
	eq(t, "band 0 rate", bands[0].Rate, 0)
	eq(t, "band 1 rate", bands[1].Rate, 0.18)

	eqInt(t, "zero-rated invoices", bands[0].Invoices, 1)
	eq(t, "zero-rated taxable", bands[0].Taxable, 100.25)
	eq(t, "zero-rated tax", bands[0].Tax, 0)
	eq(t, "zero-rated further tax", bands[0].FurtherTax, 5.01)

	eqInt(t, "18% invoices", bands[1].Invoices, 4)
	eq(t, "18% taxable", bands[1].Taxable, 3900)
	eq(t, "18% tax", bands[1].Tax, 702)

	// The reconciliation the accountant runs: Σ band tax is Σ tax_amount over
	// the same window, and Σ band taxable is the period's taxable base.
	var tax, further, taxable float64
	var invoices int
	for _, b := range bands {
		tax += b.Tax
		further += b.FurtherTax
		taxable += b.Taxable
		invoices += b.Invoices
	}
	var ledgerTax float64
	if err := db.QueryRow(`
		SELECT COALESCE(SUM(tax_amount), 0)::float8 FROM invoices
		WHERE status = 'completed' AND business_date BETWEEN $1::date AND $2::date`,
		day1, day3).Scan(&ledgerTax); err != nil {
		t.Fatalf("Σ tax_amount: %v", err)
	}
	eq(t, "Σ band tax vs Σ invoices.tax_amount", tax, ledgerTax)

	s, err := LoadPeriodSummary(ctx(), db, testRange())
	if err != nil {
		t.Fatalf("LoadPeriodSummary: %v", err)
	}
	eq(t, "Σ band tax vs summary", tax, s.Tax)
	eq(t, "Σ band further tax vs summary", further, s.FurtherTax)
	eq(t, "Σ band taxable vs summary", taxable, s.Taxable)
	eqInt(t, "Σ band invoices vs summary", invoices, s.Invoices)
}

func TestCashiers(t *testing.T) {
	db := testdb.Fresh(t)
	f := seedPeriod(t, db)

	rows, err := Cashiers(ctx(), db, testRange())
	if err != nil {
		t.Fatalf("Cashiers: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("got %d cashier rows, want 2: %+v", len(rows), rows)
	}

	// Biggest gross first: ali rang 1000 + 2000 + 400.
	if rows[0].CashierID == nil || *rows[0].CashierID != f.cashier1 {
		t.Fatalf("row 0 cashier = %v, want %v", rows[0].CashierID, f.cashier1)
	}
	if rows[0].Name != "Test ali" {
		t.Errorf("name = %q, want %q", rows[0].Name, "Test ali")
	}
	eqInt(t, "ali invoices", rows[0].Invoices, 3)
	eq(t, "ali gross", rows[0].Gross, 3400)
	eq(t, "ali average", rows[0].Average, 1133.33)
	// The void is ali's, counted but carrying no money.
	eqInt(t, "ali voids", rows[0].Voids, 1)

	eqInt(t, "sana invoices", rows[1].Invoices, 2)
	eq(t, "sana gross", rows[1].Gross, 620.25)
	eq(t, "sana average", rows[1].Average, 310.13)
	eqInt(t, "sana voids", rows[1].Voids, 0)

	s, err := LoadPeriodSummary(ctx(), db, testRange())
	if err != nil {
		t.Fatalf("LoadPeriodSummary: %v", err)
	}
	eq(t, "Σ cashier gross", rows[0].Gross+rows[1].Gross, s.Gross)
	eqInt(t, "Σ cashier invoices", rows[0].Invoices+rows[1].Invoices, s.Invoices)
	eqInt(t, "Σ cashier voids", rows[0].Voids+rows[1].Voids, s.Voids)
}

// INV-0006 was written at 23:30 UTC. Asia/Karachi is UTC+5, so it belongs to
// hour 4 — not hour 23, which is what a UTC bucket (or a session that lost
// its TimeZone option) would say.
func TestHourlyBucketsInBusinessTimezone(t *testing.T) {
	db := testdb.Fresh(t)
	seedPeriod(t, db)

	rows, err := Hourly(ctx(), db, Range{From: mustDate(day3), To: mustDate(day3)})
	if err != nil {
		t.Fatalf("Hourly: %v", err)
	}
	if len(rows) != 24 {
		t.Fatalf("got %d hour buckets, want 24", len(rows))
	}
	for i, r := range rows {
		if r.Hour != i {
			t.Fatalf("bucket %d has hour %d", i, r.Hour)
		}
		switch i {
		case 4:
			eqInt(t, "hour 4 invoices", r.Invoices, 1)
			eq(t, "hour 4 net", r.Net, 472)
		default:
			eqInt(t, "hour "+strconv.Itoa(i)+" invoices", r.Invoices, 0)
			eq(t, "empty hour net", r.Net, 0)
		}
	}
	if util.BusinessTimezoneName() != "Asia/Karachi" {
		t.Fatalf("business timezone moved to %s — this test's 23:30 UTC → hour 4 assumption no longer holds",
			util.BusinessTimezoneName())
	}
}

// The window's five completed invoices land on three different weekdays at
// five different hours (day1=Tuesday, day2=Wednesday, day3=Thursday); the
// voided INV-0003 (Tuesday, hour 12 in Asia/Karachi) must not appear
// anywhere in the grid.
func TestHourlyHeatGridInBusinessTimezone(t *testing.T) {
	db := testdb.Fresh(t)
	seedPeriod(t, db)

	cells, err := HourlyHeat(ctx(), db, testRange())
	if err != nil {
		t.Fatalf("HourlyHeat: %v", err)
	}
	if len(cells) != 168 {
		t.Fatalf("got %d heat cells, want 168", len(cells))
	}

	byKey := make(map[[2]int]HeatCell, len(cells))
	for _, c := range cells {
		byKey[[2]int{c.Weekday, c.Hour}] = c
	}

	want := map[[2]int]struct {
		invoices int
		net      float64
	}{
		{1, 10}: {1, 1180}, // Tuesday 10:00 — INV-0001
		{1, 11}: {1, 590},  // Tuesday 11:00 — INV-0002
		{1, 12}: {0, 0},    // Tuesday 12:00 — INV-0003 is voided, excluded
		{2, 10}: {1, 2360}, // Wednesday 10:00 — INV-0004
		{2, 11}: {1, 105},  // Wednesday 11:00 — INV-0005
		{3, 4}:  {1, 472},  // Thursday 04:00 — INV-0006, rung 23:30 UTC the evening before
	}
	for key, w := range want {
		c, ok := byKey[key]
		if !ok {
			t.Fatalf("missing cell weekday=%d hour=%d", key[0], key[1])
		}
		label := "weekday=" + strconv.Itoa(key[0]) + " hour=" + strconv.Itoa(key[1])
		eqInt(t, "invoices "+label, c.Invoices, w.invoices)
		eq(t, "net "+label, c.Net, w.net)
	}

	// Every other cell in the grid is zero, and the grid's own total agrees
	// with the window's period summary — the same reconciliation Hourly's
	// own test relies on.
	var totalInvoices int
	var totalNet float64
	for _, c := range cells {
		totalInvoices += c.Invoices
		totalNet += c.Net
	}
	eqInt(t, "Σ cell invoices", totalInvoices, 5)
	eq(t, "Σ cell net", totalNet, 4707)

	if util.BusinessTimezoneName() != "Asia/Karachi" {
		t.Fatalf("business timezone moved to %s — the weekday/hour assumptions above no longer hold",
			util.BusinessTimezoneName())
	}
}

// Narrowing the range to day3 alone must exclude day1's and day2's
// invoices from the grid — the same proof TestHourlyBucketsInBusinessTimezone
// runs for the flat Hourly rows, pinned here against HourlyHeat's own
// business_date BETWEEN parameters rather than assumed from the shared SQL
// shape.
func TestHourlyHeatGridExcludesOtherDays(t *testing.T) {
	db := testdb.Fresh(t)
	seedPeriod(t, db)

	cells, err := HourlyHeat(ctx(), db, Range{From: mustDate(day3), To: mustDate(day3)})
	if err != nil {
		t.Fatalf("HourlyHeat: %v", err)
	}
	if len(cells) != 168 {
		t.Fatalf("got %d heat cells, want 168", len(cells))
	}

	var totalInvoices int
	var totalNet float64
	for _, c := range cells {
		totalInvoices += c.Invoices
		totalNet += c.Net
		switch {
		case c.Weekday == 3 && c.Hour == 4:
			// Thursday 04:00 — INV-0006, the only invoice on day3.
			eqInt(t, "hour 4 invoices", c.Invoices, 1)
			eq(t, "hour 4 net", c.Net, 472)
		default:
			// Every other cell, including Tuesday 10:00/11:00 (INV-0001,
			// INV-0002 — day1) and Wednesday 10:00/11:00 (INV-0004,
			// INV-0005 — day2), must be zero: those invoices sit outside
			// this narrowed window and must not leak in.
			if c.Invoices != 0 || c.Net != 0 {
				t.Errorf("weekday=%d hour=%d = {%d, %.2f}, want zero (outside the day3-only window)",
					c.Weekday, c.Hour, c.Invoices, c.Net)
			}
		}
	}
	eqInt(t, "Σ cell invoices", totalInvoices, 1)
	eq(t, "Σ cell net", totalNet, 472)

	if util.BusinessTimezoneName() != "Asia/Karachi" {
		t.Fatalf("business timezone moved to %s — this test's 23:30 UTC → hour 4 assumption no longer holds",
			util.BusinessTimezoneName())
	}
}

// FIFO ageing, hand-computed: an invoice of 1,000 seventy days ago, an
// invoice of 2,000 twenty days ago, a receipt of 1,200 ten days ago.
// Balance 1,800, all of it in 0–30, because the payment settles the oldest
// invoice first.
func TestReceivablesFIFOAgeing(t *testing.T) {
	db := testdb.Fresh(t)

	today := util.BusinessDate(time.Now())
	key := func(daysAgo int) string { return today.AddDate(0, 0, -daysAgo).Format(dateLayout) }

	customer := seedCustomer(t, db, "Ledger Co", "03009998877")
	settled := seedCustomer(t, db, "Settled Co", "03007776655")
	day := seedDay(t, db, key(10), dayops.StatusClosed, 0)

	seedLedger(t, db, customer, "invoice", 1000, 0, key(70), nil)
	seedLedger(t, db, customer, "invoice", 2000, 0, key(20), nil)
	receipt := seedReceipt(t, db, "REC-9001", day, key(10), customer, 1200, "cash", false)
	seedLedger(t, db, customer, "receipt", 0, 1200, key(10), &receipt)

	// A customer whose account nets to zero is not a receivable.
	seedLedger(t, db, settled, "invoice", 500, 0, key(40), nil)
	seedLedger(t, db, settled, "receipt", 0, 500, key(5), nil)

	rows, err := Receivables(ctx(), db, today)
	if err != nil {
		t.Fatalf("Receivables: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d receivable rows, want 1 (the settled account must drop out): %+v", len(rows), rows)
	}
	r := rows[0]
	if r.CustomerID != customer {
		t.Fatalf("row is customer %v, want %v", r.CustomerID, customer)
	}
	if r.Name != "Ledger Co" || r.Phone != "03009998877" {
		t.Errorf("customer = %q / %q", r.Name, r.Phone)
	}
	eq(t, "balance", r.Balance, 1800)
	eq(t, "0–30", r.B0_30, 1800)
	eq(t, "31–60", r.B31_60, 0)
	eq(t, "61–90", r.B61_90, 0)
	eq(t, "90+", r.B90, 0)
	eq(t, "buckets sum to balance", r.B0_30+r.B31_60+r.B61_90+r.B90, r.Balance)

	if r.LastReceipt == nil {
		t.Fatalf("last receipt is nil")
	}
	if *r.LastReceipt != key(10) {
		t.Errorf("last receipt = %s, want %s", *r.LastReceipt, key(10))
	}
}

// The dashboard scalar and the receivables report are two different
// implementations of the same number — one grouped SQL aggregate, one Go
// FIFO walk over every ledger row — and the owner reads them on two screens.
// This pins them together on a fixture that exercises every way they could
// drift: an account in debt, one settled to exactly zero, one in advance
// (a negative balance that must not net the others down), and a void, which
// is a reversing credit rather than a row either side excludes.
//
// Hand-computed: Owing Co has 1,000 + 2,000.55 invoiced, 1,200 receipted and
// a 400 invoice reversed by its own void — 1,800.55 outstanding. Settled Co
// is 500 in and 500 out, so it is not a receivable at all. Advance Co has
// paid 700 against 500, so it is −200 and contributes nothing.
func TestOutstandingReceivablesMatchesReportPositives(t *testing.T) {
	db := testdb.Fresh(t)

	today := util.BusinessDate(time.Now())
	key := func(daysAgo int) string { return today.AddDate(0, 0, -daysAgo).Format(dateLayout) }

	owing := seedCustomer(t, db, "Owing Co", "03001112233")
	settled := seedCustomer(t, db, "Settled Co", "03002223344")
	advance := seedCustomer(t, db, "Advance Co", "03003334455")
	day := seedDay(t, db, key(10), dayops.StatusClosed, 0)

	seedLedger(t, db, owing, "invoice", 1000, 0, key(70), nil)
	seedLedger(t, db, owing, "invoice", 2000.55, 0, key(20), nil)
	receipt := seedReceipt(t, db, "REC-9200", day, key(10), owing, 1200, "cash", false)
	seedLedger(t, db, owing, "receipt", 0, 1200, key(10), &receipt)
	seedLedger(t, db, owing, "invoice", 400, 0, key(3), nil)
	seedLedger(t, db, owing, "invoice_void", 0, 400, key(3), nil)

	seedLedger(t, db, settled, "invoice", 500, 0, key(40), nil)
	seedLedger(t, db, settled, "receipt", 0, 500, key(5), nil)

	seedLedger(t, db, advance, "invoice", 500, 0, key(10), nil)
	seedLedger(t, db, advance, "receipt", 0, 700, key(2), nil)

	rows, err := Receivables(ctx(), db, today)
	if err != nil {
		t.Fatalf("Receivables: %v", err)
	}
	var want float64
	for _, r := range rows {
		if r.Balance > 0 {
			want += r.Balance
		}
	}
	want = pricing.Round2(want)

	got, err := OutstandingReceivables(ctx(), db, today)
	if err != nil {
		t.Fatalf("OutstandingReceivables: %v", err)
	}
	eq(t, "aggregate vs report positives", got, want)
	eq(t, "aggregate", got, 1800.55)

	// asOf is respected the same way: as of the day before the last receipt,
	// Advance Co is still 500 in debt and counts.
	asOf := today.AddDate(0, 0, -3)
	rows, err = Receivables(ctx(), db, asOf)
	if err != nil {
		t.Fatalf("Receivables asOf: %v", err)
	}
	want = 0
	for _, r := range rows {
		if r.Balance > 0 {
			want += r.Balance
		}
	}
	want = pricing.Round2(want)

	got, err = OutstandingReceivables(ctx(), db, asOf)
	if err != nil {
		t.Fatalf("OutstandingReceivables asOf: %v", err)
	}
	eq(t, "aggregate vs report positives asOf", got, want)
	eq(t, "aggregate asOf", got, 2300.55)
}

// With nothing paid, each invoice ages into its own bucket — the other half
// of the FIFO story, and the one that proves the bucket boundaries.
func TestReceivablesBucketBoundaries(t *testing.T) {
	db := testdb.Fresh(t)

	today := util.BusinessDate(time.Now())
	key := func(daysAgo int) string { return today.AddDate(0, 0, -daysAgo).Format(dateLayout) }

	customer := seedCustomer(t, db, "Aged Co", "03001234567")
	seedLedger(t, db, customer, "invoice", 10, 0, key(0), nil)     // today → 0–30
	seedLedger(t, db, customer, "invoice", 20, 0, key(30), nil)    // exactly 30 → 0–30
	seedLedger(t, db, customer, "invoice", 40, 0, key(31), nil)    // 31 → 31–60
	seedLedger(t, db, customer, "invoice", 80, 0, key(60), nil)    // exactly 60 → 31–60
	seedLedger(t, db, customer, "invoice", 160, 0, key(61), nil)   // 61 → 61–90
	seedLedger(t, db, customer, "invoice", 320, 0, key(90), nil)   // exactly 90 → 61–90
	seedLedger(t, db, customer, "invoice", 640, 0, key(91), nil)   // 91 → 90+
	seedLedger(t, db, customer, "invoice", 1280, 0, key(400), nil) // very old → 90+

	rows, err := Receivables(ctx(), db, today)
	if err != nil {
		t.Fatalf("Receivables: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	r := rows[0]
	eq(t, "balance", r.Balance, 2550)
	eq(t, "0–30", r.B0_30, 30)
	eq(t, "31–60", r.B31_60, 120)
	eq(t, "61–90", r.B61_90, 480)
	eq(t, "90+", r.B90, 1920)
	if r.LastReceipt != nil {
		t.Errorf("last receipt = %v, want nil for a customer who has never paid", *r.LastReceipt)
	}
}

// A customer who has paid more than they currently owe carries a negative
// balance (an advance) rather than dropping out of the report the way a
// perfectly settled account (Balance == 0) does — an advance is still worth
// the owner's attention, just in the other direction. FIFO settles the one
// credit against the one debit in full, so every ageing bucket is empty:
// there is nothing unpaid left to age.
func TestReceivablesCustomerInAdvance(t *testing.T) {
	db := testdb.Fresh(t)

	today := util.BusinessDate(time.Now())
	key := func(daysAgo int) string { return today.AddDate(0, 0, -daysAgo).Format(dateLayout) }

	customer := seedCustomer(t, db, "Advance Co", "03005554433")
	day := seedDay(t, db, key(5), dayops.StatusClosed, 0)

	seedLedger(t, db, customer, "invoice", 500, 0, key(10), nil)
	receipt := seedReceipt(t, db, "REC-9100", day, key(5), customer, 700, "cash", false)
	seedLedger(t, db, customer, "receipt", 0, 700, key(5), &receipt)

	rows, err := Receivables(ctx(), db, today)
	if err != nil {
		t.Fatalf("Receivables: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1 (an advance is still a receivable row): %+v", len(rows), rows)
	}
	r := rows[0]
	if r.CustomerID != customer {
		t.Fatalf("row is customer %v, want %v", r.CustomerID, customer)
	}
	eq(t, "balance", r.Balance, -200)
	eq(t, "0–30", r.B0_30, 0)
	eq(t, "31–60", r.B31_60, 0)
	eq(t, "61–90", r.B61_90, 0)
	eq(t, "90+", r.B90, 0)
	if r.LastReceipt == nil || *r.LastReceipt != key(5) {
		t.Errorf("last receipt = %v, want %s", r.LastReceipt, key(5))
	}
}

func TestDayCloses(t *testing.T) {
	db := testdb.Fresh(t)
	f := seedPeriod(t, db)
	e1 := sealDay(t, db, f.day1, f.cashier1, -50)
	sealDay(t, db, f.day2, f.cashier1, 0)

	rows, err := DayCloses(ctx(), db, wideRange())
	if err != nil {
		t.Fatalf("DayCloses: %v", err)
	}
	if len(rows) != 4 {
		t.Fatalf("got %d day rows, want 4: %+v", len(rows), rows)
	}
	// Newest first.
	if rows[0].BusinessDate != day3 || rows[3].BusinessDate != dayReceiptsOnly {
		t.Fatalf("order = %s … %s", rows[0].BusinessDate, rows[3].BusinessDate)
	}
	if rows[0].Status != dayops.StatusOpen {
		t.Errorf("%s status = %q, want open", day3, rows[0].Status)
	}

	closed := rows[2] // 2026-03-10
	if closed.BusinessDate != day1 {
		t.Fatalf("row 2 is %s", closed.BusinessDate)
	}
	if closed.Status != dayops.StatusClosed {
		t.Errorf("status = %q", closed.Status)
	}
	if closed.Label != "10-03-2026" {
		t.Errorf("label = %q", closed.Label)
	}
	if closed.ClosedBy != "Test ali" {
		t.Errorf("closed by %q", closed.ClosedBy)
	}
	// Opening 1000 + cash sales 1180 + cash receipts 500 = 2680 expected,
	// counted 50 short.
	eq(t, "expected cash", closed.ExpectedCash, 2680)
	eq(t, "expected cash vs dayops", closed.ExpectedCash, e1.Cash)
	eq(t, "counted cash", closed.CountedCash, 2630)
	eq(t, "cash variance", closed.CashVariance, -50)
	eq(t, "card variance", closed.CardVariance, 0)
	eq(t, "online variance", closed.OnlineVariance, 0)
	eq(t, "net", closed.Net, 1770)
	if closed.ID == uuid.Nil {
		t.Errorf("day id is nil")
	}

	// An unsealed day is listed with its variance columns at zero rather
	// than hidden.
	eq(t, "open day net", rows[0].Net, 0)
	eq(t, "open day expected cash", rows[0].ExpectedCash, 0)
}

// Nothing in this package may window on created_at: an invoice whose
// business date and calendar date disagree (the till was open past
// midnight) must be reported on its business date.
func TestReportsWindowOnBusinessDateNotCreatedAt(t *testing.T) {
	db := testdb.Fresh(t)
	f := seedPeriod(t, db)

	// INV-0006's business_date is 2026-03-12 but its created_at is
	// 2026-03-11T23:30Z. A window covering only day2 must not see it.
	only2 := Range{From: mustDate(day2), To: mustDate(day2)}
	s, err := LoadPeriodSummary(ctx(), db, only2)
	if err != nil {
		t.Fatalf("LoadPeriodSummary: %v", err)
	}
	eqInt(t, "day2 invoices", s.Invoices, 2)
	eq(t, "day2 net", s.Net, 2465)

	only3 := Range{From: mustDate(day3), To: mustDate(day3)}
	s3, err := LoadPeriodSummary(ctx(), db, only3)
	if err != nil {
		t.Fatalf("LoadPeriodSummary: %v", err)
	}
	eqInt(t, "day3 invoices", s3.Invoices, 1)
	eq(t, "day3 net", s3.Net, 472)

	// Same for the per-cashier and per-product cuts.
	cashiers, err := Cashiers(ctx(), db, only3)
	if err != nil {
		t.Fatalf("Cashiers: %v", err)
	}
	if len(cashiers) != 1 || cashiers[0].CashierID == nil || *cashiers[0].CashierID != f.cashier1 {
		t.Fatalf("day3 cashiers = %+v", cashiers)
	}
	products, err := Products(ctx(), db, only3)
	if err != nil {
		t.Fatalf("Products: %v", err)
	}
	if len(products) != 1 || math.Abs(products[0].Share-100) > tolerance {
		t.Fatalf("day3 products = %+v", products)
	}
}
