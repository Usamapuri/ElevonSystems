package reports

import (
	"context"
	"database/sql"
	"math"
	"testing"
	"time"

	"elevon-backend/internal/dayops"

	"github.com/google/uuid"
)

// The seed below is the whole fixture for this package's DB tests: four
// business dates, two cashiers, two products, one credit customer, one
// voided invoice and one voided receipt. Every figure the tests assert is
// hand-computed in seedPeriod's doc comment, so a query that drifts fails
// with a number the reader can check by eye rather than against another
// query.
//
// Rows go in by direct INSERT: the invoice and receipt APIs would not let a
// test write an invoice dated four days ago against a closed day, which is
// exactly the shape a reporting window needs.

const (
	dayReceiptsOnly = "2026-03-09" // a business day whose only activity is a payment
	day1            = "2026-03-10"
	day2            = "2026-03-11"
	day3            = "2026-03-12" // still open
)

// testRange is the canonical window: day1 … day3.
func testRange() Range {
	return Range{From: mustDate(day1), To: mustDate(day3)}
}

// wideRange spans two empty dates either side of the seeded ones, so tests
// can prove a date with no activity produces no row.
func wideRange() Range {
	return Range{From: mustDate("2026-03-08"), To: mustDate("2026-03-14")}
}

func mustDate(s string) time.Time {
	d, err := time.Parse(dateLayout, s)
	if err != nil {
		panic(err)
	}
	return d
}

// fixture is what seedPeriod hands back so tests can name rows.
type fixture struct {
	day0, day1, day2, day3 uuid.UUID
	cashier1, cashier2     uuid.UUID
	product1, product2     uuid.UUID
	customer               uuid.UUID
}

func seedUser(t *testing.T, db *sql.DB, username, role string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := db.QueryRow(`INSERT INTO users (username, password_hash, first_name, last_name, role, is_active)
		VALUES ($1, 'x', 'Test', $1, $2, true) RETURNING id`, username, role).Scan(&id); err != nil {
		t.Fatalf("seed user %s: %v", username, err)
	}
	return id
}

func seedProduct(t *testing.T, db *sql.DB, name string, rate float64) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := db.QueryRow(`INSERT INTO products (name, rate) VALUES ($1, $2) RETURNING id`, name, rate).Scan(&id); err != nil {
		t.Fatalf("seed product %s: %v", name, err)
	}
	return id
}

func seedCustomer(t *testing.T, db *sql.DB, name, phone string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := db.QueryRow(`INSERT INTO customers (name, phone, credit_allowed) VALUES ($1, $2, true) RETURNING id`,
		name, phone).Scan(&id); err != nil {
		t.Fatalf("seed customer %s: %v", name, err)
	}
	return id
}

func seedDay(t *testing.T, db *sql.DB, dateKey, status string, openingCash float64) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := db.QueryRow(`INSERT INTO business_days (business_date, status, opening_cash)
		VALUES ($1::date, $2, $3) RETURNING id`, dateKey, status, openingCash).Scan(&id); err != nil {
		t.Fatalf("seed day %s: %v", dateKey, err)
	}
	return id
}

// invoiceIn is one seeded invoice. TotalAmount is derived so the seed can
// never contradict itself: subtotal − discount + tax + further tax.
type invoiceIn struct {
	number     string
	dayID      uuid.UUID
	dateKey    string
	status     string
	method     string
	cashier    uuid.UUID
	cashierRef string
	customer   *uuid.UUID
	subtotal   float64
	discount   float64
	taxRate    float64
	tax        float64
	furtherTax float64
	rounding   float64
	payable    float64
	createdAt  time.Time
}

func seedInvoice(t *testing.T, db *sql.DB, in invoiceIn) uuid.UUID {
	t.Helper()
	total := in.subtotal - in.discount + in.tax + in.furtherTax
	if math.Abs(total+in.rounding-in.payable) > 0.005 {
		t.Fatalf("seed %s is self-contradictory: total %.2f + rounding %.2f != payable %.2f",
			in.number, total, in.rounding, in.payable)
	}
	var voidedAt any
	if in.status == "voided" {
		voidedAt = in.createdAt
	}
	var id uuid.UUID
	if err := db.QueryRow(`
		INSERT INTO invoices (invoice_number, business_day_id, business_date, status,
			cashier_id, cashier_name, customer_id,
			subtotal, discount_amount, tax_rate, tax_amount, further_tax_amount,
			total_amount, rounding_adjustment, total_payable, payment_method, created_at,
			voided_at)
		VALUES ($1, $2, $3::date, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
		RETURNING id`,
		in.number, in.dayID, in.dateKey, in.status, in.cashier, in.cashierRef, in.customer,
		in.subtotal, in.discount, in.taxRate, in.tax, in.furtherTax,
		total, in.rounding, in.payable, in.method, in.createdAt, voidedAt).Scan(&id); err != nil {
		t.Fatalf("seed invoice %s: %v", in.number, err)
	}
	return id
}

func seedLine(t *testing.T, db *sql.DB, invoiceID, productID uuid.UUID, name string, qty, unitPrice, lineTotal float64) {
	t.Helper()
	if _, err := db.Exec(`
		INSERT INTO invoice_lines (invoice_id, product_id, product_name, quantity, unit_price, line_total)
		VALUES ($1, $2, $3, $4, $5, $6)`, invoiceID, productID, name, qty, unitPrice, lineTotal); err != nil {
		t.Fatalf("seed line for %s: %v", name, err)
	}
}

func seedReceipt(t *testing.T, db *sql.DB, number string, dayID uuid.UUID, dateKey string,
	customerID uuid.UUID, amount float64, method string, voided bool) uuid.UUID {
	t.Helper()
	var voidedAt any
	if voided {
		voidedAt = time.Now()
	}
	var id uuid.UUID
	if err := db.QueryRow(`
		INSERT INTO customer_receipts (receipt_number, customer_id, amount, method, business_day_id, business_date, voided_at)
		VALUES ($1, $2, $3, $4, $5, $6::date, $7) RETURNING id`,
		number, customerID, amount, method, dayID, dateKey, voidedAt).Scan(&id); err != nil {
		t.Fatalf("seed receipt %s: %v", number, err)
	}
	return id
}

func seedLedger(t *testing.T, db *sql.DB, customerID uuid.UUID, entryType string, debit, credit float64,
	dateKey string, receiptID *uuid.UUID) {
	t.Helper()
	if _, err := db.Exec(`
		INSERT INTO customer_ledger_entries (customer_id, entry_type, receipt_id, debit, credit, business_date)
		VALUES ($1, $2, $3, $4, $5, $6::date)`, customerID, entryType, receiptID, debit, credit, dateKey); err != nil {
		t.Fatalf("seed ledger %s: %v", entryType, err)
	}
}

// sealDay closes a day the way dayops.Close does: it computes the expected
// figures with dayops.ComputeExpected and writes them onto the row. Tests
// then compare those sealed columns with this package's Daily row for the
// same date — the point being that the two packages must agree, not that
// either agrees with a number typed into a test.
func sealDay(t *testing.T, db *sql.DB, dayID, closedBy uuid.UUID, cashVariance float64) dayops.Expected {
	t.Helper()
	e, err := dayops.ComputeExpected(db, dayID)
	if err != nil {
		t.Fatalf("compute expected: %v", err)
	}
	if _, err := db.Exec(`
		UPDATE business_days SET status = 'closed', closed_at = now(), closed_by = $2,
			counted_cash = $3, counted_card = $4, counted_online = $5,
			expected_cash = $6, expected_card = $7, expected_online = $8,
			cash_variance = $9, card_variance = 0, online_variance = 0,
			gross_sales = $10, discounts = $11, tax_collected = $12, net_sales = $13,
			on_account_sales = $14, receipts_collected = $15, invoice_count = $16, void_count = $17
		WHERE id = $1`,
		dayID, closedBy,
		e.Cash+cashVariance, e.Card, e.Online,
		e.Cash, e.Card, e.Online,
		cashVariance,
		e.GrossSales, e.Discounts, e.TaxCollected, e.NetSales,
		e.OnAccountSales, e.ReceiptsCollected, e.InvoiceCount, e.VoidCount); err != nil {
		t.Fatalf("seal day: %v", err)
	}
	return e
}

// seedPeriod writes the whole fixture. Hand-computed expectations:
//
//	2026-03-09  receipts only:            cash receipts 300
//	2026-03-10  INV-0001 cash   1000.00 + 18% = 1180 (p1, 4.000 kg)
//	            INV-0002 card    520.00 − 20 disc + 18% on 500 = 590 (p2, 2.000 kg)
//	            INV-0003 VOIDED  300.00 + 18% = 354 (p1, 1.200 kg) — excluded everywhere
//	            REC-0001 cash 500 · REC-0002 online 250 VOIDED — excluded
//	            → invoices 2, voids 1, kg 6.000, gross 1520, disc 20, taxable 1500,
//	              tax 270, further 0, rounding 0, net 1770; cash 1180, card 590
//	              receipts cash 500, total 500
//	2026-03-11  INV-0004 credit 2000.00 + 18% = 2360 (p1, 8.000 kg) — on account
//	            INV-0005 online  100.25 + 0% + further 5.01 = 105.26 → payable 105 (rounding −0.26)
//	            REC-0003 card 1000
//	            → invoices 2, voids 0, kg 8.401, gross 2100.25, disc 0, taxable 2100.25,
//	              tax 360, further 5.01, rounding −0.26, net 2465; online 105, on-account 2360
//	              receipts card 1000, total 1000
//	2026-03-12  INV-0006 cash    400.00 + 18% = 472 (p1, 1.600 kg), rung at 23:30 UTC
//	            → hour 4 in Asia/Karachi; day still open
//
//	window day1…day3: invoices 5, voids 1, kg 16.001, gross 4020.25, disc 20,
//	  taxable 4000.25, tax 702, further 5.01, rounding −0.26, net 4707
//	  tenders cash 1652 / card 590 / online 105 / on-account 2360
//	  receipts cash 500 / card 1000 / total 1500
func seedPeriod(t *testing.T, db *sql.DB) fixture {
	t.Helper()
	f := fixture{
		cashier1: seedUser(t, db, "ali", "counter"),
		cashier2: seedUser(t, db, "sana", "counter"),
		product1: seedProduct(t, db, "LPG Domestic", 250),
		product2: seedProduct(t, db, "LPG Commercial", 260),
		customer: seedCustomer(t, db, "Gas Traders", "03001112233"),
	}
	f.day0 = seedDay(t, db, dayReceiptsOnly, dayops.StatusClosed, 0)
	f.day1 = seedDay(t, db, day1, dayops.StatusClosed, 1000)
	f.day2 = seedDay(t, db, day2, dayops.StatusClosed, 500)
	f.day3 = seedDay(t, db, day3, dayops.StatusOpen, 500)

	at := func(dateKey string, hour, min int) time.Time {
		d := mustDate(dateKey)
		return time.Date(d.Year(), d.Month(), d.Day(), hour, min, 0, 0, time.UTC)
	}

	// 2026-03-09 — a payment against account and nothing else.
	seedReceipt(t, db, "REC-0000", f.day0, dayReceiptsOnly, f.customer, 300, "cash", false)

	// 2026-03-10
	inv1 := seedInvoice(t, db, invoiceIn{number: "INV-0001", dayID: f.day1, dateKey: day1, status: "completed",
		method: "cash", cashier: f.cashier1, cashierRef: "ali",
		subtotal: 1000, taxRate: 0.18, tax: 180, payable: 1180, createdAt: at(day1, 5, 0)})
	seedLine(t, db, inv1, f.product1, "LPG Domestic", 4.000, 250, 1000)

	inv2 := seedInvoice(t, db, invoiceIn{number: "INV-0002", dayID: f.day1, dateKey: day1, status: "completed",
		method: "card", cashier: f.cashier2, cashierRef: "sana",
		subtotal: 520, discount: 20, taxRate: 0.18, tax: 90, payable: 590, createdAt: at(day1, 6, 0)})
	seedLine(t, db, inv2, f.product2, "LPG Commercial", 2.000, 260, 520)

	inv3 := seedInvoice(t, db, invoiceIn{number: "INV-0003", dayID: f.day1, dateKey: day1, status: "voided",
		method: "cash", cashier: f.cashier1, cashierRef: "ali",
		subtotal: 300, taxRate: 0.18, tax: 54, payable: 354, createdAt: at(day1, 7, 0)})
	seedLine(t, db, inv3, f.product1, "LPG Domestic", 1.200, 250, 300)

	seedReceipt(t, db, "REC-0001", f.day1, day1, f.customer, 500, "cash", false)
	seedReceipt(t, db, "REC-0002", f.day1, day1, f.customer, 250, "online", true)

	// 2026-03-11
	inv4 := seedInvoice(t, db, invoiceIn{number: "INV-0004", dayID: f.day2, dateKey: day2, status: "completed",
		method: "credit", cashier: f.cashier1, cashierRef: "ali", customer: &f.customer,
		subtotal: 2000, taxRate: 0.18, tax: 360, payable: 2360, createdAt: at(day2, 5, 0)})
	seedLine(t, db, inv4, f.product1, "LPG Domestic", 8.000, 250, 2000)

	inv5 := seedInvoice(t, db, invoiceIn{number: "INV-0005", dayID: f.day2, dateKey: day2, status: "completed",
		method: "online", cashier: f.cashier2, cashierRef: "sana",
		subtotal: 100.25, taxRate: 0, tax: 0, furtherTax: 5.01, rounding: -0.26, payable: 105,
		createdAt: at(day2, 6, 0)})
	seedLine(t, db, inv5, f.product1, "LPG Domestic", 0.401, 250, 100.25)

	seedReceipt(t, db, "REC-0003", f.day2, day2, f.customer, 1000, "card", false)

	// 2026-03-12 — rung at 23:30 UTC the previous evening, which is 04:30 here.
	inv6 := seedInvoice(t, db, invoiceIn{number: "INV-0006", dayID: f.day3, dateKey: day3, status: "completed",
		method: "cash", cashier: f.cashier1, cashierRef: "ali",
		subtotal: 400, taxRate: 0.18, tax: 72, payable: 472,
		createdAt: time.Date(2026, 3, 11, 23, 30, 0, 0, time.UTC)})
	seedLine(t, db, inv6, f.product1, "LPG Domestic", 1.600, 250, 400)

	return f
}

// ── assertion helpers ────────────────────────────────────────────────────

const tolerance = 0.005

func eq(t *testing.T, label string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > tolerance {
		t.Errorf("%s = %.4f, want %.4f", label, got, want)
	}
}

func eqInt(t *testing.T, label string, got, want int) {
	t.Helper()
	if got != want {
		t.Errorf("%s = %d, want %d", label, got, want)
	}
}

func ctx() context.Context { return context.Background() }
