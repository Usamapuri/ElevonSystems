# Demo Path — Phases 2–4 Condensed Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** An end-to-end sale on the real stack by 2026-09-21: sign in → set today's rates → pick a customer → weigh gas (kg / tonne / amount / gross−tare) → tender cash, card, online or credit → invoice created with server-side pricing, numbered, ledgered → printed (thermal or A4) → reprint and void → day opened and closed with a Z-report.

**Architecture:** One migration (`002_trading.sql`) creates every trading table from spec §5 with append-only triggers. Backend packages: `pricing` (the one money function), `invoice` (numbering), `dayops` (business day), handlers for products, rates, customers, receipts, business days, invoices. Frontend: `/rates` (products and today's rates), `/customers`, `/pos` (WeightPad, CartRail, CustomerPicker, TenderDialog), `/day-close`, `/invoices`, print pipeline under `lib/print/`. Everything the spec defers (FBR, reports/exports, desktop) stays out; `fiscal_status` is written as `off`.

**Tech Stack:** as Phase 1. Copy list for ports: spec §9 — `internal/pricing/pricing.go` (reshape to §6.3), `handlers/payments.go` `moneyPaisa`, `internal/dayops/service.go` (open / ensure-open / close / reopen / force-close + `writeAuditTx`, drop staging/shifts/tabs/offline/email), `handlers/order_numbers.go` (atomic counter upsert), `handlers/pin.go` shape, frontend `lib/print/{printTransport,printPageSize,thermalPrintCss,printDebug}.ts`, `components/counter/{NumericKeypad,PinEntryModal}.tsx`, `components/admin/dayclose/*`. RETAIL = `../POS-System-General`; rename `bhookly → elevon`, never reference its tenants.

**Spec:** `docs/superpowers/specs/2026-09-19-elevon-lpg-pos-design.md` §5.2–5.5, §6.2–6.7, §8.3–8.4, §10, §13. Phase 1 plan and handover describe what already exists (`settings.Load`, `staffpin.Identify`, `checkPassword`, `util.BusinessDate`, test helpers, `useSettings`, `Field`, `apiClient` shape).

## Global Constraints (verbatim from the spec and CLAUDE.md)

- Money `NUMERIC(12,2)`; weight `NUMERIC(14,3)` kg; every pooled session is `TimeZone=Asia/Karachi`; reports and numbering use `business_date` = `util.BusinessDate(now)`.
- **Invoice money is computed server-side from `products.rate`** and the submitted quantities; client money is never trusted. Go `pricing.ComputeTotals` and TS `lib/pricing.ts` share one JSON fixture (`backend/internal/pricing/testdata/pricing_fixture.json`, symlink-free copy at `frontend/src/lib/pricing_fixture.json` — identical bytes, pinned by a test that hashes both).
- §6.3 exactly: `line_total = round2(qty × rate)`; `discount = pct ? round2(subtotal×pct/100) : min(amount, subtotal)`; pro-rata `line_discount` with the last line absorbing the paisa remainder; `line_tax = round2((line_total − line_discount) × tax_rate(tender))`; `tax = Σ line_tax`; `further_tax = round2(Σ taxable × further_tax_rate)` only when the buyer is unregistered; `total_amount = Σ taxable + tax + further_tax`; `total_payable = round_half_up_to_rupee(total_amount)`; `rounding_adjustment = total_payable − total_amount`. `round2` is round-half-up on paisa using `int64` paisa arithmetic (`moneyPaisa`). Tax rate for `credit` = `tax_rate_credit` unless null → `tax_rate_cash`.
- **Customer-visible money is stop-and-ask (CLAUDE.md invariant 8):** the rounding examples in Task D4 are put to the owner before `dev → main`; the demo runs on the spec's D9/D10 decisions.
- `void_log`, `customer_ledger_entries`, `day_close_audit_log` are append-only (BEFORE UPDATE OR DELETE triggers raise). Every invoice has a `business_day_id`. **No auto-open**: `EnsureOpenDayForInvoice` has exactly the four branches of §6.7 (open today → use; other date open → `previous_day_open` 409; today closed → reopen for late sale with audit; none → `day_not_open` 409) and a contract test greps that no `INSERT INTO business_days` exists on that path.
- Invoice numbers `YYYYMMDD-NNN` from `invoice_number_counters` via one atomic upsert in its own short transaction; receipts `R-YYYYMMDD-NNN` likewise. `client_op_id` UUID makes `POST /invoices` idempotent (repeat returns the existing invoice, 200).
- Voids are whole-invoice, admin PIN (`staffpin.Identify(tx, pin, staffpin.AdminOnly)`), reason required, `void_log` row, ledger mirror for credit invoices, invoice stays visible with status `voided`, excluded from every total.
- Credit tender requires a customer with `credit_allowed`; if `credit_limit_enforced` and `balance + total_payable > credit_limit` → 409 `credit_limit_exceeded` unless a valid admin `pin` is in the request (override is logged in `notes`).
- Every rate change from any screen writes `product_rate_history` in the same transaction.
- All routes on `staff`/`admin`; `APIResponse` envelope; stable snake_case codes (`product_not_found`, `invalid_quantity`, `invoice_not_found`, `invoice_already_voided`, `invalid_pin`, `day_not_open`, `previous_day_open`, `day_already_open`, `customer_required`, `credit_not_allowed`, `credit_limit_exceeded`, `variance_note_required`, `duplicate_client_op`, `rate_limited`, …); never `err.Error()` to a client.
- Route guards use `beforeLoad` + `redirect`. `routeTree.gen.ts` is regenerated by `npx vite build` and committed.
- Printing: off-screen iframe + `window.print()` with `@page` size locked; `window.elevon.print(html, {pageSize})` when present.
- Commits `<scope>(<subsystem>): E-0N — <imperative>` (N = 2, 3 or 4 by task), trailer `Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>`. Gate: `cd backend && go vet ./... && go test ./...` (with `TEST_DATABASE_URL`), `cd frontend && npm run type-check && npm run test`.
- No Bhookly tenant/host/appId/release-repo/Railway-ID references.

---

## File structure

```
backend/
  migrations/002_trading.sql
  internal/
    pricing/pricing.go  pricing_test.go  testdata/pricing_fixture.json
    invoice/numbers.go  numbers_test.go            AllocateInvoiceNumber, AllocateReceiptNumber
    dayops/service.go  service_db_test.go  no_auto_open_contract_test.go
    ledger/ledger.go  ledger_db_test.go            entries, Balance, Statement
    models/{product,customer,invoice,dayops}.go
    handlers/products.go  products_db_test.go     products CRUD + rates (history)
    handlers/customers.go  customers_db_test.go   CRUD, balance, statement, receipts, receipt void
    handlers/dayops.go  dayops_db_test.go         current, open, close, reopen, force-close, movements, z-data
    handlers/invoices.go  invoices_db_test.go     create, list, get, void, recent
    api/routes.go
frontend/src/
  types/index.ts  api/client.ts
  lib/pricing.ts  pricing.test.ts  pricing_fixture.json  fixture_parity.test.ts
  lib/weight.ts  weight.test.ts                   kg/tonne/amount/gross−tare conversions
  lib/print/{printTransport,printPageSize,thermalPrintCss,receipt,invoiceA4,zReport}.ts  receipt.test.ts
  routes/_app/{rates,customers,pos,day-close,invoices}.tsx
  components/rates/{ProductDialog,RatesTable}.tsx
  components/customers/{CustomerDialog,CustomerDetail,ReceivePaymentDialog}.tsx
  components/pos/{ProductTiles,WeightPad,CartRail,CustomerPicker,TenderDialog,DayGateBanner,RecentInvoices}.tsx
  components/dayclose/{OpenDayDialog,MovementDialog,CloseDayForm,ZReportView}.tsx
  components/shared/{NumericKeypad,PinEntryModal,Money}.tsx
```

---

### Task D1 (E-02): Migration `002_trading.sql` — every trading table, counters, append-only triggers

**Files:** Create `backend/migrations/002_trading.sql`; extend `backend/internal/database/migrate_test.go` with a DB-backed check that the three append-only triggers raise.

**Interfaces produced:** the tables below, exactly as spec §5.2–5.5 (columns, checks, indexes). Later tasks read column names from here.

- [ ] **Step 1: Write the migration** (idempotent forms only — `CREATE TABLE IF NOT EXISTS`, `CREATE INDEX IF NOT EXISTS`, `DROP CONSTRAINT IF EXISTS` before `ADD CONSTRAINT`, `CREATE OR REPLACE FUNCTION`, `DROP TRIGGER IF EXISTS` before `CREATE TRIGGER`; the idempotency contract test enforces the first four):

```sql
-- 002_trading — products, rates, customers, ledger, business days, invoices (spec §5.2–5.5)

CREATE TABLE IF NOT EXISTS products (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  name VARCHAR(120) NOT NULL,
  sku VARCHAR(40),
  sell_by VARCHAR(8) NOT NULL DEFAULT 'weight',
  unit_label VARCHAR(12) NOT NULL DEFAULT 'kg',
  rate NUMERIC(12,2) NOT NULL,
  hs_code VARCHAR(12),
  fbr_uom VARCHAR(32),
  sort_order INT NOT NULL DEFAULT 0,
  is_active BOOLEAN NOT NULL DEFAULT true,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
ALTER TABLE products DROP CONSTRAINT IF EXISTS products_sell_by_check;
ALTER TABLE products ADD CONSTRAINT products_sell_by_check CHECK (sell_by IN ('weight','unit'));
ALTER TABLE products DROP CONSTRAINT IF EXISTS products_hs_code_check;
ALTER TABLE products ADD CONSTRAINT products_hs_code_check CHECK (hs_code IS NULL OR hs_code ~ '^[0-9]{4}\.[0-9]{4}$');
ALTER TABLE products DROP CONSTRAINT IF EXISTS products_rate_check;
ALTER TABLE products ADD CONSTRAINT products_rate_check CHECK (rate >= 0);
CREATE UNIQUE INDEX IF NOT EXISTS uniq_products_sku ON products (sku) WHERE sku IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS uniq_products_name_lower ON products (lower(name));

CREATE TABLE IF NOT EXISTS product_rate_history (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  product_id UUID NOT NULL REFERENCES products(id) ON DELETE CASCADE,
  old_rate NUMERIC(12,2) NOT NULL,
  new_rate NUMERIC(12,2) NOT NULL,
  changed_by UUID REFERENCES users(id) ON DELETE SET NULL,
  changed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  note TEXT
);
CREATE INDEX IF NOT EXISTS idx_rate_history_product ON product_rate_history (product_id, changed_at DESC);

CREATE TABLE IF NOT EXISTS customers (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  name VARCHAR(120) NOT NULL,
  phone VARCHAR(30),
  ntn VARCHAR(20),
  cnic VARCHAR(15),
  buyer_registration_type VARCHAR(12) NOT NULL DEFAULT 'Unregistered',
  address TEXT,
  province VARCHAR(40),
  credit_allowed BOOLEAN NOT NULL DEFAULT false,
  credit_limit NUMERIC(12,2),
  is_active BOOLEAN NOT NULL DEFAULT true,
  notes TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
ALTER TABLE customers DROP CONSTRAINT IF EXISTS customers_buyer_type_check;
ALTER TABLE customers ADD CONSTRAINT customers_buyer_type_check CHECK (buyer_registration_type IN ('Registered','Unregistered'));
CREATE UNIQUE INDEX IF NOT EXISTS uniq_customers_phone_lower ON customers (lower(phone)) WHERE phone IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_customers_name_lower ON customers (lower(name));

CREATE TABLE IF NOT EXISTS business_days (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  business_date DATE NOT NULL UNIQUE,
  status VARCHAR(10) NOT NULL,
  opened_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  opened_by UUID REFERENCES users(id) ON DELETE SET NULL,
  opening_cash NUMERIC(12,2) NOT NULL DEFAULT 0,
  opening_notes TEXT,
  closed_at TIMESTAMPTZ,
  closed_by UUID REFERENCES users(id) ON DELETE SET NULL,
  counted_cash NUMERIC(12,2), counted_card NUMERIC(12,2), counted_online NUMERIC(12,2),
  expected_cash NUMERIC(12,2), expected_card NUMERIC(12,2), expected_online NUMERIC(12,2),
  cash_variance NUMERIC(12,2), card_variance NUMERIC(12,2), online_variance NUMERIC(12,2),
  gross_sales NUMERIC(12,2), discounts NUMERIC(12,2), tax_collected NUMERIC(12,2), net_sales NUMERIC(12,2),
  on_account_sales NUMERIC(12,2), receipts_collected NUMERIC(12,2),
  invoice_count INT, void_count INT,
  closing_notes TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
ALTER TABLE business_days DROP CONSTRAINT IF EXISTS business_days_status_check;
ALTER TABLE business_days ADD CONSTRAINT business_days_status_check CHECK (status IN ('open','closed','reopened'));
CREATE UNIQUE INDEX IF NOT EXISTS uniq_business_days_single_open ON business_days ((true)) WHERE status IN ('open','reopened');

CREATE TABLE IF NOT EXISTS cash_drawer_movements (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  business_day_id UUID NOT NULL REFERENCES business_days(id) ON DELETE CASCADE,
  movement_type VARCHAR(10) NOT NULL,
  amount NUMERIC(12,2) NOT NULL,
  reason VARCHAR(200) NOT NULL,
  notes TEXT,
  created_by UUID REFERENCES users(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
ALTER TABLE cash_drawer_movements DROP CONSTRAINT IF EXISTS cash_drawer_movements_type_check;
ALTER TABLE cash_drawer_movements ADD CONSTRAINT cash_drawer_movements_type_check CHECK (movement_type IN ('paid_in','paid_out'));
ALTER TABLE cash_drawer_movements DROP CONSTRAINT IF EXISTS cash_drawer_movements_amount_check;
ALTER TABLE cash_drawer_movements ADD CONSTRAINT cash_drawer_movements_amount_check CHECK (amount > 0);

CREATE TABLE IF NOT EXISTS day_close_audit_log (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  business_day_id UUID REFERENCES business_days(id) ON DELETE SET NULL,
  business_date DATE NOT NULL,
  action VARCHAR(40) NOT NULL,
  actor_id UUID, actor_name VARCHAR(100), actor_role VARCHAR(20),
  summary TEXT NOT NULL,
  metadata JSONB,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS invoices (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  invoice_number VARCHAR(16) NOT NULL UNIQUE,
  client_op_id UUID UNIQUE,
  business_day_id UUID NOT NULL REFERENCES business_days(id),
  business_date DATE NOT NULL,
  status VARCHAR(10) NOT NULL DEFAULT 'completed',
  cashier_id UUID REFERENCES users(id) ON DELETE SET NULL,
  cashier_name VARCHAR(100) NOT NULL DEFAULT '',
  customer_id UUID REFERENCES customers(id),
  customer_name VARCHAR(120), customer_phone VARCHAR(30), customer_ntn VARCHAR(20), customer_cnic VARCHAR(15),
  subtotal NUMERIC(12,2) NOT NULL,
  discount_amount NUMERIC(12,2) NOT NULL DEFAULT 0,
  discount_percent NUMERIC(5,2),
  tax_rate NUMERIC(6,4) NOT NULL,
  tax_amount NUMERIC(12,2) NOT NULL DEFAULT 0,
  further_tax_amount NUMERIC(12,2) NOT NULL DEFAULT 0,
  total_amount NUMERIC(12,2) NOT NULL,
  rounding_adjustment NUMERIC(4,2) NOT NULL DEFAULT 0,
  total_payable NUMERIC(12,0) NOT NULL,
  payment_method VARCHAR(10) NOT NULL,
  payment_reference VARCHAR(100), payment_sub_method VARCHAR(30),
  notes TEXT,
  fiscal_status VARCHAR(12) NOT NULL DEFAULT 'off',
  fiscal_invoice_number VARCHAR(64),
  fiscal_details JSONB,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  voided_at TIMESTAMPTZ, voided_by UUID REFERENCES users(id) ON DELETE SET NULL, void_reason TEXT
);
ALTER TABLE invoices DROP CONSTRAINT IF EXISTS invoices_status_check;
ALTER TABLE invoices ADD CONSTRAINT invoices_status_check CHECK (status IN ('completed','voided'));
ALTER TABLE invoices DROP CONSTRAINT IF EXISTS invoices_payment_method_check;
ALTER TABLE invoices ADD CONSTRAINT invoices_payment_method_check CHECK (payment_method IN ('cash','card','online','credit'));
ALTER TABLE invoices DROP CONSTRAINT IF EXISTS invoices_fiscal_status_check;
ALTER TABLE invoices ADD CONSTRAINT invoices_fiscal_status_check CHECK (fiscal_status IN ('off','pending','synced','failed'));
ALTER TABLE invoices DROP CONSTRAINT IF EXISTS invoices_credit_needs_customer;
ALTER TABLE invoices ADD CONSTRAINT invoices_credit_needs_customer CHECK (payment_method <> 'credit' OR customer_id IS NOT NULL);
CREATE INDEX IF NOT EXISTS idx_invoices_business_date ON invoices (business_date, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_invoices_customer ON invoices (customer_id, created_at DESC);

CREATE TABLE IF NOT EXISTS invoice_lines (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  invoice_id UUID NOT NULL REFERENCES invoices(id) ON DELETE CASCADE,
  product_id UUID REFERENCES products(id) ON DELETE SET NULL,
  product_name VARCHAR(120) NOT NULL,
  hs_code VARCHAR(12), fbr_uom VARCHAR(32),
  quantity NUMERIC(14,3) NOT NULL,
  entered_as VARCHAR(12),
  gross_weight NUMERIC(14,3), tare_weight NUMERIC(14,3),
  unit_price NUMERIC(12,2) NOT NULL,
  line_total NUMERIC(12,2) NOT NULL,
  line_discount NUMERIC(12,2) NOT NULL DEFAULT 0,
  line_tax NUMERIC(12,2) NOT NULL DEFAULT 0,
  sort_order INT NOT NULL DEFAULT 0
);
ALTER TABLE invoice_lines DROP CONSTRAINT IF EXISTS invoice_lines_quantity_check;
ALTER TABLE invoice_lines ADD CONSTRAINT invoice_lines_quantity_check CHECK (quantity > 0);
ALTER TABLE invoice_lines DROP CONSTRAINT IF EXISTS invoice_lines_entered_as_check;
ALTER TABLE invoice_lines ADD CONSTRAINT invoice_lines_entered_as_check CHECK (entered_as IS NULL OR entered_as IN ('kg','tonne','amount','gross_tare'));
CREATE INDEX IF NOT EXISTS idx_invoice_lines_invoice ON invoice_lines (invoice_id, sort_order);

CREATE TABLE IF NOT EXISTS void_log (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  invoice_id UUID REFERENCES invoices(id) ON DELETE SET NULL,
  invoice_number VARCHAR(16) NOT NULL,
  voided_by UUID REFERENCES users(id) ON DELETE SET NULL,
  authorized_by UUID REFERENCES users(id) ON DELETE SET NULL,
  total_payable NUMERIC(12,0) NOT NULL,
  reason TEXT NOT NULL,
  fiscal_credit_note VARCHAR(64),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS customer_receipts (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  receipt_number VARCHAR(16) NOT NULL UNIQUE,
  customer_id UUID NOT NULL REFERENCES customers(id),
  amount NUMERIC(12,2) NOT NULL,
  method VARCHAR(10) NOT NULL,
  sub_method VARCHAR(30), reference VARCHAR(100),
  business_day_id UUID NOT NULL REFERENCES business_days(id),
  business_date DATE NOT NULL,
  received_by UUID REFERENCES users(id) ON DELETE SET NULL,
  note TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  voided_at TIMESTAMPTZ, voided_by UUID REFERENCES users(id) ON DELETE SET NULL, void_reason TEXT
);
ALTER TABLE customer_receipts DROP CONSTRAINT IF EXISTS customer_receipts_amount_check;
ALTER TABLE customer_receipts ADD CONSTRAINT customer_receipts_amount_check CHECK (amount > 0);
ALTER TABLE customer_receipts DROP CONSTRAINT IF EXISTS customer_receipts_method_check;
ALTER TABLE customer_receipts ADD CONSTRAINT customer_receipts_method_check CHECK (method IN ('cash','card','online'));

CREATE TABLE IF NOT EXISTS customer_ledger_entries (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  customer_id UUID NOT NULL REFERENCES customers(id),
  entry_type VARCHAR(16) NOT NULL,
  invoice_id UUID REFERENCES invoices(id) ON DELETE SET NULL,
  receipt_id UUID REFERENCES customer_receipts(id) ON DELETE SET NULL,
  debit NUMERIC(12,2) NOT NULL DEFAULT 0,
  credit NUMERIC(12,2) NOT NULL DEFAULT 0,
  business_date DATE NOT NULL,
  created_by UUID REFERENCES users(id) ON DELETE SET NULL,
  note TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
ALTER TABLE customer_ledger_entries DROP CONSTRAINT IF EXISTS customer_ledger_entries_type_check;
ALTER TABLE customer_ledger_entries ADD CONSTRAINT customer_ledger_entries_type_check CHECK (entry_type IN ('invoice','invoice_void','receipt','receipt_void','adjustment'));
CREATE INDEX IF NOT EXISTS idx_ledger_customer ON customer_ledger_entries (customer_id, created_at);

CREATE TABLE IF NOT EXISTS invoice_number_counters (
  business_date DATE PRIMARY KEY,
  last_value INT NOT NULL DEFAULT 0
);
ALTER TABLE invoice_number_counters DROP CONSTRAINT IF EXISTS invoice_number_counters_last_value_check;
ALTER TABLE invoice_number_counters ADD CONSTRAINT invoice_number_counters_last_value_check CHECK (last_value >= 0);
CREATE TABLE IF NOT EXISTS receipt_number_counters (
  business_date DATE PRIMARY KEY,
  last_value INT NOT NULL DEFAULT 0
);
ALTER TABLE receipt_number_counters DROP CONSTRAINT IF EXISTS receipt_number_counters_last_value_check;
ALTER TABLE receipt_number_counters ADD CONSTRAINT receipt_number_counters_last_value_check CHECK (last_value >= 0);

-- Append-only tables: any UPDATE or DELETE raises.
CREATE OR REPLACE FUNCTION elevon_append_only() RETURNS trigger AS $$
BEGIN
  RAISE EXCEPTION '% is append-only', TG_TABLE_NAME USING ERRCODE = 'restrict_violation';
END;
$$ LANGUAGE plpgsql;
DROP TRIGGER IF EXISTS void_log_append_only ON void_log;
CREATE TRIGGER void_log_append_only BEFORE UPDATE OR DELETE ON void_log FOR EACH ROW EXECUTE FUNCTION elevon_append_only();
DROP TRIGGER IF EXISTS ledger_append_only ON customer_ledger_entries;
CREATE TRIGGER ledger_append_only BEFORE UPDATE OR DELETE ON customer_ledger_entries FOR EACH ROW EXECUTE FUNCTION elevon_append_only();
DROP TRIGGER IF EXISTS day_close_audit_append_only ON day_close_audit_log;
CREATE TRIGGER day_close_audit_append_only BEFORE UPDATE OR DELETE ON day_close_audit_log FOR EACH ROW EXECUTE FUNCTION elevon_append_only();
```

- [ ] **Step 2: Test** — in `migrate_test.go` add `TestMigrations_AppendOnlyTriggersRaise`: after `testdb.Fresh`, insert a `business_days` row and a `day_close_audit_log` row, then `UPDATE day_close_audit_log SET summary='x'` and `DELETE FROM day_close_audit_log` must both return an error containing `append-only`; same for `void_log` (needs an invoice? no: `invoice_id` is nullable — insert with `invoice_number`, `total_payable`, `reason`) and `customer_ledger_entries` (needs a customer). Also assert `Migrate` runs twice without error (idempotency on the real DB).
- [ ] **Step 3:** `go vet ./... && go test ./...`; commit `backend(database): E-02 — 002_trading migration: products, customers, ledger, business days, invoices, counters, append-only triggers`.

---

### Task D2 (E-02): Products and rates — API + `/rates` screen

**Files:** `backend/internal/models/product.go`, `backend/internal/handlers/products.go` (+`products_db_test.go`), `routes.go`; frontend `types`, `client`, `routes/_app/rates.tsx`, `components/rates/{RatesTable,ProductDialog}.tsx`.

**API (produces):**
- `GET /products?active=1` (staff) → `Product[]` sorted by `sort_order, name`. `Product{id,name,sku,sell_by,unit_label,rate,hs_code,fbr_uom,sort_order,is_active,created_at,updated_at}` (`rate` as JSON number with 2 dp; scan into `string` then `strconv.ParseFloat`, or use `NUMERIC::float8` in SQL — pick one and use it everywhere: **cast in SQL `rate::float8`**).
- `POST /admin/products {name, sku?, rate, hs_code?, fbr_uom?, sort_order?}` → 201; rules: name 1–120 (409 `product_name_taken` on the lower-name index), sku ≤40 (409 `sku_taken`), rate ≥ 0 with ≤2 dp (`invalid_rate`), hs_code blank or `^\d{4}\.\d{4}$` (`invalid_hs_code`), `sell_by` fixed `'weight'`, `unit_label` `'kg'`. Creation writes a `product_rate_history` row `old_rate = 0, new_rate = rate`.
- `PUT /admin/products/:id {name?, sku?, hs_code?, fbr_uom?, sort_order?, is_active?}` (no rate here) → 200 product.
- `PUT /admin/rates {changes:[{product_id, rate}], note?}` → in one transaction, for each change with `rate <> current`: `UPDATE products SET rate, updated_at` + `INSERT product_rate_history(old,new,changed_by,note)`; unchanged rates are skipped; response `{updated: n, products: Product[]}`. `invalid_rate` on any bad value aborts everything.
- `GET /admin/rates/history?product_id?&limit=50` → `[{id, product_id, product_name, old_rate, new_rate, changed_by_name, changed_at, note}]`.

**Tests (DB-backed):** create → list active; duplicate name/sku 409; rates PUT writes history only for changed rows and is atomic (one invalid entry → no row changed); history endpoint returns the change with the actor's name.

**Frontend:** `/rates` (admin): table of active products with an editable "Today's rate" column (RHF field array or controlled inputs), a "Save rates" button that PUTs only changed rows, an "Add product" dialog (name, sku, rate, HS code, sort order), an "Edit" per row (name/sku/hs/active), and a "History" card (last 50 changes: when, who, product, old → new). `Money` helper from `lib/money.ts` for display. Rates cached under `['products']`, invalidated after any change.

Commit `products(rates): E-02 — products CRUD, atomic rate changes with history, /rates screen`.

---

### Task D3 (E-02): Customers — API + `/customers` screen (list, create, edit)

**Files:** `models/customer.go`, `handlers/customers.go` (+ test), `internal/ledger/ledger.go` (+ DB test; used again in D6), `routes.go`; frontend `types`, `client`, `routes/_app/customers.tsx`, `components/customers/{CustomerDialog,CustomerDetail}.tsx`.

**Ledger package (produces, used by D6):**
```go
package ledger
type Entry struct{ ID uuid.UUID; CustomerID uuid.UUID; EntryType string; InvoiceID, ReceiptID *uuid.UUID; Debit, Credit float64; BusinessDate time.Time; CreatedBy *uuid.UUID; Note *string; CreatedAt time.Time }
type Querier interface{ QueryRow(string, ...any) *sql.Row; Query(string, ...any) (*sql.Rows, error); Exec(string, ...any) (sql.Result, error) }
func Post(q Querier, e Entry) (uuid.UUID, error)            // INSERT; caller passes tx
func Balance(q Querier, customerID uuid.UUID) (float64, error) // COALESCE(SUM(debit)-SUM(credit),0)::float8
func Statement(q Querier, customerID uuid.UUID, from, to *time.Time) ([]StatementRow, error) // running balance via SUM() OVER (ORDER BY created_at, id)
```

**API:**
- `GET /customers?search&active&page&per_page` (staff) → paginated `Customer` with `balance` (LEFT JOIN a ledger sum subquery). `Customer{id,name,phone,ntn,cnic,buyer_registration_type,address,province,credit_allowed,credit_limit,is_active,notes,balance,created_at,updated_at}`.
- `GET /customers/:id` (staff) → customer + `balance`.
- `GET /customers/:id/statement?from&to` (staff) → `StatementRow[]` `{id, business_date, entry_type, invoice_number?, receipt_number?, debit, credit, running_balance, note}`.
- `POST /admin/customers`, `PUT /admin/customers/:id` (admin): name 1–120 required; phone ≤30, digits/+/spaces (409 `phone_taken`); ntn ≤20 digits/dash; cnic digits only ≤15 (`invalid_cnic`); buyer_registration_type enum; credit_limit ≥ 0 or null; `credit_allowed` bool. Counter may not write (route gate).

**Frontend:** `/customers`: search + table (name, phone, balance, credit allowed/limit, status), "New customer" and per-row Edit dialogs (all fields, registration type Select, credit switch + limit), row click opens a detail drawer/card with the statement table (running balance) — "Receive payment" button is added in D6. Counter sees the list and detail but no create/edit buttons (`user.role` check) — the API gate is the real guard.

**Tests:** create/list/search; phone conflict; balance is 0 with no ledger rows and reflects posted entries (post two entries directly via `ledger.Post` in a tx); statement running balance order; append-only trigger blocks `UPDATE customer_ledger_entries`.

Commit `customers(core): E-02 — customers CRUD with ledger balance and statement, /customers screen`.

---

### Task D4 (E-03): Pricing — Go `pricing.ComputeTotals`, TS mirror, shared fixture

**Files:** `backend/internal/pricing/{pricing.go,pricing_test.go,testdata/pricing_fixture.json}`; `frontend/src/lib/{pricing.ts,pricing.test.ts,pricing_fixture.json,fixture_parity.test.ts}`; `backend/internal/pricing/fixture_parity_test.go`.

**Interfaces produced:**
```go
package pricing
type LineIn struct{ ProductID uuid.UUID; Quantity float64 /* kg, 3 dp */; Rate float64 /* per kg, 2 dp */ }
type Input struct{ Lines []LineIn; DiscountAmount float64; DiscountPercent *float64; TaxRate float64; FurtherTaxRate float64; BuyerRegistered bool }
type LineOut struct{ Quantity, UnitPrice, LineTotal, LineDiscount, LineTaxable, LineTax float64 }
type Totals struct{ Lines []LineOut; Subtotal, DiscountAmount, TaxRate, TaxAmount, FurtherTaxAmount, TotalAmount, RoundingAdjustment float64; TotalPayable int64 }
func ComputeTotals(in Input) (Totals, error)   // errors: ErrNoLines, ErrInvalidQuantity (≤0 or >4 dp), ErrInvalidRate
func Round2(v float64) float64                  // round-half-up on paisa via int64
func Paisa(v float64) int64                     // moneyPaisa: math.Round(v*100)
func TaxRateFor(tender string, s map[string]json.RawMessage) float64 // cash/card/online/credit; credit null → cash
```
TS `lib/pricing.ts` exports the same shapes (`computeTotals(input): Totals`, `round2`, `paisa`, `taxRateFor(tender, settings)`) with numbers in rupees and `total_payable` an integer.

**Algorithm (all arithmetic in int64 paisa after the first rounding):** per line `lineTotal = Round2(qty × rate)` computed as `Paisa(qty*rate)` with half-up; `subtotal = Σ`; `discount = pct != nil ? Round2(subtotal×pct/100) : min(amount, subtotal)`, both clamped `≥ 0`; pro-rata `lineDiscount_i = floor(discount × lineTotal_i / subtotal)` in paisa for all but the last line, last line = `discount − Σ others` (so Σ is exact; if subtotal is 0, all zero); `taxable_i = lineTotal_i − lineDiscount_i`; `lineTax_i = Round2(taxable_i × taxRate)`; `tax = Σ lineTax_i`; `furtherTax = buyerRegistered ? 0 : Round2(Σ taxable × furtherTaxRate)`; `totalAmount = Σ taxable + tax + furtherTax`; `totalPayable = roundHalfUpRupee(totalAmount)` (paisa ≥ 50 rounds up); `roundingAdjustment = totalPayable − totalAmount` (in −0.50…+0.49).

**Fixture** (`pricing_fixture.json`, array of cases `{name, input, expected}`; write these exact cases and compute expected by hand, then confirm both suites agree):
1. `single_line_cash`: 12.500 kg × 265.00, tax 0.18, no discount → subtotal 3312.50, tax 596.25, total 3908.75, payable 3909, rounding +0.25.
2. `tonne_entry`: 1.000 t = 1000.000 kg × 265.00, tax 0 → 265000.00, payable 265000, rounding 0.
3. `amount_entry_paisa_drift`: qty = round3(5000 ÷ 265) = 18.868 kg × 265.00 → 5000.02 (not 5000.00), tax 0 → payable 5000, rounding −0.02.
4. `percent_discount_pro_rata`: lines 10.000 × 265.00 (2650.00) and 3.333 × 265.00 (883.25); 5 % → discount 176.66; line discounts 132.50 and 44.16; taxable 2517.50 + 839.09; tax 0.18 → 453.15 + 151.04 = 604.19; total 3960.78; payable 3961; rounding +0.22.
5. `amount_discount_capped`: one line 1.000 × 100.00, discount amount 150 → discount 100.00, taxable 0, tax 0, payable 0.
6. `further_tax_unregistered`: 10.000 × 265.00, tax 0.18, further 0.04, unregistered → tax 477.00, further 106.00, total 3233.00, payable 3233.
7. `further_tax_registered_buyer`: same but registered → further 0.
8. `credit_rate_null_falls_back`: tested in Go/TS `TaxRateFor` unit tests, not the fixture.
9. `rounding_half_up`: 0.500 kg × 1.00 = 0.50, tax 0 → payable 1, rounding +0.50 (documents the half-up rule; **owner must confirm**).
10. `rejects_zero_quantity`: expected error `invalid_quantity`.

**Parity test:** Go and TS each load the fixture and assert every case; a third test in each suite hashes the two fixture files (sha256 of bytes, path relative to repo root) and fails if they differ — so a change to one without the other breaks the build.

**Owner check (invariant 8) — put in the handover and the final message, do not wait for it:** rounding is nearest rupee, half-up (Rs 3,908.75 → 3,909; Rs 0.50 → 1); the rounding line is shown on the receipt; FBR later sees paisa-exact totals. Alternatives the owner may prefer: always round down (customer-favourable) or round to nearest 5 rupees.

Commit `backend(pricing): E-03 — ComputeTotals with shared fixture and TS mirror`.

---

### Task D5 (E-03): Business day service and API

**Files:** `backend/internal/dayops/{service.go,service_db_test.go,no_auto_open_contract_test.go}`, `models/dayops.go`, `handlers/dayops.go` (+ test), `routes.go`. Port shape from RETAIL `internal/dayops/service.go` (open / ensure-open / close / reopen / force-close / `writeAuditTx`), dropping staging, shifts, tab release, offline reconciliation and report email.

**Service (produces):**
```go
package dayops
type Day struct{ ID uuid.UUID; BusinessDate time.Time; Status string; OpenedAt time.Time; OpenedBy *uuid.UUID; OpeningCash float64; OpeningNotes *string; ClosedAt *time.Time; /* counted/expected/variance/summary fields as in the table */ }
type Actor struct{ ID uuid.UUID; Name, Role string }
var ErrDayNotOpen, ErrPreviousDayOpen, ErrDayAlreadyOpen, ErrDayNotFound, ErrDayClosed, ErrVarianceNoteRequired, ErrInvalidPin error
func Current(db) (*Day, error)                                  // the open/reopened day or nil
func Open(db, actor, openingCash float64, notes *string) (Day, error)  // today only; ErrDayAlreadyOpen / ErrPreviousDayOpen
func EnsureOpenDayForInvoice(tx *sql.Tx, actor Actor, now time.Time) (Day, error) // §6.7: 4 branches, NO INSERT
func Expected(q, dayID) (Expected, error)   // cash = opening + cash invoices + cash receipts + paid_in − paid_out; card/online = invoices + receipts; excludes voided; on_account = credit invoices
func Close(db, actor, dayID, counted Counted, closingNotes *string, threshold float64) (Day, error) // one step; |variance| > threshold on any tender without notes → ErrVarianceNoteRequired; writes summary columns + audit row 'close'
func Reopen(db, actor, dayID, pin string) (Day, error)          // staffpin.Identify AdminOnly; status 'reopened'; audit 'reopen'
func ForceClose(db, actor, dayID, pin, reason) (Day, error)     // admin PIN; counted = expected; audit 'force_close'
func AddMovement(db, actor, dayID, kind string, amount float64, reason string, notes *string) (Movement, error)
func ZData(q, dayID) (ZReport, error)  // everything the Z-report prints: day, expected/counted/variance per tender, sales summary, movements, invoice count, void count, on-account, receipts
```
`business_date` for "today" is `util.BusinessDate(time.Now())` formatted `2006-01-02`; compare dates by `business_date` column, never by timestamps.

**Contract test `no_auto_open_contract_test.go`:** embed `service.go`, locate the `EnsureOpenDayForInvoice` function body (from its `func` line to the next top-level `func`), and fail if it contains `INSERT INTO business_days`.

**API:**
- `GET /day/current` (staff) → `{day: Day|null, expected: Expected|null, movements: []}`.
- `POST /day/open {opening_cash, notes?}` (staff) → 201 Day; 409 `day_already_open` / `previous_day_open`.
- `POST /day/movements {type: paid_in|paid_out, amount, reason, notes?}` (staff) → 201; 409 `day_not_open`.
- `POST /day/close {counted_cash, counted_card, counted_online, closing_notes?}` (staff) → 200 Day with variances; 400 `variance_note_required`; 409 `day_not_open`. Threshold from `settings.day_close_variance_threshold`.
- `POST /day/reopen {pin}` (admin) → 200; 401 `invalid_pin`. `POST /day/force-close {pin, reason}` (admin).
- `GET /day/:id/z` (staff) → ZReport. `GET /day/history?limit=30` (admin) → closed days.

**Tests (DB-backed):** open → current; second open 409; ensure-open returns the open day; with yesterday open (insert a row dated yesterday) ensure-open → ErrPreviousDayOpen; with today closed → ensure-open reopens and writes an audit row; with no row → ErrDayNotOpen and **no row created**; close computes expected from seeded invoices/receipts/movements (insert rows directly) and rejects an over-threshold variance without notes; reopen needs a valid PIN; audit log rows are append-only.

Commit `backend(dayops): E-03 — business day open/ensure/close/reopen/force-close, movements, Z data, no-auto-open contract`.

---

### Task D6 (E-03): Invoices, receipts, voids — the money core

**Files:** `backend/internal/invoice/{numbers.go,numbers_test.go}`, `models/invoice.go`, `handlers/invoices.go` (+ `invoices_db_test.go`), `handlers/customers.go` (receipts + receipt void added), `routes.go`.

**Numbering (produces):** `invoice.AllocateInvoiceNumber(db, businessDate) (string, error)` — its own short transaction: `INSERT INTO invoice_number_counters (business_date, last_value) VALUES ($1, 1) ON CONFLICT (business_date) DO UPDATE SET last_value = invoice_number_counters.last_value + 1 RETURNING last_value` → `YYYYMMDD-NNN` (`%03d`, grows past 999 naturally). `AllocateReceiptNumber` same with `R-` prefix. Unit test the format; DB test 50 concurrent allocations are unique and dense.

**`POST /invoices` (staff)** body:
```json
{"client_op_id":"uuid","lines":[{"product_id":"uuid","quantity":12.5,"entered_as":"kg|tonne|amount|gross_tare","gross_weight":null,"tare_weight":null}],
 "discount_amount":0,"discount_percent":null,"customer_id":null,"payment_method":"cash|card|online|credit","payment_sub_method":null,"payment_reference":null,"notes":null,"pin":null}
```
Server flow, in order: (1) validate shape (`invalid_request`), quantities > 0 with ≤ 3 dp (`invalid_quantity`), gross/tare present and `gross − tare == quantity` when `entered_as = gross_tare`; (2) idempotency: if `client_op_id` exists → 200 with that invoice; (3) begin tx; (4) `dayops.EnsureOpenDayForInvoice(tx, actor, now)` → 409 `day_not_open` / `previous_day_open`; (5) lock and load products (`SELECT … FOR SHARE`), active only (`product_not_found`); (6) load settings (`settings.Load`) → tax rate via `pricing.TaxRateFor`, further tax, `credit_limit_enforced`; (7) customer: required for credit (`customer_required`), must be active; credit needs `credit_allowed` (`credit_not_allowed`); if enforced and `balance + payable > credit_limit` and no valid admin PIN → 409 `credit_limit_exceeded` (a valid PIN appends `"credit limit overridden by <name>"` to notes); (8) `pricing.ComputeTotals`; (9) allocate the number **outside** the tx (before step 3 is fine — a gap on failure is acceptable and documented); (10) insert invoice (snapshots: cashier_name, customer_name/phone/ntn/cnic, tax_rate, hs_code/fbr_uom per line from the product), lines, and for credit a ledger `invoice` entry with `debit = total_payable`; (11) commit; (12) 201 with the full `Invoice{…, lines: [...]}`. `fiscal_status` = `off`.

**Other routes:** `GET /invoices?from&to&search&customer_id&cashier_id&payment_method&status&page&per_page` (staff; `search` matches invoice_number/customer_name) → paginated summaries; `GET /invoices/recent?limit=10` (staff); `GET /invoices/:id` (staff) → full invoice with lines; `POST /invoices/:id/void {reason, pin}` (staff — the PIN is the authority) → tx: lock invoice, `invoice_already_voided` 409, `staffpin.Identify(tx, pin, AdminOnly)` → `invalid_pin` 401, set status/voided_*; `void_log` row (voided_by = actor, authorized_by = PIN holder); credit → ledger `invoice_void` with `credit = total_payable`; 200 invoice.

**Receipts (customers.go):** `POST /customers/:id/receipts {amount, method, sub_method?, reference?, note?}` (staff) → tx: `EnsureOpenDayForInvoice` (same gate — receipts count in a tender), allocate `R-…`, insert receipt + ledger `receipt` (credit = amount); 201. `POST /customers/:id/receipts/:rid/void {reason, pin}` (staff, admin PIN) → mirror `receipt_void`.

**Tests (DB-backed, the §10 list):** open day → invoice: lines recomputed from `products.rate` even when the client sends other money fields (they are ignored), number allocated, `business_day_id` set; `day_not_open` and `previous_day_open`; duplicate `client_op_id` returns the same invoice; credit invoice writes the ledger and respects the limit (409, then 201 with a valid PIN); receipt lowers the balance; void mirrors the ledger and the trigger blocks `UPDATE void_log`; voided invoices are excluded from `dayops.Expected`; amount-entered line stores `entered_as` and the recomputed total.

Commit `backend(invoices): E-03 — POST /invoices with server-side pricing, numbering, idempotency, credit ledger and limit; receipts; voids`.

---

### Task D7 (E-04): The till — `/pos`

**Files:** frontend `types` (Invoice, InvoiceLine, CreateInvoiceRequest, DayCurrent), `client` (products, day current, invoices create/recent/get/void, customers search), `lib/weight.ts` (+ test), `routes/_app/pos.tsx`, `components/pos/{ProductTiles,WeightPad,CartRail,CustomerPicker,TenderDialog,DayGateBanner,RecentInvoices}.tsx`, `components/shared/{NumericKeypad,PinEntryModal,Money}.tsx` (NumericKeypad and PinEntryModal ported from RETAIL `components/counter/`).

**`lib/weight.ts` (produces, unit-tested):** `kgFromTonne(t) = round3(t*1000)`, `kgFromAmount(amount, rate) = round3(amount / rate)` (rate > 0), `netFromGrossTare(gross, tare) = round3(gross − tare)` (must be > 0), `round3`, `formatKg(n) = n.toFixed(3)`; and `toLineInput(mode, values, rate): {quantity, entered_as, gross_weight?, tare_weight?} | {error}`.

**Screen behaviour (spec §6.2):**
- Layout: left = product tiles (`GET /products?active=1`, `staleTime` 5 min) with name and rate/kg; right rail = cart. Top banner from `DayGateBanner` when `GET /day/current` has no open day (`day_not_open`) or a previous date open — with a button to `/day-close`; the Charge button is disabled while the banner shows.
- Tap a tile → `WeightPad` dialog: mode toggle **kg | tonne | amount | gross − tare**; a big numeric input (uses `NumericKeypad` on touch, keyboard otherwise); live preview "12.500 kg × 265.00 = Rs 3,312.50" computed with `lib/pricing.round2`; amount mode shows both the typed amount and the recomputed line total; gross−tare shows the net; Confirm adds/updates the line. Tapping a cart line reopens the pad with its values.
- Cart rail: lines (`12.500 kg × 265.00`, total), remove, invoice discount (amount or %), customer picker (search `GET /customers?search=`, shows balance and limit; required for credit), live totals from `lib/pricing.computeTotals` using `useSettings()` rates (`taxRateFor(tender, settings)`) — the tender chosen in the dialog drives the preview; totals show subtotal, discount, tax with rate, rounding, **total payable**.
- `TenderDialog`: Cash / Card / Online (sub-method Select: Easypaisa, JazzCash, Bank transfer) / Credit; reference field for card/online; document toggle thermal/A4 (default from settings); "Charge" posts `POST /invoices` with a fresh `client_op_id` (uuid v4 via `crypto.randomUUID()`), disabled while pending; on success: print (Task D8 `printInvoice(invoice, document)`), clear cart, toast "Invoice 20260921-001", focus search. Error mapping: `day_not_open`/`previous_day_open` → banner; `credit_limit_exceeded` → `PinEntryModal` then retry with `pin`; `customer_required`, `credit_not_allowed`, `product_not_found` → inline message.
- `RecentInvoices` panel (`GET /invoices/recent`): number, time, total, tender; Reprint (thermal/A4) and Void (reason + `PinEntryModal` → `POST /invoices/:id/void`).
- Hotkeys: `/` focuses product search, `F2` opens the tender dialog, `Esc` closes dialogs.

**Tests (vitest):** `weight.test.ts` (conversions, gross ≤ tare rejected, amount with rate 0 rejected), `pricing.test.ts` already covers totals; a `cartReducer` if you extract one gets a test (add/update/remove/discount).

Commit `frontend(pos): E-04 — till with WeightPad (kg/tonne/amount/gross−tare), cart rail, customer picker, tender dialog, day gate, recent invoices`.

---

### Task D8 (E-04): Printing — thermal receipt, A4 invoice, Z-report, transport

**Files:** `frontend/src/lib/print/{printTransport,printPageSize,thermalPrintCss,printDebug}.ts` (ported from RETAIL `lib/print/` with `bhookly → elevon` and the existing `lib/print/transport.ts` bridge seam kept as the single entry: `printHtml(html, {pageSize: '80mm'|'58mm'|'a4'})` → `window.elevon?.print` if present else off-screen iframe + `window.print()`), `lib/print/receipt.ts` (+ `receipt.test.ts` snapshot of the HTML for the fixture invoice), `lib/print/invoiceA4.ts`, `lib/print/zReport.ts`, `lib/print/printInvoice.ts` (chooses by document setting), `public/kiosk-print.cmd` (Chrome/Edge launcher with `--kiosk-printing` for silent printing; document in README).

**Thermal receipt (§8.3):** header (logo if set, business name, address, phone, NTN/STRN, header lines), "INVOICE" + number, date/time (Asia/Karachi), cashier, customer (name/phone; NTN/CNIC if set), lines `Product` / `12.500 kg × 265.00` / `3,312.50` (gross/tare shown under the line when present), subtotal, discount, tax with `(18%)`, rounding line (only when ≠ 0), **TOTAL Rs 3,909**, tender (+ sub-method/reference), for credit: "On account — balance now Rs X" (the API returns `customer_balance_after` on a credit invoice; add that field in D6 if missing), "FBR submission pending"/nothing when `fiscal_status = off`, footer lines, **VOID** stamp overlay when status is voided. Width from `receipt_paper_width_mm`, printable from `receipt_printable_area_mm`.

**A4 invoice:** same data as a tax invoice with a buyer block (name, address, NTN/CNIC, registration type) and a totals table.

**Z-report:** from `GET /day/:id/z`: business date, opened/closed by and when, opening cash, sales summary (gross, discounts, tax, net, invoices, voids), tender table (expected / counted / variance for cash, card, online), on-account sales, receipts collected, paid in/out list, closing notes.

Commit `frontend(print): E-04 — thermal receipt, A4 invoice, Z-report and the print transport`.

---

### Task D9 (E-04): Day close screen and invoices list

**Files:** `routes/_app/day-close.tsx`, `components/dayclose/{OpenDayDialog,MovementDialog,CloseDayForm,ZReportView}.tsx` (ported/trimmed from RETAIL `components/admin/dayclose/*`, staging removed), `routes/_app/invoices.tsx`, `components/invoices/{InvoiceTable,InvoiceDrawer}.tsx`.

**Day close:** state from `GET /day/current`: no day → "Open day" card (opening float via `NumericKeypad`, notes) → `POST /day/open`; open → summary tiles (expected cash/card/online so far, on-account, receipts), movements list + "Paid in / Paid out" dialog, and `CloseDayForm` (counted cash/card/online, variance shown live vs expected, notes required when |variance| > threshold — the API enforces, the form mirrors), Close → `POST /day/close` → `ZReportView` with Print (Task D8 `printZReport`); closed day today → "Reopen" (admin PIN) and the Z-report. Admin "Force close" behind the PIN modal.

**Invoices:** paginated table (date, number, customer, cashier, tender, total, status, fiscal off) with filters (date range presets today/yesterday/7 days, search, tender, status); row → drawer with lines and totals, Reprint (thermal/A4), Void (reason + PIN). Counter can view and reprint; void is PIN-gated server-side.

Commit `frontend(dayclose): E-04 — day open/close/reopen with Z-report, invoices browser with reprint and void`.

---

### Task D10 (E-04): Demo data, docs, gate, smoke

- `backend/cmd/seed-demo/main.go` (dev only, refuses when `GIN_MODE=release`): inserts three products (LPG bulk 265.00/kg, LPG 11.8 kg cylinder-fill 268.00/kg, LPG 45 kg cylinder-fill 262.00/kg), three customers (one credit-allowed with a 50,000 limit), idempotent via `ON CONFLICT DO NOTHING` on the unique indexes. `Makefile` target `seed-demo`.
- Spec §13 rows 2–4 → DONE with dates and deviations; handover rewritten for "end of demo path"; CLAUDE.md key-directories rows for `pricing/`, `dayops/`, `invoice/`, `ledger/`; invariant 6 text moves from "(from Phase 3)" to active.
- Full gate; isolation grep; compose smoke: sign in → rates → customers → open day → three sales (cash kg, credit amount-entered, card gross−tare) → reprint → void one → close day → Z-report; record the invoice numbers and totals in the handover so the demo can be repeated.
- **Owner questions to carry into the final message:** rounding rule (D4), tare handling (spec D2), whether counter staff may open/close the day without a PIN (current: yes, per spec §3).

Commit `docs(handover): E-04 — demo path done, seed data, owner questions`.

---

## Self-review notes

- **Spec coverage:** §5.2–5.5 → D1; §5.2 rates history → D2; §5.3/§6.5 → D3, D6; §6.3 → D4; §6.6 → D6; §6.7 → D5; §6.2 → D7; §8.3–8.4 → D8; §3 day-close/invoices rows → D9; §10 backend DB-backed list → D5/D6 tests. Out of scope by design: reports/exports (§6.8, Phase 5), dashboard (Phase 5), FBR (§7), desktop (§8.4 Electron).
- **Condensed on purpose:** unlike the Phase 1 plan, code is specified by interface, algorithm and test list rather than verbatim, to fit the 2026-09-21 demo deadline. Implementers must read the cited spec sections and the RETAIL sources named per task; reviewers hold them to the Global Constraints verbatim.
- **Type consistency:** `pricing.Totals` field names are reused by the invoice DTO and `lib/pricing.ts`; `dayops.EnsureOpenDayForInvoice(tx, actor, now)` is what D6 calls; `ledger.Post` signature is shared by D3 and D6; `printInvoice(invoice, document)` is what D7 and D9 call.
