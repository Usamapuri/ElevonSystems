package fiscal_test

import (
	"database/sql"
	"encoding/base64"
	"strings"
	"testing"

	"elevon-backend/internal/fiscal"
	"elevon-backend/internal/testdb"
)

// seedInvoice inserts the minimum an invoice needs so the fiscal tables have
// something real to reference. The money is irrelevant here; 003's foreign
// keys are what is under test.
func seedInvoice(t *testing.T, db *sql.DB) string {
	t.Helper()
	var dayID string
	if err := db.QueryRow(`INSERT INTO business_days (business_date, status)
		VALUES (DATE '2026-09-21', 'open') RETURNING id`).Scan(&dayID); err != nil {
		t.Fatalf("seed business day: %v", err)
	}
	var invoiceID string
	if err := db.QueryRow(`INSERT INTO invoices
		(invoice_number, business_day_id, business_date, subtotal, tax_rate, tax_amount, total_amount, total_payable, payment_method)
		VALUES ('20260921-001', $1, DATE '2026-09-21', 3312.50, 0.18, 596.25, 3908.75, 3909, 'cash')
		RETURNING id`, dayID).Scan(&invoiceID); err != nil {
		t.Fatalf("seed invoice: %v", err)
	}
	return invoiceID
}

func TestLoadConfig_ReadsTheSeed(t *testing.T) {
	db := testdb.Fresh(t)

	cfg, err := fiscal.LoadConfig(db)
	if err != nil {
		t.Fatal(err)
	}
	if cfg != fiscal.Defaults() {
		t.Fatalf("migration 003 must seed exactly Defaults():\n got %+v\nwant %+v", cfg, fiscal.Defaults())
	}

	// A transaction satisfies Querier too, so a submission can read inside
	// the same transaction that writes the invoice.
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := fiscal.LoadConfig(tx); err != nil {
		t.Fatalf("*sql.Tx must satisfy Querier: %v", err)
	}
}

func TestLoadAPIKey_UnsetThenRoundTrip(t *testing.T) {
	db := testdb.Fresh(t)
	t.Setenv("GIN_MODE", "debug")
	t.Setenv(fiscal.EnvSecretsKey, base64.StdEncoding.EncodeToString(make([]byte, 32)))

	got, err := fiscal.LoadAPIKey(db)
	if err != nil || got != "" {
		t.Fatalf("the seed is an empty token: %q %v", got, err)
	}

	key, err := fiscal.KeyFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := fiscal.Encrypt(key, "iris-sandbox-token-4242")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE settings SET value = to_jsonb($1::text) WHERE key = $2`, sealed, fiscal.KeyAPIKeyEnc); err != nil {
		t.Fatal(err)
	}

	// What is stored is ciphertext, not the token.
	var stored string
	if err := db.QueryRow(`SELECT value #>> '{}' FROM settings WHERE key = $1`, fiscal.KeyAPIKeyEnc).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stored, "iris-sandbox-token") {
		t.Fatalf("the token must never be stored in the clear: %s", stored)
	}

	got, err = fiscal.LoadAPIKey(db)
	if err != nil {
		t.Fatal(err)
	}
	if got != "iris-sandbox-token-4242" {
		t.Fatalf("round trip through settings: %q", got)
	}

	// A different key cannot read it back.
	t.Setenv(fiscal.EnvSecretsKey, base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32))))
	if _, err := fiscal.LoadAPIKey(db); err != fiscal.ErrCiphertextInvalid {
		t.Fatalf("a different secrets key must fail closed: %v", err)
	}
}

// CLAUDE.md invariant 6: fiscal_audit_events is append-only.
func TestFiscalAuditEvents_AppendOnly(t *testing.T) {
	db := testdb.Fresh(t)

	var id string
	if err := db.QueryRow(`INSERT INTO fiscal_audit_events (kind, phase, outcome, error_code)
		VALUES ('sale','validate','rejected','0104') RETURNING id`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE fiscal_audit_events SET outcome = 'ok' WHERE id = $1`, id); err == nil {
		t.Fatal("UPDATE on fiscal_audit_events must raise")
	}
	if _, err := db.Exec(`DELETE FROM fiscal_audit_events WHERE id = $1`, id); err == nil {
		t.Fatal("DELETE on fiscal_audit_events must raise")
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM fiscal_audit_events`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("the row must survive both attempts, got %d", n)
	}
}

// Invariant 7: a retry consults fiscal_invoices first, which only works if
// the database itself refuses a second filing of the same document.
func TestFiscalInvoices_OneRowPerInvoiceAndKind(t *testing.T) {
	db := testdb.Fresh(t)
	invoiceID := seedInvoice(t, db)

	for _, kind := range []string{"sale", "debit_note"} {
		if _, err := db.Exec(`INSERT INTO fiscal_invoices (invoice_id, kind, fbr_invoice_number)
			VALUES ($1, $2, '6110180871403DIAJGEJ4031308')`, invoiceID, kind); err != nil {
			t.Fatalf("first %s: %v", kind, err)
		}
	}
	if _, err := db.Exec(`INSERT INTO fiscal_invoices (invoice_id, kind, fbr_invoice_number)
		VALUES ($1, 'sale', 'other')`, invoiceID); err == nil {
		t.Fatal("a second sale filing for one invoice must be refused")
	}
	if _, err := db.Exec(`INSERT INTO fiscal_invoices (invoice_id, kind, fbr_invoice_number)
		VALUES ($1, 'refund', 'x')`, invoiceID); err == nil {
		t.Fatal("an unknown kind must be refused")
	}
}

func TestFiscalOutboundJobs_OneActiveJobPerDocument(t *testing.T) {
	db := testdb.Fresh(t)
	invoiceID := seedInvoice(t, db)

	if _, err := db.Exec(`INSERT INTO fiscal_outbound_jobs (invoice_id, kind) VALUES ($1,'sale')`, invoiceID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO fiscal_outbound_jobs (invoice_id, kind) VALUES ($1,'sale')`, invoiceID); err == nil {
		t.Fatal("a second pending job for one document must be refused")
	}
	// Once the first is finished a fresh job is allowed again, which is how a
	// void files its debit note after the sale succeeded.
	if _, err := db.Exec(`UPDATE fiscal_outbound_jobs SET status='succeeded', succeeded_at=now() WHERE invoice_id=$1`, invoiceID); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO fiscal_outbound_jobs (invoice_id, kind) VALUES ($1,'sale')`, invoiceID); err != nil {
		t.Fatalf("after the first succeeded a new job must be allowed: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO fiscal_outbound_jobs (invoice_id, kind, status) VALUES ($1,'sale','queued')`, invoiceID); err == nil {
		t.Fatal("an unknown status must be refused")
	}
}

func TestVoidLog_FiscalVoidStatusDefaultsToUnfiled(t *testing.T) {
	db := testdb.Fresh(t)
	invoiceID := seedInvoice(t, db)

	var status string
	if err := db.QueryRow(`INSERT INTO void_log (invoice_id, invoice_number, total_payable, reason)
		VALUES ($1, '20260921-001', 3909, 'Wrong customer') RETURNING fiscal_void_status`, invoiceID).Scan(&status); err != nil {
		t.Fatal(err)
	}
	if status != "unfiled" {
		t.Fatalf("a void starts unfiled, got %q", status)
	}
}
