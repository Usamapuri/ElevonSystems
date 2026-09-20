// Package invoice owns document numbering: the daily invoice sequence
// (YYYYMMDD-NNN) and the receipt sequence (R-YYYYMMDD-NNN), both keyed to the
// business date (spec §6.6).
//
// The allocator is the retail POS's single atomic upsert on a counter table,
// ported unchanged in shape: one INSERT … ON CONFLICT DO UPDATE … RETURNING
// per number, in its own short transaction. Two properties matter and both
// come from that one statement:
//
//   - Atomicity. The conflicting INSERT takes a row lock on the counter and
//     the RETURNING value is read under it, so two concurrent allocations can
//     never be handed the same number — no SELECT-then-UPDATE window exists.
//   - Short lock hold. The transaction contains nothing but the upsert, so the
//     counter row is locked for microseconds rather than for the length of an
//     invoice transaction (which locks products, reads settings and posts a
//     ledger entry). A number is therefore allocated OUTSIDE the invoice
//     transaction, before it begins.
//
// The cost of that choice is deliberate and documented: an invoice
// transaction that fails after its number was allocated (day not open, a
// product gone inactive, a credit limit hit) leaves a gap in the day's
// sequence. Gaps are acceptable — FBR cares that numbers are unique and
// well-formed (error rule 0173: alphanumerics with a dash), not that they are
// dense — whereas holding the counter lock for the life of every invoice
// transaction would serialise the whole till behind the slowest sale.
package invoice

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// DateLayout is how a business date is rendered for the ::date parameter.
// Numbers are keyed to the business date (util.BusinessDate), never to the
// wall clock, so numbering day and reporting day are the same day.
const DateLayout = "2006-01-02"

// ReceiptPrefix marks a customer receipt number apart from an invoice
// number. Invoices carry no prefix.
const ReceiptPrefix = "R-"

// invoiceCounterSQL and receiptCounterSQL are written out in full rather than
// built from an interpolated table name: the two statements a store's whole
// numbering rests on should be greppable verbatim.
const invoiceCounterSQL = `
	INSERT INTO invoice_number_counters (business_date, last_value)
	VALUES ($1::date, 1)
	ON CONFLICT (business_date)
	DO UPDATE SET last_value = invoice_number_counters.last_value + 1
	RETURNING last_value`

const receiptCounterSQL = `
	INSERT INTO receipt_number_counters (business_date, last_value)
	VALUES ($1::date, 1)
	ON CONFLICT (business_date)
	DO UPDATE SET last_value = receipt_number_counters.last_value + 1
	RETURNING last_value`

// AllocateInvoiceNumber issues the next invoice number for businessDate,
// e.g. "20260920-001". Runs in its own transaction; callers pass the pool,
// never their own *sql.Tx.
func AllocateInvoiceNumber(db *sql.DB, businessDate time.Time) (string, error) {
	return allocate(db, invoiceCounterSQL, "", businessDate)
}

// AllocateReceiptNumber issues the next customer-receipt number for
// businessDate, e.g. "R-20260920-001", from its own counter table — invoices
// and receipts never share a sequence.
func AllocateReceiptNumber(db *sql.DB, businessDate time.Time) (string, error) {
	return allocate(db, receiptCounterSQL, ReceiptPrefix, businessDate)
}

// allocate runs one counter upsert in its own short transaction and formats
// the result. The business date is passed as a YYYY-MM-DD string cast to
// ::date rather than as a time.Time: a timestamptz parameter would be
// resolved against the session timezone, and the one thing a number must not
// depend on is which side of midnight the server's clock thinks it is on.
func allocate(db *sql.DB, query, prefix string, businessDate time.Time) (string, error) {
	dateKey := businessDate.Format(DateLayout)
	tx, err := db.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	var seq int
	if err := tx.QueryRow(query, dateKey).Scan(&seq); err != nil {
		return "", err
	}
	if err := tx.Commit(); err != nil {
		return "", err
	}
	return format(prefix, dateKey, seq), nil
}

// format renders prefix + compact date + zero-padded sequence. The sequence
// is padded to three digits and simply grows a fourth on the 1000th document
// of a day ("20260920-1000") rather than wrapping or erroring — a busy day is
// not a reason to refuse a sale, and the number stays unique and sortable
// within the day either way.
func format(prefix, dateKey string, seq int) string {
	return fmt.Sprintf("%s%s-%03d", prefix, strings.ReplaceAll(dateKey, "-", ""), seq)
}

// Format renders a number for a business date and sequence without touching
// the database — the formatting half of the allocator, exposed for tests and
// for any caller that needs to predict or display a number.
func Format(prefix string, businessDate time.Time, seq int) string {
	return format(prefix, businessDate.Format(DateLayout), seq)
}
