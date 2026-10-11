package fiscal

import (
	"context"
	"database/sql"
	"errors"
	"math"
	"strconv"
	"strings"
	"time"

	"elevon-backend/internal/invoice"
	"elevon-backend/internal/models"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

// Combined invoices (spec 2026-10-12 §2.3). A batch is a set of unfiled
// walk-in sales from one business date filed with FBR as a single Sale
// Invoice. The batch row owns the invoices while the filing is in flight;
// once FBR accepts it, every invoice is stamped `covered` with the combined
// number, and a rejection hands them back to `unfiled` for the operator to
// fix and refile.
//
// Every figure a batch carries is a sum of stored invoice_lines columns; no
// endpoint takes a quantity or an amount for a batch.

// Batch is one fiscal_batches row.
type Batch struct {
	ID               uuid.UUID  `json:"id"`
	BusinessDate     time.Time  `json:"business_date"`
	Status           string     `json:"status"`
	FBRInvoiceNumber *string    `json:"fbr_invoice_number"`
	InvoiceCount     int        `json:"invoice_count"`
	TotalKg          float64    `json:"total_kg"`
	ValueExclTax     float64    `json:"value_excl_tax"`
	TaxAmount        float64    `json:"tax_amount"`
	FurtherTaxAmount float64    `json:"further_tax_amount"`
	TotalAmount      float64    `json:"total_amount"`
	LastErrorCode    *string    `json:"last_error_code"`
	LastError        *string    `json:"last_error"`
	CreatedBy        *uuid.UUID `json:"created_by"`
	CreatedAt        time.Time  `json:"created_at"`
	SettledAt        *time.Time `json:"settled_at"`
}

// EligibleRow is one invoice as the File with FBR screen lists it. Eligible
// false carries a Reason the screen can show; rows that fail the basic
// filter (voided, filed, another date) are not returned at all.
type EligibleRow struct {
	ID            uuid.UUID `json:"id"`
	InvoiceNumber string    `json:"invoice_number"`
	Status        string    `json:"status"`
	CreatedAt     time.Time `json:"created_at"`
	CustomerName  *string   `json:"customer_name"`
	TotalKg       float64   `json:"total_kg"`
	TotalAmount   float64   `json:"total_amount"`
	TotalPayable  int64     `json:"total_payable"`
	FiscalStatus  string    `json:"fiscal_status"`
	Eligible      bool      `json:"eligible"`
	Reason        string    `json:"reason"`
}

// Reasons an unfiled invoice cannot join a batch.
const (
	ReasonRegisteredBuyer = "registered_buyer"
	ReasonTaxRateMismatch = "tax_rate_mismatch"
)

// IneligibleError names every invoice in a request that cannot be batched,
// so the screen can refresh exactly those rows. Error() is the stable code.
type IneligibleError struct{ IDs []uuid.UUID }

func (e *IneligibleError) Error() string { return "fiscal_batch_ineligible" }

// ErrBatchNotFound: no such batch id.
var ErrBatchNotFound = errors.New("fiscal_batch_not_found")

const batchColumns = `id, business_date, status, fbr_invoice_number, invoice_count, total_kg::float8,
	value_excl_tax::float8, tax_amount::float8, further_tax_amount::float8, total_amount::float8,
	last_error_code, last_error, created_by, created_at, settled_at`

func scanBatch(row invoice.RowScanner) (Batch, error) {
	var b Batch
	err := row.Scan(&b.ID, &b.BusinessDate, &b.Status, &b.FBRInvoiceNumber, &b.InvoiceCount, &b.TotalKg,
		&b.ValueExclTax, &b.TaxAmount, &b.FurtherTaxAmount, &b.TotalAmount,
		&b.LastErrorCode, &b.LastError, &b.CreatedBy, &b.CreatedAt, &b.SettledAt)
	return b, err
}

// ratePct is cfg.RateDesc ("18%") as a percentage number.
func ratePct(cfg Config) (float64, bool) {
	pct, err := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(cfg.RateDesc), "%"), 64)
	return pct, err == nil
}

// eligibilityReason is the one rule Eligible and CreateBatch share for a row
// that has already passed the basic filter: a registered buyer files on
// their own NTN, and FBR validates every item at cfg.RateDesc.
func eligibilityReason(buyerType string, taxRate float64, cfg Config) string {
	if strings.EqualFold(strings.TrimSpace(buyerType), "Registered") {
		return ReasonRegisteredBuyer
	}
	pct, ok := ratePct(cfg)
	if !ok || math.Abs(taxRate*100-pct) > 1e-9 {
		return ReasonTaxRateMismatch
	}
	return ""
}

// Eligible lists the business date's unfiled and failed completed invoices,
// each marked eligible or carrying the reason it is not.
func Eligible(q interface {
	Query(query string, args ...any) (*sql.Rows, error)
}, businessDate time.Time, cfg Config) ([]EligibleRow, error) {
	rows, err := q.Query(`
		SELECT i.id, i.invoice_number, i.status, i.created_at, COALESCE(i.customer_name, c.name),
		       COALESCE(k.kg, 0)::float8, i.total_amount::float8, i.total_payable::bigint, i.fiscal_status,
		       i.tax_rate::float8, COALESCE(c.buyer_registration_type, '')
		FROM invoices i
		LEFT JOIN customers c ON c.id = i.customer_id
		LEFT JOIN LATERAL (SELECT sum(quantity) AS kg FROM invoice_lines WHERE invoice_id = i.id) k ON true
		WHERE i.status = 'completed' AND i.fiscal_status IN ('unfiled','failed') AND i.business_date = $1::date
		ORDER BY i.invoice_number`, businessDate.Format("2006-01-02"))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []EligibleRow{}
	for rows.Next() {
		var r EligibleRow
		var taxRate float64
		var buyerType string
		if err := rows.Scan(&r.ID, &r.InvoiceNumber, &r.Status, &r.CreatedAt, &r.CustomerName,
			&r.TotalKg, &r.TotalAmount, &r.TotalPayable, &r.FiscalStatus, &taxRate, &buyerType); err != nil {
			return nil, err
		}
		r.Reason = eligibilityReason(buyerType, taxRate, cfg)
		r.Eligible = r.Reason == ""
		out = append(out, r)
	}
	return out, rows.Err()
}

// CreateBatch claims ids into a new pending batch in one transaction. The
// rows are locked in id order before they are judged, so two overlapping
// requests queue rather than deadlock, and the loser sees the winner's
// 'pending' and is refused whole with every offending id.
func CreateBatch(db *sql.DB, businessDate time.Time, ids []uuid.UUID, cfg Config, actor *uuid.UUID) (Batch, error) {
	ids = dedupe(ids)
	if len(ids) == 0 {
		return Batch{}, ErrBatchEmpty
	}
	tx, err := db.Begin()
	if err != nil {
		return Batch{}, err
	}
	defer tx.Rollback()

	rows, err := tx.Query(`
		SELECT i.id, i.status, i.fiscal_status, i.business_date, i.tax_rate::float8,
		       COALESCE(c.buyer_registration_type, '')
		FROM invoices i LEFT JOIN customers c ON c.id = i.customer_id
		WHERE i.id = ANY($1) ORDER BY i.id FOR UPDATE OF i`, pq.Array(ids))
	if err != nil {
		return Batch{}, err
	}
	seen := map[uuid.UUID]bool{}
	bad := []uuid.UUID{}
	for rows.Next() {
		var id uuid.UUID
		var status, fiscalStatus, buyerType string
		var date time.Time
		var taxRate float64
		if err := rows.Scan(&id, &status, &fiscalStatus, &date, &taxRate, &buyerType); err != nil {
			rows.Close()
			return Batch{}, err
		}
		seen[id] = true
		ok := status == "completed" &&
			(fiscalStatus == "unfiled" || fiscalStatus == "failed") &&
			date.Format("2006-01-02") == businessDate.Format("2006-01-02") &&
			eligibilityReason(buyerType, taxRate, cfg) == ""
		if !ok {
			bad = append(bad, id)
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return Batch{}, err
	}
	rows.Close()
	for _, id := range ids {
		if !seen[id] {
			bad = append(bad, id)
		}
	}
	if len(bad) > 0 {
		return Batch{}, &IneligibleError{IDs: bad}
	}

	b, err := scanBatch(tx.QueryRow(`
		INSERT INTO fiscal_batches (business_date, status, invoice_count, total_kg,
			value_excl_tax, tax_amount, further_tax_amount, total_amount, created_by)
		SELECT $1::date, 'pending', count(DISTINCT i.id), COALESCE(sum(l.quantity), 0),
			COALESCE(sum(l.line_total - l.line_discount), 0), COALESCE(sum(l.line_tax), 0),
			(SELECT COALESCE(sum(further_tax_amount), 0) FROM invoices WHERE id = ANY($2)),
			COALESCE(sum(l.line_total - l.line_discount + l.line_tax), 0)
			  + (SELECT COALESCE(sum(further_tax_amount), 0) FROM invoices WHERE id = ANY($2)),
			$3
		FROM invoices i JOIN invoice_lines l ON l.invoice_id = i.id WHERE i.id = ANY($2)
		RETURNING `+batchColumns, businessDate.Format("2006-01-02"), pq.Array(ids), actor))
	if err != nil {
		return Batch{}, err
	}
	if _, err := tx.Exec(`UPDATE invoices SET fiscal_status = 'pending', fiscal_batch_id = $1 WHERE id = ANY($2)`,
		b.ID, pq.Array(ids)); err != nil {
		return Batch{}, err
	}
	return b, tx.Commit()
}

func dedupe(ids []uuid.UUID) []uuid.UUID {
	seen := map[uuid.UUID]bool{}
	out := make([]uuid.UUID, 0, len(ids))
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

// SubmitBatch files one combined invoice and returns the FBR number. It is
// ledger-first on the same terms as Submit (CLAUDE.md invariant 7): the
// fiscal_invoices row for this batch is consulted before any HTTP, so a
// retry recovers the number instead of filing the same sales twice.
//
// On a rejection or a refusal it returns the error and leaves the claim in
// place; the caller decides whether to FailBatch (release the invoices) or
// to retry later. An unreachable gateway is always a retry.
func SubmitBatch(ctx context.Context, d Deps, batchID uuid.UUID, actor *uuid.UUID) (string, error) {
	ref := auditRef{BatchID: &batchID, Kind: KindBatch}

	// 1. The ledger, before anything else.
	number, filed, err := FindSyncedBatch(d.DB, batchID)
	if err != nil {
		return "", err
	}
	if filed {
		if err := applyBatch(d.DB, batchID, number); err != nil {
			return "", err
		}
		ev := ref.event(PhaseLedgerRecovery)
		ev.Outcome, ev.CreatedBy = OutcomeOK, actor
		Record(d.DB, ev)
		return number, nil
	}

	// 2. Configuration and the runtime guards.
	cfg, err := LoadConfig(d.DB)
	if err != nil {
		return "", err
	}
	if !cfg.Enabled {
		return "", ErrDisabled
	}
	if err := cfg.CheckRuntime(); err != nil {
		ev := ref.event(PhaseRefused)
		ev.Outcome, ev.ErrorCode, ev.CreatedBy = OutcomeRefused, err.Error(), actor
		Record(d.DB, ev)
		return "", err
	}
	token, err := LoadAPIKey(d.DB, cfg.Environment())
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(token) == "" {
		return "", ErrTokenMissing
	}

	// 3. The batch and its invoices.
	b, err := loadBatchRow(d.DB, batchID)
	if err != nil {
		return "", err
	}
	invoices, err := loadBatchInvoices(d.DB, batchID)
	if err != nil {
		return "", err
	}
	hsCode, err := loadDefaultHSCode(d.DB)
	if err != nil {
		return "", err
	}

	// 4. Build.
	payload, err := BuildBatchInvoice(BatchInput{BusinessDate: b.BusinessDate, Invoices: invoices, DefaultHSCode: hsCode}, cfg)
	if err != nil {
		ev := ref.event(PhaseRefused)
		ev.Outcome, ev.ErrorCode, ev.CreatedBy = OutcomeRefused, ErrorCode(err), actor
		Record(d.DB, ev)
		return "", err
	}

	// 5. Validate, then post — through the one place that talks to FBR.
	posted, number, postEvent, err := file(ctx, d, d.client(cfg, token), payload, ref, actor)
	if err != nil {
		return "", err
	}

	// 6. Ledger first, stamps second.
	if err := RecordSyncedBatch(d.DB, batchID, number, posted.Raw); err != nil {
		postEvent.ErrorCode = codeLedgerWriteFailed
		Record(d.DB, postEvent)
		return "", err
	}
	if err := applyBatch(d.DB, batchID, number); err != nil {
		postEvent.ErrorCode = codeApplyFailed
		Record(d.DB, postEvent)
		return "", err
	}
	Record(d.DB, postEvent)
	return number, nil
}

// applyBatch stamps the batch and every invoice it owns in one transaction.
func applyBatch(db *sql.DB, batchID uuid.UUID, number string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`
		UPDATE fiscal_batches
		SET status = 'synced', fbr_invoice_number = $2, settled_at = now(), last_error_code = NULL, last_error = NULL
		WHERE id = $1`, batchID, number); err != nil {
		return err
	}
	if _, err := tx.Exec(`
		UPDATE invoices SET fiscal_status = 'covered', fiscal_invoice_number = $2
		WHERE fiscal_batch_id = $1`, batchID, number); err != nil {
		return err
	}
	return tx.Commit()
}

// FailBatch records that this filing did not happen and hands the invoices
// back to unfiled so they can be fixed and refiled. The failed batch row
// stays as history. Only invoices still pending on this batch are released;
// a covered invoice is never un-covered here.
func FailBatch(db *sql.DB, batchID uuid.UUID, cause error) error {
	code, message := failureDetail(cause)
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`
		UPDATE fiscal_batches
		SET status = 'failed', last_error_code = $2, last_error = $3, settled_at = now()
		WHERE id = $1 AND status = 'pending'`, batchID, code, message); err != nil {
		return err
	}
	if _, err := tx.Exec(`
		UPDATE invoices SET fiscal_status = 'unfiled', fiscal_batch_id = NULL
		WHERE fiscal_batch_id = $1 AND fiscal_status = 'pending'`, batchID); err != nil {
		return err
	}
	return tx.Commit()
}

func loadBatchRow(q Querier, id uuid.UUID) (Batch, error) {
	b, err := scanBatch(q.QueryRow(`SELECT `+batchColumns+` FROM fiscal_batches WHERE id = $1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return Batch{}, ErrBatchNotFound
	}
	return b, err
}

// loadBatchInvoices reads the batch's invoices with their lines, in invoice
// number order, so the payload's items are stable across retries.
func loadBatchInvoices(db *sql.DB, batchID uuid.UUID) ([]models.Invoice, error) {
	rows, err := db.Query(`SELECT id FROM invoices WHERE fiscal_batch_id = $1 ORDER BY invoice_number`, batchID)
	if err != nil {
		return nil, err
	}
	ids := []uuid.UUID{}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]models.Invoice, 0, len(ids))
	for _, id := range ids {
		inv, err := invoice.LoadWithLines(db, id)
		if err != nil {
			return nil, err
		}
		out = append(out, inv)
	}
	return out, nil
}

// LoadBatch returns a batch and the invoices it owns, including any voided
// after the filing (their Status says so); the batch's figures are the
// snapshot that was sent and never move.
func LoadBatch(db *sql.DB, id uuid.UUID) (Batch, []EligibleRow, error) {
	b, err := loadBatchRow(db, id)
	if err != nil {
		return Batch{}, nil, err
	}
	rows, err := db.Query(`
		SELECT i.id, i.invoice_number, i.status, i.created_at, COALESCE(i.customer_name, c.name),
		       COALESCE(k.kg, 0)::float8, i.total_amount::float8, i.total_payable::bigint, i.fiscal_status
		FROM invoices i
		LEFT JOIN customers c ON c.id = i.customer_id
		LEFT JOIN LATERAL (SELECT sum(quantity) AS kg FROM invoice_lines WHERE invoice_id = i.id) k ON true
		WHERE i.fiscal_batch_id = $1 ORDER BY i.invoice_number`, id)
	if err != nil {
		return Batch{}, nil, err
	}
	defer rows.Close()
	out := []EligibleRow{}
	for rows.Next() {
		var r EligibleRow
		if err := rows.Scan(&r.ID, &r.InvoiceNumber, &r.Status, &r.CreatedAt, &r.CustomerName,
			&r.TotalKg, &r.TotalAmount, &r.TotalPayable, &r.FiscalStatus); err != nil {
			return Batch{}, nil, err
		}
		r.Eligible = false
		out = append(out, r)
	}
	return b, out, rows.Err()
}

// ListBatches returns the batches dated within [from, to], newest first.
func ListBatches(db *sql.DB, from, to time.Time) ([]Batch, error) {
	rows, err := db.Query(`SELECT `+batchColumns+` FROM fiscal_batches
		WHERE business_date BETWEEN $1::date AND $2::date
		ORDER BY business_date DESC, created_at DESC`, from.Format("2006-01-02"), to.Format("2006-01-02"))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Batch{}
	for rows.Next() {
		b, err := scanBatch(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}
