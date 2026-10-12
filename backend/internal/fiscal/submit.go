package fiscal

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"elevon-backend/internal/invoice"
	"elevon-backend/internal/models"

	"github.com/google/uuid"
)

// Kind is which document is being filed for an invoice: the sale itself, or
// the debit note that reverses it. They are separate rows in
// fiscal_invoices, so one invoice can be filed once as a sale and once as a
// debit note and never twice as either.
type Kind string

const (
	// KindSale files the original invoice.
	KindSale Kind = "sale"
	// KindDebitNote files the reversal of a voided invoice (there is no
	// Credit Note in DI — audit/FBR_SANDBOX_2026-09-21.md decision 1).
	KindDebitNote Kind = "debit_note"
	// KindBatch files a combined invoice covering several sales (spec
	// 2026-10-12 §2.3); its rows key on batch_id rather than invoice_id.
	KindBatch Kind = "batch"
)

// Audit phases and outcomes, matching migration 003's column comments.
const (
	PhaseValidate       = "validate"
	PhasePost           = "post"
	PhaseLedgerRecovery = "ledger_recovery"
	PhaseRefused        = "refused"

	OutcomeOK          = "ok"
	OutcomeRejected    = "rejected"
	OutcomeUnreachable = "unreachable"
	OutcomeRefused     = "refused"
)

// Stable snake_case refusals. Every one of them is a decision this app made
// before or instead of talking to FBR; FBR's own refusals are *RejectedError.
var (
	// ErrDisabled: fiscal_config.enabled is false, so nothing is filed.
	ErrDisabled = errors.New("fiscal_disabled")
	// ErrTokenMissing: no FBR token is stored yet.
	ErrTokenMissing = errors.New("fiscal_token_missing")
	// ErrInvoiceNotFound: the invoice id does not exist.
	ErrInvoiceNotFound = errors.New("fiscal_invoice_not_found")
	// ErrNotSynced: a debit note was asked for on a sale FBR never accepted.
	ErrNotSynced = errors.New("fiscal_not_synced")
	// ErrBuyerUnregistered: a debit note needs a registered buyer (sandbox
	// error 0106), so a walk-in void stays unfiled.
	ErrBuyerUnregistered = errors.New("fiscal_buyer_unregistered")
	// ErrVoidReasonMissing: a Debit Note without a reason is rejected 0027,
	// so it is refused here rather than sent.
	ErrVoidReasonMissing = errors.New("fiscal_void_reason_missing")
	// ErrNoInvoiceNumber: a post came back valid but nameless, which means
	// the invoice was not actually registered.
	ErrNoInvoiceNumber = errors.New("fiscal_no_invoice_number")
	// ErrUnknownKind: a Kind other than the two above.
	ErrUnknownKind = errors.New("fiscal_unknown_kind")
)

// codeLedgerWriteFailed and codeApplyFailed mark the rare audit row where
// FBR accepted the document but this app could not write its own record of
// it. Both leave the filing recoverable: the first from FBR's side, the
// second from fiscal_invoices on the next attempt.
const (
	codeLedgerWriteFailed = "fiscal_ledger_write_failed"
	codeApplyFailed       = "fiscal_apply_failed"
)

// unknownAddress is the placeholder buyerAddress for a buyer with no address
// on file — the same value spec §7.4 fixes for walk-ins.
const unknownAddress = "N/A"

// DefaultTimeout bounds one PRAL call. The sandbox answers in well under a
// second; a till that waits longer than this is better off queueing.
const DefaultTimeout = 30 * time.Second

// maxFailureMessage bounds the sentence MarkFailed stores next to a code.
const maxFailureMessage = 500

// Deps is what Submit needs from the process around it. NewClient and Now
// exist so a test can supply a fake FBR and a fixed clock; both fall back to
// the real thing when nil.
type Deps struct {
	DB        *sql.DB
	NewClient func(cfg Config, token string, timeout time.Duration) Client
	Timeout   time.Duration
	Now       func() time.Time
}

func (d Deps) timeout() time.Duration {
	if d.Timeout > 0 {
		return d.Timeout
	}
	return DefaultTimeout
}

func (d Deps) now() time.Time {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now()
}

func (d Deps) client(cfg Config, token string) Client {
	if d.NewClient != nil {
		return d.NewClient(cfg, token, d.timeout())
	}
	return NewHTTPClient(cfg, token, d.timeout())
}

// Submit files one document with FBR and is the only place in the app that
// does. It returns the FBR invoice number.
//
// The order of the first two steps is the whole safety property and is
// pinned by submit_ledger_first_contract_test.go: fiscal_invoices is
// consulted BEFORE any HTTP call (CLAUDE.md invariant 7). A retry after a
// crash, a timeout or a failed database write therefore recovers the number
// already filed instead of filing the same sale a second time — FBR has no
// idempotency key, so this table is the only defence against a duplicate.
//
// Submit never changes what a customer pays (CLAUDE.md invariant 8): it
// reports the numbers pricing.ComputeTotals already stored.
func Submit(ctx context.Context, d Deps, invoiceID uuid.UUID, kind Kind, actor *uuid.UUID) (string, error) {
	// 1. The ledger, before anything else.
	number, filed, err := FindSynced(d.DB, invoiceID, string(kind))
	if err != nil {
		return "", err
	}
	if filed {
		if err := apply(d.DB, invoiceID, kind, number, nil); err != nil {
			return "", err
		}
		Record(d.DB, Event{
			InvoiceID: &invoiceID,
			Kind:      string(kind),
			Phase:     PhaseLedgerRecovery,
			Outcome:   OutcomeOK,
			CreatedBy: actor,
		})
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
		Record(d.DB, Event{
			InvoiceID: &invoiceID,
			Kind:      string(kind),
			Phase:     PhaseRefused,
			Outcome:   OutcomeRefused,
			ErrorCode: err.Error(),
			CreatedBy: actor,
		})
		return "", err
	}
	token, err := LoadAPIKey(d.DB, cfg.Environment())
	if err != nil {
		// Includes ErrSecretsKeyMissing: a release store with a stored
		// token and no key cannot read it, and must not file without it.
		return "", err
	}
	if strings.TrimSpace(token) == "" {
		return "", ErrTokenMissing
	}

	// 3. The invoice, its buyer and the document-specific extras.
	inv, err := invoice.LoadWithLines(d.DB, invoiceID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrInvoiceNotFound
	}
	if err != nil {
		return "", err
	}
	buyer, err := loadBuyer(d.DB, inv)
	if err != nil {
		return "", err
	}
	hsCode, err := loadDefaultHSCode(d.DB)
	if err != nil {
		return "", err
	}
	in := BuildInput{Invoice: inv, Buyer: buyer, DefaultHSCode: hsCode}

	// 4. Build, then validate.
	payload, err := buildPayload(d.DB, in, cfg, kind, invoiceID, buyer)
	if err != nil {
		Record(d.DB, Event{
			InvoiceID: &invoiceID,
			Kind:      string(kind),
			Phase:     PhaseRefused,
			Outcome:   OutcomeRefused,
			ErrorCode: ErrorCode(err),
			CreatedBy: actor,
		})
		return "", err
	}

	// 5. Validate, then post.
	posted, number, postEvent, err := file(ctx, d, d.client(cfg, token), payload, auditRef{InvoiceID: &invoiceID, Kind: kind}, actor)
	if err != nil {
		return "", err
	}

	// The ledger row first, the invoice second: if the process dies between
	// them, step 1 of the next attempt finds the number here.
	if err := RecordSynced(d.DB, invoiceID, string(kind), number, posted.Raw); err != nil {
		postEvent.ErrorCode = codeLedgerWriteFailed
		Record(d.DB, postEvent)
		return "", err
	}

	// 6. Apply to the invoice.
	if err := apply(d.DB, invoiceID, kind, number, saleDetails(kind, cfg, payload, posted, d.now())); err != nil {
		postEvent.ErrorCode = codeApplyFailed
		Record(d.DB, postEvent)
		return "", err
	}
	Record(d.DB, postEvent)
	return number, nil
}

// auditRef says which document an audit row belongs to: exactly one of an
// invoice or a batch.
type auditRef struct {
	InvoiceID *uuid.UUID
	BatchID   *uuid.UUID
	Kind      Kind
}

func (r auditRef) event(phase string) Event {
	return Event{InvoiceID: r.InvoiceID, BatchID: r.BatchID, Kind: string(r.Kind), Phase: phase}
}

// file validates then posts one document and returns FBR's post response
// and the trimmed invoice number. It writes the audit rows for both calls
// except the successful post's, which it returns (Outcome ok, not yet
// recorded) so the caller can annotate it with a ledger or apply failure
// before recording it. The ledger row and the invoice/batch update belong
// to the caller, which is what lets Submit and SubmitBatch each stay
// ledger-first. This is the only place in the package that talks to FBR.
func file(ctx context.Context, d Deps, cl Client, payload DIInvoice, ref auditRef, actor *uuid.UUID) (Response, string, Event, error) {
	request := string(mustJSON(payload))

	started := d.now()
	validated, err := cl.Validate(ctx, payload)
	if err != nil {
		ev := ref.event(PhaseValidate)
		ev.Outcome, ev.LatencyMs, ev.ErrorCode = OutcomeUnreachable, msSince(started, d.now()), ErrorCode(err)
		ev.RequestExcerpt, ev.ResponseExcerpt, ev.CreatedBy = request, err.Error(), actor
		Record(d.DB, ev)
		return Response{}, "", Event{}, err
	}
	if rejected := validated.Rejection(); rejected != nil {
		ev := ref.event(PhaseValidate)
		ev.Outcome, ev.HTTPStatus, ev.LatencyMs = OutcomeRejected, intPtr(validated.HTTPStatus), msSince(started, d.now())
		ev.ErrorCode, ev.RequestExcerpt, ev.ResponseExcerpt, ev.CreatedBy = rejected.Code, request, string(validated.Raw), actor
		Record(d.DB, ev)
		return Response{}, "", Event{}, rejected
	}

	started = d.now()
	posted, err := cl.Post(ctx, payload)
	if err != nil {
		ev := ref.event(PhasePost)
		ev.Outcome, ev.LatencyMs, ev.ErrorCode = OutcomeUnreachable, msSince(started, d.now()), ErrorCode(err)
		ev.RequestExcerpt, ev.ResponseExcerpt, ev.CreatedBy = request, err.Error(), actor
		Record(d.DB, ev)
		return Response{}, "", Event{}, err
	}
	postEvent := ref.event(PhasePost)
	postEvent.HTTPStatus, postEvent.LatencyMs = intPtr(posted.HTTPStatus), msSince(started, d.now())
	postEvent.RequestExcerpt, postEvent.ResponseExcerpt, postEvent.CreatedBy = request, string(posted.Raw), actor
	if rejected := posted.Rejection(); rejected != nil {
		postEvent.Outcome, postEvent.ErrorCode = OutcomeRejected, rejected.Code
		Record(d.DB, postEvent)
		return Response{}, "", Event{}, rejected
	}
	number := strings.TrimSpace(posted.InvoiceNumber)
	if number == "" {
		postEvent.Outcome, postEvent.ErrorCode = OutcomeRejected, ErrNoInvoiceNumber.Error()
		Record(d.DB, postEvent)
		return Response{}, "", Event{}, ErrNoInvoiceNumber
	}
	postEvent.Outcome = OutcomeOK
	return posted, number, postEvent, nil
}

// buildPayload assembles the document for this Kind, including the checks
// that only a debit note has: a registered buyer (sandbox 0106), a sale FBR
// has already accepted, and a reason (sandbox 0027).
func buildPayload(db *sql.DB, in BuildInput, cfg Config, kind Kind, invoiceID uuid.UUID, buyer *Buyer) (DIInvoice, error) {
	switch kind {
	case KindSale:
		return BuildSaleInvoice(in, cfg)
	case KindDebitNote:
		if !isRegisteredBuyer(buyer) {
			return DIInvoice{}, ErrBuyerUnregistered
		}
		saleNumber, filed, err := FindSynced(db, invoiceID, string(KindSale))
		if err != nil {
			return DIInvoice{}, err
		}
		if !filed {
			return DIInvoice{}, ErrNotSynced
		}
		reason, err := loadVoidReason(db, in.Invoice)
		if err != nil {
			return DIInvoice{}, err
		}
		if reason == "" {
			return DIInvoice{}, ErrVoidReasonMissing
		}
		return BuildDebitNote(in, cfg, saleNumber, reason)
	default:
		return DIInvoice{}, ErrUnknownKind
	}
}

// apply writes FBR's verdict onto the invoice. details is nil on a ledger
// recovery (there is no fresh response to describe) and on a debit note,
// whose own response lives in fiscal_invoices — fiscal_details belongs to
// the sale and must not be overwritten by its reversal.
//
// void_log is never touched: it is append-only, so the debit note's state
// lives on invoices (migration 003) and its fiscal_credit_note column stays
// unused.
func apply(db *sql.DB, invoiceID uuid.UUID, kind Kind, number string, details []byte) error {
	switch kind {
	case KindSale:
		var payload any
		if len(details) > 0 {
			payload = string(details)
		}
		_, err := db.Exec(`
			UPDATE invoices
			SET fiscal_status = 'synced',
			    fiscal_invoice_number = $2,
			    fiscal_details = COALESCE($3::jsonb, fiscal_details)
			WHERE id = $1`, invoiceID, number, payload)
		return err
	case KindDebitNote:
		_, err := db.Exec(`
			UPDATE invoices
			SET fiscal_void_status = 'synced',
			    fiscal_debit_note_number = $2
			WHERE id = $1`, invoiceID, number)
		return err
	default:
		return ErrUnknownKind
	}
}

// MarkFailed is the caller's counterpart to a failed Submit: it records on
// the invoice that this filing did not happen, so the fiscal screen can show
// which invoices need attention. The outbound worker calls it once a job is
// out of attempts.
//
// The stored message is diagnostic and admin-only — fiscal_details is not
// part of the invoice projection any API returns. It can never contain the
// FBR token: no error in this package is built from it.
func MarkFailed(db *sql.DB, invoiceID uuid.UUID, kind Kind, cause error) error {
	code, message := failureDetail(cause)
	details, err := json.Marshal(struct {
		ErrorCode string `json:"error_code"`
		Error     string `json:"error"`
	}{ErrorCode: code, Error: message})
	if err != nil {
		return err
	}
	switch kind {
	case KindSale:
		_, err = db.Exec(`
			UPDATE invoices
			SET fiscal_status = 'failed',
			    fiscal_details = COALESCE(fiscal_details, '{}'::jsonb) || $2::jsonb
			WHERE id = $1`, invoiceID, string(details))
		return err
	case KindDebitNote:
		_, err = db.Exec(`
			UPDATE invoices
			SET fiscal_void_status = 'failed',
			    fiscal_details = COALESCE(fiscal_details, '{}'::jsonb) || $2::jsonb
			WHERE id = $1`, invoiceID, string(details))
		return err
	default:
		return ErrUnknownKind
	}
}

// ErrorCode maps an error out of this package to the stable snake_case code
// an API response carries. FBR's own refusals all collapse to
// "fiscal_rejected"; the four-digit code stays on the *RejectedError for the
// audit trail and the fiscal screen.
func ErrorCode(err error) string {
	if err == nil {
		return ""
	}
	var rejected *RejectedError
	if errors.As(err, &rejected) {
		return "fiscal_rejected"
	}
	if errors.Is(err, ErrUnreachable) {
		return ErrUnreachable.Error()
	}
	for _, known := range []error{
		ErrDisabled, ErrTokenMissing, ErrInvoiceNotFound, ErrNotSynced,
		ErrBuyerUnregistered, ErrVoidReasonMissing, ErrNoInvoiceNumber, ErrUnknownKind,
		ErrSandboxInRelease, ErrSecretsKeyMissing, ErrSecretsKeyInvalid, ErrCiphertextInvalid,
		ErrHSCodeMissing, ErrBatchEmpty, ErrBatchNotFound,
	} {
		if errors.Is(err, known) {
			return known.Error()
		}
	}
	return "fiscal_error"
}

// failureDetail is what MarkFailed stores: FBR's own code and sentence for a
// rejection, this app's code and text otherwise.
func failureDetail(cause error) (code, message string) {
	if cause == nil {
		return "", ""
	}
	var rejected *RejectedError
	if errors.As(cause, &rejected) {
		return rejected.Code, truncate(rejected.Message, maxFailureMessage)
	}
	// A dead job's reason (queue.go codedError) carries its own code.
	var coded interface{ Code() string }
	if errors.As(cause, &coded) {
		return coded.Code(), truncate(cause.Error(), maxFailureMessage)
	}
	return ErrorCode(cause), truncate(cause.Error(), maxFailureMessage)
}

// saleDetails is the fiscal_details object for a filed sale. A debit note
// gets nil: its response is in fiscal_invoices and the sale's details stay
// as they were.
func saleDetails(kind Kind, cfg Config, payload DIInvoice, resp Response, now time.Time) []byte {
	if kind != KindSale {
		return nil
	}
	lineNumbers := make([]string, 0, len(resp.Lines))
	for _, line := range resp.Lines {
		lineNumbers = append(lineNumbers, line.InvoiceNo)
	}
	return mustJSON(struct {
		Kind        string   `json:"kind"`
		ScenarioID  string   `json:"scenario_id"`
		FiledAt     string   `json:"filed_at"`
		LineNumbers []string `json:"line_numbers"`
		IsSandbox   bool     `json:"is_sandbox"`
	}{
		Kind:        string(kind),
		ScenarioID:  payload.ScenarioID,
		FiledAt:     now.UTC().Format(time.RFC3339),
		LineNumbers: lineNumbers,
		IsSandbox:   cfg.IsSandbox,
	})
}

// loadBuyer resolves the invoice's buyer: the customers row when there is
// one, the invoice's own snapshot when the row has gone, and nil for a
// walk-in (which the payload builder fills in itself).
func loadBuyer(q Querier, inv models.Invoice) (*Buyer, error) {
	if inv.CustomerID != nil {
		var name string
		var ntn, cnic, address, province sql.NullString
		var registration string
		err := q.QueryRow(`SELECT name, ntn, cnic, address, province, buyer_registration_type
			FROM customers WHERE id = $1`, *inv.CustomerID).
			Scan(&name, &ntn, &cnic, &address, &province, &registration)
		switch {
		case err == nil:
			return &Buyer{
				NTNCNIC:          firstNonBlank(ntn.String, cnic.String),
				Name:             name,
				Province:         strings.TrimSpace(province.String),
				Address:          firstNonBlank(address.String, unknownAddress),
				RegistrationType: registration,
			}, nil
		case errors.Is(err, sql.ErrNoRows):
			// The customer row has gone; the invoice is a fiscal document
			// and still carries its own snapshot of the buyer.
		default:
			return nil, err
		}
	}
	return snapshotBuyer(inv), nil
}

// snapshotBuyer builds a buyer from the invoice's own columns. It cannot
// know a registration type — that lived on the customers row — so it says
// Unregistered rather than claim a registration this app can no longer
// prove, which at worst leaves a void unfiled.
func snapshotBuyer(inv models.Invoice) *Buyer {
	name := trimPtr(inv.CustomerName)
	if name == "" {
		return nil
	}
	return &Buyer{
		NTNCNIC:          firstNonBlank(trimPtr(inv.CustomerNTN), trimPtr(inv.CustomerCNIC)),
		Name:             name,
		Address:          unknownAddress,
		RegistrationType: "Unregistered",
	}
}

// loadVoidReason is the reason a Debit Note must carry (sandbox 0027). It
// prefers invoices.void_reason and falls back to the void_log row, which it
// only ever reads — void_log is append-only.
func loadVoidReason(q Querier, inv models.Invoice) (string, error) {
	if reason := trimPtr(inv.VoidReason); reason != "" {
		return reason, nil
	}
	var reason sql.NullString
	err := q.QueryRow(`SELECT reason FROM void_log WHERE invoice_id = $1 ORDER BY created_at DESC LIMIT 1`, inv.ID).Scan(&reason)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(reason.String), nil
}

// loadDefaultHSCode reads settings.default_hs_code, the fallback for a line
// with no hs_code snapshot. CLAUDE.md invariant 7: the value is a setting
// the tax advisor confirms, never a literal in Go — so a blank one becomes
// ErrHSCodeMissing in the payload builder, not a guess.
func loadDefaultHSCode(q Querier) (string, error) {
	var value sql.NullString
	err := q.QueryRow(`SELECT value #>> '{}' FROM settings WHERE key = 'default_hs_code'`).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(value.String), nil
}

func trimPtr(s *string) string {
	if s == nil {
		return ""
	}
	return strings.TrimSpace(*s)
}

func firstNonBlank(values ...string) string {
	for _, v := range values {
		if trimmed := strings.TrimSpace(v); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func intPtr(v int) *int { return &v }

func msSince(start, end time.Time) *int64 {
	ms := end.Sub(start).Milliseconds()
	return &ms
}

// mustJSON marshals a value that cannot fail to marshal (plain structs of
// strings, numbers and bools), so the call sites stay readable.
func mustJSON(v any) []byte {
	data, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return data
}
