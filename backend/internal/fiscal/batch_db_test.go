package fiscal_test

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"elevon-backend/internal/fiscal"
	"elevon-backend/internal/testdb"

	"github.com/google/uuid"
)

// Combined invoices (spec 2026-10-12 §2.3). Every test here seeds plain
// walk-in sales at the sandbox rate and drives CreateBatch / SubmitBatch
// against the stub FBR.

func batchDay() time.Time { return time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC) }

type seedOpt struct {
	status   string // completed | voided
	fiscal   string // unfiled | failed | synced | off | pending
	date     string // YYYY-MM-DD
	taxRate  float64
	customer *uuid.UUID
}

// seedBatchSale inserts one 12.5 kg sale (3312.50 + 596.25 tax) on a day.
func seedBatchSale(t *testing.T, db *sql.DB, number string, o seedOpt) uuid.UUID {
	t.Helper()
	if o.status == "" {
		o.status = "completed"
	}
	if o.fiscal == "" {
		o.fiscal = "unfiled"
	}
	if o.date == "" {
		o.date = "2026-10-12"
	}
	if o.taxRate == 0 {
		o.taxRate = 0.18
	}
	var dayID uuid.UUID
	// Closed, so several dates can be seeded under the single-open-day rule.
	if err := db.QueryRow(`INSERT INTO business_days (business_date, status) VALUES ($1::date, 'closed')
		ON CONFLICT (business_date) DO UPDATE SET status = EXCLUDED.status RETURNING id`, o.date).Scan(&dayID); err != nil {
		t.Fatalf("seed day: %v", err)
	}
	var voidedAt any
	if o.status == "voided" {
		voidedAt = time.Now()
	}
	var id uuid.UUID
	if err := db.QueryRow(`INSERT INTO invoices
		(invoice_number, business_day_id, business_date, status, customer_id, subtotal, tax_rate, tax_amount,
		 total_amount, total_payable, payment_method, fiscal_status, voided_at)
		VALUES ($1, $2, $3::date, $4, $5, 3312.50, $6, 596.25, 3908.75, 3909, 'cash', $7, $8)
		RETURNING id`, number, dayID, o.date, o.status, o.customer, o.taxRate, o.fiscal, voidedAt).Scan(&id); err != nil {
		t.Fatalf("seed invoice %s: %v", number, err)
	}
	if _, err := db.Exec(`INSERT INTO invoice_lines
		(invoice_id, product_name, hs_code, quantity, unit_price, list_rate, line_total, line_discount, line_tax, sort_order)
		VALUES ($1, 'LPG bulk', '2711.1910', 12.500, 265.00, 265.00, 3312.50, 0, 596.25, 0)`, id); err != nil {
		t.Fatalf("seed line: %v", err)
	}
	return id
}

func invoiceFiscal(t *testing.T, db *sql.DB, id uuid.UUID) (status string, number sql.NullString, batch uuid.NullUUID) {
	t.Helper()
	if err := db.QueryRow(`SELECT fiscal_status, fiscal_invoice_number, fiscal_batch_id FROM invoices WHERE id = $1`, id).
		Scan(&status, &number, &batch); err != nil {
		t.Fatal(err)
	}
	return
}

func acceptingStub(t *testing.T) *stub {
	return newStub(t, fixtureBody(t, "resp_caseC.json"), fixtureBody(t, "resp_postA.json"))
}

func TestCreateBatch_ClaimsAndTotals(t *testing.T) {
	sandboxEnv(t)
	db := testdb.Fresh(t)
	enableFiscal(t, db, nil, true)
	cfg, _ := fiscal.LoadConfig(db)
	ids := []uuid.UUID{
		seedBatchSale(t, db, "20261012-001", seedOpt{}),
		seedBatchSale(t, db, "20261012-002", seedOpt{fiscal: "failed"}),
		seedBatchSale(t, db, "20261012-003", seedOpt{}),
	}

	b, err := fiscal.CreateBatch(db, batchDay(), ids, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if b.Status != "pending" || b.InvoiceCount != 3 || b.TotalKg != 37.5 ||
		b.ValueExclTax != 9937.50 || b.TaxAmount != 1788.75 || b.FurtherTaxAmount != 0 || b.TotalAmount != 11726.25 {
		t.Fatalf("batch totals: %+v", b)
	}
	for _, id := range ids {
		status, _, batch := invoiceFiscal(t, db, id)
		if status != "pending" || !batch.Valid || batch.UUID != b.ID {
			t.Fatalf("invoice %s claimed: %s %v", id, status, batch)
		}
	}
}

func TestCreateBatch_RefusesWholeRequest(t *testing.T) {
	sandboxEnv(t)
	db := testdb.Fresh(t)
	enableFiscal(t, db, nil, true)
	cfg, _ := fiscal.LoadConfig(db)
	registered := seedCustomer(t, db, "Ali Traders", "1234567", "Registered")
	good := seedBatchSale(t, db, "20261012-001", seedOpt{})
	cases := map[string]uuid.UUID{
		"voided":           seedBatchSale(t, db, "20261012-002", seedOpt{status: "voided"}),
		"already synced":   seedBatchSale(t, db, "20261012-003", seedOpt{fiscal: "synced"}),
		"registered buyer": seedBatchSale(t, db, "20261012-004", seedOpt{customer: &registered}),
		"other date":       seedBatchSale(t, db, "20261011-001", seedOpt{date: "2026-10-11"}),
		"other tax rate":   seedBatchSale(t, db, "20261012-005", seedOpt{taxRate: 0.17}),
		"fiscal was off":   seedBatchSale(t, db, "20261012-006", seedOpt{fiscal: "off"}),
		"unknown id":       uuid.New(),
	}
	for name, bad := range cases {
		_, err := fiscal.CreateBatch(db, batchDay(), []uuid.UUID{good, bad}, cfg, nil)
		var inel *fiscal.IneligibleError
		if !errors.As(err, &inel) || len(inel.IDs) != 1 || inel.IDs[0] != bad {
			t.Fatalf("%s: err = %v, want IneligibleError{%s}", name, err, bad)
		}
		var n int
		if err := db.QueryRow(`SELECT count(*) FROM fiscal_batches`).Scan(&n); err != nil || n != 0 {
			t.Fatalf("%s: no batch may be written: %d %v", name, n, err)
		}
		if status, _, batch := invoiceFiscal(t, db, good); status != "unfiled" || batch.Valid {
			t.Fatalf("%s: the good invoice must be untouched: %s %v", name, status, batch)
		}
	}
	if _, err := fiscal.CreateBatch(db, batchDay(), nil, cfg, nil); !errors.Is(err, fiscal.ErrBatchEmpty) {
		t.Fatalf("empty: %v", err)
	}
}

// Review Focus 1: two admins file overlapping selections at once.
func TestCreateBatch_ConcurrentOverlap(t *testing.T) {
	sandboxEnv(t)
	db := testdb.Fresh(t)
	enableFiscal(t, db, nil, true)
	cfg, _ := fiscal.LoadConfig(db)
	a := seedBatchSale(t, db, "20261012-001", seedOpt{})
	b := seedBatchSale(t, db, "20261012-002", seedOpt{})
	c := seedBatchSale(t, db, "20261012-003", seedOpt{})

	var wg sync.WaitGroup
	start := make(chan struct{})
	results := make([]error, 2)
	for i, sel := range [][]uuid.UUID{{a, b}, {b, c}} {
		wg.Add(1)
		go func(i int, sel []uuid.UUID) {
			defer wg.Done()
			<-start
			_, results[i] = fiscal.CreateBatch(db, batchDay(), sel, cfg, nil)
		}(i, sel)
	}
	close(start)
	wg.Wait()

	ok, refused := 0, 0
	for _, err := range results {
		var inel *fiscal.IneligibleError
		switch {
		case err == nil:
			ok++
		case errors.As(err, &inel) && len(inel.IDs) == 1 && inel.IDs[0] == b:
			refused++
		default:
			t.Fatalf("unexpected: %v", err)
		}
	}
	if ok != 1 || refused != 1 {
		t.Fatalf("one wins, one is refused on B: ok=%d refused=%d", ok, refused)
	}
	var claimed int
	if err := db.QueryRow(`SELECT count(*) FROM invoices WHERE fiscal_batch_id IS NOT NULL`).Scan(&claimed); err != nil || claimed != 2 {
		t.Fatalf("exactly the winner's two invoices are claimed: %d %v", claimed, err)
	}
}

func TestSubmitBatch_Synced(t *testing.T) {
	sandboxEnv(t)
	db := testdb.Fresh(t)
	s := acceptingStub(t)
	enableFiscal(t, db, s, true)
	cfg, _ := fiscal.LoadConfig(db)
	ids := []uuid.UUID{seedBatchSale(t, db, "20261012-001", seedOpt{}), seedBatchSale(t, db, "20261012-002", seedOpt{})}
	b, err := fiscal.CreateBatch(db, batchDay(), ids, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}

	number, err := fiscal.SubmitBatch(context.Background(), deps(db), b.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if number == "" || s.validateHits != 1 || s.postHits != 1 || s.lastAuth != "Bearer "+sandboxToken {
		t.Fatalf("validate then post with the token: %q %d/%d %q", number, s.validateHits, s.postHits, s.lastAuth)
	}
	if items, _ := s.payload(t)["items"].([]any); len(items) != 2 {
		t.Fatalf("two source lines → two items: %v", s.payload(t)["items"])
	}
	got, _, err := fiscal.LoadBatch(db, b.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "synced" || got.FBRInvoiceNumber == nil || *got.FBRInvoiceNumber != number || got.SettledAt == nil {
		t.Fatalf("batch after: %+v", got)
	}
	for _, id := range ids {
		status, num, batch := invoiceFiscal(t, db, id)
		if status != "covered" || !num.Valid || num.String != number || batch.UUID != b.ID {
			t.Fatalf("invoice %s covered: %s %v", id, status, num)
		}
	}
	if n, filed, err := fiscal.FindSyncedBatch(db, b.ID); err != nil || !filed || n != number {
		t.Fatalf("ledger row: %q %v %v", n, filed, err)
	}
	// A clean validate writes no row (as for a single sale); the post does.
	var audits int
	if err := db.QueryRow(`SELECT count(*) FROM fiscal_audit_events WHERE batch_id = $1 AND kind = 'batch' AND phase = 'post' AND outcome = 'ok'`, b.ID).Scan(&audits); err != nil || audits != 1 {
		t.Fatalf("audit row carries the batch: %d %v", audits, err)
	}
}

func TestSubmitBatch_RejectedThenFailBatchReleases(t *testing.T) {
	sandboxEnv(t)
	db := testdb.Fresh(t)
	s := newStub(t, fixtureBody(t, "resp_negTax.json"), fixtureBody(t, "resp_postA.json"))
	enableFiscal(t, db, s, true)
	cfg, _ := fiscal.LoadConfig(db)
	ids := []uuid.UUID{seedBatchSale(t, db, "20261012-001", seedOpt{})}
	b, _ := fiscal.CreateBatch(db, batchDay(), ids, cfg, nil)

	_, err := fiscal.SubmitBatch(context.Background(), deps(db), b.ID, nil)
	var rejected *fiscal.RejectedError
	if !errors.As(err, &rejected) || s.postHits != 0 {
		t.Fatalf("rejected at validate, never posted: %v %d", err, s.postHits)
	}
	if status, _, _ := invoiceFiscal(t, db, ids[0]); status != "pending" {
		t.Fatalf("SubmitBatch itself leaves the claim; the caller decides: %s", status)
	}

	if err := fiscal.FailBatch(db, b.ID, err); err != nil {
		t.Fatal(err)
	}
	got, _, _ := fiscal.LoadBatch(db, b.ID)
	if got.Status != "failed" || got.LastErrorCode == nil || *got.LastErrorCode != "0104" {
		t.Fatalf("failed batch keeps the code: %+v", got)
	}
	if status, _, batch := invoiceFiscal(t, db, ids[0]); status != "unfiled" || batch.Valid {
		t.Fatalf("released: %s %v", status, batch)
	}
	if _, err := fiscal.CreateBatch(db, batchDay(), ids, cfg, nil); err != nil {
		t.Fatalf("released invoices can be batched again: %v", err)
	}
}

// Review Focus 2: FBR accepted, the process died before the stamps.
func TestSubmitBatch_LedgerRecovery(t *testing.T) {
	sandboxEnv(t)
	db := testdb.Fresh(t)
	s := newStub(t, []byte(`{"never":"called"}`), []byte(`{"never":"called"}`))
	enableFiscal(t, db, s, true)
	cfg, _ := fiscal.LoadConfig(db)
	ids := []uuid.UUID{seedBatchSale(t, db, "20261012-001", seedOpt{})}
	b, _ := fiscal.CreateBatch(db, batchDay(), ids, cfg, nil)
	if err := fiscal.RecordSyncedBatch(db, b.ID, "6110180871403DIBATCH0001", nil); err != nil {
		t.Fatal(err)
	}

	number, err := fiscal.SubmitBatch(context.Background(), deps(db), b.ID, nil)
	if err != nil || number != "6110180871403DIBATCH0001" {
		t.Fatalf("recovered from the ledger: %q %v", number, err)
	}
	if s.hits() != 0 {
		t.Fatalf("no HTTP on a recovery: %d", s.hits())
	}
	got, _, _ := fiscal.LoadBatch(db, b.ID)
	if got.Status != "synced" {
		t.Fatalf("batch: %+v", got)
	}
	if status, num, _ := invoiceFiscal(t, db, ids[0]); status != "covered" || num.String != number {
		t.Fatalf("invoice: %s %v", status, num)
	}
}

func TestSubmitBatch_Unreachable(t *testing.T) {
	sandboxEnv(t)
	db := testdb.Fresh(t)
	s := newStub(t, []byte(`<html>bad gateway</html>`), fixtureBody(t, "resp_postA.json"))
	s.validateStatus = http.StatusBadGateway
	enableFiscal(t, db, s, true)
	cfg, _ := fiscal.LoadConfig(db)
	ids := []uuid.UUID{seedBatchSale(t, db, "20261012-001", seedOpt{})}
	b, _ := fiscal.CreateBatch(db, batchDay(), ids, cfg, nil)

	_, err := fiscal.SubmitBatch(context.Background(), deps(db), b.ID, nil)
	if !errors.Is(err, fiscal.ErrUnreachable) {
		t.Fatalf("err = %v", err)
	}
	got, _, _ := fiscal.LoadBatch(db, b.ID)
	status, _, _ := invoiceFiscal(t, db, ids[0])
	if got.Status != "pending" || status != "pending" {
		t.Fatalf("still pending for the retry: %s / %s", got.Status, status)
	}
}

func TestEligible_ListsAndExplains(t *testing.T) {
	sandboxEnv(t)
	db := testdb.Fresh(t)
	enableFiscal(t, db, nil, true)
	cfg, _ := fiscal.LoadConfig(db)
	registered := seedCustomer(t, db, "Ali Traders", "1234567", "Registered")
	ok := seedBatchSale(t, db, "20261012-001", seedOpt{})
	failed := seedBatchSale(t, db, "20261012-002", seedOpt{fiscal: "failed"})
	reg := seedBatchSale(t, db, "20261012-003", seedOpt{customer: &registered})
	rate := seedBatchSale(t, db, "20261012-004", seedOpt{taxRate: 0.17})
	seedBatchSale(t, db, "20261012-005", seedOpt{status: "voided"})
	seedBatchSale(t, db, "20261012-006", seedOpt{fiscal: "synced"})
	seedBatchSale(t, db, "20261012-007", seedOpt{fiscal: "off"})
	seedBatchSale(t, db, "20261011-001", seedOpt{date: "2026-10-11"})

	rows, err := fiscal.Eligible(db, batchDay(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	got := map[uuid.UUID]fiscal.EligibleRow{}
	for _, r := range rows {
		got[r.ID] = r
	}
	if len(rows) != 4 {
		t.Fatalf("voided, synced, off and other-day rows are not listed: %+v", rows)
	}
	if !got[ok].Eligible || got[ok].TotalKg != 12.5 || got[ok].TotalPayable != 3909 || got[ok].FiscalStatus != "unfiled" {
		t.Fatalf("ok row: %+v", got[ok])
	}
	if !got[failed].Eligible || got[failed].FiscalStatus != "failed" {
		t.Fatalf("failed row is eligible again: %+v", got[failed])
	}
	if got[reg].Eligible || got[reg].Reason != "registered_buyer" || got[reg].CustomerName == nil {
		t.Fatalf("registered row: %+v", got[reg])
	}
	if got[rate].Eligible || got[rate].Reason != "tax_rate_mismatch" {
		t.Fatalf("rate row: %+v", got[rate])
	}
}

func TestLoadBatch_ListsCoveredInvoicesIncludingLaterVoids(t *testing.T) {
	sandboxEnv(t)
	db := testdb.Fresh(t)
	s := acceptingStub(t)
	enableFiscal(t, db, s, true)
	cfg, _ := fiscal.LoadConfig(db)
	ids := []uuid.UUID{seedBatchSale(t, db, "20261012-001", seedOpt{}), seedBatchSale(t, db, "20261012-002", seedOpt{})}
	b, _ := fiscal.CreateBatch(db, batchDay(), ids, cfg, nil)
	if _, err := fiscal.SubmitBatch(context.Background(), deps(db), b.ID, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE invoices SET status = 'voided', voided_at = now() WHERE id = $1`, ids[1]); err != nil {
		t.Fatal(err)
	}

	got, rows, err := fiscal.LoadBatch(db, b.ID)
	if err != nil || got.ID != b.ID || len(rows) != 2 {
		t.Fatalf("%v %+v %d", err, got, len(rows))
	}
	voided := 0
	for _, r := range rows {
		if r.Status == "voided" {
			voided++
		}
	}
	if voided != 1 {
		t.Fatalf("the later void is shown, not hidden: %+v", rows)
	}
	if got.InvoiceCount != 2 || got.TotalAmount != 7817.50 {
		t.Fatalf("batch figures are a snapshot, unchanged by the void: %+v", got)
	}

	list, err := fiscal.ListBatches(db, batchDay().AddDate(0, 0, -1), batchDay())
	if err != nil || len(list) != 1 || list[0].ID != b.ID {
		t.Fatalf("list: %v %+v", err, list)
	}
	if _, _, err := fiscal.LoadBatch(db, uuid.New()); !errors.Is(err, fiscal.ErrBatchNotFound) {
		t.Fatalf("unknown batch: %v", err)
	}
}
