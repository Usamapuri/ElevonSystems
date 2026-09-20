// Package ledger posts and reads customer_ledger_entries — the append-only
// account history behind every customer's balance. Post is called by the
// customers handlers directly (adjustments — not built in this task) and,
// starting in Task D6, from inside the invoice and receipt transactions, so
// every function here takes a Querier rather than a concrete *sql.DB: the
// caller decides whether it runs inside a transaction.
package ledger

import (
	"database/sql"
	"time"

	"elevon-backend/internal/util"

	"github.com/google/uuid"
)

// Entry is one row to post to customer_ledger_entries. BusinessDate is the
// business day (not the wall clock) the entry belongs to; a zero value is
// filled in with util.BusinessDate(time.Now()) by Post so most callers can
// leave it unset.
type Entry struct {
	ID           uuid.UUID
	CustomerID   uuid.UUID
	EntryType    string
	InvoiceID    *uuid.UUID
	ReceiptID    *uuid.UUID
	Debit        float64
	Credit       float64
	BusinessDate time.Time
	CreatedBy    *uuid.UUID
	Note         *string
	CreatedAt    time.Time
}

// Querier is satisfied by *sql.DB and *sql.Tx, so Post/Balance/Statement run
// identically inside or outside a caller's transaction.
type Querier interface {
	QueryRow(query string, args ...any) *sql.Row
	Query(query string, args ...any) (*sql.Rows, error)
	Exec(query string, args ...any) (sql.Result, error)
}

// Post inserts one ledger entry and returns its id. The table is
// append-only (BEFORE UPDATE OR DELETE trigger in migrations/002_trading.sql)
// so this is the only way an entry is ever written.
func Post(q Querier, e Entry) (uuid.UUID, error) {
	businessDate := e.BusinessDate
	if businessDate.IsZero() {
		businessDate = util.BusinessDate(time.Now())
	}
	var id uuid.UUID
	err := q.QueryRow(`
		INSERT INTO customer_ledger_entries (customer_id, entry_type, invoice_id, receipt_id, debit, credit, business_date, created_by, note)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id`,
		e.CustomerID, e.EntryType, e.InvoiceID, e.ReceiptID, e.Debit, e.Credit, businessDate, e.CreatedBy, e.Note,
	).Scan(&id)
	return id, err
}

// Balance returns a customer's current balance: Σ debit − Σ credit. Zero for
// a customer with no ledger entries.
func Balance(q Querier, customerID uuid.UUID) (float64, error) {
	var balance float64
	err := q.QueryRow(`
		SELECT COALESCE(SUM(debit) - SUM(credit), 0)::float8
		FROM customer_ledger_entries
		WHERE customer_id = $1`, customerID).Scan(&balance)
	return balance, err
}

// StatementRow is one line of a customer statement: the entry plus the
// running balance immediately after it.
type StatementRow struct {
	ID             uuid.UUID `json:"id"`
	BusinessDate   time.Time `json:"business_date"`
	EntryType      string    `json:"entry_type"`
	InvoiceNumber  *string   `json:"invoice_number"`
	ReceiptNumber  *string   `json:"receipt_number"`
	Debit          float64   `json:"debit"`
	Credit         float64   `json:"credit"`
	RunningBalance float64   `json:"running_balance"`
	Note           *string   `json:"note"`
}

// Statement returns a customer's ledger entries in date order with a
// running balance computed in SQL (SUM() OVER, ordered by created_at, id —
// created_at alone is not unique enough to be a stable order). The running
// balance always reflects the whole account (every entry up to that row),
// even when from/to narrow which rows are returned, so a filtered statement
// still shows correct balances rather than a balance reset to zero at the
// window start. from/to are inclusive on business_date; either may be nil.
func Statement(q Querier, customerID uuid.UUID, from, to *time.Time) ([]StatementRow, error) {
	var fromArg, toArg sql.NullTime
	if from != nil {
		fromArg = sql.NullTime{Time: *from, Valid: true}
	}
	if to != nil {
		toArg = sql.NullTime{Time: *to, Valid: true}
	}
	rows, err := q.Query(`
		WITH ledger AS (
			SELECT e.id, e.business_date, e.entry_type, i.invoice_number, r.receipt_number,
			       e.debit::float8 AS debit, e.credit::float8 AS credit, e.note, e.created_at,
			       SUM(e.debit - e.credit) OVER (ORDER BY e.created_at, e.id)::float8 AS running_balance
			FROM customer_ledger_entries e
			LEFT JOIN invoices i ON i.id = e.invoice_id
			LEFT JOIN customer_receipts r ON r.id = e.receipt_id
			WHERE e.customer_id = $1
		)
		SELECT id, business_date, entry_type, invoice_number, receipt_number, debit, credit, running_balance, note
		FROM ledger
		WHERE ($2::timestamptz IS NULL OR business_date >= $2::date)
		  AND ($3::timestamptz IS NULL OR business_date <= $3::date)
		ORDER BY created_at, id`, customerID, fromArg, toArg)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	statement := []StatementRow{}
	for rows.Next() {
		var r StatementRow
		if err := rows.Scan(&r.ID, &r.BusinessDate, &r.EntryType, &r.InvoiceNumber, &r.ReceiptNumber, &r.Debit, &r.Credit, &r.RunningBalance, &r.Note); err != nil {
			return nil, err
		}
		statement = append(statement, r)
	}
	return statement, rows.Err()
}
