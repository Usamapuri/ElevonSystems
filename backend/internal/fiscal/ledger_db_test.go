package fiscal_test

import (
	"database/sql"
	"encoding/json"
	"testing"

	"elevon-backend/internal/fiscal"
	"elevon-backend/internal/testdb"

	"github.com/google/uuid"
)

// The filing ledger is what makes a retry safe (CLAUDE.md invariant 7).
// These are its two properties: what goes in comes back out, and a second
// write for the same (invoice, kind) never displaces the first — because the
// first is the number FBR actually issued.
func TestRecordSynced_RoundTripAndIdempotence(t *testing.T) {
	db := testdb.Fresh(t)
	invoiceID := uuid.MustParse(seedInvoice(t, db))

	if _, found, err := fiscal.FindSynced(db, invoiceID, "sale"); err != nil || found {
		t.Fatalf("nothing is filed yet: found=%v err=%v", found, err)
	}

	raw := json.RawMessage(`{"invoiceNumber":"6110180871403DIAJGEJ4031308"}`)
	if err := fiscal.RecordSynced(db, invoiceID, "sale", "6110180871403DIAJGEJ4031308", raw); err != nil {
		t.Fatalf("record: %v", err)
	}

	number, found, err := fiscal.FindSynced(db, invoiceID, "sale")
	if err != nil || !found {
		t.Fatalf("found=%v err=%v", found, err)
	}
	if number != "6110180871403DIAJGEJ4031308" {
		t.Fatalf("number = %q", number)
	}

	// A retry that got as far as posting a second time must not rewrite the
	// number this app already told the customer about.
	if err := fiscal.RecordSynced(db, invoiceID, "sale", "SOME-OTHER-NUMBER", nil); err != nil {
		t.Fatalf("second record: %v", err)
	}
	number, _, err = fiscal.FindSynced(db, invoiceID, "sale")
	if err != nil {
		t.Fatal(err)
	}
	if number != "6110180871403DIAJGEJ4031308" {
		t.Fatalf("the first filing must stand, got %q", number)
	}

	var rows int
	if err := db.QueryRow(`SELECT count(*) FROM fiscal_invoices WHERE invoice_id = $1`, invoiceID).Scan(&rows); err != nil {
		t.Fatal(err)
	}
	if rows != 1 {
		t.Fatalf("fiscal_invoices rows = %d, want 1", rows)
	}

	// The raw envelope is stored as jsonb, not as bytea: the fiscal screen
	// reads fields out of it.
	var stored sql.NullString
	if err := db.QueryRow(`SELECT response ->> 'invoiceNumber' FROM fiscal_invoices
		WHERE invoice_id = $1 AND kind = 'sale'`, invoiceID).Scan(&stored); err != nil {
		t.Fatalf("response must be queryable jsonb: %v", err)
	}
	if stored.String != "6110180871403DIAJGEJ4031308" {
		t.Fatalf("response ->> invoiceNumber = %q", stored.String)
	}
}

// The sale and its debit note are separate rows, so filing one never looks
// like filing the other.
func TestFindSynced_KindsAreSeparate(t *testing.T) {
	db := testdb.Fresh(t)
	invoiceID := uuid.MustParse(seedInvoice(t, db))

	if err := fiscal.RecordSynced(db, invoiceID, "sale", "SALE-1", nil); err != nil {
		t.Fatal(err)
	}
	if _, found, err := fiscal.FindSynced(db, invoiceID, "debit_note"); err != nil || found {
		t.Fatalf("a filed sale is not a filed debit note: found=%v err=%v", found, err)
	}
	if err := fiscal.RecordSynced(db, invoiceID, "debit_note", "DN-1", nil); err != nil {
		t.Fatal(err)
	}
	number, found, err := fiscal.FindSynced(db, invoiceID, "debit_note")
	if err != nil || !found || number != "DN-1" {
		t.Fatalf("debit note: %q found=%v err=%v", number, found, err)
	}

	// A nil envelope is a NULL response, not an empty string that later
	// fails to parse.
	var response sql.NullString
	if err := db.QueryRow(`SELECT response::text FROM fiscal_invoices
		WHERE invoice_id = $1 AND kind = 'debit_note'`, invoiceID).Scan(&response); err != nil {
		t.Fatal(err)
	}
	if response.Valid {
		t.Fatalf("response = %q, want NULL", response.String)
	}
}

// Execer and Querier are the narrow interfaces they are so a caller can file
// inside its own transaction.
func TestFiscalLedger_WorksInsideATransaction(t *testing.T) {
	db := testdb.Fresh(t)
	invoiceID := uuid.MustParse(seedInvoice(t, db))

	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()

	if err := fiscal.RecordSynced(tx, invoiceID, "sale", "TX-1", nil); err != nil {
		t.Fatalf("*sql.Tx must satisfy Execer: %v", err)
	}
	number, found, err := fiscal.FindSynced(tx, invoiceID, "sale")
	if err != nil || !found || number != "TX-1" {
		t.Fatalf("read back inside the transaction: %q found=%v err=%v", number, found, err)
	}
}
