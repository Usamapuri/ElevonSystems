package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"elevon-backend/internal/fiscal"
	"elevon-backend/internal/invoice"
	"elevon-backend/internal/models"
	"elevon-backend/internal/settings"
	"elevon-backend/internal/util"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// FiscalHandler is the admin side of FBR filing (spec 2026-10-12): the two
// token slots and the per-environment test, the reference lists, combined
// invoices, the outbound queue, and the pending summary every signed-in user
// may read. Nothing here changes what a customer pays.
type FiscalHandler struct {
	db   *sql.DB
	deps fiscal.Deps
}

// NewFiscalHandler builds a FiscalHandler.
func NewFiscalHandler(db *sql.DB, deps fiscal.Deps) *FiscalHandler {
	return &FiscalHandler{db: db, deps: deps}
}

// batchCallTimeout bounds the inline filing of a combined invoice: it has
// more items than a sale and the operator is waiting on a screen, not at a
// till with a customer.
const batchCallTimeout = 30 * time.Second

func internalFail(c *gin.Context, where string, err error) {
	log.Printf("fiscal %s: %v", where, err)
	c.JSON(http.StatusInternalServerError, models.Fail("Something went wrong on the FBR side of the app", "internal_error"))
}

// ── status and tokens ────────────────────────────────────────────────────

type tokenSlot struct {
	Set    bool   `json:"set"`
	Masked string `json:"masked"`
}

type queueView struct {
	Pending       int        `json:"pending"`
	Dead          int        `json:"dead"`
	LastSuccessAt *time.Time `json:"last_success_at"`
}

type fiscalStatusView struct {
	fiscal.PublicConfig
	Tokens        map[fiscal.Environment]tokenSlot `json:"tokens"`
	Queue         queueView                        `json:"queue"`
	SecretsKeySet bool                             `json:"secrets_key_set"`
	Reference     json.RawMessage                  `json:"reference"`
}

// tokenSlotState reads a slot without decrypting more than it has to: a
// masked tail needs the plaintext, so a key that cannot read the ciphertext
// reports "set" with an empty mask rather than failing the whole screen.
func (h *FiscalHandler) tokenSlotState(env fiscal.Environment) tokenSlot {
	token, err := fiscal.LoadAPIKey(h.db, env)
	if err != nil {
		var raw string
		_ = h.db.QueryRow(`SELECT value #>> '{}' FROM settings WHERE key = $1`, fiscal.KeyFor(env)).Scan(&raw)
		return tokenSlot{Set: strings.TrimSpace(raw) != ""}
	}
	if strings.TrimSpace(token) == "" {
		return tokenSlot{}
	}
	return tokenSlot{Set: true, Masked: fiscal.MaskAPIKey(token)}
}

// Status is GET /admin/fiscal/status.
func (h *FiscalHandler) Status(c *gin.Context) {
	cfg, err := fiscal.LoadConfig(h.db)
	if err != nil {
		internalFail(c, "status", err)
		return
	}
	active := h.tokenSlotState(cfg.Environment())
	v := fiscalStatusView{
		PublicConfig: cfg.Public(active.Set, active.Masked),
		Tokens: map[fiscal.Environment]tokenSlot{
			fiscal.EnvSandbox:    h.tokenSlotState(fiscal.EnvSandbox),
			fiscal.EnvProduction: h.tokenSlotState(fiscal.EnvProduction),
		},
		SecretsKeySet: secretsKeySet(),
		Reference:     json.RawMessage("{}"),
	}
	pending, dead, last, err := fiscal.Counts(h.db)
	if err != nil {
		internalFail(c, "status counts", err)
		return
	}
	v.Queue = queueView{Pending: pending, Dead: dead, LastSuccessAt: last}
	var ref []byte
	if err := h.db.QueryRow(`SELECT value FROM settings WHERE key = $1`, fiscal.KeyReference).Scan(&ref); err == nil && len(ref) > 0 {
		v.Reference = ref
	}
	c.JSON(http.StatusOK, models.OK("", v))
}

func secretsKeySet() bool {
	_, err := fiscal.KeyFromEnv()
	return err == nil
}

type tokenRequest struct {
	Environment string `json:"environment"`
	APIKey      string `json:"api_key"`
}

func parseEnvironment(c *gin.Context, raw string) (fiscal.Environment, bool) {
	env, ok := fiscal.ParseEnvironment(strings.TrimSpace(raw))
	if !ok {
		c.JSON(http.StatusBadRequest, models.Fail("environment must be sandbox or production", "invalid_environment"))
		return "", false
	}
	return env, true
}

// SaveToken is PUT /admin/fiscal/token. The token is encrypted with the
// process's secrets key before it is stored and is never echoed back.
func (h *FiscalHandler) SaveToken(c *gin.Context) {
	var req tokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.Fail("Invalid request body", "invalid_request"))
		return
	}
	env, ok := parseEnvironment(c, req.Environment)
	if !ok {
		return
	}
	token := strings.TrimSpace(req.APIKey)
	if len(token) < 8 || len(token) > 512 {
		c.JSON(http.StatusBadRequest, models.Fail("The token must be between 8 and 512 characters", "invalid_request"))
		return
	}
	key, err := fiscal.KeyFromEnv()
	if err != nil {
		c.JSON(http.StatusInternalServerError, models.Fail("The server has no FISCAL_SECRETS_KEY, so a token cannot be stored", "fiscal_secrets_key_missing"))
		return
	}
	sealed, err := fiscal.Encrypt(key, token)
	if err != nil {
		internalFail(c, "encrypt token", err)
		return
	}
	enc, _ := json.Marshal(sealed)
	if err := settings.Save(h.db, map[string]json.RawMessage{fiscal.KeyFor(env): enc}); err != nil {
		internalFail(c, "save token", err)
		return
	}
	c.JSON(http.StatusOK, models.OK("Token saved", h.tokenSlotState(env)))
}

// DeleteToken is DELETE /admin/fiscal/token. The slot filing currently uses
// cannot be cleared while filing is on.
func (h *FiscalHandler) DeleteToken(c *gin.Context) {
	var req tokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.Fail("Invalid request body", "invalid_request"))
		return
	}
	env, ok := parseEnvironment(c, req.Environment)
	if !ok {
		return
	}
	cfg, err := fiscal.LoadConfig(h.db)
	if err != nil {
		internalFail(c, "delete token", err)
		return
	}
	if cfg.Enabled && cfg.Environment() == env {
		c.JSON(http.StatusConflict, models.Fail("Switch filing off before removing the token it uses", "fiscal_enabled"))
		return
	}
	if err := settings.Save(h.db, map[string]json.RawMessage{fiscal.KeyFor(env): json.RawMessage(`""`)}); err != nil {
		internalFail(c, "clear token", err)
		return
	}
	c.JSON(http.StatusOK, models.OK("Token removed", tokenSlot{}))
}

// Test is POST /admin/fiscal/test. Always 200: a failure is a result the
// dialog shows, not an error.
func (h *FiscalHandler) Test(c *gin.Context) {
	var req tokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.Fail("Invalid request body", "invalid_request"))
		return
	}
	env, ok := parseEnvironment(c, req.Environment)
	if !ok {
		return
	}
	all, err := settings.Load(h.db)
	if err != nil {
		internalFail(c, "test settings", err)
		return
	}
	var hsCode string
	_ = json.Unmarshal(all["default_hs_code"], &hsCode)
	sample := fiscal.TestSample{TaxRate: settingNumber(all, "tax_rate_cash"), DefaultHSCode: hsCode}
	ctx, cancel := context.WithTimeout(c.Request.Context(), batchCallTimeout)
	defer cancel()
	actor := invoiceActor(c)
	res := fiscal.RunTest(ctx, h.deps, env, sample, actorOrNil(actor.ID))
	c.JSON(http.StatusOK, models.OK("", res))
}

// RefreshReference is POST /admin/fiscal/reference/refresh.
func (h *FiscalHandler) RefreshReference(c *gin.Context) {
	cfg, err := fiscal.LoadConfig(h.db)
	if err != nil {
		internalFail(c, "reference config", err)
		return
	}
	token, err := fiscal.LoadAPIKey(h.db, cfg.Environment())
	if err != nil {
		c.JSON(http.StatusConflict, models.Fail("The stored token cannot be read on this server", fiscal.ErrorCode(err)))
		return
	}
	if strings.TrimSpace(token) == "" {
		c.JSON(http.StatusConflict, models.Fail("Save a token for the selected environment first", "fiscal_token_missing"))
		return
	}
	codes := []string{}
	var defaultHS string
	if err := h.db.QueryRow(`SELECT value #>> '{}' FROM settings WHERE key = 'default_hs_code'`).Scan(&defaultHS); err == nil {
		codes = append(codes, defaultHS)
	}
	rows, err := h.db.Query(`SELECT DISTINCT hs_code FROM products WHERE hs_code IS NOT NULL AND hs_code <> ''`)
	if err != nil {
		internalFail(c, "reference hs codes", err)
		return
	}
	for rows.Next() {
		var code string
		if err := rows.Scan(&code); err == nil {
			codes = append(codes, code)
		}
	}
	rows.Close()
	ctx, cancel := context.WithTimeout(c.Request.Context(), batchCallTimeout)
	defer cancel()
	ref, err := fiscal.Refresh(ctx, h.deps, cfg, token, codes, time.Now())
	if err != nil {
		c.JSON(http.StatusBadGateway, models.Fail("FBR's reference lists could not be read", fiscal.ErrorCode(err)))
		return
	}
	raw, err := json.Marshal(ref)
	if err != nil {
		internalFail(c, "reference marshal", err)
		return
	}
	if err := settings.Save(h.db, map[string]json.RawMessage{fiscal.KeyReference: raw}); err != nil {
		internalFail(c, "reference save", err)
		return
	}
	c.JSON(http.StatusOK, models.OK("Reference lists refreshed", ref))
}

// ── pending, eligible, batches ───────────────────────────────────────────

type pendingView struct {
	Enabled bool                  `json:"enabled"`
	Today   fiscal.PendingSummary `json:"today"`
	All     fiscal.PendingSummary `json:"all"`
}

// Pending is GET /fiscal/pending, readable by any signed-in user.
func (h *FiscalHandler) Pending(c *gin.Context) {
	cfg, err := fiscal.LoadConfig(h.db)
	if err != nil {
		internalFail(c, "pending config", err)
		return
	}
	v := pendingView{Enabled: cfg.Enabled}
	if cfg.Enabled {
		v.Today, v.All, err = fiscal.Pending(h.db, util.BusinessDate(time.Now()))
		if err != nil {
			internalFail(c, "pending", err)
			return
		}
	}
	c.JSON(http.StatusOK, models.OK("", v))
}

func businessDateParam(c *gin.Context, key string) (time.Time, bool) {
	raw := strings.TrimSpace(c.Query(key))
	if raw == "" {
		return util.BusinessDate(time.Now()), true
	}
	d, err := time.Parse("2006-01-02", raw)
	if err != nil {
		c.JSON(http.StatusBadRequest, models.Fail(key+" must be YYYY-MM-DD", "invalid_request"))
		return time.Time{}, false
	}
	return d, true
}

// Eligible is GET /admin/fiscal/eligible?business_date=.
func (h *FiscalHandler) Eligible(c *gin.Context) {
	date, ok := businessDateParam(c, "business_date")
	if !ok {
		return
	}
	cfg, err := fiscal.LoadConfig(h.db)
	if err != nil {
		internalFail(c, "eligible config", err)
		return
	}
	rows, err := fiscal.Eligible(h.db, date, cfg)
	if err != nil {
		internalFail(c, "eligible", err)
		return
	}
	c.JSON(http.StatusOK, models.OK("", rows))
}

type createBatchRequest struct {
	BusinessDate string   `json:"business_date"`
	InvoiceIDs   []string `json:"invoice_ids"`
}

// CreateBatch is POST /admin/fiscal/batches: claim, then file inline.
func (h *FiscalHandler) CreateBatch(c *gin.Context) {
	var req createBatchRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.Fail("Invalid request body", "invalid_request"))
		return
	}
	date, err := time.Parse("2006-01-02", strings.TrimSpace(req.BusinessDate))
	if err != nil {
		c.JSON(http.StatusBadRequest, models.Fail("business_date must be YYYY-MM-DD", "invalid_request"))
		return
	}
	ids := make([]uuid.UUID, 0, len(req.InvoiceIDs))
	for _, raw := range req.InvoiceIDs {
		id, err := uuid.Parse(strings.TrimSpace(raw))
		if err != nil {
			c.JSON(http.StatusBadRequest, models.Fail("invoice_ids must be UUIDs", "invalid_request"))
			return
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		c.JSON(http.StatusBadRequest, models.Fail("Select at least one invoice", "fiscal_batch_empty"))
		return
	}
	cfg, err := fiscal.LoadConfig(h.db)
	if err != nil {
		internalFail(c, "batch config", err)
		return
	}
	if !cfg.Enabled {
		c.JSON(http.StatusConflict, models.Fail("FBR filing is switched off", "fiscal_disabled"))
		return
	}
	actor := invoiceActor(c)
	b, err := fiscal.CreateBatch(h.db, date, ids, cfg, actorOrNil(actor.ID))
	var ineligible *fiscal.IneligibleError
	switch {
	case errors.As(err, &ineligible):
		c.JSON(http.StatusConflict, models.APIResponse{
			Success: false,
			Message: "Some of these invoices cannot be filed together",
			Error:   models.StringPtr("fiscal_batch_ineligible"),
			Data:    gin.H{"invoice_ids": ineligible.IDs},
		})
		return
	case errors.Is(err, fiscal.ErrBatchEmpty):
		c.JSON(http.StatusBadRequest, models.Fail("Select at least one invoice", "fiscal_batch_empty"))
		return
	case err != nil:
		internalFail(c, "create batch", err)
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), batchCallTimeout)
	defer cancel()
	_, ferr := fiscal.SubmitBatch(ctx, h.deps, b.ID, actorOrNil(actor.ID))
	status := http.StatusCreated
	switch {
	case ferr == nil:
	case errors.Is(ferr, fiscal.ErrUnreachable) || errors.Is(ferr, context.DeadlineExceeded):
		if err := fiscal.Enqueue(h.db, fiscal.JobRef{BatchID: &b.ID}, fiscal.KindBatch, time.Now()); err != nil {
			log.Printf("fiscal batch: enqueue: %v", err)
		}
		status = http.StatusAccepted
	default:
		if err := fiscal.FailBatch(h.db, b.ID, ferr); err != nil {
			log.Printf("fiscal batch: fail: %v", err)
		}
	}
	after, _, err := fiscal.LoadBatch(h.db, b.ID)
	if err != nil {
		internalFail(c, "reload batch", err)
		return
	}
	c.JSON(status, models.OK("", after))
}

// ListBatches is GET /admin/fiscal/batches?from=&to=.
func (h *FiscalHandler) ListBatches(c *gin.Context) {
	to, ok := businessDateParam(c, "to")
	if !ok {
		return
	}
	from := to.AddDate(0, 0, -30)
	if raw := strings.TrimSpace(c.Query("from")); raw != "" {
		d, err := time.Parse("2006-01-02", raw)
		if err != nil {
			c.JSON(http.StatusBadRequest, models.Fail("from must be YYYY-MM-DD", "invalid_request"))
			return
		}
		from = d
	}
	list, err := fiscal.ListBatches(h.db, from, to)
	if err != nil {
		internalFail(c, "list batches", err)
		return
	}
	c.JSON(http.StatusOK, models.OK("", list))
}

// GetBatch is GET /admin/fiscal/batches/:id.
func (h *FiscalHandler) GetBatch(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, models.Fail("Batch not found", "fiscal_batch_not_found"))
		return
	}
	b, rows, err := fiscal.LoadBatch(h.db, id)
	if errors.Is(err, fiscal.ErrBatchNotFound) {
		c.JSON(http.StatusNotFound, models.Fail("Batch not found", "fiscal_batch_not_found"))
		return
	}
	if err != nil {
		internalFail(c, "get batch", err)
		return
	}
	c.JSON(http.StatusOK, models.OK("", gin.H{"batch": b, "invoices": rows}))
}

// ── queue ────────────────────────────────────────────────────────────────

type jobView struct {
	fiscal.Job
	// Document labels the job for a person: the invoice number, or
	// "Combined <date>" for a batch.
	Document string `json:"document"`
}

// ListJobs is GET /admin/fiscal/jobs?status=pending|dead|all&limit=.
func (h *FiscalHandler) ListJobs(c *gin.Context) {
	status := strings.TrimSpace(c.Query("status"))
	if status == "" {
		status = "all"
	}
	limit := 100
	if raw := strings.TrimSpace(c.Query("limit")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 1000 {
			c.JSON(http.StatusBadRequest, models.Fail("limit must be between 1 and 1000", "invalid_request"))
			return
		}
		limit = n
	}
	where := ""
	switch status {
	case "pending":
		where = "WHERE j.status IN ('pending','processing')"
	case "dead":
		where = "WHERE j.status = 'dead'"
	case "all":
	default:
		c.JSON(http.StatusBadRequest, models.Fail("status must be pending, dead or all", "invalid_request"))
		return
	}
	rows, err := h.db.Query(`
		SELECT j.id, j.invoice_id, j.batch_id, j.kind, j.status, j.attempt_count, j.max_attempts, j.next_run_at,
		       j.last_error, j.last_error_code, j.created_at,
		       COALESCE(i.invoice_number, 'Combined ' || to_char(b.business_date, 'YYYY-MM-DD'), '')
		FROM fiscal_outbound_jobs j
		LEFT JOIN invoices i ON i.id = j.invoice_id
		LEFT JOIN fiscal_batches b ON b.id = j.batch_id
		`+where+`
		ORDER BY j.created_at DESC LIMIT $1`, limit)
	if err != nil {
		internalFail(c, "list jobs", err)
		return
	}
	defer rows.Close()
	out := []jobView{}
	for rows.Next() {
		var v jobView
		if err := rows.Scan(&v.ID, &v.InvoiceID, &v.BatchID, &v.Kind, &v.Status, &v.AttemptCount, &v.MaxAttempts, &v.NextRunAt,
			&v.LastError, &v.LastErrorCode, &v.CreatedAt, &v.Document); err != nil {
			internalFail(c, "scan job", err)
			return
		}
		out = append(out, v)
	}
	c.JSON(http.StatusOK, models.OK("", out))
}

// RetryJob is POST /admin/fiscal/jobs/:id/retry: a dead job goes back to
// pending, due now, with its attempts reset.
func (h *FiscalHandler) RetryJob(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, models.Fail("Job not found", "fiscal_job_not_found"))
		return
	}
	res, err := h.db.Exec(`UPDATE fiscal_outbound_jobs
		SET status = 'pending', next_run_at = now(), attempt_count = 0, updated_at = now()
		WHERE id = $1 AND status = 'dead'`, id)
	if err != nil {
		internalFail(c, "retry job", err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		c.JSON(http.StatusNotFound, models.Fail("No dead job with that id", "fiscal_job_not_found"))
		return
	}
	c.JSON(http.StatusOK, models.OK("Job queued", gin.H{"id": id}))
}

// SubmitInvoice is POST /admin/fiscal/invoices/:id/submit: file one unfiled
// or failed sale now.
func (h *FiscalHandler) SubmitInvoice(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, models.Fail("Invoice not found", "invoice_not_found"))
		return
	}
	cfg, err := fiscal.LoadConfig(h.db)
	if err != nil {
		internalFail(c, "submit config", err)
		return
	}
	if !cfg.Enabled {
		c.JSON(http.StatusConflict, models.Fail("FBR filing is switched off", "fiscal_disabled"))
		return
	}
	res, err := h.db.Exec(`UPDATE invoices SET fiscal_status = 'pending'
		WHERE id = $1 AND status = 'completed' AND fiscal_status IN ('unfiled','failed')`, id)
	if err != nil {
		internalFail(c, "submit claim", err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		inv, err := invoice.LoadWithLines(h.db, id)
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, models.Fail("Invoice not found", "invoice_not_found"))
			return
		}
		if err != nil {
			internalFail(c, "submit load", err)
			return
		}
		_ = inv
		c.JSON(http.StatusConflict, models.Fail("This invoice is already filed, being filed, or voided", "fiscal_filing_in_flight"))
		return
	}
	inv, err := invoice.LoadWithLines(h.db, id)
	if err != nil {
		internalFail(c, "submit load", err)
		return
	}
	actor := invoiceActor(c)
	runInlineFiling(c, h.db, h.deps, &inv, fiscal.KindSale, actorOrNil(actor.ID))
	c.JSON(http.StatusOK, models.OK("", inv))
}
