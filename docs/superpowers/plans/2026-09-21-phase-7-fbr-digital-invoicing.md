# Phase 7 — FBR Digital Invoicing Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Every completed sale is filed with FBR Digital Invoicing (PRAL DI API v1.12) as a `Sale Invoice`, its FBR number and QR print on the receipt, a void of a registered-buyer sale files a `Debit Note`, and the admin can switch the whole thing on from Settings once the store's token is entered — sandbox on the test environment first, production later.

**Architecture:** One new Go package `internal/fiscal` owns everything that touches FBR: config (token encrypted at rest), a pure payload builder golden-tested against the payloads the sandbox validated on 2026-09-21, an HTTP client that reads FBR's two-level error envelope, a ledger that makes retries idempotent, an outbound job queue with a worker goroutine, and an append-only audit trail. Invoice create and void call `fiscal.Submit` inline with a short deadline and fall back to the queue. The frontend adds a Settings → FBR tab, an FBR block with QR on the receipt and A4 invoice, fiscal columns on the tax report and a fiscal queue tab.

**Tech Stack:** as the rest of the repo (Go 1.24 / Gin / lib-pq raw SQL; React 18 + TS strict + TanStack; vitest node-only). New deps: Go `github.com/skip2/go-qrcode` is NOT used — the QR is rendered in the browser with npm `qrcode` (+ `@types/qrcode`) because print HTML is built client-side. Nothing else new.

**Spec:** `docs/superpowers/specs/2026-09-19-elevon-lpg-pos-design.md` §6.8 (fiscal column, fiscal queue), §7 (all), §8.3 (receipt). **Sandbox facts that override the spec's guesses:** `audit/FBR_SANDBOX_2026-09-21.md` — read it before Task 2. Sibling retail POS for porting shapes only: `../POS-System-General/backend/internal/fiscal/{fbr.go,crypto.go,queue.go,sync.go,audit.go}`; rename everything, never copy its tenant/host strings, and note its discount handling is **wrong for us** (audit addendum).

## Global Constraints (verbatim from the spec, CLAUDE.md and the sandbox audit)

- **Endpoints (§7.1):** validate `https://gw.fbr.gov.pk/di_data/v1/di/validateinvoicedata_sb` / `…/validateinvoicedata`; post `…/postinvoicedata_sb` / `…/postinvoicedata`; reference `https://gw.fbr.gov.pk/pdi/v1/{provinces,uom,doctypecode,transtypecode}`, `…/pdi/v2/SaleTypeToRate?date=DD-MMM-YYYY&transTypeId=75&originationSupplier=1`, `…/pdi/v2/HS_UOM?hs_code=&annexure_id=3`. Header `Authorization: Bearer <token>`. Flow is **validate → post**. The sandbox validate URL is the `_sb` one (contract test greps the constant).
- **Sandbox in release refuses (§7.1)** unless the environment sets `FISCAL_ALLOW_SANDBOX=true` — the test environment sets it, a production environment never does. A refusal is the stable code `sandbox_in_release`; it never files anything.
- **CLAUDE.md invariant 7:** no HS-code literal in Go code (the payload takes `invoice_lines.hs_code`, then `settings.default_hs_code`, then refuses with `fiscal_hs_code_missing`); sandbox validate URL is `_sb`; sandbox config in release refuses; **retries consult `fiscal_invoices` first**.
- **CLAUDE.md invariant 6:** `fiscal_audit_events` is append-only (BEFORE UPDATE OR DELETE trigger raises). Invariant 8: nothing here changes what a customer pays — the payload only reports §6.3's numbers.
- **Payload (§7.4 + audit):** `invoiceType "Sale Invoice"`; `invoiceDate` = `business_date` `YYYY-MM-DD`; seller fields from `fiscal_config`; buyer from the invoice's customer snapshot, walk-in = `buyerNTNCNIC ""`, `buyerBusinessName "Walk-in Customer"`, `buyerProvince` = seller province, `buyerAddress "N/A"`, `buyerRegistrationType "Unregistered"`; `scenarioId` **sandbox only**: `SN001` when the customer is `Registered` else `SN002`; provinces are **upper-case exactly as `/pdi/v1/provinces`** spells them (`PUNJAB`). Per line: `hsCode`, `productDescription` = `product_name + " " + formatKg(quantity) + " kg"`, `rate` = `fiscal_config.rate_desc`, `uoM` = `line.fbr_uom` else `fiscal_config.default_uom`, `quantity` = decimal 3 dp, **`valueSalesExcludingST = line_total − line_discount`**, **`salesTaxApplicable = line_tax`**, `discount = line_discount`, `furtherTax` = the line's share of `further_tax_amount` (largest-remainder split by taxable value, last line absorbs), `totalValues = valueSalesExcludingST + salesTaxApplicable + furtherTax`, `fixedNotifiedValueOrRetailPrice 0`, `salesTaxWithheldAtSource 0`, `extraTax ""`, `fedPayable 0`, `sroScheduleNo ""`, `sroItemSerialNo ""`, `saleType` = `fiscal_config.sale_type`. The rounding adjustment is **not** sent.
- **Void (audit decisions 1–2):** `invoiceType "Debit Note"`, `invoiceRefNo` = the original FBR `invoiceNumber`, top-level `reason` = the void reason, same lines; filed **only when the original invoice's buyer was `Registered`**; otherwise the void is `unfiled`. There is no `Credit Note`.
- **Responses:** HTTP 200 always; `validationResponse.statusCode` `"00"` valid / `"01"` invalid; invoice-level rejections carry `validationResponse.errorCode` + `error` with `invoiceStatuses: null`; line-level rejections carry `invoiceStatuses[i].errorCode/error`; `invoiceNumber` on a successful post. The client reads both levels.
- **Money in the payload is `float64` with 2 dp**, derived from the stored `NUMERIC(12,2)` columns; quantity 3 dp. Never recompute tax in `fiscal` — it copies `invoice_lines.line_tax`.
- All routes on `staff`/`admin` (everything new here is admin-only except nothing); `APIResponse` envelope; stable snake_case codes (`fiscal_disabled`, `fiscal_token_missing`, `fiscal_reference_stale`, `fiscal_hs_code_missing`, `fiscal_uom_not_allowed`, `tax_rate_mismatch`, `sandbox_in_release`, `fiscal_rejected`, `fiscal_unreachable`, `fiscal_job_not_found`, `private_setting`); never `err.Error()` to a client (the textual contract test already greps handlers).
- Settings key `fiscal_api_key_enc` is **private**: never returned by `GET /settings`, never writable through `PUT /admin/settings` (`private_setting`), only through `PUT /admin/fiscal/token`. The admin API returns `api_key_set` and `api_key_masked` (`****` + last 4), never the token.
- `FISCAL_SECRETS_KEY` env: base64 of exactly 32 bytes; **mandatory in release mode when `fiscal_config.enabled`** (boot logs a warning if unset; `Submit` refuses with `fiscal_secrets_key_missing`). Dev fallback: sha256 of a fixed dev string, never used in release.
- Frontend: TS strict, no `any`; colours via tokens; `.tabular` on numbers; print builders stay pure and DOM-free at module scope (`receipt.test.ts` runs in node).
- Commits `<scope>(<subsystem>): E-07 — <imperative>`, trailer `Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>`. Gate: `cd backend && go vet ./... && go test ./...` (with `TEST_DATABASE_URL`), `cd frontend && npm run type-check && npm run test && npx vite build`.
- No Bhookly tenant/host/appId/release-repo/Railway-ID references.

---

## File structure

```
backend/migrations/003_fiscal.sql                 fiscal_invoices, fiscal_outbound_jobs, fiscal_audit_events (+trigger), invoices.fiscal_void_status + fiscal_debit_note_number, settings seeds
backend/internal/fiscal/
  config.go        config_test.go                Config, ParseConfig, Validate, Public
  crypto.go        crypto_test.go                Encrypt/Decrypt AES-256-GCM, KeyFromEnv
  urls.go          urls_contract_test.go         URL selection, SandboxAllowed, ErrSandboxInRelease
  payload.go       payload_test.go  testdata/    DIInvoice/DIItem, BuildSaleInvoice, BuildDebitNote, golden fixtures
  client.go        client_test.go                Client interface, HTTPClient, Response, RejectedError
  ledger.go        ledger_db_test.go             RecordSynced, FindSynced
  audit.go                                       Event, Record
  submit.go        submit_db_test.go             Submit — the one orchestration; ledger-first contract test
  queue.go         queue_db_test.go              Enqueue, ClaimNext, Succeed, Retry, Dead, backoff, Worker
  reference.go     reference_test.go             Refresh reference lists into settings.fiscal_reference
backend/internal/settings/settings.go            + fiscal_config / fiscal_api_key_enc / fiscal_reference rules, Private(), CheckFiscalConsistency
backend/internal/handlers/settings.go            strip private keys on GetAll, reject on Update
backend/internal/handlers/fiscal.go  fiscal_db_test.go   admin endpoints
backend/internal/handlers/invoices.go            create/void hooks
backend/internal/reports/queries.go              tax bands fiscal counts; FiscalJobs
backend/internal/api/routes.go                   /admin/fiscal/*
backend/main.go                                  fiscal.StartWorker(db)
frontend/src/types/index.ts                      FiscalConfig, FiscalStatus, FiscalJob, FiscalReference
frontend/src/api/client.ts                       fiscal endpoints
frontend/src/components/settings/FiscalForm.tsx  Settings → FBR
frontend/src/lib/fiscalSchema.ts (+test)         zod schema + patch mapping
frontend/src/lib/print/fbrBlock.ts (+test)       FBR block HTML (mark, number, QR img), used by receipt + A4
frontend/src/lib/print/fbrMark.ts                inline SVG wordmark (no network)
frontend/src/lib/print/{receipt,invoiceA4,printInvoice}.ts   extras.qrDataUrl plumbing
frontend/src/components/reports/FiscalQueueTab.tsx + reportColumns fiscal columns
```

---

### Task F1 (E-07): Migration, config, crypto, URLs, private settings

**Files:** `backend/migrations/003_fiscal.sql`, `internal/fiscal/{config,crypto,urls}.go` (+tests, `urls_contract_test.go`), `internal/settings/settings.go`, `internal/handlers/settings.go` (+ test in `settings_db_test.go`).

**Migration `003_fiscal.sql`** (idempotent DDL, same style as 002):
```sql
CREATE TABLE IF NOT EXISTS fiscal_invoices (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  invoice_id UUID NOT NULL REFERENCES invoices(id) ON DELETE CASCADE,
  kind VARCHAR(12) NOT NULL,                 -- 'sale' | 'debit_note'
  fbr_invoice_number VARCHAR(64) NOT NULL,
  response JSONB,
  submitted_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
ALTER TABLE fiscal_invoices DROP CONSTRAINT IF EXISTS fiscal_invoices_kind_check;
ALTER TABLE fiscal_invoices ADD CONSTRAINT fiscal_invoices_kind_check CHECK (kind IN ('sale','debit_note'));
CREATE UNIQUE INDEX IF NOT EXISTS fiscal_invoices_invoice_kind ON fiscal_invoices (invoice_id, kind);

CREATE TABLE IF NOT EXISTS fiscal_outbound_jobs (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  invoice_id UUID NOT NULL REFERENCES invoices(id) ON DELETE CASCADE,
  kind VARCHAR(12) NOT NULL,
  status VARCHAR(12) NOT NULL DEFAULT 'pending', -- pending|processing|succeeded|dead
  attempt_count INT NOT NULL DEFAULT 0,
  max_attempts INT NOT NULL DEFAULT 20,
  next_run_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  last_error TEXT, last_error_code VARCHAR(32),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(), updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  started_at TIMESTAMPTZ, succeeded_at TIMESTAMPTZ
);
ALTER TABLE fiscal_outbound_jobs DROP CONSTRAINT IF EXISTS fiscal_outbound_jobs_status_check;
ALTER TABLE fiscal_outbound_jobs ADD CONSTRAINT fiscal_outbound_jobs_status_check CHECK (status IN ('pending','processing','succeeded','dead'));
CREATE UNIQUE INDEX IF NOT EXISTS fiscal_outbound_jobs_one_active ON fiscal_outbound_jobs (invoice_id, kind) WHERE status IN ('pending','processing');
CREATE INDEX IF NOT EXISTS idx_fiscal_jobs_dequeue ON fiscal_outbound_jobs (status, next_run_at, created_at);

CREATE TABLE IF NOT EXISTS fiscal_audit_events (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  invoice_id UUID REFERENCES invoices(id) ON DELETE SET NULL,
  kind VARCHAR(12) NOT NULL,                 -- sale|debit_note|test|reference
  phase VARCHAR(24) NOT NULL,                -- validate|post|ledger_recovery|refused|reference
  outcome VARCHAR(16) NOT NULL,              -- ok|rejected|unreachable|refused
  http_status INT, latency_ms BIGINT, error_code VARCHAR(32),
  request_excerpt TEXT, response_excerpt TEXT,
  created_by UUID REFERENCES users(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_fiscal_audit_invoice ON fiscal_audit_events (invoice_id, created_at DESC);
-- append-only, same trigger function 002 defines for void_log (reuse its name; CREATE OR REPLACE is idempotent)
DROP TRIGGER IF EXISTS fiscal_audit_events_append_only ON fiscal_audit_events;
CREATE TRIGGER fiscal_audit_events_append_only BEFORE UPDATE OR DELETE ON fiscal_audit_events FOR EACH ROW EXECUTE FUNCTION raise_append_only();

-- void_log is append-only (invariant 6), so the debit-note state lives on the mutable invoices row:
ALTER TABLE invoices ADD COLUMN IF NOT EXISTS fiscal_void_status VARCHAR(12) NOT NULL DEFAULT 'unfiled'; -- unfiled|pending|synced|failed
ALTER TABLE invoices ADD COLUMN IF NOT EXISTS fiscal_debit_note_number VARCHAR(64);
ALTER TABLE invoices DROP CONSTRAINT IF EXISTS invoices_fiscal_void_status_check;
ALTER TABLE invoices ADD CONSTRAINT invoices_fiscal_void_status_check CHECK (fiscal_void_status IN ('unfiled','pending','synced','failed'));

INSERT INTO settings (key, value) VALUES
  ('fiscal_config', '{"enabled":false,"is_sandbox":true,"seller_ntn_cnic":"","seller_business_name":"","seller_province":"","seller_address":"","scenario_registered":"SN001","scenario_unregistered":"SN002","rate_desc":"18%","sale_type":"Goods at Standard Rate (default)","trans_type_id":75,"default_uom":"KG","buyer_registration_default":"Unregistered","validate_url":"","post_url":""}'),
  ('fiscal_api_key_enc', '""'),
  ('fiscal_reference', '{}')
ON CONFLICT (key) DO NOTHING;
```
Check 002 for the actual name of the append-only trigger function and reuse it; if 002 inlines a function per table, define `fiscal_audit_events_no_mutation()` the same way. Extend `migrations_idempotent_contract_test.go`'s file list if it enumerates files.

**`config.go`:**
```go
type Config struct {
    Enabled bool `json:"enabled"`; IsSandbox bool `json:"is_sandbox"`
    SellerNTNCNIC, SellerBusinessName, SellerProvince, SellerAddress string
    ScenarioRegistered, ScenarioUnregistered string   // "SN001","SN002"
    RateDesc, SaleType string; TransTypeID int; DefaultUoM string
    BuyerRegistrationDefault string                  // "Unregistered"
    ValidateURL, PostURL string                      // optional overrides
}
func ParseConfig(raw json.RawMessage) (Config, error)      // strict: unknown fields rejected, defaults applied for missing
func (c Config) Validate() error                            // when Enabled: seller fields non-empty, NTN digits 7 or 13, province in upper-case list shape, rate_desc matches ^\d+(\.\d+)?%$, scenarios ^SN\d{3}$, TransTypeID > 0
func (c Config) Public(apiKeySet bool, masked string) PublicConfig  // Config + api_key_set + api_key_masked + sandbox_allowed
func LoadConfig(q Querier) (Config, error)                  // SELECT value FROM settings WHERE key='fiscal_config'
func LoadAPIKey(q Querier) (string, error)                  // decrypts fiscal_api_key_enc; "" when unset
```
**`crypto.go`:** `KeyFromEnv() ([]byte, error)` — `FISCAL_SECRETS_KEY` base64 → exactly 32 bytes else error; unset → dev key `sha256("elevon-fiscal-dev-only")` but `KeyFromEnv` returns `ErrSecretsKeyMissing` when `GIN_MODE=release`. `Encrypt(key []byte, plaintext string) (string, error)` → base64(nonce‖ciphertext) AES-256-GCM; `Decrypt(key []byte, b64 string) (string, error)`. Tests: round-trip, tamper → error, wrong key → error, 31-byte key rejected.

**`urls.go`:** constants exactly as §7.1; `func (c Config) ValidateURL() string` / `PostURL()` (override → sandbox/live); `func SandboxAllowed() bool` = `GIN_MODE != "release" || FISCAL_ALLOW_SANDBOX == "true"`; `var ErrSandboxInRelease = errors.New("sandbox_in_release")`; `func (c Config) CheckRuntime() error` → `ErrSandboxInRelease` when `IsSandbox && !SandboxAllowed()`. **`urls_contract_test.go`** greps `urls.go` for the literal `validateinvoicedata_sb` assigned to the sandbox-validate constant and asserts it differs from the post constant; a behavioural test sets `GIN_MODE=release` (t.Setenv) and asserts `CheckRuntime` returns `ErrSandboxInRelease` without `FISCAL_ALLOW_SANDBOX`, and nil with it.

**Settings:** add rules `fiscal_config` (rule: `fiscal.ParseConfig` then `Validate` — import cycle: put `ParseConfig` in `internal/fiscal/config.go` and have `settings` import `fiscal`; `fiscal` must not import `settings` — it takes a `Querier`), `fiscal_api_key_enc` (`text(0, 512)`), `fiscal_reference` (any JSON object). `func Private(key string) bool` → true for `fiscal_api_key_enc`. `settings.Load` stays raw; add `settings.LoadPublic(db)` that deletes private keys, used by `GetAll`. `Update` rejects any private key with 400 `private_setting`. `CheckConsistency` gains: when `fiscal_config.enabled` is true, `rate_desc` numeric must equal `tax_rate_cash × 100` (`"18%"` ↔ `0.18`) else `ValueError{Key:"fiscal_config", Message:"rate_desc does not match tax_rate_cash"}` → the handler maps `ValueError` on that key to code `tax_rate_mismatch`; enabling also requires `fiscal_api_key_enc` non-empty (`fiscal_token_missing`) and `fiscal_reference.hs_uom[default_hs_code]` to contain `default_uom` (`fiscal_reference_stale` when the reference is empty, `fiscal_uom_not_allowed` when present but missing). Tests in `settings_db_test.go`: GET never returns the key; PUT of it is `private_setting`; enabling without token is `fiscal_token_missing`.

Commit `fiscal(config): E-07 — migration 003, encrypted token, sandbox-in-release guard, private settings`.

---

### Task F2 (E-07): Payload builder, golden-tested against the sandbox

**Files:** `internal/fiscal/payload.go`, `payload_test.go`, `testdata/case_a_sn002.json`, `case_c_discount.json`, `case_d_further_tax.json`, `case_e_debit_note.json` (the exact JSON the sandbox validated on 2026-09-21, with seller placeholders replaced by fixed test values `8951943` / `Elevon Test Seller` / `PUNJAB` / `Lahore` and `invoiceDate "2026-09-21"`).

**Produces:**
```go
type DIItem struct {
    HSCode string `json:"hsCode"`; ProductDescription string `json:"productDescription"`; Rate string `json:"rate"`; UoM string `json:"uoM"`
    Quantity float64 `json:"quantity"`; TotalValues float64 `json:"totalValues"`; ValueSalesExcludingST float64 `json:"valueSalesExcludingST"`
    FixedNotifiedValueOrRetailPrice float64 `json:"fixedNotifiedValueOrRetailPrice"`; SalesTaxApplicable float64 `json:"salesTaxApplicable"`
    SalesTaxWithheldAtSource float64 `json:"salesTaxWithheldAtSource"`; ExtraTax string `json:"extraTax"`; FurtherTax float64 `json:"furtherTax"`
    SroScheduleNo string `json:"sroScheduleNo"`; FedPayable float64 `json:"fedPayable"`; Discount float64 `json:"discount"`
    SaleType string `json:"saleType"`; SroItemSerialNo string `json:"sroItemSerialNo"`
}
type DIInvoice struct {
    InvoiceType string `json:"invoiceType"`; InvoiceDate string `json:"invoiceDate"`
    SellerNTNCNIC string `json:"sellerNTNCNIC"`; SellerBusinessName string `json:"sellerBusinessName"`; SellerProvince string `json:"sellerProvince"`; SellerAddress string `json:"sellerAddress"`
    BuyerNTNCNIC string `json:"buyerNTNCNIC"`; BuyerBusinessName string `json:"buyerBusinessName"`; BuyerProvince string `json:"buyerProvince"`; BuyerAddress string `json:"buyerAddress"`; BuyerRegistrationType string `json:"buyerRegistrationType"`
    InvoiceRefNo string `json:"invoiceRefNo"`; ScenarioID string `json:"scenarioId,omitempty"`; Reason string `json:"reason,omitempty"`
    Items []DIItem `json:"items"`
}
type Buyer struct{ NTNCNIC, Name, Province, Address, RegistrationType string } // from customers row; zero value = walk-in
type BuildInput struct{ Invoice models.Invoice /* with Lines */; Buyer *Buyer; DefaultHSCode string }
func BuildSaleInvoice(in BuildInput, cfg Config) (DIInvoice, error)
func BuildDebitNote(in BuildInput, cfg Config, refNumber, reason string) (DIInvoice, error)  // InvoiceType "Debit Note", InvoiceRefNo, Reason; same lines
var ErrHSCodeMissing = errors.New("fiscal_hs_code_missing")
func SplitFurtherTax(total float64, taxable []float64) []float64   // largest remainder on paisa, last line absorbs; Σ == total exactly
```
Rules: buyer registered iff `Buyer.RegistrationType == "Registered"` **and** NTN/CNIC digits non-empty; scenario = registered ? `cfg.ScenarioRegistered` : `cfg.ScenarioUnregistered`, emitted only when `cfg.IsSandbox`; buyer province defaults to seller's when empty; `BuyerNTNCNIC` is digits only (strip `-` and spaces). Money via `math.Round(x*100)/100`. `productDescription` uses `strconv.FormatFloat(q, 'f', 3, 64)`.

**Tests:** for each fixture, build from a `models.Invoice` constructed in the test with the matching lines, marshal, and compare **field-by-field to the fixture after unmarshalling both into `map[string]any`** (order-independent, exact numbers). Plus: walk-in defaults; registered buyer gives SN001 in sandbox and no `scenarioId` in production; `ErrHSCodeMissing` when line and default are both empty; `SplitFurtherTax(132.50, [3202.83, 11399.67])` sums exactly; debit note carries ref + reason and no scenario when not sandbox.

Commit `fiscal(payload): E-07 — DI payload builder golden-tested against the sandbox cases`.

---

### Task F3 (E-07): Client, ledger, audit, Submit

**Files:** `internal/fiscal/{client,ledger,audit,submit}.go`, `client_test.go` (httptest), `ledger_db_test.go`, `submit_db_test.go`, `submit_ledger_first_contract_test.go`.

**`client.go`:**
```go
type Response struct{ InvoiceNumber string; StatusCode string; ErrorCode, Error string; Lines []LineStatus; HTTPStatus int; Raw json.RawMessage }
type LineStatus struct{ ItemSNo, StatusCode, InvoiceNo, ErrorCode, Error string }
type RejectedError struct{ Code, Message string; Line int }   // Line 0 = invoice level; Error() = "fiscal_rejected " + Code
type Client interface { Validate(ctx context.Context, inv DIInvoice) (Response, error); Post(ctx, DIInvoice) (Response, error); Get(ctx, url string) ([]byte, error) }
func NewHTTPClient(cfg Config, token string, timeout time.Duration) Client   // sets Authorization, Content-Type; http.Client{Timeout}; TLS verification on
func (r Response) Rejection() *RejectedError   // nil when StatusCode=="00" and every line "00"; else the first error found (invoice level first)
```
Transport failures and non-200 return a plain error wrapped as `fmt.Errorf("fiscal_unreachable: %w", err)`; the JSON envelope is parsed leniently (FBR pads the body with tabs; `json.Unmarshal` copes). Tests with `httptest.Server`: the 5 real response bodies from the audit (valid, 0058 line-level, 0205 invoice-level with `invoiceStatuses:null`, 0104, post success with `invoiceNumber`) — `Rejection()` returns the right code and line; 500 → unreachable; timeout → unreachable; the `Authorization` header is `Bearer <token>`.

**`ledger.go`:** `RecordSynced(q Execer, invoiceID uuid.UUID, kind, number string, raw json.RawMessage) error` (`ON CONFLICT (invoice_id, kind) DO NOTHING`); `FindSynced(q Querier, invoiceID uuid.UUID, kind string) (string, bool, error)`.

**`audit.go`:** `type Event struct{ InvoiceID *uuid.UUID; Kind, Phase, Outcome string; HTTPStatus *int; LatencyMs *int64; ErrorCode, RequestExcerpt, ResponseExcerpt string; CreatedBy *uuid.UUID }`; `Record(db *sql.DB, e Event)` — best effort, excerpts truncated to 4000 runes, **token never appears in an excerpt** (the request excerpt is the payload JSON, which has no token).

**`submit.go` — the one orchestration:**
```go
type Kind string; const KindSale Kind = "sale"; const KindDebitNote Kind = "debit_note"
type Deps struct{ DB *sql.DB; NewClient func(cfg Config, token string, timeout time.Duration) Client; Timeout time.Duration; Now func() time.Time }
func Submit(ctx context.Context, d Deps, invoiceID uuid.UUID, kind Kind, actor *uuid.UUID) (string, error)
```
Steps, in this order (the contract test pins 1 before 4):
1. `FindSynced` → if found: apply to the invoice (step 6) with phase `ledger_recovery`, return the number. **This runs before any HTTP.**
2. `LoadConfig`; if `!Enabled` → `ErrDisabled` (`fiscal_disabled`); `CheckRuntime()` → `ErrSandboxInRelease` (audit phase `refused`, outcome `refused`); `LoadAPIKey` empty → `ErrTokenMissing`; `KeyFromEnv` error in release → `ErrSecretsKeyMissing`.
3. Load invoice + lines (`handlers.loadInvoiceWithLines` is in `handlers`; move that query into a small `internal/invoice/load.go` `LoadWithLines(q, id)` so `fiscal` can use it without importing handlers) and the customer row when `customer_id` is set → `Buyer`. For `KindDebitNote` also load `void_log.reason` and `fiscal_invoices` sale number (`ErrNotSynced` if the sale was never filed; `ErrBuyerUnregistered` when the buyer is not registered → caller marks `unfiled`).
4. Build; `Validate`; `Rejection()` → audit `validate/rejected`, return `*RejectedError`.
5. `Post`; transport error → audit `post/unreachable`, return it (retryable). `Rejection()` → audit `post/rejected`, return `*RejectedError`. Otherwise `RecordSynced` **first**, then step 6, audit `post/ok` with latency.
6. Apply: sale → `UPDATE invoices SET fiscal_status='synced', fiscal_invoice_number=$2, fiscal_details=$3` where `fiscal_details = {"kind","scenario_id","filed_at","line_numbers":[…],"is_sandbox"}`; debit note → `UPDATE invoices SET fiscal_void_status='synced', fiscal_debit_note_number=$2 WHERE id=$1` (never touch `void_log`, it is append-only; its `fiscal_credit_note` column stays unused).
`MarkFailed(db, invoiceID, kind, code)` sets `invoices.fiscal_status='failed'` / `invoices.fiscal_void_status='failed'` and stores `{"error_code","error"}` in `fiscal_details`.

**Tests (DB + httptest):** happy path sale sets status/number/ledger/audit rows; ledger-first: pre-insert a `fiscal_invoices` row and a client that panics if called → Submit returns the number without HTTP; rejection marks nothing synced and returns `*RejectedError` with the code; unreachable leaves `pending`; sandbox in release refuses with an audit row and no HTTP; debit note for an unregistered buyer returns `ErrBuyerUnregistered`. **Contract test** greps `submit.go` for `FindSynced(` appearing before `.Validate(` in source order.

Commit `fiscal(submit): E-07 — FBR client, ledger-first submit, audit trail`.

---

### Task F4 (E-07): Queue, worker, invoice hooks, admin endpoints

**Files:** `internal/fiscal/{queue,reference}.go` (+ `queue_db_test.go`, `reference_test.go`), `internal/handlers/fiscal.go` (+ `fiscal_db_test.go`), `internal/handlers/invoices.go`, `internal/api/routes.go`, `main.go`.

**`queue.go`:**
```go
func Enqueue(q Execer, invoiceID uuid.UUID, kind Kind, runAt time.Time) error   // INSERT … ON CONFLICT DO NOTHING on the partial unique index (use a WHERE NOT EXISTS since partial indexes cannot be ON CONFLICT targets without the predicate; write it as INSERT … SELECT … WHERE NOT EXISTS (SELECT 1 … status IN ('pending','processing')))
func ClaimNext(db *sql.DB, now time.Time) (*Job, error)   // UPDATE … SET status='processing', started_at=now WHERE id = (SELECT id … WHERE status='pending' AND next_run_at <= $1 ORDER BY next_run_at, created_at LIMIT 1 FOR UPDATE SKIP LOCKED) RETURNING …
func Succeed(db, jobID) error; func Retry(db, job Job, err error) error; func Dead(db, jobID uuid.UUID, code, msg string) error
func Backoff(attempt int) time.Duration   // 30s, 1m, 2m, 5m, 10m, then 15m flat; attempt is 1-based
func ProcessOnce(ctx context.Context, d Deps) (bool, error)   // claim → Submit → Succeed | Retry (unreachable) | Dead+MarkFailed (rejected, disabled, refused, token missing, unregistered)
func StartWorker(ctx context.Context, d Deps, every time.Duration)   // goroutine: tick, drain with ProcessOnce until false; log errors; recover panics
func Counts(q Querier) (pending, dead int, lastSuccess *time.Time, err error)
```
Rejections are dead immediately (retrying a `01` cannot succeed without a config change); `attempt_count+1 >= max_attempts` → dead with code `fiscal_max_attempts`. Tests: enqueue is idempotent while active; claim skips future `next_run_at`; two concurrent claims get different jobs (goroutines); backoff table; `ProcessOnce` with an unreachable fake client reschedules and increments; with a rejecting fake client marks dead and the invoice `failed`.

**`reference.go`:** `Refresh(ctx, d Deps, cfg Config, today time.Time) (Reference, error)` fetches provinces, uom, `HS_UOM` for `settings.default_hs_code` (passed in), `SaleTypeToRate` for `cfg.TransTypeID` and `today` formatted `02-Jan-2006`; `type Reference struct{ Provinces []string; UoMs []struct{ID int; Description string}; HSUoM map[string][]string; Rates []struct{ID int; Desc string; Value float64}; RefreshedAt time.Time }` saved as settings `fiscal_reference`. Test with httptest replaying the four real bodies from the audit (province names upper-case; HS_UOM `[{13,KG}]`; rates `[{728,"18%",18}]`).

**Invoice hooks (`invoices.go`):**
- Create: read `fiscal.LoadConfig(h.db)` alongside settings (before the transaction). Insert `fiscal_status` as `'pending'` when `cfg.Enabled` else `'off'`. **After commit**, when enabled: `number, err := fiscal.Submit(ctx with 8 s, deps, inv.ID, KindSale, actor)`; on success set `inv.FiscalStatus='synced'`, `inv.FiscalInvoiceNumber=&number` in the response; on `*RejectedError` → `fiscal.MarkFailed` and `inv.FiscalStatus='failed'`; on any other error → `fiscal.Enqueue(now)` and leave `pending`. The sale is **never** rolled back because of FBR. Response gains `fiscal_error_code` (string, omitempty) so the till can toast "FBR rejected: 0104".
- Void: after commit, when the invoice was `synced` and its customer's `buyer_registration_type == 'Registered'` (read inside the void transaction) → `UPDATE invoices SET fiscal_void_status='pending' WHERE id=$1` in the same transaction, then after commit `Submit(KindDebitNote)` inline with the same 8 s rule; else the row stays `unfiled`. Existing void tests unchanged (fiscal disabled → `unfiled`). The invoice DTO and TS type gain `fiscal_void_status` and `fiscal_debit_note_number`.

**`handlers/fiscal.go` (all under `admin`):**
| Route | Body → Response |
|---|---|
| `GET /admin/fiscal/status` | `PublicConfig` + `queue: {pending, dead, last_success_at}` + `secrets_key_set` |
| `PUT /admin/fiscal/token` | `{api_key}` (trim, 8–512 chars) → encrypt with `KeyFromEnv` → save `fiscal_api_key_enc`; 500 `fiscal_secrets_key_missing` in release without the env |
| `DELETE /admin/fiscal/token` | clears it; refuses `fiscal_enabled` if config is enabled |
| `POST /admin/fiscal/test-connection` | builds a one-line sample sale (`quantity 1.000`, `valueSalesExcludingST 100.00`, tax from `tax_rate_cash`, HS from `default_hs_code`, walk-in buyer, scenario unregistered) and calls **Validate only** (never Post) with a 30 s deadline → `{ok, status_code, error_code, error, line, url, is_sandbox}`; 409 `sandbox_in_release` when refused; audit kind `test` |
| `POST /admin/fiscal/reference/refresh` | `Refresh` → saves → returns `Reference` |
| `GET /admin/fiscal/jobs?status=pending|dead|all&limit=` | rows joined to `invoices.invoice_number, total_payable, business_date` |
| `POST /admin/fiscal/jobs/:id/retry` | dead → pending, `next_run_at=now`, `attempt_count=0`; 404 `fiscal_job_not_found` |
| `POST /admin/fiscal/jobs/retry-dead` | all dead → pending |
| `POST /admin/fiscal/invoices/:id/submit` | for a `failed`/`pending` sale with no active job: enqueue now and return the job |

**`main.go`:** after `LoadDayBoundaryHour`: `fiscal.StartWorker(ctx, fiscal.Deps{DB: db, NewClient: fiscal.NewHTTPClient, Timeout: 60*time.Second, Now: time.Now}, 5*time.Second)`; log a warning at boot when `GIN_MODE=release`, the config is enabled and `FISCAL_SECRETS_KEY` is unset. `routes_role_gates_contract_test.go` is structural and pins the new routes automatically — confirm it still passes.

Tests (`fiscal_db_test.go`): status masks the token; token round-trip sets `api_key_set` and `****1234`; test-connection against httptest returns ok with the valid body and `error_code 0104` with the rejected one, and `sandbox_in_release` under `GIN_MODE=release`; jobs list/retry; invoice create with fiscal enabled and an unreachable fake → `pending` + one job; with a rejecting fake → `failed` + `fiscal_error_code`.

Commit `fiscal(queue): E-07 — outbound queue and worker, invoice create/void hooks, admin endpoints`.

---

### Task F5 (E-07): Settings → FBR tab

**Files:** `frontend/src/types/index.ts` (`FiscalConfig`, `FiscalPublicConfig`, `FiscalStatusResponse`, `FiscalTestResult`, `FiscalReference`, `FiscalJob`), `api/client.ts` (`getFiscalStatus`, `setFiscalToken`, `clearFiscalToken`, `testFiscalConnection`, `refreshFiscalReference`, `listFiscalJobs`, `retryFiscalJob`, `retryDeadFiscalJobs`, `submitFiscalInvoice`), `lib/fiscalSchema.ts` (+ test), `components/settings/FiscalForm.tsx`, `routes/_app/settings.tsx` (tab `fbr`, label "FBR").

**Screen:** a status strip (Off / Sandbox / Production, token set or not, secrets key set or not, pending/dead counts, last success). Sections: **Token** (password input, "Save token", "Remove"; shows `****1234`), **Seller** (NTN/CNIC, business name, province as a `Select` fed from `fiscal_reference.provinces` with a "Refresh reference lists" button beside it, address), **Classification** (default HS code — existing setting, shown read-only with a link to Tax; UoM select from `fiscal_reference.hs_uom[hs]`; rate select from `fiscal_reference.rates` — must show `18%`; sale type; trans type id), **Mode** (sandbox/production radio; sandbox scenarios SN001/SN002 shown only in sandbox; a warning when sandbox is chosen and `sandbox_allowed` is false), **Enable** switch with the server's refusal codes mapped to inline messages (`fiscal_token_missing`, `fiscal_reference_stale`, `fiscal_uom_not_allowed`, `tax_rate_mismatch`). **"Test connection"** button shows the result inline: green "Valid" or the FBR error code + message. Saving uses `useSaveSettings('FBR')` with a `fiscal_config` patch; the token has its own mutation.

Copy rules per the design pass: sentence case, plain verbs, errors say what to do next ("Refresh reference lists, then enable").

Tests: `fiscalSchema.test.ts` (NTN digits 7 or 13; rate `%` shape; province required when enabling; `fiscalToPatch` round-trip).

Commit `frontend(settings): E-07 — FBR tab: token, seller, classification, mode, test connection`.

---

### Task F6 (E-07): FBR block on the receipt and A4, QR, reprint refresh

**Files:** `frontend/package.json` (+ `qrcode`, `@types/qrcode`), `lib/print/fbrMark.ts` (inline SVG string, an "FBR Digital Invoicing" wordmark in black, no external URL), `lib/print/fbrBlock.ts` (+ `fbrBlock.test.ts`), `lib/print/receipt.ts` (replace `fiscalBlock`), `lib/print/invoiceA4.ts`, `lib/print/printInvoice.ts`, `components/pos/RecentInvoices.tsx` + `components/invoices/InvoiceTable.tsx` (fiscal badge), `components/pos/TenderDialog.tsx` or the charge flow (toast on `fiscal_error_code`).

**`fbrBlock.ts`:**
```ts
export interface FbrBlockInput { status: string; number: string | null; qrDataUrl?: string; width: 'thermal' | 'a4' }
export function fbrBlockHtml(i: FbrBlockInput): string
```
`synced` + number → mark, "FBR Invoice No." + number, QR `<img src="data:…">` (160 px thermal, 120 px A4) when `qrDataUrl` given, else the number alone; `pending` → "FBR submission pending"; `failed` → "FBR submission failed — see Settings › FBR"; `off` → `''`. Pure string, tested with snapshots for the four states.

**Plumbing:** `buildReceiptHtml(invoice, settings, extras: PrintExtras = {})` and `buildInvoiceA4Html(invoice, settings, buyer = {}, extras = {})` with `interface PrintExtras { qrDataUrl?: string }`. `printInvoice` (already async) does: `const fresh = await apiClient.getInvoice(invoice.id)` when `invoice.fiscal_status === 'pending'` (reprint refresh, §7.7) and `qrDataUrl = fresh.fiscal_invoice_number ? await QRCode.toDataURL(number, { margin: 0, width: 320, errorCorrectionLevel: 'M' }) : undefined`. The QR payload is the FBR invoice number (spec §7.4). `qrcode` is imported only inside `printInvoice.ts` (browser path) so the node tests of the builders never load it.

**Badges:** RecentInvoices and the invoice table show a small `FBR ✓` / `FBR …` / `FBR ✕` chip from `fiscal_status` (`off` shows nothing), tokens only. Charge flow: when the create response carries `fiscal_error_code`, toast "Sale recorded. FBR rejected it (0104) — the admin can retry from Settings › FBR" (destructive variant); the sale itself proceeds and prints.

Commit `frontend(print): E-07 — FBR block with QR on receipt and A4, reprint refresh, fiscal badges`.

---

### Task F7 (E-07): Reports — fiscal columns and the fiscal queue tab

**Files:** `backend/internal/reports/queries.go` (`TaxBands` gains `Synced, Pending, Failed int` per band and totals; `FiscalJobs(ctx, db, status, limit)` reused by the handler), `reports` CSV/XLSX tax sheet gains the three columns, `handlers/reports.go` (`fiscal-queue` is **not** a report export — it is the admin jobs endpoint from F4), `frontend/src/components/reports/reportColumns.tsx` (tax tab columns), `components/reports/FiscalQueueTab.tsx`, `routes/_app/reports.tsx` (tab "FBR queue", visible only when `fiscal_config.enabled` or there are jobs).

Queue tab: table of jobs (invoice number, business date, kind, status, attempts, last error code + message, next run), "Retry" per dead row, "Retry all dead", a per-row "Submit now" for failed invoices without a job; polls every 15 s while any job is pending. Tests: `TaxBands` counts on a fixture with one synced/one pending/one failed invoice (DB test); column definitions test if one exists for other tabs.

Commit `reports(fiscal): E-07 — fiscal counts on the tax report, FBR queue tab`.

---

### Task F8 (E-07): Deployment, docs, sandbox smoke through the app

- `backend/.env.example` (+ README section): `FISCAL_SECRETS_KEY` (generate: `openssl rand -base64 32`), `FISCAL_ALLOW_SANDBOX=true` **only** on the test environment. `docs/HANDOVER_2026-09-20.md`: a "Phase 7" section with the enable checklist (set the two vars → redeploy → Settings › FBR: token → refresh reference → seller → rate 18% → test connection → enable), and the two open items (registered-buyer NTN to exercise SN001/Debit Note; advisor's answer on voids of unregistered sales). Spec §13 Phase 7 row → DONE with deviations (Debit Note instead of Credit Note; unregistered voids unfiled; `trans_type_id` 75). CLAUDE.md invariant 7 text moves from "(from Phase 7)" to active and gains "`fiscal_api_key_enc` is private"; key-directories row for `internal/fiscal/`.
- Full gate; isolation grep.
- **Sandbox smoke via the app** (with the owner's token, entered through the UI on the local compose stack with `FISCAL_ALLOW_SANDBOX=true`): enable → ring a cash sale for a walk-in → receipt shows FBR number and QR → `fiscal_invoices` has the row → stop the network (point `validate_url` at an unreachable port), ring a sale → `pending` + job → restore → worker syncs it → reprint shows the number. Record numbers in `audit/FBR_SANDBOX_<date>.md`. Do **not** post from the Railway environment until the owner says so (it files sandbox invoices under the store's NTN; harmless but visible in IRIS).

Commit `docs(handover): E-07 — FBR enable checklist, sandbox smoke, open items`.

---

## Self-review notes

- **Spec coverage:** §7.1 → F1 (URLs, refusal), F3 (validate→post); §7.2 → F1, F5; §7.3 → F1 (reference check on enable), F2 (HS/UoM/quantity/scenario), F4 (reference refresh); §7.4 → F2; §7.5 → F2 (`SplitFurtherTax`); §7.6 → F3 (ledger-first), F4 (queue, backoff, dead letter, debit note on void); §7.7 → F6; §6.8 fiscal column + fiscal queue → F7; CLAUDE.md invariant 7 → F1 contract test, F2 `ErrHSCodeMissing`, F3 contract test. Out of scope: production cut-over (owner decision), advisor questions.
- **Condensed on purpose**, like the demo and Phase 5 plans: interfaces, algorithms and test lists rather than verbatim code, because the sandbox audit fixes the payload precisely and the sibling gives the shapes. Implementers read §7, the audit and the named sibling files.
- **Type consistency:** `fiscal.Config`/`ParseConfig` are used by F1 settings rules, F2 builder, F3 submit, F4 handlers; `Deps` is shared by F3 `Submit`, F4 `ProcessOnce`/`StartWorker`/`Refresh`/handlers; `Kind` constants by F3/F4/F7; `PrintExtras` by F6's three builders; `FiscalJob` TS type by F5 client and F7 tab.
- **Rulings recorded here:** (1) the token lives in its own private settings key rather than inside `fiscal_config`, so the generic settings map can never leak it; (2) `01` rejections dead-letter immediately, retries are for transport only; (3) the QR is browser-rendered, no Go QR dependency; (4) `FISCAL_ALLOW_SANDBOX` is the only way a release build files to `_sb`.
