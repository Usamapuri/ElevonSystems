package handlers

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"elevon-backend/internal/fiscal"
	"elevon-backend/internal/middleware"
	"elevon-backend/internal/models"
	"elevon-backend/internal/testdb"
	"elevon-backend/internal/util"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// The admin side of on-demand filing (spec 2026-10-12 §2.3, §2.4, §2.7):
// token slots, the per-environment test, eligibility and batches, the
// queue, and the pending summary every signed-in user may read.

func fiscalAdminRouter(db *sql.DB, a actor) *gin.Engine {
	r := gin.New()
	h := NewFiscalHandler(db, fiscal.Deps{DB: db, Timeout: 5 * time.Second})
	staff := r.Group("", asActor(a), middleware.RequireRoles(util.AllRoles))
	staff.GET("/fiscal/pending", h.Pending)
	admin := r.Group("/admin", asActor(a), middleware.RequireRoles([]string{util.RoleAdmin}))
	admin.GET("/fiscal/status", h.Status)
	admin.PUT("/fiscal/token", h.SaveToken)
	admin.DELETE("/fiscal/token", h.DeleteToken)
	admin.POST("/fiscal/test", h.Test)
	admin.POST("/fiscal/reference/refresh", h.RefreshReference)
	admin.GET("/fiscal/eligible", h.Eligible)
	admin.POST("/fiscal/batches", h.CreateBatch)
	admin.GET("/fiscal/batches", h.ListBatches)
	admin.GET("/fiscal/batches/:id", h.GetBatch)
	admin.GET("/fiscal/jobs", h.ListJobs)
	admin.POST("/fiscal/jobs/:id/retry", h.RetryJob)
	admin.POST("/fiscal/invoices/:id/submit", h.SubmitInvoice)
	return r
}

type tokenState struct {
	Set    bool   `json:"set"`
	Masked string `json:"masked"`
}

type statusView struct {
	Enabled bool                  `json:"enabled"`
	Tokens  map[string]tokenState `json:"tokens"`
	Queue   struct {
		Pending int `json:"pending"`
		Dead    int `json:"dead"`
	} `json:"queue"`
	SecretsKeySet bool `json:"secrets_key_set"`
}

func readStatus(t *testing.T, r http.Handler) statusView {
	t.Helper()
	w := doJSON(r, http.MethodGet, "/admin/fiscal/status", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("status: %d %s", w.Code, w.Body.String())
	}
	var v statusView
	dataAs(t, decodeEnvelope(t, w), &v)
	return v
}

func TestFiscalStatus_TokensMaskedPerEnvironment(t *testing.T) {
	db := testdb.Fresh(t)
	adminID := seedUser(t, db, "owner", "admin", "owner-pass-1", nil)
	t.Setenv("GIN_MODE", "debug")
	t.Setenv(fiscal.EnvSecretsKey, "")
	r := fiscalAdminRouter(db, actor{id: adminID, username: "owner", role: "admin"})

	v := readStatus(t, r)
	if v.Tokens["sandbox"].Set || v.Tokens["production"].Set || v.Tokens["sandbox"].Masked != "" {
		t.Fatalf("fresh: %+v", v)
	}

	w := doJSON(r, http.MethodPut, "/admin/fiscal/token", map[string]any{"environment": "sandbox", "api_key": "a8a942d8-sandbox-1234"})
	if w.Code != http.StatusOK {
		t.Fatalf("save sandbox: %d %s", w.Code, w.Body.String())
	}
	v = readStatus(t, r)
	if !v.Tokens["sandbox"].Set || v.Tokens["sandbox"].Masked != "****1234" || v.Tokens["production"].Set {
		t.Fatalf("after sandbox save: %+v", v.Tokens)
	}
	w = doJSON(r, http.MethodPut, "/admin/fiscal/token", map[string]any{"environment": "production", "api_key": "live-token-5678"})
	if w.Code != http.StatusOK {
		t.Fatalf("save production: %d %s", w.Code, w.Body.String())
	}
	v = readStatus(t, r)
	if v.Tokens["production"].Masked != "****5678" || v.Tokens["sandbox"].Masked != "****1234" {
		t.Fatalf("slots independent: %+v", v.Tokens)
	}
	if w.Body.String() != "" && (strings.Contains(w.Body.String(), "live-token-5678") || strings.Contains(w.Body.String(), "a8a942d8")) {
		t.Fatal("a token must never be echoed")
	}

	for _, bad := range []map[string]any{{"environment": "live", "api_key": "x-long-enough"}, {"environment": "sandbox", "api_key": "short"}} {
		w = doJSON(r, http.MethodPut, "/admin/fiscal/token", bad)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("%v: %d %s", bad, w.Code, w.Body.String())
		}
	}
	w = doJSON(r, http.MethodPut, "/admin/fiscal/token", map[string]any{"environment": "live", "api_key": "x-long-enough"})
	if errCode(decodeEnvelope(t, w)) != "invalid_environment" {
		t.Fatalf("bad environment: %s", w.Body.String())
	}

	w = doJSON(r, http.MethodDelete, "/admin/fiscal/token", map[string]any{"environment": "production"})
	if w.Code != http.StatusOK || readStatus(t, r).Tokens["production"].Set {
		t.Fatalf("delete production: %d %s", w.Code, w.Body.String())
	}
}

func TestFiscalToken_ActiveSlotCannotBeClearedWhileEnabled(t *testing.T) {
	db := testdb.Fresh(t)
	adminID := seedUser(t, db, "owner", "admin", "owner-pass-1", nil)
	enableFiscalAt(t, db, acceptingFBR(t)) // sandbox, enabled
	r := fiscalAdminRouter(db, actor{id: adminID, username: "owner", role: "admin"})

	w := doJSON(r, http.MethodDelete, "/admin/fiscal/token", map[string]any{"environment": "sandbox"})
	if w.Code != http.StatusConflict || errCode(decodeEnvelope(t, w)) != "fiscal_enabled" {
		t.Fatalf("active slot: %d %s", w.Code, w.Body.String())
	}
	w = doJSON(r, http.MethodDelete, "/admin/fiscal/token", map[string]any{"environment": "production"})
	if w.Code != http.StatusOK {
		t.Fatalf("the idle slot may be cleared: %d %s", w.Code, w.Body.String())
	}
}

func TestFiscalTest_ReturnsTheResultBody(t *testing.T) {
	db := testdb.Fresh(t)
	adminID := seedUser(t, db, "owner", "admin", "owner-pass-1", nil)
	enableFiscalAt(t, db, acceptingFBR(t))
	r := fiscalAdminRouter(db, actor{id: adminID, username: "owner", role: "admin"})

	w := doJSON(r, http.MethodPost, "/admin/fiscal/test", map[string]any{"environment": "sandbox"})
	if w.Code != http.StatusOK {
		t.Fatalf("test: %d %s", w.Code, w.Body.String())
	}
	var res struct {
		OK            bool            `json:"ok"`
		Environment   string          `json:"environment"`
		InvoiceNumber string          `json:"invoice_number"`
		RequestBody   json.RawMessage `json:"request_body"`
		Steps         []struct {
			Phase string `json:"phase"`
		} `json:"steps"`
	}
	dataAs(t, decodeEnvelope(t, w), &res)
	if !res.OK || res.Environment != "sandbox" || res.InvoiceNumber == "" || len(res.Steps) != 2 || len(res.RequestBody) == 0 {
		t.Fatalf("result: %+v", res)
	}
	// A failure is still a 200 with ok:false — the dialog shows it.
	w = doJSON(r, http.MethodPost, "/admin/fiscal/test", map[string]any{"environment": "production"})
	if w.Code != http.StatusOK {
		t.Fatalf("production without a token: %d %s", w.Code, w.Body.String())
	}
	var failed struct {
		OK        bool   `json:"ok"`
		ErrorCode string `json:"error_code"`
	}
	dataAs(t, decodeEnvelope(t, w), &failed)
	if failed.OK || failed.ErrorCode != "fiscal_token_missing" {
		t.Fatalf("failed result: %+v", failed)
	}
}

type pendingResp struct {
	Enabled bool `json:"enabled"`
	Today   struct {
		Count        int     `json:"count"`
		Kg           float64 `json:"kg"`
		TotalPayable int64   `json:"total_payable"`
	} `json:"today"`
	All struct {
		Count int `json:"count"`
	} `json:"all"`
}

// Review Focus 4: 'off' invoices never count; 'unfiled' ones survive fiscal
// being switched off and come back when it is on.
func TestFiscalPending_CountsOnlyUnfiledAndFailed(t *testing.T) {
	db := testdb.Fresh(t)
	adminID, productID := fiscalSetup(t, db)
	counterID := seedUser(t, db, "counter", "counter", "counter-pass-1", nil)
	till := fiscalRouter(db, actor{id: adminID, username: "owner", role: "admin"})
	ringSale(t, till, productID, fiscalSale{}) // fiscal off → 'off'
	enableFiscalAt(t, db, acceptingFBR(t))
	a := ringSale(t, till, productID, fiscalSale{})         // unfiled
	ringSale(t, till, productID, fiscalSale{})              // unfiled
	ringSale(t, till, productID, fiscalSale{fileFBR: true}) // synced
	voided := ringSale(t, till, productID, fiscalSale{})    // unfiled, then voided
	doJSON(till, http.MethodPost, voidPath(voided), models.VoidInvoiceRequest{Reason: "wrong weight", Pin: "4321"})
	if _, err := db.Exec(`UPDATE invoices SET fiscal_status = 'failed' WHERE id = $1`, a.ID); err != nil {
		t.Fatal(err)
	}

	r := fiscalAdminRouter(db, actor{id: counterID, username: "counter", role: "counter"})
	w := doJSON(r, http.MethodGet, "/fiscal/pending", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("pending is staff-readable: %d %s", w.Code, w.Body.String())
	}
	var p pendingResp
	dataAs(t, decodeEnvelope(t, w), &p)
	if !p.Enabled || p.Today.Count != 2 || p.Today.Kg != 25 || p.Today.TotalPayable != 2*3909 || p.All.Count != 2 {
		t.Fatalf("unfiled + failed, not off/synced/voided: %+v", p)
	}

	w = doJSON(r, http.MethodGet, "/admin/fiscal/eligible?business_date="+businessToday().Format("2006-01-02"), nil)
	if w.Code != http.StatusForbidden {
		t.Fatalf("admin routes are admin only: %d", w.Code)
	}
	ar := fiscalAdminRouter(db, actor{id: adminID, username: "owner", role: "admin"})
	w = doJSON(ar, http.MethodGet, "/admin/fiscal/eligible?business_date="+businessToday().Format("2006-01-02"), nil)
	var rows []fiscal.EligibleRow
	dataAs(t, decodeEnvelope(t, w), &rows)
	if len(rows) != 2 {
		t.Fatalf("eligible lists the same two: %+v", rows)
	}

	// Fiscal off: the pending line goes quiet, but the rows are still there
	// for when it comes back.
	if _, err := db.Exec(`UPDATE settings SET value = jsonb_set(value, '{enabled}', 'false') WHERE key = 'fiscal_config'`); err != nil {
		t.Fatal(err)
	}
	w = doJSON(r, http.MethodGet, "/fiscal/pending", nil)
	dataAs(t, decodeEnvelope(t, w), &p)
	if p.Enabled || p.Today.Count != 0 {
		t.Fatalf("disabled: %+v", p)
	}
	w = doJSON(ar, http.MethodGet, "/admin/fiscal/eligible?business_date="+businessToday().Format("2006-01-02"), nil)
	dataAs(t, decodeEnvelope(t, w), &rows)
	if len(rows) != 2 {
		t.Fatalf("unfiled rows survive fiscal being off: %+v", rows)
	}
}

func batchBody(date time.Time, ids ...uuid.UUID) map[string]any {
	s := make([]string, len(ids))
	for i, id := range ids {
		s[i] = id.String()
	}
	return map[string]any{"business_date": date.Format("2006-01-02"), "invoice_ids": s}
}

func TestFiscalBatches_CreateSyncsInline(t *testing.T) {
	db := testdb.Fresh(t)
	adminID, productID := fiscalSetup(t, db)
	enableFiscalAt(t, db, acceptingFBR(t))
	till := fiscalRouter(db, actor{id: adminID, username: "owner", role: "admin"})
	a := ringSale(t, till, productID, fiscalSale{})
	b := ringSale(t, till, productID, fiscalSale{})
	r := fiscalAdminRouter(db, actor{id: adminID, username: "owner", role: "admin"})

	w := doJSON(r, http.MethodPost, "/admin/fiscal/batches", batchBody(businessToday(), a.ID, b.ID))
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var batch fiscal.Batch
	dataAs(t, decodeEnvelope(t, w), &batch)
	if batch.Status != "synced" || batch.FBRInvoiceNumber == nil || batch.InvoiceCount != 2 {
		t.Fatalf("synced inline: %+v", batch)
	}

	w = doJSON(r, http.MethodGet, "/admin/fiscal/batches/"+batch.ID.String(), nil)
	var detail struct {
		Batch    fiscal.Batch         `json:"batch"`
		Invoices []fiscal.EligibleRow `json:"invoices"`
	}
	dataAs(t, decodeEnvelope(t, w), &detail)
	if detail.Batch.ID != batch.ID || len(detail.Invoices) != 2 {
		t.Fatalf("detail: %+v", detail)
	}
	// Review Focus 3: a covered invoice voided later shows as voided here.
	doJSON(till, http.MethodPost, voidPath(a), models.VoidInvoiceRequest{Reason: "wrong weight", Pin: "4321"})
	w = doJSON(r, http.MethodGet, "/admin/fiscal/batches/"+batch.ID.String(), nil)
	dataAs(t, decodeEnvelope(t, w), &detail)
	voided := 0
	for _, row := range detail.Invoices {
		if row.Status == "voided" {
			voided++
		}
	}
	if voided != 1 {
		t.Fatalf("void shown: %+v", detail.Invoices)
	}

	w = doJSON(r, http.MethodGet, "/admin/fiscal/batches?from="+businessToday().Format("2006-01-02")+"&to="+businessToday().Format("2006-01-02"), nil)
	var list []fiscal.Batch
	dataAs(t, decodeEnvelope(t, w), &list)
	if len(list) != 1 {
		t.Fatalf("list: %+v", list)
	}

	// Already covered → ineligible, with the ids named.
	w = doJSON(r, http.MethodPost, "/admin/fiscal/batches", batchBody(businessToday(), b.ID))
	if w.Code != http.StatusConflict || errCode(decodeEnvelope(t, w)) != "fiscal_batch_ineligible" {
		t.Fatalf("covered again: %d %s", w.Code, w.Body.String())
	}
	var data struct {
		InvoiceIDs []uuid.UUID `json:"invoice_ids"`
	}
	dataAs(t, decodeEnvelope(t, w), &data)
	if len(data.InvoiceIDs) != 1 || data.InvoiceIDs[0] != b.ID {
		t.Fatalf("names the id: %+v", data)
	}
	w = doJSON(r, http.MethodPost, "/admin/fiscal/batches", batchBody(businessToday()))
	if w.Code != http.StatusBadRequest || errCode(decodeEnvelope(t, w)) != "fiscal_batch_empty" {
		t.Fatalf("empty: %d %s", w.Code, w.Body.String())
	}
	w = doJSON(r, http.MethodGet, "/admin/fiscal/batches/"+uuid.NewString(), nil)
	if w.Code != http.StatusNotFound || errCode(decodeEnvelope(t, w)) != "fiscal_batch_not_found" {
		t.Fatalf("unknown: %d %s", w.Code, w.Body.String())
	}
}

func TestFiscalBatches_RejectedReleasesAndUnreachableQueues(t *testing.T) {
	db := testdb.Fresh(t)
	adminID, productID := fiscalSetup(t, db)
	enableFiscalAt(t, db, rejectingFBR(t))
	till := fiscalRouter(db, actor{id: adminID, username: "owner", role: "admin"})
	a := ringSale(t, till, productID, fiscalSale{})
	r := fiscalAdminRouter(db, actor{id: adminID, username: "owner", role: "admin"})

	w := doJSON(r, http.MethodPost, "/admin/fiscal/batches", batchBody(businessToday(), a.ID))
	if w.Code != http.StatusCreated {
		t.Fatalf("rejected still returns the failed batch: %d %s", w.Code, w.Body.String())
	}
	var batch fiscal.Batch
	dataAs(t, decodeEnvelope(t, w), &batch)
	if batch.Status != "failed" || batch.LastErrorCode == nil || *batch.LastErrorCode != "0104" {
		t.Fatalf("failed with the code: %+v", batch)
	}
	var st string
	if err := db.QueryRow(`SELECT fiscal_status FROM invoices WHERE id = $1`, a.ID).Scan(&st); err != nil || st != "unfiled" {
		t.Fatalf("released: %s %v", st, err)
	}

	enableFiscalAt(t, db, unreachableFBR(t))
	w = doJSON(r, http.MethodPost, "/admin/fiscal/batches", batchBody(businessToday(), a.ID))
	if w.Code != http.StatusAccepted {
		t.Fatalf("unreachable queues: %d %s", w.Code, w.Body.String())
	}
	dataAs(t, decodeEnvelope(t, w), &batch)
	var jobs int
	if err := db.QueryRow(`SELECT count(*) FROM fiscal_outbound_jobs WHERE batch_id = $1 AND status = 'pending'`, batch.ID).Scan(&jobs); err != nil || jobs != 1 {
		t.Fatalf("one batch job: %d %v", jobs, err)
	}

	w = doJSON(r, http.MethodGet, "/admin/fiscal/jobs?status=pending", nil)
	var list []map[string]any
	dataAs(t, decodeEnvelope(t, w), &list)
	if len(list) != 1 || list[0]["document"] == nil {
		t.Fatalf("jobs list labels the document: %+v", list)
	}
}

func TestFiscalJobs_RetryDeadAndSubmitInvoice(t *testing.T) {
	db := testdb.Fresh(t)
	adminID, productID := fiscalSetup(t, db)
	enableFiscalAt(t, db, rejectingFBR(t))
	till := fiscalRouter(db, actor{id: adminID, username: "owner", role: "admin"})
	a := ringSale(t, till, productID, fiscalSale{fileFBR: true}) // failed
	r := fiscalAdminRouter(db, actor{id: adminID, username: "owner", role: "admin"})

	enableFiscalAt(t, db, acceptingFBR(t))
	w := doJSON(r, http.MethodPost, "/admin/fiscal/invoices/"+a.ID.String()+"/submit", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("submit a failed sale: %d %s", w.Code, w.Body.String())
	}
	var inv models.Invoice
	dataAs(t, decodeEnvelope(t, w), &inv)
	if inv.FiscalStatus != "synced" {
		t.Fatalf("filed on request: %+v", inv)
	}
	w = doJSON(r, http.MethodPost, "/admin/fiscal/invoices/"+a.ID.String()+"/submit", nil)
	if w.Code != http.StatusConflict || errCode(decodeEnvelope(t, w)) != "fiscal_filing_in_flight" {
		t.Fatalf("already filed: %d %s", w.Code, w.Body.String())
	}

	// A dead job can be retried from the screen.
	b := ringSale(t, till, productID, fiscalSale{})
	if _, err := db.Exec(`INSERT INTO fiscal_outbound_jobs (invoice_id, kind, status, attempt_count, last_error_code)
		VALUES ($1, 'sale', 'dead', 20, 'fiscal_max_attempts')`, b.ID); err != nil {
		t.Fatal(err)
	}
	var jobID uuid.UUID
	if err := db.QueryRow(`SELECT id FROM fiscal_outbound_jobs WHERE invoice_id = $1`, b.ID).Scan(&jobID); err != nil {
		t.Fatal(err)
	}
	w = doJSON(r, http.MethodPost, "/admin/fiscal/jobs/"+jobID.String()+"/retry", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("retry: %d %s", w.Code, w.Body.String())
	}
	var status string
	var attempts int
	if err := db.QueryRow(`SELECT status, attempt_count FROM fiscal_outbound_jobs WHERE id = $1`, jobID).Scan(&status, &attempts); err != nil || status != "pending" || attempts != 0 {
		t.Fatalf("back to pending now: %s %d %v", status, attempts, err)
	}
	w = doJSON(r, http.MethodPost, "/admin/fiscal/jobs/"+uuid.NewString()+"/retry", nil)
	if w.Code != http.StatusNotFound || errCode(decodeEnvelope(t, w)) != "fiscal_job_not_found" {
		t.Fatalf("unknown job: %d %s", w.Code, w.Body.String())
	}
}

func TestFiscalPending_CarriesTheBusinessDate(t *testing.T) {
	db := testdb.Fresh(t)
	adminID := seedUser(t, db, "owner", "admin", "owner-pass-1", nil)
	r := fiscalAdminRouter(db, actor{id: adminID, username: "owner", role: "admin"})
	w := doJSON(r, http.MethodGet, "/fiscal/pending", nil)
	var p struct {
		BusinessDate string `json:"business_date"`
	}
	dataAs(t, decodeEnvelope(t, w), &p)
	if p.BusinessDate != businessToday().Format("2006-01-02") {
		t.Fatalf("business_date = %q", p.BusinessDate)
	}
}

func TestFiscalJobs_RetryRefusesACoveredSale(t *testing.T) {
	db := testdb.Fresh(t)
	adminID, productID := fiscalSetup(t, db)
	enableFiscalAt(t, db, acceptingFBR(t))
	till := fiscalRouter(db, actor{id: adminID, username: "owner", role: "admin"})
	a := ringSale(t, till, productID, fiscalSale{})
	if _, err := db.Exec(`UPDATE invoices SET fiscal_status = 'covered' WHERE id = $1`, a.ID); err != nil {
		t.Fatal(err)
	}
	var jobID uuid.UUID
	if err := db.QueryRow(`INSERT INTO fiscal_outbound_jobs (invoice_id, kind, status, attempt_count, last_error_code)
		VALUES ($1, 'sale', 'dead', 20, 'fiscal_max_attempts') RETURNING id`, a.ID).Scan(&jobID); err != nil {
		t.Fatal(err)
	}
	r := fiscalAdminRouter(db, actor{id: adminID, username: "owner", role: "admin"})
	w := doJSON(r, http.MethodPost, "/admin/fiscal/jobs/"+jobID.String()+"/retry", nil)
	if w.Code != http.StatusConflict || errCode(decodeEnvelope(t, w)) != "fiscal_not_retryable" {
		t.Fatalf("covered sale: %d %s", w.Code, w.Body.String())
	}
}
