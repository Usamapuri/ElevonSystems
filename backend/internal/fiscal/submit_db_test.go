package fiscal_test

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"elevon-backend/internal/fiscal"
	"elevon-backend/internal/testdb"

	"github.com/google/uuid"
)

// sandboxToken is the stand-in for the store's FBR token. Every test asserts
// it never leaves the Authorization header.
const sandboxToken = "sandbox-token-value-9f3c"

// filedAt is the fixed clock the tests run Submit on, so fiscal_details is
// deterministic.
func filedAt() time.Time { return time.Date(2026, 9, 21, 6, 0, 0, 0, time.UTC) }

func sandboxEnv(t *testing.T) {
	t.Helper()
	t.Setenv("GIN_MODE", "debug")
	t.Setenv(fiscal.EnvSecretsKey, base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef")))
}

func fixtureBody(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return data
}

// stub is the fake FBR. It serves one body on /validate and another on
// /post, and records what it was asked — including the Authorization header,
// which is how the tests prove the encrypted token round-tripped.
type stub struct {
	server *httptest.Server

	validateBody   []byte
	validateStatus int
	postBody       []byte
	postStatus     int

	validateHits int
	postHits     int
	lastAuth     string
	lastPayload  []byte
}

func newStub(t *testing.T, validateBody, postBody []byte) *stub {
	t.Helper()
	s := &stub{validateBody: validateBody, postBody: postBody, validateStatus: http.StatusOK, postStatus: http.StatusOK}
	s.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.lastAuth = r.Header.Get("Authorization")
		body := make([]byte, r.ContentLength)
		if r.ContentLength > 0 {
			_, _ = io.ReadFull(r.Body, body)
		}
		s.lastPayload = body
		switch r.URL.Path {
		case "/validate":
			s.validateHits++
			w.WriteHeader(s.validateStatus)
			_, _ = w.Write(s.validateBody)
		case "/post":
			s.postHits++
			w.WriteHeader(s.postStatus)
			_, _ = w.Write(s.postBody)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(s.server.Close)
	return s
}

func (s *stub) hits() int { return s.validateHits + s.postHits }

func (s *stub) payload(t *testing.T) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(s.lastPayload, &m); err != nil {
		t.Fatalf("stub payload: %v\n%s", err, s.lastPayload)
	}
	return m
}

// enableFiscal writes a fiscal_config that points at the stub and stores the
// encrypted token, exactly as the admin screen would.
func enableFiscal(t *testing.T, db *sql.DB, s *stub, sandbox bool) {
	t.Helper()
	cfg := fiscal.Defaults()
	cfg.Enabled = true
	cfg.IsSandbox = sandbox
	cfg.SellerNTNCNIC = "8951943"
	cfg.SellerBusinessName = "Elevon Test Seller"
	cfg.SellerProvince = "PUNJAB"
	cfg.SellerAddress = "Lahore"
	if s != nil {
		cfg.ValidateURLOverride = s.server.URL + "/validate"
		cfg.PostURLOverride = s.server.URL + "/post"
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	putSetting(t, db, fiscal.KeyConfig, string(raw))

	key, err := fiscal.KeyFromEnv()
	if err != nil {
		t.Fatalf("secrets key: %v", err)
	}
	sealed, err := fiscal.Encrypt(key, sandboxToken)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(sealed)
	if err != nil {
		t.Fatal(err)
	}
	putSetting(t, db, fiscal.KeyAPIKeyEnc, string(encoded))
	putSetting(t, db, "default_hs_code", `"2711.1910"`)
}

func putSetting(t *testing.T, db *sql.DB, key, value string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO settings (key, value) VALUES ($1, $2::jsonb)
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`, key, value); err != nil {
		t.Fatalf("set %s: %v", key, err)
	}
}

// seedCustomer inserts a buyer. registration is Registered or Unregistered.
func seedCustomer(t *testing.T, db *sql.DB, name, ntn, registration string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := db.QueryRow(`INSERT INTO customers (name, ntn, address, province, buyer_registration_type)
		VALUES ($1, $2, 'Gulberg, Lahore', 'PUNJAB', $3) RETURNING id`, name, ntn, registration).Scan(&id); err != nil {
		t.Fatalf("seed customer: %v", err)
	}
	return id
}

// seedSale inserts the sandbox's Case A invoice: one line, 12.5 kg, 3312.50
// excluding sales tax, 596.25 tax — the numbers that validated against the
// real API. fiscal_status starts at 'pending', where an enqueued invoice
// sits before its first attempt.
func seedSale(t *testing.T, db *sql.DB, customerID *uuid.UUID) uuid.UUID {
	t.Helper()
	var dayID uuid.UUID
	if err := db.QueryRow(`INSERT INTO business_days (business_date, status)
		VALUES (DATE '2026-09-21', 'open')
		ON CONFLICT (business_date) DO UPDATE SET status = EXCLUDED.status
		RETURNING id`).Scan(&dayID); err != nil {
		t.Fatalf("seed day: %v", err)
	}

	var customerName, customerNTN any
	if customerID != nil {
		if err := db.QueryRow(`SELECT name, ntn FROM customers WHERE id = $1`, *customerID).
			Scan(&customerName, &customerNTN); err != nil {
			t.Fatalf("read customer snapshot: %v", err)
		}
	}

	var invoiceID uuid.UUID
	if err := db.QueryRow(`INSERT INTO invoices
		(invoice_number, business_day_id, business_date, customer_id, customer_name, customer_ntn,
		 subtotal, tax_rate, tax_amount, total_amount, total_payable, payment_method, fiscal_status)
		VALUES ('20260921-001', $1, DATE '2026-09-21', $2, $3, $4,
		        3312.50, 0.18, 596.25, 3908.75, 3909, 'cash', 'pending')
		RETURNING id`, dayID, customerID, customerName, customerNTN).Scan(&invoiceID); err != nil {
		t.Fatalf("seed invoice: %v", err)
	}

	// hs_code is deliberately NULL so the payload has to fall back to
	// settings.default_hs_code (CLAUDE.md invariant 7).
	if _, err := db.Exec(`INSERT INTO invoice_lines
		(invoice_id, product_name, quantity, unit_price, line_total, line_discount, line_tax, sort_order)
		VALUES ($1, 'LPG bulk', 12.500, 265.00, 3312.50, 0, 596.25, 0)`, invoiceID); err != nil {
		t.Fatalf("seed line: %v", err)
	}
	return invoiceID
}

// voidInvoice marks the sale voided the way the till does, including the
// append-only void_log row a debit note reads its reason from.
func voidInvoice(t *testing.T, db *sql.DB, invoiceID uuid.UUID, reason string) {
	t.Helper()
	if _, err := db.Exec(`UPDATE invoices SET status = 'voided', voided_at = now(), void_reason = $2
		WHERE id = $1`, invoiceID, reason); err != nil {
		t.Fatalf("void invoice: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO void_log (invoice_id, invoice_number, total_payable, reason)
		VALUES ($1, '20260921-001', 3909, $2)`, invoiceID, reason); err != nil {
		t.Fatalf("void log: %v", err)
	}
}

type invoiceFiscalState struct {
	status        string
	number        sql.NullString
	voidStatus    string
	debitNote     sql.NullString
	detailsAsText sql.NullString
}

func readFiscalState(t *testing.T, db *sql.DB, invoiceID uuid.UUID) invoiceFiscalState {
	t.Helper()
	var s invoiceFiscalState
	if err := db.QueryRow(`SELECT fiscal_status, fiscal_invoice_number, fiscal_void_status,
		fiscal_debit_note_number, fiscal_details::text FROM invoices WHERE id = $1`, invoiceID).
		Scan(&s.status, &s.number, &s.voidStatus, &s.debitNote, &s.detailsAsText); err != nil {
		t.Fatalf("read invoice: %v", err)
	}
	return s
}

type auditRow struct {
	kind, phase, outcome string
	errorCode            sql.NullString
	httpStatus           sql.NullInt64
	latencyMs            sql.NullInt64
	request              sql.NullString
	response             sql.NullString
}

func readAudit(t *testing.T, db *sql.DB, invoiceID uuid.UUID) []auditRow {
	t.Helper()
	rows, err := db.Query(`SELECT kind, phase, outcome, error_code, http_status, latency_ms,
		request_excerpt, response_excerpt FROM fiscal_audit_events
		WHERE invoice_id = $1 ORDER BY created_at, id`, invoiceID)
	if err != nil {
		t.Fatalf("read audit: %v", err)
	}
	defer rows.Close()
	var out []auditRow
	for rows.Next() {
		var r auditRow
		if err := rows.Scan(&r.kind, &r.phase, &r.outcome, &r.errorCode, &r.httpStatus,
			&r.latencyMs, &r.request, &r.response); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func mustOneAudit(t *testing.T, db *sql.DB, invoiceID uuid.UUID) auditRow {
	t.Helper()
	rows := readAudit(t, db, invoiceID)
	if len(rows) != 1 {
		t.Fatalf("audit rows = %d, want 1: %+v", len(rows), rows)
	}
	return rows[0]
}

func countFiled(t *testing.T, db *sql.DB, invoiceID uuid.UUID) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM fiscal_invoices WHERE invoice_id = $1`, invoiceID).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func deps(db *sql.DB) fiscal.Deps {
	return fiscal.Deps{DB: db, Timeout: 5 * time.Second, Now: filedAt}
}

// ── the happy path ─────────────────────────────────────────────────────────

func TestSubmit_SaleHappyPath(t *testing.T) {
	sandboxEnv(t)
	db := testdb.Fresh(t)
	s := newStub(t, fixtureBody(t, "resp_caseC.json"), fixtureBody(t, "resp_postA.json"))
	enableFiscal(t, db, s, true)
	invoiceID := seedSale(t, db, nil)

	number, err := fiscal.Submit(context.Background(), deps(db), invoiceID, fiscal.KindSale, nil)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if number != "6110180871403DIAJGEJ4031308" {
		t.Fatalf("number = %q", number)
	}
	if s.validateHits != 1 || s.postHits != 1 {
		t.Fatalf("validate then post, once each: %d/%d", s.validateHits, s.postHits)
	}
	if s.lastAuth != "Bearer "+sandboxToken {
		t.Fatalf("Authorization = %q — the stored token must decrypt and travel in the header", s.lastAuth)
	}

	// The payload carries §6.3's numbers verbatim; fiscal never recomputes.
	payload := s.payload(t)
	if payload["invoiceType"] != "Sale Invoice" {
		t.Fatalf("invoiceType = %v", payload["invoiceType"])
	}
	if payload["scenarioId"] != "SN002" {
		t.Fatalf("a walk-in in the sandbox is SN002, got %v", payload["scenarioId"])
	}
	if payload["buyerBusinessName"] != "Walk-in Customer" {
		t.Fatalf("buyer = %v", payload["buyerBusinessName"])
	}
	items, ok := payload["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("items = %v", payload["items"])
	}
	item := items[0].(map[string]any)
	if item["hsCode"] != "2711.1910" {
		t.Fatalf("hsCode must come from settings.default_hs_code, got %v", item["hsCode"])
	}
	if item["salesTaxApplicable"] != 596.25 || item["valueSalesExcludingST"] != 3312.5 {
		t.Fatalf("money must be copied from invoice_lines, got %v / %v",
			item["valueSalesExcludingST"], item["salesTaxApplicable"])
	}

	// The filing ledger, then the invoice.
	filed, found, err := fiscal.FindSynced(db, invoiceID, "sale")
	if err != nil || !found || filed != number {
		t.Fatalf("fiscal_invoices: %q found=%v err=%v", filed, found, err)
	}

	state := readFiscalState(t, db, invoiceID)
	if state.status != "synced" || state.number.String != number {
		t.Fatalf("invoice = %s / %q", state.status, state.number.String)
	}
	if state.voidStatus != "unfiled" {
		t.Fatalf("a sale must not touch the void status, got %q", state.voidStatus)
	}
	var details struct {
		Kind        string   `json:"kind"`
		ScenarioID  string   `json:"scenario_id"`
		FiledAt     string   `json:"filed_at"`
		LineNumbers []string `json:"line_numbers"`
		IsSandbox   bool     `json:"is_sandbox"`
	}
	if err := json.Unmarshal([]byte(state.detailsAsText.String), &details); err != nil {
		t.Fatalf("fiscal_details: %v (%s)", err, state.detailsAsText.String)
	}
	if details.Kind != "sale" || details.ScenarioID != "SN002" || !details.IsSandbox {
		t.Fatalf("fiscal_details = %+v", details)
	}
	if details.FiledAt != filedAt().UTC().Format(time.RFC3339) {
		t.Fatalf("filed_at = %q", details.FiledAt)
	}
	if len(details.LineNumbers) != 1 || details.LineNumbers[0] != "6110180871403DIAJGEJ4031308-1" {
		t.Fatalf("line_numbers = %v", details.LineNumbers)
	}

	// One audit row: the post that worked. A clean validate is not an event.
	row := mustOneAudit(t, db, invoiceID)
	if row.kind != "sale" || row.phase != "post" || row.outcome != "ok" {
		t.Fatalf("audit = %+v", row)
	}
	if row.errorCode.Valid {
		t.Fatalf("a successful filing has no error code, got %q", row.errorCode.String)
	}
	if !row.latencyMs.Valid || row.httpStatus.Int64 != 200 {
		t.Fatalf("audit latency/status = %+v / %+v", row.latencyMs, row.httpStatus)
	}
	if !strings.Contains(row.request.String, "Sale Invoice") {
		t.Fatalf("request excerpt = %q", row.request.String)
	}
	if !strings.Contains(row.response.String, number) {
		t.Fatalf("response excerpt = %q", row.response.String)
	}
	for _, row := range readAudit(t, db, invoiceID) {
		if strings.Contains(row.request.String, sandboxToken) || strings.Contains(row.response.String, sandboxToken) {
			t.Fatal("the token must never reach an audit excerpt")
		}
	}
}

// ── the ledger is consulted before any HTTP ────────────────────────────────

// panicClient fails the test loudly if Submit opens a connection at all.
type panicClient struct{ t *testing.T }

func (p panicClient) Validate(context.Context, fiscal.DIInvoice) (fiscal.Response, error) {
	p.t.Fatal("Submit talked to FBR for an invoice that was already filed")
	return fiscal.Response{}, nil
}

func (p panicClient) Post(context.Context, fiscal.DIInvoice) (fiscal.Response, error) {
	p.t.Fatal("Submit posted an invoice that was already filed")
	return fiscal.Response{}, nil
}

func (p panicClient) Get(context.Context, string) ([]byte, error) {
	p.t.Fatal("Submit fetched a reference list")
	return nil, nil
}

func TestSubmit_AlreadyFiledRecoversWithoutHTTP(t *testing.T) {
	sandboxEnv(t)
	db := testdb.Fresh(t)
	enableFiscal(t, db, nil, true)
	invoiceID := seedSale(t, db, nil)

	// FBR accepted this invoice on a previous attempt that died before it
	// could write the invoice row.
	if err := fiscal.RecordSynced(db, invoiceID, "sale", "ALREADY-FILED-1", nil); err != nil {
		t.Fatal(err)
	}

	d := deps(db)
	d.NewClient = func(fiscal.Config, string, time.Duration) fiscal.Client { return panicClient{t} }

	number, err := fiscal.Submit(context.Background(), d, invoiceID, fiscal.KindSale, nil)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if number != "ALREADY-FILED-1" {
		t.Fatalf("number = %q, want the one already filed", number)
	}

	state := readFiscalState(t, db, invoiceID)
	if state.status != "synced" || state.number.String != "ALREADY-FILED-1" {
		t.Fatalf("recovery must finish the interrupted write: %s / %q", state.status, state.number.String)
	}

	row := mustOneAudit(t, db, invoiceID)
	if row.phase != "ledger_recovery" || row.outcome != "ok" {
		t.Fatalf("audit = %+v", row)
	}
}

// ── FBR says no ────────────────────────────────────────────────────────────

func TestSubmit_ValidationRejectionFilesNothing(t *testing.T) {
	sandboxEnv(t)
	db := testdb.Fresh(t)
	s := newStub(t, fixtureBody(t, "resp_negTax.json"), fixtureBody(t, "resp_postA.json"))
	enableFiscal(t, db, s, true)
	invoiceID := seedSale(t, db, nil)

	_, err := fiscal.Submit(context.Background(), deps(db), invoiceID, fiscal.KindSale, nil)
	if err == nil {
		t.Fatal("expected a rejection")
	}
	var rejected *fiscal.RejectedError
	if !errors.As(err, &rejected) {
		t.Fatalf("err = %v (%T), want *fiscal.RejectedError", err, err)
	}
	if rejected.Code != "0104" || rejected.Line != 1 {
		t.Fatalf("rejection = %+v", rejected)
	}
	if got := rejected.Error(); got != "fiscal_rejected 0104" {
		t.Fatalf("Error() = %q", got)
	}
	if fiscal.ErrorCode(err) != "fiscal_rejected" {
		t.Fatalf("ErrorCode = %q", fiscal.ErrorCode(err))
	}

	if s.postHits != 0 {
		t.Fatal("a document that fails validation is never posted")
	}
	if countFiled(t, db, invoiceID) != 0 {
		t.Fatal("nothing may be recorded as filed")
	}
	state := readFiscalState(t, db, invoiceID)
	if state.status != "pending" || state.number.Valid {
		t.Fatalf("invoice = %s / %q", state.status, state.number.String)
	}

	row := mustOneAudit(t, db, invoiceID)
	if row.phase != "validate" || row.outcome != "rejected" || row.errorCode.String != "0104" {
		t.Fatalf("audit = %+v", row)
	}
	if !strings.Contains(row.response.String, "0104") {
		t.Fatalf("FBR's own words belong in the audit trail: %q", row.response.String)
	}
}

// ── FBR cannot be reached ──────────────────────────────────────────────────

func TestSubmit_UnreachableLeavesTheInvoicePending(t *testing.T) {
	sandboxEnv(t)
	db := testdb.Fresh(t)
	s := newStub(t, []byte(`<html>bad gateway</html>`), fixtureBody(t, "resp_postA.json"))
	s.validateStatus = http.StatusBadGateway
	enableFiscal(t, db, s, true)
	invoiceID := seedSale(t, db, nil)

	_, err := fiscal.Submit(context.Background(), deps(db), invoiceID, fiscal.KindSale, nil)
	if !errors.Is(err, fiscal.ErrUnreachable) {
		t.Fatalf("err = %v, want fiscal_unreachable", err)
	}
	var rejected *fiscal.RejectedError
	if errors.As(err, &rejected) {
		t.Fatal("a gateway failure is not FBR refusing the invoice")
	}

	if countFiled(t, db, invoiceID) != 0 {
		t.Fatal("nothing may be recorded as filed")
	}
	state := readFiscalState(t, db, invoiceID)
	if state.status != "pending" {
		t.Fatalf("an unreachable FBR leaves the invoice pending for the retry, got %q", state.status)
	}

	row := mustOneAudit(t, db, invoiceID)
	if row.phase != "validate" || row.outcome != "unreachable" || row.errorCode.String != "fiscal_unreachable" {
		t.Fatalf("audit = %+v", row)
	}
	if strings.Contains(row.response.String, sandboxToken) {
		t.Fatal("the token must never reach an audit excerpt")
	}
}

// ── the refusals that never reach the wire ─────────────────────────────────

func TestSubmit_SandboxInReleaseRefusesBeforeAnyHTTP(t *testing.T) {
	db := testdb.Fresh(t)
	t.Setenv("GIN_MODE", "debug")
	t.Setenv(fiscal.EnvSecretsKey, base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef")))
	s := newStub(t, fixtureBody(t, "resp_caseC.json"), fixtureBody(t, "resp_postA.json"))
	enableFiscal(t, db, s, true)
	invoiceID := seedSale(t, db, nil)

	// The store goes live with a sandbox config still in place.
	t.Setenv("GIN_MODE", "release")
	t.Setenv(fiscal.EnvAllowSandbox, "")

	_, err := fiscal.Submit(context.Background(), deps(db), invoiceID, fiscal.KindSale, nil)
	if !errors.Is(err, fiscal.ErrSandboxInRelease) {
		t.Fatalf("err = %v, want sandbox_in_release", err)
	}
	if s.hits() != 0 {
		t.Fatal("a refused submission must not touch FBR at all")
	}
	if countFiled(t, db, invoiceID) != 0 {
		t.Fatal("nothing may be recorded as filed")
	}

	row := mustOneAudit(t, db, invoiceID)
	if row.phase != "refused" || row.outcome != "refused" || row.errorCode.String != "sandbox_in_release" {
		t.Fatalf("audit = %+v", row)
	}
}

func TestSubmit_DisabledAndTokenMissing(t *testing.T) {
	sandboxEnv(t)
	db := testdb.Fresh(t)
	s := newStub(t, fixtureBody(t, "resp_caseC.json"), fixtureBody(t, "resp_postA.json"))
	invoiceID := seedSale(t, db, nil)

	// Straight off the migration's seed: enabled false.
	if _, err := fiscal.Submit(context.Background(), deps(db), invoiceID, fiscal.KindSale, nil); !errors.Is(err, fiscal.ErrDisabled) {
		t.Fatalf("err = %v, want fiscal_disabled", err)
	}

	enableFiscal(t, db, s, true)
	putSetting(t, db, fiscal.KeyAPIKeyEnc, `""`)
	if _, err := fiscal.Submit(context.Background(), deps(db), invoiceID, fiscal.KindSale, nil); !errors.Is(err, fiscal.ErrTokenMissing) {
		t.Fatalf("err = %v, want fiscal_token_missing", err)
	}
	if s.hits() != 0 {
		t.Fatal("neither refusal may touch FBR")
	}
}

// A line with no hs_code and no settings default is refused, never guessed
// (CLAUDE.md invariant 7).
func TestSubmit_MissingHSCodeIsRefused(t *testing.T) {
	sandboxEnv(t)
	db := testdb.Fresh(t)
	s := newStub(t, fixtureBody(t, "resp_caseC.json"), fixtureBody(t, "resp_postA.json"))
	enableFiscal(t, db, s, true)
	putSetting(t, db, "default_hs_code", `""`)
	invoiceID := seedSale(t, db, nil)

	_, err := fiscal.Submit(context.Background(), deps(db), invoiceID, fiscal.KindSale, nil)
	if !errors.Is(err, fiscal.ErrHSCodeMissing) {
		t.Fatalf("err = %v, want fiscal_hs_code_missing", err)
	}
	if s.hits() != 0 {
		t.Fatal("a payload that cannot be built is never sent")
	}
	row := mustOneAudit(t, db, invoiceID)
	if row.phase != "refused" || row.errorCode.String != "fiscal_hs_code_missing" {
		t.Fatalf("audit = %+v", row)
	}
}

// ── debit notes ────────────────────────────────────────────────────────────

func TestSubmit_DebitNoteNeedsARegisteredBuyer(t *testing.T) {
	sandboxEnv(t)
	db := testdb.Fresh(t)
	s := newStub(t, fixtureBody(t, "resp_caseC.json"), fixtureBody(t, "resp_postA.json"))
	enableFiscal(t, db, s, true)

	customerID := seedCustomer(t, db, "Anwar Traders", "", "Unregistered")
	invoiceID := seedSale(t, db, &customerID)
	if err := fiscal.RecordSynced(db, invoiceID, "sale", "SALE-FILED-1", nil); err != nil {
		t.Fatal(err)
	}
	voidInvoice(t, db, invoiceID, "Wrong customer at the till")

	_, err := fiscal.Submit(context.Background(), deps(db), invoiceID, fiscal.KindDebitNote, nil)
	if !errors.Is(err, fiscal.ErrBuyerUnregistered) {
		t.Fatalf("err = %v, want fiscal_buyer_unregistered (sandbox 0106)", err)
	}
	if s.hits() != 0 {
		t.Fatal("an unfilable void must not touch FBR")
	}

	state := readFiscalState(t, db, invoiceID)
	if state.voidStatus != "unfiled" || state.debitNote.Valid {
		t.Fatalf("the void stays unfiled: %s / %q", state.voidStatus, state.debitNote.String)
	}
}

func TestSubmit_DebitNoteRefusedWhenTheSaleWasNeverFiled(t *testing.T) {
	sandboxEnv(t)
	db := testdb.Fresh(t)
	s := newStub(t, fixtureBody(t, "resp_caseC.json"), fixtureBody(t, "resp_postA.json"))
	enableFiscal(t, db, s, true)

	customerID := seedCustomer(t, db, "Registered Traders", "1234567", "Registered")
	invoiceID := seedSale(t, db, &customerID)
	voidInvoice(t, db, invoiceID, "Wrong quantity")

	_, err := fiscal.Submit(context.Background(), deps(db), invoiceID, fiscal.KindDebitNote, nil)
	if !errors.Is(err, fiscal.ErrNotSynced) {
		t.Fatalf("err = %v, want fiscal_not_synced", err)
	}
	if s.hits() != 0 {
		t.Fatal("there is nothing to reverse, so nothing is sent")
	}
}

// The void path's whole shape: a Debit Note referencing the sale's FBR
// number, carrying the cashier's reason, landing on invoices — never on
// void_log, which is append-only.
func TestSubmit_DebitNoteHappyPathLeavesVoidLogAlone(t *testing.T) {
	sandboxEnv(t)
	db := testdb.Fresh(t)
	s := newStub(t, fixtureBody(t, "resp_caseC.json"), fixtureBody(t, "resp_postA.json"))
	enableFiscal(t, db, s, true)

	customerID := seedCustomer(t, db, "Registered Traders", "1234567", "Registered")
	invoiceID := seedSale(t, db, &customerID)
	if err := fiscal.RecordSynced(db, invoiceID, "sale", "SALE-FILED-1", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE invoices SET fiscal_status = 'synced', fiscal_invoice_number = 'SALE-FILED-1',
		fiscal_details = '{"kind":"sale","scenario_id":"SN001"}'::jsonb WHERE id = $1`, invoiceID); err != nil {
		t.Fatal(err)
	}
	voidInvoice(t, db, invoiceID, "Sale voided at the till")

	number, err := fiscal.Submit(context.Background(), deps(db), invoiceID, fiscal.KindDebitNote, nil)
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	if number != "6110180871403DIAJGEJ4031308" {
		t.Fatalf("number = %q", number)
	}

	payload := s.payload(t)
	if payload["invoiceType"] != "Debit Note" {
		t.Fatalf("invoiceType = %v (there is no Credit Note in DI)", payload["invoiceType"])
	}
	if payload["invoiceRefNo"] != "SALE-FILED-1" {
		t.Fatalf("invoiceRefNo = %v", payload["invoiceRefNo"])
	}
	if payload["reason"] != "Sale voided at the till" {
		t.Fatalf("reason = %v (a Debit Note without one is rejected 0027)", payload["reason"])
	}
	if payload["scenarioId"] != "SN001" {
		t.Fatalf("a registered buyer in the sandbox is SN001, got %v", payload["scenarioId"])
	}

	state := readFiscalState(t, db, invoiceID)
	if state.voidStatus != "synced" || state.debitNote.String != number {
		t.Fatalf("void state = %s / %q", state.voidStatus, state.debitNote.String)
	}
	if state.status != "synced" || state.number.String != "SALE-FILED-1" {
		t.Fatalf("the sale's own filing must be untouched: %s / %q", state.status, state.number.String)
	}
	if !strings.Contains(state.detailsAsText.String, "SN001") || strings.Contains(state.detailsAsText.String, "debit_note") {
		t.Fatalf("fiscal_details belongs to the sale: %s", state.detailsAsText.String)
	}

	// void_log is append-only and fiscal code only ever reads it.
	var creditNote sql.NullString
	var voidRows int
	if err := db.QueryRow(`SELECT count(*), max(fiscal_credit_note) FROM void_log WHERE invoice_id = $1`,
		invoiceID).Scan(&voidRows, &creditNote); err != nil {
		t.Fatal(err)
	}
	if voidRows != 1 || creditNote.Valid {
		t.Fatalf("void_log must be untouched: %d rows, credit note %q", voidRows, creditNote.String)
	}

	filed, found, err := fiscal.FindSynced(db, invoiceID, "debit_note")
	if err != nil || !found || filed != number {
		t.Fatalf("fiscal_invoices debit_note: %q found=%v err=%v", filed, found, err)
	}
}

// ── MarkFailed ─────────────────────────────────────────────────────────────

func TestMarkFailed_RecordsTheCodeOnTheRightColumn(t *testing.T) {
	sandboxEnv(t)
	db := testdb.Fresh(t)
	invoiceID := seedSale(t, db, nil)

	rejected := &fiscal.RejectedError{Code: "0104", Message: "Provided sales tax amount does not match", Line: 1}
	if err := fiscal.MarkFailed(db, invoiceID, fiscal.KindSale, rejected); err != nil {
		t.Fatalf("mark failed: %v", err)
	}
	state := readFiscalState(t, db, invoiceID)
	if state.status != "failed" {
		t.Fatalf("fiscal_status = %q", state.status)
	}
	var details map[string]any
	if err := json.Unmarshal([]byte(state.detailsAsText.String), &details); err != nil {
		t.Fatal(err)
	}
	if details["error_code"] != "0104" || details["error"] != "Provided sales tax amount does not match" {
		t.Fatalf("fiscal_details = %v", details)
	}

	if err := fiscal.MarkFailed(db, invoiceID, fiscal.KindDebitNote, fiscal.ErrBuyerUnregistered); err != nil {
		t.Fatalf("mark failed: %v", err)
	}
	state = readFiscalState(t, db, invoiceID)
	if state.voidStatus != "failed" {
		t.Fatalf("fiscal_void_status = %q", state.voidStatus)
	}
	if err := json.Unmarshal([]byte(state.detailsAsText.String), &details); err != nil {
		t.Fatal(err)
	}
	if details["error_code"] != "fiscal_buyer_unregistered" {
		t.Fatalf("fiscal_details = %v", details)
	}
}
