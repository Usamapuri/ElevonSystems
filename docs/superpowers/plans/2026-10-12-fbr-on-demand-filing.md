# On-demand FBR Filing Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Nothing is filed with FBR automatically; a sale can be filed on its own from the till ("Charge + FBR"), the unfiled sales of a business date can be filed together as one combined FBR invoice, the app shows how much is unfiled, and Settings → FBR holds a sandbox token and a production token, each with a test that shows exactly what was sent and what came back.

**Architecture:** `internal/fiscal` (config, crypto, payload, client, ledger, audit, `Submit` — built in tasks F1–F3 of the Phase 7 plan) gains a batch document: a `fiscal_batches` row that owns a set of invoices, a builder that emits one DI item per source invoice line, and `SubmitBatch`, which shares the validate → post core with `Submit` and is ledger-first in the same way. An outbound queue and worker retry anything FBR could not be reached for. Invoice create files only when asked (`file_fbr`); everything else is admin endpoints under `/admin/fiscal/*`.

**Tech Stack:** Go 1.24 / Gin / lib-pq raw SQL; React 18 + TS strict + TanStack Router/Query; vitest (node, logic only). New frontend dependency: `qrcode` + `@types/qrcode` (QR is rendered client-side; print HTML is built in the browser).

**Spec:** `docs/superpowers/specs/2026-10-12-on-demand-fbr-and-deal-rates-design.md` §2, §4, §5, §6. Sandbox facts: `audit/FBR_SANDBOX_2026-09-21.md`. This plan **replaces tasks F4–F8** of `docs/superpowers/plans/2026-09-21-phase-7-fbr-digital-invoicing.md`; that plan's Global Constraints still apply except its "every completed sale is filed" goal.

## Global Constraints

- **Nothing files automatically.** A sale is filed only when the request says `file_fbr: true`, or an admin files it or batches it later.
- **Every figure sent to FBR is copied from stored `invoice_lines`.** No endpoint accepts a quantity or amount for a combined invoice. Never recompute tax in `fiscal`.
- **Ledger first (CLAUDE.md invariant 7):** `Submit` and `SubmitBatch` consult `fiscal_invoices` before any HTTP call. `submit_ledger_first_contract_test.go` pins both.
- **The sale is never rolled back because of FBR.** Filing happens after commit.
- **Sandbox in release refuses** (`sandbox_in_release`) unless `FISCAL_ALLOW_SANDBOX=true`; sandbox validate URL is the `_sb` one.
- **No HS-code literal in Go code.**
- **Tokens are private settings:** `fiscal_api_key_sandbox_enc`, `fiscal_api_key_production_enc`. Never returned by `GET /settings`, never writable through `PUT /admin/settings` (`private_setting`), never in an audit excerpt, a log line or a test-result body. API shows `set` + `masked` (`****` + last 4) only.
- **A production test is validate-only.** It must never call Post.
- **Pending-to-file is information only:** no code path reads it to warn, block a sale, or block day close.
- `fiscal_audit_events` stays append-only. `void_log` is not touched.
- Every new route sits inside `RequireRoles`; `APIResponse` envelope; stable snake_case codes; no `err.Error()` to a client. New codes: `fiscal_batch_ineligible`, `fiscal_batch_not_found`, `fiscal_batch_empty`, `fiscal_filing_in_flight`, `fiscal_job_not_found`, `fiscal_enabled`, `invalid_environment`.
- Migrations idempotent. Money in payloads is `float64` at 2 dp from `NUMERIC(12,2)`; quantity 3 dp.
- Frontend: TS strict, no `any`; tokens for colour; `.tabular` on numbers; print builders pure and DOM-free at module scope.
- Commits `<scope>(<subsystem>): E-07 — <imperative>`, trailer `Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>`. Gate: `cd backend && go vet ./... && go test ./...` (with `TEST_DATABASE_URL`); `cd frontend && npm run type-check && npm run test && npx vite build`.
- No Bhookly tenant/host/appId/release-repo/Railway-ID references. The sandbox token lives only in gitignored `backend/.env.sandbox.local`; never commit it, never paste it into a doc or fixture.

## Review Focus

1. **Two admins press "File" on overlapping selections at once.** Expected: one batch gets the shared invoices; the other request is refused whole with `fiscal_batch_ineligible` naming them; no invoice is in two batches. (Test in B3.)
2. **FBR accepts the batch, then the process dies before the invoices are stamped.** Expected: the retry finds the number in `fiscal_invoices` and stamps the invoices `covered` without posting again. (Test in B3.)
3. **A covered or in-flight invoice is voided.** Expected: in-flight → `fiscal_filing_in_flight` 409; covered → voids locally, batch figures unchanged, batch detail marks it voided. (Tests in B4, B5.)
4. **Fiscal is switched off with invoices `unfiled`, or on with invoices `off`.** Expected: `off` invoices never appear as pending or eligible; `unfiled` ones stay listed and can be filed once fiscal is back on. (Test in B5.)
5. **Run test with no token saved, or a sandbox test on a release server.** Expected: a Failed result with `fiscal_token_missing` / `sandbox_in_release` and no HTTP call — not a 500. (Test in B5.)

## File structure

```
backend/migrations/005_fiscal_on_demand.sql      statuses, fiscal_batches, batch_id columns, token slots
backend/internal/fiscal/
  config.go                 Environment, KeyAPIKeySandboxEnc/ProductionEnc, LoadAPIKey(q, env)
  payload.go                header() split out of build(); BuildBatchInvoice
  testdata/batch_two_invoices.json                golden
  ledger.go                 RecordSyncedBatch, FindSyncedBatch
  audit.go                  Event.BatchID
  submit.go                 file() shared core; KindBatch
  batch.go   batch_db_test.go                     Eligible, CreateBatch, SubmitBatch, releaseBatch, ListBatches, LoadBatch
  queue.go   queue_db_test.go                     Enqueue, ClaimNext, ProcessOnce, StartWorker, Counts
  reference.go reference_test.go                  Refresh
  probe.go   probe_test.go                        RunTest (the Settings test)
  pending.go                                      Pending summary
backend/internal/settings/settings.go            two private keys; checkFiscal reads the active slot
backend/internal/handlers/invoices.go            file_fbr hook; void rules
backend/internal/handlers/fiscal.go (+_db_test)  admin + pending endpoints
backend/internal/reports/queries.go              tax bands filed/unfiled/failed
backend/internal/api/routes.go, backend/main.go  routes, worker
frontend/src/types/index.ts, api/client.ts       fiscal types and calls
frontend/src/lib/fiscal.ts (+test)               status labels, pending formatting, selection totals
frontend/src/lib/print/fbrBlock.ts (+test), fbrQr.ts   FBR block + QR data URL
frontend/src/components/settings/FiscalForm.tsx, FiscalEnvCard.tsx, FiscalTestDialog.tsx
frontend/src/components/fiscal/FileWithFbr.tsx, BatchList.tsx
frontend/src/routes/_app/fbr.tsx                 the File with FBR screen (admin)
frontend/src/components/pos/CartRail.tsx, routes/_app/pos.tsx   second button, pending line
frontend/src/components/invoices/InvoiceDrawer.tsx              fiscal state, File with FBR action
frontend/src/components/reports/reportColumns.tsx, FiscalQueueTab.tsx
scripts/fbr-sandbox-checks.http                  the §5 checks
```

---

### Task B1 (E-07): Migration 005 and the two token slots

**Files:** Create `backend/migrations/005_fiscal_on_demand.sql`. Modify `internal/fiscal/config.go`, `config_test.go`, `config_db_test.go`, `submit.go` (one call site), `internal/settings/settings.go`, `settings_test.go`, `internal/handlers/settings_db_test.go`.

**Interfaces — Produces:**
```go
type Environment string
const ( EnvSandbox Environment = "sandbox"; EnvProduction Environment = "production" )
const ( KeyAPIKeySandboxEnc = "fiscal_api_key_sandbox_enc"; KeyAPIKeyProductionEnc = "fiscal_api_key_production_enc" )
func ParseEnvironment(s string) (Environment, bool)
func (c Config) Environment() Environment            // EnvSandbox when c.IsSandbox
func KeyFor(env Environment) string
func LoadAPIKey(q Querier, env Environment) (string, error)   // replaces the one-argument form
```
`KeyAPIKeyEnc` is deleted.

- [ ] **Step 1: Write the migration.**

```sql
-- 005_fiscal_on_demand — filing becomes the operator's action (spec
-- 2026-10-12 §2). Idempotent.

-- unfiled: fiscal on, not filed. covered: filed inside a combined invoice.
ALTER TABLE invoices DROP CONSTRAINT IF EXISTS invoices_fiscal_status_check;
ALTER TABLE invoices ADD CONSTRAINT invoices_fiscal_status_check
  CHECK (fiscal_status IN ('off','unfiled','pending','synced','covered','failed'));

-- One combined FBR invoice. Totals are a snapshot of what was sent; they are
-- never edited afterwards, including when a covered invoice is later voided.
CREATE TABLE IF NOT EXISTS fiscal_batches (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  business_date DATE NOT NULL,
  status VARCHAR(12) NOT NULL DEFAULT 'pending',   -- pending | synced | failed
  fbr_invoice_number VARCHAR(64),
  invoice_count INT NOT NULL,
  total_kg NUMERIC(14,3) NOT NULL,
  value_excl_tax NUMERIC(14,2) NOT NULL,
  tax_amount NUMERIC(14,2) NOT NULL,
  further_tax_amount NUMERIC(14,2) NOT NULL,
  total_amount NUMERIC(14,2) NOT NULL,
  last_error_code VARCHAR(32),
  last_error TEXT,
  created_by UUID REFERENCES users(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  settled_at TIMESTAMPTZ
);
ALTER TABLE fiscal_batches DROP CONSTRAINT IF EXISTS fiscal_batches_status_check;
ALTER TABLE fiscal_batches ADD CONSTRAINT fiscal_batches_status_check CHECK (status IN ('pending','synced','failed'));
CREATE INDEX IF NOT EXISTS idx_fiscal_batches_date ON fiscal_batches (business_date DESC, created_at DESC);

ALTER TABLE invoices ADD COLUMN IF NOT EXISTS fiscal_batch_id UUID REFERENCES fiscal_batches(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_invoices_fiscal_batch ON invoices (fiscal_batch_id) WHERE fiscal_batch_id IS NOT NULL;
-- What "pending to file" and the eligible list scan.
CREATE INDEX IF NOT EXISTS idx_invoices_fiscal_unfiled ON invoices (business_date) WHERE fiscal_status IN ('unfiled','failed');

-- The ledger, the queue and the audit trail each learn about batches. A row
-- belongs to exactly one of an invoice or a batch.
ALTER TABLE fiscal_invoices ADD COLUMN IF NOT EXISTS batch_id UUID REFERENCES fiscal_batches(id) ON DELETE CASCADE;
ALTER TABLE fiscal_invoices ALTER COLUMN invoice_id DROP NOT NULL;
ALTER TABLE fiscal_invoices DROP CONSTRAINT IF EXISTS fiscal_invoices_one_owner;
ALTER TABLE fiscal_invoices ADD CONSTRAINT fiscal_invoices_one_owner CHECK ((invoice_id IS NULL) <> (batch_id IS NULL));
ALTER TABLE fiscal_invoices DROP CONSTRAINT IF EXISTS fiscal_invoices_kind_check;
ALTER TABLE fiscal_invoices ADD CONSTRAINT fiscal_invoices_kind_check CHECK (kind IN ('sale','debit_note','batch'));
CREATE UNIQUE INDEX IF NOT EXISTS fiscal_invoices_batch_kind ON fiscal_invoices (batch_id, kind) WHERE batch_id IS NOT NULL;

ALTER TABLE fiscal_outbound_jobs ADD COLUMN IF NOT EXISTS batch_id UUID REFERENCES fiscal_batches(id) ON DELETE CASCADE;
ALTER TABLE fiscal_outbound_jobs ALTER COLUMN invoice_id DROP NOT NULL;
ALTER TABLE fiscal_outbound_jobs DROP CONSTRAINT IF EXISTS fiscal_outbound_jobs_one_owner;
ALTER TABLE fiscal_outbound_jobs ADD CONSTRAINT fiscal_outbound_jobs_one_owner CHECK ((invoice_id IS NULL) <> (batch_id IS NULL));
ALTER TABLE fiscal_outbound_jobs DROP CONSTRAINT IF EXISTS fiscal_outbound_jobs_kind_check;
ALTER TABLE fiscal_outbound_jobs ADD CONSTRAINT fiscal_outbound_jobs_kind_check CHECK (kind IN ('sale','debit_note','batch'));
CREATE UNIQUE INDEX IF NOT EXISTS fiscal_outbound_jobs_one_active_batch ON fiscal_outbound_jobs (batch_id, kind)
  WHERE batch_id IS NOT NULL AND status IN ('pending','processing');

ALTER TABLE fiscal_audit_events ADD COLUMN IF NOT EXISTS batch_id UUID REFERENCES fiscal_batches(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_fiscal_audit_batch ON fiscal_audit_events (batch_id, created_at DESC) WHERE batch_id IS NOT NULL;

-- Two token slots. A token saved under the old single key was a sandbox
-- token (no production token has ever been issued to a store).
INSERT INTO settings (key, value)
  SELECT 'fiscal_api_key_sandbox_enc', value FROM settings WHERE key = 'fiscal_api_key_enc'
  ON CONFLICT (key) DO NOTHING;
INSERT INTO settings (key, value) VALUES
  ('fiscal_api_key_sandbox_enc', '""'), ('fiscal_api_key_production_enc', '""')
  ON CONFLICT (key) DO NOTHING;
DELETE FROM settings WHERE key = 'fiscal_api_key_enc';
```

Note: `fiscal_audit_events` is append-only by trigger, but `ON DELETE SET NULL` on its FKs is an UPDATE the trigger would refuse; 003 already has the same shape for `invoice_id`, so follow whatever 003's trigger does for that case and add no new behaviour. If `ADD COLUMN … REFERENCES … ON DELETE SET NULL` is rejected by the idempotency contract test, report it.

- [ ] **Step 2: Failing tests.** `config_db_test.go`: saving an encrypted token under `KeyAPIKeySandboxEnc` is returned by `LoadAPIKey(db, EnvSandbox)` and `LoadAPIKey(db, EnvProduction)` is `""`; and the reverse. `config_test.go`: `ParseEnvironment("sandbox")`, `("production")` ok, `("live")`, `("")` not ok; `Config{IsSandbox:true}.Environment() == EnvSandbox`. `settings_test.go`: key count 26 (25 after the deal-rates plan; 25 if that plan has not landed — read the current number and add 1); both new keys `Known` and `Private`; `fiscal_api_key_enc` not `Known`; `checkFiscal` with `is_sandbox:false`, a sandbox token set and no production token → `fiscal_token_missing`. `settings_db_test.go`: `GET /settings` returns neither key; `PUT /admin/settings` with either → `private_setting`.
- [ ] **Step 3: Run** `go test ./internal/fiscal/ ./internal/settings/ ./internal/handlers/ -run 'Config|Environment|Keys|Fiscal|Private'` — expect FAIL (undefined).
- [ ] **Step 4: Implement.** `config.go`: the types and constants above; `KeyFor`; `LoadAPIKey(q, env)` is the existing body reading `KeyFor(env)`. `submit.go`: `LoadAPIKey(d.DB, cfg.Environment())`. `settings.go`: replace the `fiscal_api_key_enc` rule with the two keys (`notNull(text(0, 512))`), `privateKeys` holds both, and `checkFiscal` reads `all[fiscal.KeyFor(cfg.Environment())]`.
- [ ] **Step 5: Run** `go vet ./... && go test ./...` — expect PASS.
- [ ] **Step 6: Commit** `fiscal(migration): E-07 — 005: on-demand statuses, batches, sandbox and production token slots`.

---

### Task B2 (E-07): Combined-invoice payload

**Files:** Modify `internal/fiscal/payload.go`, `payload_test.go`. Create `internal/fiscal/testdata/batch_two_invoices.json`.

**Interfaces — Consumes:** `buildItems(inv, cfg, defaultHSCode, further)`, `SplitFurtherTax`, `taxableValues`, `buildBuyerFields(nil, cfg)`, `scenarioFor(false, cfg)`.
**Produces:**
```go
type BatchInput struct {
	BusinessDate  time.Time
	Invoices      []models.Invoice   // each with Lines, in invoice_number order
	DefaultHSCode string
}
var ErrBatchEmpty = errors.New("fiscal_batch_empty")
func BuildBatchInvoice(in BatchInput, cfg Config) (DIInvoice, error)
```

- [ ] **Step 1: Failing test** in `payload_test.go`:

```go
func TestBuildBatchInvoice_OneItemPerSourceLine(t *testing.T) {
	cfg := testConfig() // the helper the existing golden tests use
	hs := "2711.1910"
	day := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	a := models.Invoice{InvoiceNumber: "20261012-001", BusinessDate: day, TaxRate: 0.18, Lines: []models.InvoiceLine{
		{ProductName: "LPG bulk", HSCode: &hs, Quantity: 12.5, LineTotal: 3312.50, LineDiscount: 0, LineTax: 596.25},
		{ProductName: "LPG bulk", HSCode: &hs, Quantity: 3.333, LineTotal: 883.25, LineDiscount: 0, LineTax: 158.99},
	}}
	b := models.Invoice{InvoiceNumber: "20261012-002", BusinessDate: day, TaxRate: 0.18, Lines: []models.InvoiceLine{
		{ProductName: "LPG cylinder", HSCode: &hs, Quantity: 11.8, LineTotal: 3127.00, LineDiscount: 127.00, LineTax: 540.00},
	}}
	got, err := BuildBatchInvoice(BatchInput{BusinessDate: day, Invoices: []models.Invoice{a, b}, DefaultHSCode: ""}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got.InvoiceType != "Sale Invoice" || got.InvoiceDate != "2026-10-12" || got.BuyerRegistrationType != "Unregistered" || got.BuyerBusinessName != "Walk-in Customer" {
		t.Fatalf("header: %+v", got)
	}
	if len(got.Items) != 3 {
		t.Fatalf("one item per source line, got %d", len(got.Items))
	}
	// Per-item figures are the stored line figures, untouched: FBR recomputes
	// tax per item (0104), so nothing may be summed and re-rounded.
	if got.Items[1].ValueSalesExcludingST != 883.25 || got.Items[1].SalesTaxApplicable != 158.99 {
		t.Fatalf("line 2 copied verbatim: %+v", got.Items[1])
	}
	if got.Items[2].ValueSalesExcludingST != 3000.00 || got.Items[2].Discount != 127.00 {
		t.Fatalf("discounted line: %+v", got.Items[2])
	}
	if got.Items[0].ProductDescription != "20261012-001 LPG bulk 12.500 kg" {
		t.Fatalf("description carries the source invoice: %q", got.Items[0].ProductDescription)
	}
	assertGolden(t, "batch_two_invoices.json", got) // same helper as the sale goldens
}

func TestBuildBatchInvoice_Refusals(t *testing.T) {
	cfg := testConfig()
	if _, err := BuildBatchInvoice(BatchInput{}, cfg); !errors.Is(err, ErrBatchEmpty) {
		t.Fatalf("empty batch: %v", err)
	}
	inv := models.Invoice{InvoiceNumber: "x", Lines: []models.InvoiceLine{{ProductName: "LPG", Quantity: 1, LineTotal: 100, LineTax: 18}}}
	if _, err := BuildBatchInvoice(BatchInput{Invoices: []models.Invoice{inv}}, cfg); !errors.Is(err, ErrHSCodeMissing) {
		t.Fatalf("no HS code anywhere: %v", err)
	}
}
```

Use the real names of the existing config and golden helpers in `payload_test.go`; generate the golden with the suite's update flag and read it once by eye against the assertions above.

- [ ] **Step 2: Run** `go test ./internal/fiscal/ -run BuildBatch` — expect FAIL (undefined).
- [ ] **Step 3: Implement.** Split the `DIInvoice{…}` literal in `build` into `header(businessDate time.Time, buyer *Buyer, cfg Config, invoiceType, refNumber, reason string) DIInvoice` (everything but `Items`) and have `build` call it. Then:

```go
// BuildBatchInvoice is one combined Sale Invoice to the walk-in buyer for a
// set of sales. It emits one item per source invoice line and sums nothing:
// FBR recomputes tax on every item with no tolerance (sandbox 0104), and a
// sum of per-line rounded taxes can differ from the tax on a summed value.
func BuildBatchInvoice(in BatchInput, cfg Config) (DIInvoice, error) {
	out := header(in.BusinessDate, nil, cfg, invoiceTypeSale, "", "")
	out.Items = []DIItem{}
	for _, inv := range in.Invoices {
		further := SplitFurtherTax(round2(inv.FurtherTaxAmount), taxableValues(inv))
		items, err := buildItems(inv, cfg, in.DefaultHSCode, further)
		if err != nil {
			return DIInvoice{}, err
		}
		for i := range items {
			items[i].ProductDescription = inv.InvoiceNumber + " " + items[i].ProductDescription
		}
		out.Items = append(out.Items, items...)
	}
	if len(out.Items) == 0 {
		return DIInvoice{}, ErrBatchEmpty
	}
	return out, nil
}
```

(`invoiceTypeSale` is whatever constant or literal `BuildSaleInvoice` passes today.) Add `ErrBatchEmpty` to `ErrorCode`'s known list.

- [ ] **Step 4: Run** `go test ./internal/fiscal/` — expect PASS, existing sale/debit goldens byte-identical.
- [ ] **Step 5: Commit** `fiscal(payload): E-07 — combined invoice, one item per source line`.

---

### Task B3 (E-07): Batches — eligibility, claim, SubmitBatch

**Files:** Create `internal/fiscal/batch.go`, `batch_db_test.go`. Modify `submit.go`, `ledger.go`, `audit.go`, `submit_ledger_first_contract_test.go`.

**Interfaces — Produces:**
```go
const KindBatch Kind = "batch"

type Batch struct {
	ID uuid.UUID; BusinessDate time.Time; Status string; FBRInvoiceNumber *string
	InvoiceCount int; TotalKg, ValueExclTax, TaxAmount, FurtherTaxAmount, TotalAmount float64
	LastErrorCode, LastError *string; CreatedBy *uuid.UUID; CreatedAt time.Time; SettledAt *time.Time
}   // json tags snake_case

type EligibleRow struct {
	ID uuid.UUID; InvoiceNumber string; CreatedAt time.Time; CustomerName *string
	TotalKg, TotalAmount float64; TotalPayable int64; FiscalStatus string
	Eligible bool; Reason string   // "" | "registered_buyer" | "tax_rate_mismatch"
}

type IneligibleError struct{ IDs []uuid.UUID }       // Error() == "fiscal_batch_ineligible"
var ErrBatchNotFound = errors.New("fiscal_batch_not_found")

func Eligible(q Querier, businessDate time.Time, cfg Config) ([]EligibleRow, error)
func CreateBatch(db *sql.DB, businessDate time.Time, ids []uuid.UUID, cfg Config, actor *uuid.UUID) (Batch, error)
func SubmitBatch(ctx context.Context, d Deps, batchID uuid.UUID, actor *uuid.UUID) (string, error)
func FailBatch(db *sql.DB, batchID uuid.UUID, cause error) error    // batch failed, its invoices back to unfiled
func LoadBatch(q Querier, id uuid.UUID) (Batch, []EligibleRow, error) // rows = covered invoices, incl. later-voided
func ListBatches(q Querier, from, to time.Time) ([]Batch, error)
func RecordSyncedBatch(q Execer, batchID uuid.UUID, number string, raw json.RawMessage) error
func FindSyncedBatch(q Querier, batchID uuid.UUID) (string, bool, error)
```
`Event` gains `BatchID *uuid.UUID`, written to the new column.

**Eligibility rule (one definition, used by `Eligible` and re-checked under lock by `CreateBatch`):** `i.status = 'completed' AND i.fiscal_status IN ('unfiled','failed') AND i.business_date = $date`, the joined customer's `buyer_registration_type` is not `'Registered'` (a walk-in has no customer row and is eligible), and `abs(i.tax_rate*100 − pct(cfg.RateDesc)) < 1e-9`. Rows failing only the last two are returned by `Eligible` with `Eligible:false` and a `Reason`; rows failing the first three are not returned at all.

- [ ] **Step 1: Failing tests** in `batch_db_test.go`, using the fake `Client` and seeding helpers `submit_db_test.go` already has (seed invoices by direct `INSERT` with `fiscal_status='unfiled'`):
  - `TestCreateBatch_ClaimsAndTotals` — three eligible invoices → batch `pending`, `invoice_count 3`, totals equal the sums of the stored line columns, invoices `pending` with `fiscal_batch_id`.
  - `TestCreateBatch_RefusesWholeRequest` — for each of: a voided invoice, a `synced` one, a `Registered` buyer's, one from another business date, one at a different `tax_rate`, an unknown id → `*IneligibleError` listing exactly that id, **no** batch row, the other invoices still `unfiled`.
  - `TestCreateBatch_Empty` → `ErrBatchEmpty`.
  - `TestCreateBatch_ConcurrentOverlap` (Review Focus 1) — two goroutines call `CreateBatch` with `{A,B}` and `{B,C}` behind a start barrier; exactly one succeeds, the other is `*IneligibleError{B}`; `SELECT count(*) FROM invoices WHERE fiscal_batch_id IS NOT NULL` equals the winner's size.
  - `TestSubmitBatch_Synced` — accepting fake → returns the number; batch `synced` with `settled_at`; invoices `covered`, `fiscal_invoice_number` = the number; one `fiscal_invoices` row with `batch_id`; audit rows carry `batch_id`.
  - `TestSubmitBatch_RejectedLeavesItPending` + `TestFailBatch_ReleasesInvoices` — rejecting fake → `*RejectedError`; then `FailBatch` → batch `failed` with `last_error_code`, invoices `unfiled`, `fiscal_batch_id` NULL; a new `CreateBatch` over the same ids succeeds.
  - `TestSubmitBatch_LedgerRecovery` (Review Focus 2) — pre-insert the `fiscal_invoices` batch row, leave the batch `pending`; `SubmitBatch` with a fake that fails the test if `Validate` or `Post` is called → invoices `covered`, batch `synced`.
  - `TestSubmitBatch_Unreachable` → `ErrUnreachable`, batch and invoices still `pending`.
  - Extend `submit_ledger_first_contract_test.go` with the same textual assertion for `SubmitBatch`: `FindSyncedBatch(` appears before the first `.Validate(` / `file(` call in its body.
- [ ] **Step 2: Run** `go test ./internal/fiscal/ -run 'Batch|LedgerFirst'` — expect FAIL.
- [ ] **Step 3: Extract the shared core from `Submit`.** Steps 4b–5 of `Submit` (validate, post, number check, audit rows) move into:

```go
// auditRef says which document an audit row belongs to.
type auditRef struct{ InvoiceID, BatchID *uuid.UUID; Kind Kind }

// file validates then posts one document and returns FBR's response to the
// post. It writes the audit rows for both calls and nothing else: the ledger
// row and the invoice/batch update belong to the caller, which is what lets
// Submit and SubmitBatch each stay ledger-first.
func file(ctx context.Context, d Deps, cl Client, payload DIInvoice, ref auditRef, actor *uuid.UUID) (Response, string, error)
```

It returns the response, the trimmed invoice number, and the same errors as today. `Submit` calls it and keeps its own `RecordSynced` → `apply` sequence and the two `codeLedgerWriteFailed` / `codeApplyFailed` audit rows. Run the existing `submit_db_test.go` — it must pass unchanged before going on.

- [ ] **Step 4: `CreateBatch`.**

```go
func CreateBatch(db *sql.DB, businessDate time.Time, ids []uuid.UUID, cfg Config, actor *uuid.UUID) (Batch, error) {
	if len(ids) == 0 {
		return Batch{}, ErrBatchEmpty
	}
	tx, err := db.Begin()
	if err != nil {
		return Batch{}, err
	}
	defer tx.Rollback()

	// Lock first, judge second: the status read below is only trustworthy
	// once no other batch can be claiming the same rows.
	rows, err := tx.Query(`
		SELECT i.id, i.status, i.fiscal_status, i.business_date, i.tax_rate::float8,
		       COALESCE(c.buyer_registration_type, '')
		FROM invoices i LEFT JOIN customers c ON c.id = i.customer_id
		WHERE i.id = ANY($1) ORDER BY i.id FOR UPDATE OF i`, pq.Array(ids))
	…scan into a map; an id is ineligible when missing from the map or failing the rule…
	if len(bad) > 0 {
		return Batch{}, &IneligibleError{IDs: bad}
	}

	var b Batch
	err = tx.QueryRow(`
		INSERT INTO fiscal_batches (business_date, status, invoice_count, total_kg,
			value_excl_tax, tax_amount, further_tax_amount, total_amount, created_by)
		SELECT $1::date, 'pending', count(DISTINCT i.id), COALESCE(sum(l.quantity), 0),
			COALESCE(sum(l.line_total - l.line_discount), 0), COALESCE(sum(l.line_tax), 0),
			(SELECT COALESCE(sum(further_tax_amount), 0) FROM invoices WHERE id = ANY($2)),
			COALESCE(sum(l.line_total - l.line_discount + l.line_tax), 0)
			  + (SELECT COALESCE(sum(further_tax_amount), 0) FROM invoices WHERE id = ANY($2)),
			$3
		FROM invoices i JOIN invoice_lines l ON l.invoice_id = i.id WHERE i.id = ANY($2)
		RETURNING `+batchColumns, businessDate.Format("2006-01-02"), pq.Array(ids), actor).Scan(…)
	…
	if _, err := tx.Exec(`UPDATE invoices SET fiscal_status = 'pending', fiscal_batch_id = $1 WHERE id = ANY($2)`, b.ID, pq.Array(ids)); err != nil {
		return Batch{}, err
	}
	return b, tx.Commit()
}
```

`ORDER BY i.id FOR UPDATE` gives every request the same lock order, so two overlapping requests queue instead of deadlocking; the loser re-reads `fiscal_status = 'pending'` and is refused. Deduplicate `ids` before the query.

- [ ] **Step 5: `SubmitBatch`.** Same shape as `Submit`: (1) `FindSyncedBatch` → if filed, `applyBatch` and a `ledger_recovery` audit row; (2) `LoadConfig`, `Enabled`, `CheckRuntime`, `LoadAPIKey(db, cfg.Environment())`; (3) load the batch (`ErrBatchNotFound`), its invoices with lines ordered by `invoice_number`, the default HS code; (4) `BuildBatchInvoice`; (5) `file(…, auditRef{BatchID: &batchID, Kind: KindBatch}, …)`; (6) `RecordSyncedBatch`, then `applyBatch`:

```go
// applyBatch stamps the batch and every invoice it owns in one transaction.
func applyBatch(db *sql.DB, batchID uuid.UUID, number string) error {
	tx, err := db.Begin()
	…
	UPDATE fiscal_batches SET status = 'synced', fbr_invoice_number = $2, settled_at = now(), last_error_code = NULL, last_error = NULL WHERE id = $1
	UPDATE invoices SET fiscal_status = 'covered', fiscal_invoice_number = $2 WHERE fiscal_batch_id = $1
	…commit
}
```

`FailBatch` is the mirror: batch `failed` with `failureDetail(cause)`, `settled_at = now()`, and `UPDATE invoices SET fiscal_status = 'unfiled', fiscal_batch_id = NULL WHERE fiscal_batch_id = $1 AND fiscal_status = 'pending'`, in one transaction. `SubmitBatch` does **not** call it; the caller decides (inline handler and worker both do, on a rejection or a refusal, never on `ErrUnreachable`).

- [ ] **Step 6: `Eligible`, `LoadBatch`, `ListBatches`, ledger functions, `Event.BatchID`** as specified. `LoadBatch`'s rows come from `invoices WHERE fiscal_batch_id = $1` and include voided ones with their `status`.
- [ ] **Step 7: Run** `go vet ./... && go test ./...` — expect PASS.
- [ ] **Step 8: Commit** `fiscal(batch): E-07 — combined invoices: eligibility, atomic claim, ledger-first submit`.

---

### Task B4 (E-07): Queue, worker, `file_fbr` on create, void rules

**Files:** Create `internal/fiscal/queue.go`, `queue_db_test.go`. Modify `internal/models/invoice.go`, `internal/invoice/load.go`, `internal/handlers/invoices.go`, `invoices_db_test.go`, `backend/main.go`.

**Interfaces — Produces:**
```go
type JobRef struct{ InvoiceID, BatchID *uuid.UUID }     // exactly one set
type Job struct { ID uuid.UUID; Ref JobRef; Kind Kind; Status string; AttemptCount, MaxAttempts int; NextRunAt time.Time; LastError, LastErrorCode *string; CreatedAt time.Time }
func Enqueue(q Execer, ref JobRef, kind Kind, runAt time.Time) error   // no-op while an active job exists for the ref+kind
func ClaimNext(db *sql.DB, now time.Time) (*Job, error)                 // FOR UPDATE SKIP LOCKED
func Backoff(attempt int) time.Duration                                 // 30s, 1m, 2m, 5m, 10m, then 15m; 1-based
func ProcessOnce(ctx context.Context, d Deps) (bool, error)
func StartWorker(ctx context.Context, d Deps, every time.Duration)
func Counts(q Querier) (pending, dead int, lastSuccess *time.Time, err error)

// handlers
func (h *InvoicesHandler) WithFiscal(d fiscal.Deps) *InvoicesHandler
// models
CreateInvoiceRequest.FileFBR bool `json:"file_fbr"`
Invoice.FiscalBatchID *uuid.UUID `json:"fiscal_batch_id"`; Invoice.FiscalErrorCode string `json:"fiscal_error_code,omitempty"` (response-only, not a column)
```

`ProcessOnce`: claim → `Submit` or `SubmitBatch` by ref → success: job `succeeded`; `ErrUnreachable`: reschedule with `Backoff`, or dead with code `fiscal_max_attempts` when `attempt_count+1 >= max_attempts`; anything else (rejected, disabled, refused, token missing, buyer unregistered): job `dead`, then `MarkFailed` for an invoice or `FailBatch` for a batch.

- [ ] **Step 1: Failing queue tests** (`queue_db_test.go`): enqueue is idempotent while a job is active, for an invoice ref and for a batch ref; claim skips a future `next_run_at`; two concurrent claims get different jobs; the backoff table; `ProcessOnce` with an unreachable fake reschedules and increments; with a rejecting fake marks the job dead and the invoice `failed`; with a rejecting fake on a **batch** job marks it dead and the batch's invoices are `unfiled` again; at `max_attempts` an unreachable job dies as `fiscal_max_attempts` and the batch is released.
- [ ] **Step 2: Failing handler tests** (`invoices_db_test.go`; build the router with `NewInvoicesHandler(db).WithFiscal(fiscal.Deps{DB: db, NewClient: fake})`, enable fiscal by `setSetting(fiscal_config …enabled:true, is_sandbox:true)` plus an encrypted sandbox token):
  - fiscal disabled, plain sale → `fiscal_status "off"`; with `file_fbr:true` → still `off`, fake never called.
  - fiscal enabled, plain sale → `unfiled`, fake never called, no job.
  - enabled + `file_fbr`, accepting fake → 201, `synced`, `fiscal_invoice_number` set.
  - enabled + `file_fbr`, rejecting fake → 201 (sale stands), `failed`, `fiscal_error_code "fiscal_rejected"`.
  - enabled + `file_fbr`, unreachable fake → 201, `pending`, exactly one job for the invoice.
  - idempotent replay of a `file_fbr` sale does not file twice (fake call count unchanged).
  - void of a `pending` invoice → 409 `fiscal_filing_in_flight` (Review Focus 3), nothing voided.
  - void of a `covered` invoice → 200, `status voided`, `fiscal_void_status "unfiled"`, the batch row's totals unchanged, fake not called.
  - void of a `synced` sale to a `Registered` buyer → `fiscal_void_status` `synced` with a debit-note number (accepting fake); to a walk-in → `unfiled`, fake not called.
  - void of an `unfiled` invoice → voided; it no longer counts in `SELECT count(*) … fiscal_status IN ('unfiled','failed') AND status='completed'`.
- [ ] **Step 3: Run** — expect FAIL.
- [ ] **Step 4: Implement `queue.go`.** `Enqueue` is `INSERT … SELECT … WHERE NOT EXISTS (SELECT 1 FROM fiscal_outbound_jobs WHERE <ref match> AND kind = $k AND status IN ('pending','processing'))` (a partial unique index cannot be an `ON CONFLICT` target without its predicate). `ClaimNext` is `UPDATE … SET status='processing', started_at=$now WHERE id = (SELECT id FROM fiscal_outbound_jobs WHERE status='pending' AND next_run_at <= $now ORDER BY next_run_at, created_at LIMIT 1 FOR UPDATE SKIP LOCKED) RETURNING …`. `StartWorker` ticks every `every`, drains with `ProcessOnce` until it returns false, logs errors, recovers panics, stops on `ctx.Done()`.
- [ ] **Step 5: Create hook.** `InvoicesHandler` gains `fiscal fiscal.Deps` and `WithFiscal`. Read `fiscal.LoadConfig(h.db)` beside `settings.Load` (before the transaction). Replace the literal `'off'` in the invoice `INSERT` with a parameter:

```go
fiscalStatus := "off"
switch {
case fiscalCfg.Enabled && p.fileFBR:
	fiscalStatus = "pending"
case fiscalCfg.Enabled:
	fiscalStatus = "unfiled"
}
```

After the commit, before the response:

```go
// Filing is the cashier's choice (spec 2026-10-12 §2.2) and happens after
// the sale is safely committed: whatever FBR says, the sale stands.
if fiscalStatus == "pending" {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 8*time.Second)
	number, ferr := fiscal.Submit(ctx, h.fiscal, inv.ID, fiscal.KindSale, actorOrNil(actor.ID))
	cancel()
	switch {
	case ferr == nil:
		inv.FiscalStatus, inv.FiscalInvoiceNumber = "synced", &number
	case errors.Is(ferr, fiscal.ErrUnreachable) || errors.Is(ferr, context.DeadlineExceeded):
		if err := fiscal.Enqueue(h.db, fiscal.JobRef{InvoiceID: &inv.ID}, fiscal.KindSale, time.Now()); err != nil {
			log.Printf("invoice create: enqueue fiscal: %v", err)
		}
	default:
		if err := fiscal.MarkFailed(h.db, inv.ID, fiscal.KindSale, ferr); err != nil {
			log.Printf("invoice create: mark fiscal failed: %v", err)
		}
		inv.FiscalStatus, inv.FiscalErrorCode = "failed", fiscal.ErrorCode(ferr)
	}
}
```

`h.fiscal.DB` nil (a handler built without `WithFiscal`) is treated as fiscal disabled. Add `fiscal_batch_id` to `invoice.Columns` / `ScanInvoice`.

- [ ] **Step 6: Void rules.** Inside the void transaction, after the invoice row is locked: `fiscal_status = 'pending'` → 409 `fiscal_filing_in_flight` ("This invoice is being filed with FBR — try again in a moment"). `synced` and the customer's `buyer_registration_type = 'Registered'` → `fiscal_void_status = 'pending'` in the same transaction, then after commit `Submit(KindDebitNote)` with the same 8 s / enqueue / `MarkFailed` handling. Every other status → `fiscal_void_status` stays `unfiled`.
- [ ] **Step 7: `main.go`.** After `LoadDayBoundaryHour`: `fiscalDeps := fiscal.Deps{DB: db}`; `fiscal.StartWorker(ctx, fiscal.Deps{DB: db, Timeout: 60 * time.Second}, 5*time.Second)`; pass `fiscalDeps` to the router so `routes.go` builds `NewInvoicesHandler(db).WithFiscal(fiscalDeps)`. Log a warning when `GIN_MODE=release`, fiscal is enabled and `FISCAL_SECRETS_KEY` is unset. `boot_order_contract_test.go` must still pass (worker starts after `Migrate` and `EnsureInitialAdmin`).
- [ ] **Step 8: Run** `go vet ./... && go test ./...` — expect PASS.
- [ ] **Step 9: Commit** `fiscal(queue): E-07 — outbound worker, file-on-request at the till, void rules`.

---

### Task B5 (E-07): Admin endpoints, the Settings test, pending summary

**Files:** Create `internal/fiscal/probe.go`, `probe_test.go`, `reference.go`, `reference_test.go`, `pending.go`, `internal/handlers/fiscal.go`, `fiscal_db_test.go`. Modify `internal/api/routes.go`, `internal/reports/queries.go`, `queries_db_test.go`.

**Interfaces — Produces:**
```go
// probe.go
type TestStep struct { Phase, URL string; HTTPStatus int; StatusCode, ErrorCode, Error string; Line int; RawResponse json.RawMessage }  // snake_case json
type TestResult struct { OK bool; Environment Environment; Steps []TestStep; RequestBody json.RawMessage; InvoiceNumber string; ErrorCode string }
type TestSample struct { TaxRate float64; DefaultHSCode string }
func RunTest(ctx context.Context, d Deps, env Environment, sample TestSample, actor *uuid.UUID) TestResult
// pending.go
type PendingSummary struct { Count int; Kg float64; TotalPayable int64 }
func Pending(q Querier, businessDate time.Time) (today, all PendingSummary, err error)
// reference.go
type Reference struct { Provinces []string; UoMs []UoM; HSUoM map[string][]string; Rates []Rate; RefreshedAt time.Time }
func Refresh(ctx context.Context, d Deps, cfg Config, hsCodes []string, today time.Time) (Reference, error)
```

**`RunTest`:** copy the stored config with `IsSandbox = (env == EnvSandbox)`; refusals return `OK:false` with `ErrorCode` and **no steps**: `sandbox_in_release` (from `CheckRuntime`), `fiscal_token_missing`, `fiscal_secrets_key_missing`, `fiscal_hs_code_missing`. Build a one-line walk-in sale: quantity `1.000`, `LineTotal 100.00`, `LineTax round2(100 × sample.TaxRate)`, description "Connection test". Sandbox: Validate, then Post if valid → `InvoiceNumber`. Production: **Validate only**. `RequestBody` is the marshalled payload. One `fiscal_audit_events` row per call, kind `test`. Nothing is written to `fiscal_invoices`, `invoices` or the queue. It does not require `cfg.Enabled` — the test is how an admin checks the setup before switching filing on.

**`Refresh`:** `hsCodes` = `default_hs_code` plus every distinct non-null `products.hs_code` (so `2711.1400` is covered once a product uses it), each through `HS_UOM`; plus provinces, uom, `SaleTypeToRate` for `cfg.TransTypeID` and `today` as `02-Jan-2006`. Saved as settings `fiscal_reference`.

**Routes (`routes.go`):**

| Route | Group | Body → Response |
|---|---|---|
| `GET /fiscal/pending` | staff | `{enabled, today:{count,kg,total_payable}, all:{…}}`; zeros and `enabled:false` when fiscal is off |
| `GET /admin/fiscal/status` | admin | `PublicConfig` + `tokens:{sandbox:{set,masked}, production:{set,masked}}` + `queue:{pending,dead,last_success_at}` + `secrets_key_set` |
| `PUT /admin/fiscal/token` | admin | `{environment, api_key}` (trim, 8–512) → encrypted into that slot; `invalid_environment`; `fiscal_secrets_key_missing` in release without the env var |
| `DELETE /admin/fiscal/token` | admin | `{environment}`; 409 `fiscal_enabled` when fiscal is enabled and that is the active environment |
| `POST /admin/fiscal/test` | admin | `{environment}` → `TestResult` (always 200; failure is in the body) |
| `POST /admin/fiscal/reference/refresh` | admin | → `Reference` |
| `GET /admin/fiscal/eligible?business_date=` | admin | `[]EligibleRow` |
| `POST /admin/fiscal/batches` | admin | `{business_date, invoice_ids[]}` → `CreateBatch`, then `SubmitBatch` inline (30 s): synced → 201 batch; rejected/refused → `FailBatch`, 201 with the failed batch (`last_error_code` set); unreachable → `Enqueue`, 202 pending batch. 409 `fiscal_batch_ineligible` with `data:{invoice_ids:[…]}`; 400 `fiscal_batch_empty`; 409 `fiscal_disabled` |
| `GET /admin/fiscal/batches?from=&to=` | admin | `[]Batch` |
| `GET /admin/fiscal/batches/:id` | admin | `{batch, invoices}`; 404 `fiscal_batch_not_found` |
| `GET /admin/fiscal/jobs?status=pending\|dead\|all&limit=` | admin | jobs joined to the invoice number or batch date |
| `POST /admin/fiscal/jobs/:id/retry` | admin | dead → pending now, `attempt_count 0`; 404 `fiscal_job_not_found` |
| `POST /admin/fiscal/invoices/:id/submit` | admin | an `unfiled`/`failed` sale: set `pending`, `Submit` inline with the create hook's handling; 409 `fiscal_filing_in_flight` if already pending/synced/covered |

- [ ] **Step 1: Failing tests.**
  - `probe_test.go` (httptest server recording paths): sandbox → hits the validate then the post path, `OK`, `InvoiceNumber` set, `RequestBody` contains `"Sale Invoice"` and does **not** contain the token; production → hits **only** the validate path, `OK`, empty `InvoiceNumber`; rejected validate → `OK:false`, step carries `ErrorCode "0104"` and the line index, and Post is not called; no token → `fiscal_token_missing`, zero requests (Review Focus 5); `GIN_MODE=release` without `FISCAL_ALLOW_SANDBOX` → `sandbox_in_release`, zero requests; the test writes no `fiscal_invoices` row.
  - `reference_test.go`: httptest replaying the four real bodies from the audit; two HS codes in → both keys in `HSUoM`.
  - `fiscal_db_test.go`: status masks both tokens independently; token round-trip per environment (`****` + last 4) and `invalid_environment`; delete of the active slot while enabled → `fiscal_enabled`; pending summary counts `unfiled` + `failed`, excludes voided, `off`, `covered`, `synced` (Review Focus 4: seed one `off` invoice and assert it is absent from both `pending` and `eligible`; disable fiscal and assert `unfiled` rows are still in `eligible`); batch create → 201 synced with an accepting fake, 201 failed + invoices released with a rejecting fake, 202 + one batch job with an unreachable fake, 409 with the ineligible ids; batch detail lists a covered invoice that was voided afterwards with `status "voided"` (Review Focus 3); single submit; jobs list and retry; a staff token gets 403 on every `/admin/fiscal/*` route and 200 on `/fiscal/pending`.
  - `queries_db_test.go`: the tax-band row gains `filed_count`, `filed_amount`, `unfiled_count`, `unfiled_amount`, `failed_count` (filed = `synced` + `covered`).
- [ ] **Step 2: Run** — expect FAIL.
- [ ] **Step 3: Implement** `probe.go`, `reference.go`, `pending.go`, the handler and routes, and the report columns. `Pending`:

```sql
SELECT count(*) FILTER (WHERE i.business_date = $1::date),
       COALESCE(sum(k.kg) FILTER (WHERE i.business_date = $1::date), 0)::float8,
       COALESCE(sum(i.total_payable) FILTER (WHERE i.business_date = $1::date), 0)::bigint,
       count(*), COALESCE(sum(k.kg), 0)::float8, COALESCE(sum(i.total_payable), 0)::bigint
FROM invoices i
JOIN LATERAL (SELECT COALESCE(sum(quantity), 0) AS kg FROM invoice_lines WHERE invoice_id = i.id) k ON true
WHERE i.status = 'completed' AND i.fiscal_status IN ('unfiled','failed')
```

Nothing in `dayops`, `handlers/invoices.go` or the day-close handlers may import or call `Pending` — it is display only.

- [ ] **Step 4: Run** `go vet ./... && go test ./...` — expect PASS, `routes_role_gates_contract_test.go` included.
- [ ] **Step 5: Commit** `fiscal(api): E-07 — admin endpoints, per-environment test, pending summary, fiscal report columns`.

---

### Task B6 (E-07): Settings → FBR tab with the two-environment test

**Files:** Create `frontend/src/lib/fiscal.ts`, `fiscal.test.ts`, `lib/print/fbrQr.ts`, `components/settings/FiscalForm.tsx`, `FiscalEnvCard.tsx`, `FiscalTestDialog.tsx`. Modify `frontend/package.json` (`qrcode`, `@types/qrcode`), `types/index.ts`, `api/client.ts`, `routes/_app/settings.tsx`.

**Interfaces — Produces:**
```ts
export type FiscalEnvironment = 'sandbox' | 'production'
export interface FiscalTokenState { set: boolean; masked: string }
export interface FiscalStatus extends FiscalConfig { tokens: Record<FiscalEnvironment, FiscalTokenState>; queue: { pending: number; dead: number; last_success_at: string | null }; secrets_key_set: boolean }
export interface FiscalTestStep { phase: 'validate' | 'post'; url: string; http_status: number; status_code: string; error_code: string; error: string; line: number; raw_response: unknown }
export interface FiscalTestResult { ok: boolean; environment: FiscalEnvironment; steps: FiscalTestStep[]; request_body: unknown; invoice_number: string; error_code: string }
// client.ts
getFiscalStatus(); saveFiscalToken(environment, api_key); deleteFiscalToken(environment); runFiscalTest(environment); refreshFiscalReference()
// lib/fiscal.ts
export function testHeadline(r: FiscalTestResult): { passed: boolean; title: string; detail: string }
export function prettyJson(v: unknown): string
// lib/print/fbrQr.ts
export function fbrQrDataUrl(invoiceNumber: string): Promise<string>   // qrcode.toDataURL, error-correction M, margin 1
```

- [ ] **Step 1: Failing tests** (`fiscal.test.ts`): `testHeadline` — sandbox pass → `{passed:true, title:'Passed', detail:'FBR accepted the test invoice.'}`; production pass → detail `'FBR validated the test invoice. Nothing was filed.'`; refusal with no steps → the message for `fiscal_token_missing` ("Save a token for this environment first."), `sandbox_in_release` ("This server does not allow sandbox filing."), `fiscal_hs_code_missing` ("Set a default HS code in Tax settings first."); a rejected step → `'FBR rejected it: 0104 — <error> (line 1)'`. `prettyJson` is stable 2-space JSON.
- [ ] **Step 2: Run** `npm run test -- fiscal` — expect FAIL.
- [ ] **Step 3: Implement** `lib/fiscal.ts`, the types, the client methods, `fbrQr.ts`; `npm install qrcode @types/qrcode`.
- [ ] **Step 4: `FiscalForm.tsx`** (a new "FBR" tab in `settings.tsx`, admin only): the seller profile and config fields (enabled, environment switch bound to `is_sandbox`, NTN, business name, province select from the reference list, address, rate description, sale type, default UoM) saved through `PUT /admin/settings` as `fiscal_config`; a "Refresh FBR reference lists" button with the refreshed-at time; server refusals (`tax_rate_mismatch`, `fiscal_token_missing`, `fiscal_reference_stale`, `fiscal_uom_not_allowed`) shown beside the field they concern; a warning strip when `secrets_key_set` is false.
- [ ] **Step 5: `FiscalEnvCard.tsx`**, rendered twice (Sandbox, Production). Props `{ environment; token: FiscalTokenState; active: boolean }`. Shows: an "In use" badge when `active`; the masked token or "No token saved"; a password-type input + **Save token**; **Remove** (confirm dialog); **Run test**. The production card's test button carries the helper text "Validates only — nothing is filed."
- [ ] **Step 6: `FiscalTestDialog.tsx`**. Props `{ result: FiscalTestResult | null; open; onOpenChange }`. Top to bottom: Passed/Failed badge + environment; `testHeadline(...).detail`; **Outbound body** — the URL(s) from `steps`, `prettyJson(request_body)` in a scrollable `<pre>`, **Download JSON** (the existing `lib/download.ts` helper, filename `fbr-test-<environment>.json`); **FBR invoice number** + QR image from `fbrQrDataUrl(invoice_number)` (only when `invoice_number` is non-empty); **Gateway response** — `prettyJson(step.raw_response)` per step, labelled Validate / Post. No token anywhere in the dialog.
- [ ] **Step 7: Run** `npm run type-check && npm run test && npx vite build` — expect PASS.
- [ ] **Step 8: Commit** `frontend(settings): E-07 — FBR tab: sandbox and production tokens, test with request and response`.

---

### Task B7 (E-07): Till button, pending line, File with FBR screen

**Files:** Create `components/fiscal/FileWithFbr.tsx`, `BatchList.tsx`, `routes/_app/fbr.tsx`. Modify `lib/fiscal.ts` (+test), `types/index.ts`, `api/client.ts`, `components/pos/CartRail.tsx`, `routes/_app/pos.tsx`, `components/shell/Sidebar.tsx`, `components/invoices/InvoiceDrawer.tsx`, `InvoiceTable.tsx`, `routes/_app/dashboard.tsx` (or its card component).

**Interfaces — Produces:**
```ts
export interface FiscalPending { enabled: boolean; today: PendingSummary; all: PendingSummary }
export interface PendingSummary { count: number; kg: number; total_payable: number }
export interface EligibleInvoice { id: string; invoice_number: string; created_at: string; customer_name: string | null; total_kg: number; total_amount: number; total_payable: number; fiscal_status: string; eligible: boolean; reason: '' | 'registered_buyer' | 'tax_rate_mismatch' }
export interface FiscalBatch { id; business_date; status: 'pending'|'synced'|'failed'; fbr_invoice_number: string | null; invoice_count; total_kg; value_excl_tax; tax_amount; further_tax_amount; total_amount; last_error_code: string | null; created_at; settled_at: string | null }
// lib/fiscal.ts
export function fiscalStatusLabel(status: string): string          // off→'Not filed', unfiled→'Unfiled', pending→'Filing…', synced→'Filed', covered→'Filed (combined)', failed→'Rejected'
export function pendingLine(p: FiscalPending): string | null       // null when !enabled or today.count === 0 && all.count === 0
export function selectionTotals(rows: EligibleInvoice[], selected: Set<string>): PendingSummary
export function ineligibleReason(reason: EligibleInvoice['reason']): string
// CreateInvoiceRequest.file_fbr?: boolean; Invoice.fiscal_batch_id: string | null; Invoice.fiscal_error_code?: string
```

- [ ] **Step 1: Failing tests** (`fiscal.test.ts`): every `fiscalStatusLabel`; `pendingLine` → `'Unfiled today: 14 invoices · 3,240.500 kg · Rs 812,340'`, with `' · 31 in all'` appended when `all.count > today.count`, `null` when disabled; `selectionTotals` sums only selected **and eligible** rows; `ineligibleReason` texts ("Registered buyer — file this one on its own", "Tax rate differs from the FBR rate").
- [ ] **Step 2: Run** — expect FAIL.
- [ ] **Step 3: Implement** the helpers, types and client methods (`getFiscalPending`, `getFiscalEligible(date)`, `createFiscalBatch(date, ids)`, `listFiscalBatches(from, to)`, `getFiscalBatch(id)`, `submitInvoiceToFbr(id)`).
- [ ] **Step 4: Till.** `CartRail` gains optional props `onChargeFbr?: () => void` and `pendingText?: string | null`. When `onChargeFbr` is set (fiscal enabled) the payable plate shows two buttons side by side: **Charge** (as today) and **Charge + FBR** (outline variant on the rail, same height, same disabled rule). `pendingText` renders as one muted `.tabular` line under the totals — no icon, no colour, no link to day close. In `pos.tsx`, `onChargeFbr` runs the same tender flow with `file_fbr: true` in the request (the stable `client_op_id` and PIN retry are unchanged); after the response, toast by `fiscal_status`: `synced` → "Filed with FBR · <number>", `pending` → "Sale recorded. FBR is not answering — it will file automatically.", `failed` → "Sale recorded. FBR rejected it (<fiscal_error_code>)." The pending query (`/fiscal/pending`) refetches after every sale.
- [ ] **Step 5: `/fbr` screen** (admin; sidebar entry "File with FBR" shown only when fiscal is enabled). Date picker defaulting to the current business date; table of `EligibleInvoice` with a checkbox per row — eligible rows ticked by default, ineligible rows greyed with `ineligibleReason`; a footer with `selectionTotals` ("12 of 14 selected · 2,980.000 kg · Rs 745,100") and **File selected with FBR**; a confirm dialog restating count, kg and rupees. Result handling: 201 synced → success panel with the FBR number; 201 failed → error panel with `last_error_code` and "The invoices are back in the list."; 202 → "FBR is not answering — it will file automatically."; 409 `fiscal_batch_ineligible` → refetch and toast "Some invoices changed — the list has been refreshed." Below: `BatchList` for the last 30 days (date, status, FBR number, count, kg, total), each row expanding to its invoices with a "Voided later" tag on voided ones.
- [ ] **Step 6: Invoices browser.** `InvoiceTable` gets a fiscal column (`fiscalStatusLabel`); `InvoiceDrawer` shows the status, the FBR number, "Part of combined invoice <number>" for `covered`, and for admins on `unfiled`/`failed` a **File with FBR** button calling `submitInvoiceToFbr`.
- [ ] **Step 7: Dashboard card** "Unfiled with FBR" (count, kg, rupees for today and in all), shown only when enabled, linking to `/fbr`.
- [ ] **Step 8: Run** `npm run type-check && npm run test && npx vite build` — expect PASS. Regenerate `routeTree.gen.ts` through the Vite plugin; never hand-edit it.
- [ ] **Step 9: Commit** `frontend(fiscal): E-07 — Charge + FBR, pending line, File with FBR screen`.

---

### Task B8 (E-07): FBR block on receipts, fiscal report columns

**Files:** Create `lib/print/fbrBlock.ts`, `fbrBlock.test.ts`, `components/reports/FiscalQueueTab.tsx`. Modify `lib/print/receipt.ts`, `invoiceA4.ts`, `printInvoice.ts`, `receipt.test.ts`, `components/reports/reportColumns.tsx`, `ReportsTabs.tsx`.

**Interfaces — Produces:** `fbrBlockHtml(inv: Pick<Invoice,'fiscal_status'|'fiscal_invoice_number'>, qrDataUrl: string | null): string` — `''` unless `fiscal_status === 'synced'` and a number is present.

- [ ] **Step 1: Failing tests.** `fbrBlock.test.ts`: `synced` + number + QR → HTML containing "FBR Invoice No.", the number and an `<img src="data:image/png` tag; `synced` without a QR → number only; `covered`, `unfiled`, `pending`, `failed`, `off` → `''` (a covered sale's FBR number belongs to the combined invoice, not to this receipt). `receipt.test.ts`: a synced invoice's receipt contains the block; a covered one does not contain "FBR".
- [ ] **Step 2: Run** — expect FAIL.
- [ ] **Step 3: Implement.** `fbrBlock.ts` is pure and DOM-free. `printInvoice.ts` computes the QR (`fbrQrDataUrl`) only for `synced` invoices and passes it to both builders through their existing extras argument; a QR failure prints the block without the image.
- [ ] **Step 4: Reports.** Tax tab: Filed (count, amount), Unfiled (count, amount), Rejected (count) columns from B5's fields, included in CSV/XLSX. New "FBR queue" tab (`FiscalQueueTab`): jobs with status filter, document (invoice number or "Combined <date>"), attempts, last error code, next run, a Retry button on dead jobs.
- [ ] **Step 5: Run** the frontend gate — expect PASS.
- [ ] **Step 6: Commit** `frontend(print): E-07 — FBR block and QR on individually filed sales; fiscal report columns and queue tab`.

---

### Task B9 (E-07): Sandbox checks, docs

**Files:** Create `scripts/fbr-sandbox-checks.http`, `audit/FBR_SANDBOX_2026-10-12.md` (dated the day it is run). Modify `docs/superpowers/specs/2026-09-19-elevon-lpg-pos-design.md`, `docs/superpowers/plans/2026-09-21-phase-7-fbr-digital-invoicing.md`, `CLAUDE.md`, `backend/.env.example`.

- [ ] **Step 1: The checks file.** A REST-client `.http` file that reads `{{$dotenv FBR_SANDBOX_TOKEN}}` from `backend/.env.sandbox.local` (no token in the file) with these requests: `HS_UOM?hs_code=2711.1400&annexure_id=3`; `SaleTypeToRate` for today and transTypeId 75; a validate of a one-line SN002 sale on `2711.1400` with each returned UoM; a validate of a combined invoice with 50 items and one with 300 (generated from the golden's item, descriptions numbered); a validate of a combined invoice dated yesterday and one dated 7 days back.
- [ ] **Step 2: Run them** and record each request/response pair (token redacted) in the audit file with a decisions section: allowed UoM(s) and rate for `2711.1400`; the item-count limit, if any; whether back-dated `invoiceDate` validates.
- [ ] **Step 3: Act on the results.** If FBR caps items at N: `CreateBatch` refuses a selection with more than N lines with `fiscal_batch_too_large` and the `/fbr` screen pre-ticks only as many invoices as fit, saying so — add the test and the constant from a setting, not a literal, then re-run the gate. If back-dated invoices are refused: the `/fbr` date picker stays, and the failed batch's error text explains it; no code change. If `2711.1400` needs a different UoM: nothing in code — it is set per product (`products.fbr_uom`).
- [ ] **Step 4: Smoke through the app** on a local stack with `FISCAL_ALLOW_SANDBOX=true`: save the sandbox token in Settings → FBR, Run test (expect Passed with a number and QR), refresh reference, enable fiscal; ring one plain sale (unfiled), one Charge + FBR (filed, FBR block on the receipt), three more plain sales; File with FBR with one unticked (filed, three covered, one still pending); void a covered one; check the tax report and the pending line. Record the FBR numbers in the audit file.
- [ ] **Step 5: Docs.** Main spec §13: Phase 7 row → DONE with the date and both plans, deviations listed (on-demand filing, combined invoices, two token slots). Old Phase 7 plan: a note at the top of F4–F8 "Superseded by `2026-10-12-fbr-on-demand-filing.md`". `CLAUDE.md`: setting-key count; invariant 7 gains "…retries consult `fiscal_invoices` first, for single filings and batches; nothing files without an operator action; a production test never posts". `.env.example`: `FISCAL_SECRETS_KEY`, `FISCAL_ALLOW_SANDBOX` with one-line comments.
- [ ] **Step 6: Run both gates** — expect PASS.
- [ ] **Step 7: Commit** `docs(fbr): E-07 — sandbox checks for 2711.1400, combined invoices and back-dating; Phase 7 closed`.

---

## Self-review notes

- Spec coverage: §2.1 → B1 (statuses), B4 (assignment). §2.2 → B4, B5 (later single filing), B7 (button). §2.3 → B2 (payload), B3 (model, claim, flow), B5 (endpoints), B7 (screen). §2.4 → B5 (`Pending`), B7 (till line, dashboard). §2.5 → B4. §2.6 → B8 (receipt, tax report, queue tab), B5 (report fields). §2.7 → B1 (slots), B5 (`RunTest`, token routes), B6 (cards, dialog). §4 → Global Constraints. §5 → B9. §6 → tests named in B2–B5, B6–B8.
- Additions beyond the spec, recorded here: error codes `fiscal_batch_empty`, `fiscal_enabled`, `invalid_environment`; `fiscal_batches.last_error`; reference refresh covers every product HS code, not only the default.
- Migration numbering: this plan uses `005`; `004` is the deal-rates plan. If this plan lands first, rename to `004` and the other to `005` — the files do not depend on each other.
- Type names are consistent across tasks: `Environment`/`EnvSandbox`/`EnvProduction`, `KindBatch`, `JobRef`, `Batch`, `EligibleRow`, `TestResult`, `PendingSummary`.
- UI tasks (B6–B8) specify props, data flow, copy and the pure logic with tests; component markup follows the neighbouring files named in each step, since vitest here is node-only and components have no test harness.
