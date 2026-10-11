package fiscal

import (
	"database/sql"
	"log"

	"github.com/google/uuid"
)

// maxExcerpt bounds a stored request/response excerpt. The real payloads and
// envelopes are well under this; the cap is there so a gateway's HTML error
// page cannot fill the table.
const maxExcerpt = 4000

// maxErrorCode matches fiscal_audit_events.error_code VARCHAR(32). Codes are
// ours or FBR's four-digit ones, so the truncation is defensive only — but
// an over-long code must never be the reason an audit row is lost.
const maxErrorCode = 32

// Event is one conversation with PRAL, as fiscal_audit_events stores it.
// Kind is sale | debit_note | test | reference, Phase is validate | post |
// ledger_recovery | refused | reference, Outcome is ok | rejected |
// unreachable | refused.
//
// RequestExcerpt is the payload JSON, which carries no credential: the token
// travels in an Authorization header the payload never sees. Nothing that
// writes an Event may put the token in either excerpt.
type Event struct {
	InvoiceID *uuid.UUID
	// BatchID is set instead of InvoiceID for a combined invoice's rows.
	BatchID *uuid.UUID
	Kind    string
	Phase   string
	Outcome string

	HTTPStatus *int
	LatencyMs  *int64

	ErrorCode       string
	RequestExcerpt  string
	ResponseExcerpt string

	CreatedBy *uuid.UUID
}

// Record appends one audit row. It is best effort by design: the table is
// the tax advisor's record of what was said to FBR, but a failed insert must
// never turn a filed invoice into a failed one, so a write error is logged
// and swallowed.
//
// The table is append-only (migration 003's trigger), so this is the only
// way a row ever appears and no row is ever amended.
func Record(db *sql.DB, e Event) {
	if db == nil {
		return
	}
	_, err := db.Exec(`
		INSERT INTO fiscal_audit_events
			(invoice_id, batch_id, kind, phase, outcome, http_status, latency_ms, error_code, request_excerpt, response_excerpt, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		e.InvoiceID, e.BatchID, e.Kind, e.Phase, e.Outcome, e.HTTPStatus, e.LatencyMs,
		nullableText(truncate(e.ErrorCode, maxErrorCode)),
		nullableText(truncate(e.RequestExcerpt, maxExcerpt)),
		nullableText(truncate(e.ResponseExcerpt, maxExcerpt)),
		e.CreatedBy)
	if err != nil {
		// The event's own fields are all that could be named here and none
		// of them is a secret, but there is nothing useful to add: the code
		// and phase are already in the error's context for the reader.
		log.Printf("fiscal: audit event not recorded (%s/%s): %v", e.Kind, e.Phase, err)
	}
}

// truncate cuts a string to at most n runes, never mid-rune, so a payload
// that ends in a multi-byte character cannot store invalid UTF-8.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n])
}

// nullableText stores "" as SQL NULL, so an empty excerpt reads as absent
// rather than as an empty string someone has to interpret.
func nullableText(s string) any {
	if s == "" {
		return nil
	}
	return s
}
