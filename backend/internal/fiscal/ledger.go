package fiscal

import (
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
)

// Execer is the slice of database/sql the filing ledger writes through.
// *sql.DB and *sql.Tx both satisfy it.
type Execer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

// RecordSynced writes the fact that FBR accepted a document. It is the first
// thing Submit does after a successful post and before it touches the
// invoice, because this row — not the invoice's status — is what a retry
// consults (CLAUDE.md invariant 7). If the process dies between the two
// writes, the next attempt finds the number here and recovers instead of
// filing the same sale twice.
//
// ON CONFLICT DO NOTHING makes that recovery safe to repeat: the unique
// index on (invoice_id, kind) means the first number filed is the number
// that stands, and a second call with a different number is ignored rather
// than overwriting history.
func RecordSynced(q Execer, invoiceID uuid.UUID, kind, number string, raw json.RawMessage) error {
	var response any
	if len(raw) > 0 {
		// lib/pq sends []byte as bytea, which will not cast into jsonb; the
		// raw envelope has to travel as text.
		response = string(raw)
	}
	_, err := q.Exec(`
		INSERT INTO fiscal_invoices (invoice_id, kind, fbr_invoice_number, response)
		VALUES ($1, $2, $3, $4::jsonb)
		ON CONFLICT (invoice_id, kind) DO NOTHING`,
		invoiceID, kind, number, response)
	return err
}

// FindSynced returns the FBR number already filed for this invoice and kind.
// The bool is false when nothing has been filed, which is not an error —
// it is the ordinary state of every invoice before its first submission.
func FindSynced(q Querier, invoiceID uuid.UUID, kind string) (string, bool, error) {
	var number string
	err := q.QueryRow(`SELECT fbr_invoice_number FROM fiscal_invoices WHERE invoice_id = $1 AND kind = $2`,
		invoiceID, kind).Scan(&number)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return number, true, nil
}

// RecordSyncedBatch is RecordSynced for a combined invoice: one row per
// batch, first number filed stands.
func RecordSyncedBatch(q Execer, batchID uuid.UUID, number string, raw json.RawMessage) error {
	var response any
	if len(raw) > 0 {
		response = string(raw)
	}
	_, err := q.Exec(`
		INSERT INTO fiscal_invoices (batch_id, kind, fbr_invoice_number, response)
		VALUES ($1, $2, $3, $4::jsonb)
		ON CONFLICT (batch_id, kind) WHERE batch_id IS NOT NULL DO NOTHING`,
		batchID, string(KindBatch), number, response)
	return err
}

// FindSyncedBatch is FindSynced for a combined invoice.
func FindSyncedBatch(q Querier, batchID uuid.UUID) (string, bool, error) {
	var number string
	err := q.QueryRow(`SELECT fbr_invoice_number FROM fiscal_invoices WHERE batch_id = $1 AND kind = $2`,
		batchID, string(KindBatch)).Scan(&number)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return number, true, nil
}
