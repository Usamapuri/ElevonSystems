# Elevon LPG POS — design spec

**Date:** 2026-09-19 (revised same day after owner answers)
**Repo:** `Usamapuri/ElevonSystems` (this repo, empty at the time of writing)
**Source of patterns:** `ArtyReal/POS-System-General` ("Bhookly Retail"), local checkout at `../POS-System-General`
**Status:** design agreed on the points in §2; two residual assumptions (D1 name, D2 tare entry) flagged inline. Next step is the Phase 0 implementation plan.

---

## 1. What we are building and why

A single-store point-of-sale for an LPG supplier in Pakistan. The customer sells gas by weight (kilogrammes, sometimes quoted in tonnes), including filled cylinders, at a rate that changes most weeks. Some customers buy on credit. They need:

1. **One till** where the cashier enters the weight (or the rupee amount), picks cash / card / online / credit, and prints an invoice.
2. **Customers with a ledger** for credit sales, receipts against account, statements and ageing.
3. **Day close** counting cash, card and online, with credit sales shown separately.
4. **A dashboard** with today's numbers and a short trend.
5. **Reports** (sales, weight sold, tax, cashier, receivables, invoice lookup) with CSV/Excel export.
6. **Settings** for the business identity, tax rates, receipt layout, today's rates, users, and FBR Digital Invoicing.
7. **FBR Digital Invoicing** (DI) that switches on the moment it is configured.

Everything else the retail POS carries (inventory engine, purchase orders, marketing, storefront, WhatsApp, online orders, split bills, offline mode) is deliberately absent.

**Why a new repo instead of the retail fork.** The 2026-09-17 feasibility study (`../POS-System-General/docs/superpowers/specs/2026-09-17-sell-by-weight-design.md`) found that selling by weight touches 19 Go sites and 25 files around `order_items.quantity INTEGER`, plus the till, receipts, reports, exports and fiscal payloads. Doing that inside a 3,700-line counter component and a 3,600-line schema-patch file, for a customer who needs perhaps 10% of the product, costs more than a small purpose-built app that borrows the retail POS's proven pieces.

**Relationship to the retail POS.** This app is a *sibling*, not a fork. We copy files selectively (§9) and rewrite the rest. Nothing here may point at a Bhookly or Bhookly Retail deployment, release feed, or Railway project (§12).

---

## 2. Decisions

Confirmed by the owner on 2026-09-19 unless marked *assumed*.

| # | Topic | Decision | Affects |
|---|---|---|---|
| D1 | Branding | *Assumed:* "Elevon POS" by "Elevon Systems"; bridge object `window.elevon`; env prefix `ELEVON_`. Change the three constants in Phase 0 if wrong. | §8, §11 |
| D2 | Cylinders | **Filled cylinders are sold by weight too.** Every product in v1 is `sell_by = 'weight'` in kg. *Assumed:* the weight pad offers a gross − tare entry (cylinder on the scale minus the tare stamped on it) so the net gas weight is what is invoiced; both weights are kept on the line for the customer's dispute. Empty-cylinder deposits/returns are not in v1. | §5.2, §6.2 |
| D3 | Credit sales | **Yes.** Customers table, `credit` tender on the invoice, a per-customer ledger (invoices debit, receipts credit), a Receive Payment screen, statement and ageing. Optional credit limit per customer. | §5.3, §5.4, §6.5, §6.8 |
| D4 | FBR | **FBR Digital Invoicing (DI) is built in and activates when configured.** Payload, reference lookups and HS code are specified in §7 from the FBR technical spec v1.12. PRA e-IMS is out of scope (LPG is goods, federal). | §7 |
| D5 | Desktop | **Browser now, Electron soon.** The frontend ships with the `window.elevon` print bridge from day one; the Electron wrapper is Phase 8 and needs no frontend change. | §8.4, §13 |
| D6 | Roles | **`admin` and `counter`** for now. | §5.1 |
| D7 | Day close | **Yes, for all three tenders (cash, card, online).** Credit sales appear as "on account" (not counted); receipts against account count in the tender they arrived in. Model ported from the retail POS's business days, trimmed. | §5.5, §6.7 |
| D8 | Tonnes | Cashier may *enter* tonnes; the invoice always *prints* kilogrammes. | §6.2, §8.3 |
| D9 | Rounding | **Nearest rupee.** Line math stays at paisa precision (FBR validates per-line tax against the rate, §7.4); the invoice total is rounded to a whole rupee with the difference shown as a "Rounding" line. The rounded figure is what the customer pays and what the ledger carries. | §6.3 |
| D10 | Tax base | Exclusive tax at the configured per-tender rate, as in the retail POS. Tax on credit sales uses the cash rate unless a `tax_rate_credit` is set. | §6.3 |

---

## 3. Product surface (screens)

All screens live under one React app with one sidebar. No module hiding, no storefront, no public routes except `/health`.

| Route | Role | What it does |
|---|---|---|
| `/login` | — | Username or email + password. Same two-panel layout as the retail POS. |
| `/pos` | counter, admin | **The till.** Product tiles (all by weight). Tapping a product opens the weight pad: kg (3 dp) with kg/tonne toggle, "Rs amount → kg", or gross − tare. Cart on the right rail with live tax preview per tender, invoice discount (amount or %), customer picker (optional for cash/card/online, required for credit; shows balance and limit), then Cash / Card / Online / Credit → invoice created, fiscalised if DI is on, printed. Reprint from "Recent invoices". Blocked with a clear banner if no business day is open. |
| `/day-close` | counter, admin | Open day (opening float) · paid-in / paid-out · close day (count cash, card, online; expected vs counted; variance note over threshold) · reopen with admin PIN · Z-report print. |
| `/customers` | admin, counter (read + receive payment) | List with balance; detail with statement, receive payment (cash/card/online with reference), credit limit, NTN/CNIC/address for FBR. |
| `/dashboard` | admin | Today: revenue, kg sold, invoice count, average invoice, tender mix incl. on-account, receivables outstanding; 7-day and 30-day revenue + kg chart; top products; recent invoices. Polling every 30 s. |
| `/invoices` | admin, counter (read) | Searchable, paginated list by date / number / customer / cashier / tender / fiscal status. Detail drawer with reprint (thermal or A4), **Void** (admin PIN; reason required; whole-invoice; becomes an FBR credit note when the invoice was fiscalised), FBR retry. |
| `/reports` | admin | Tabs: Daily sales · Products (kg by product) · Tax summary (by effective rate band) · Cashiers · Hourly · Receivables & ageing · Day closes · Fiscal queue. Date-range picker with presets. CSV + Excel export per tab. |
| `/rates` | admin | Today's rate per product; edit several, save once; history table (who, when, old → new). This is the weekly job. |
| `/settings` | admin | Business (name, address, phone, NTN, STRN, province, day boundary hour) · Tax (per-tender rates, further tax, default HS code) · Receipt (paper width, logo, header/footer lines, default document) · Users (create, deactivate, reset password, set PIN) · Day close (variance threshold) · Credit (limit enforcement) · Fiscal (DI token, sandbox/live, seller profile, scenario, test connection, reference-list refresh). |

---

## 4. Architecture

Same shape as the retail POS, cut down:

```
ElevonSystems/
  backend/                Go 1.24, Gin, database/sql + lib/pq, raw SQL, no ORM
    main.go               boot: env → DB → migrate → ensure admin → workers → router
    migrations/           NNN_name.sql, go:embed, applied in order, each idempotent
    cmd/migrate/          apply migrations without booting the server (CI, local resets)
    internal/
      api/routes.go       ALL route registration; every route inside RequireRoles
      handlers/           auth, users, settings, products, rates, customers, invoices,
                          dayops, reports, dashboard, fiscal
      middleware/         JWT auth, RequireRoles, body limit
      database/           connection (business-tz option), migrate, initial_admin
      pricing/            ComputeTotals — the one money function
      invoice/            number allocator, line math, void, credit-note trigger
      ledger/             customer ledger entries, balances, ageing
      dayops/             business day open / close / reopen, drawer movements, Z data
      fiscal/             FBR DI client, outbound queue, invoice ledger, audit (ported)
      reports/            period summary + report queries + xlsx writer
      util/               business date, money helpers, pg error helpers
      models/             APIResponse + DTOs
      staffpin/           bcrypt PIN identify (ported verbatim)
      testdb/             DB-backed test harness (§10)
  frontend/               React 18 + TS, Vite 5, TanStack Router (file-based) + Query,
                          shadcn/ui (vendored), Tailwind 3, RHF + Zod, recharts, vitest
    src/api/client.ts     the only place that talks HTTP
    src/lib/pricing.ts    mirror of backend pricing (shared fixture test)
    src/lib/print/        printTransport, printPageSize, thermalPrintCss, receipt, A4, Z-report
    src/routes/           login, pos, day-close, customers, dashboard, invoices, reports, rates, settings
    src/components/       pos/ (WeightPad, CartRail, TenderDialog, CustomerPicker), dayclose/, ui/, shared/
  electron/               Phase 8, ported from the retail POS
  docs/superpowers/specs|plans/
  audit/                  dated findings, never chat-only
  .github/workflows/      backend-tests.yml (Postgres service, vet + test), frontend-checks.yml
  docker-compose.dev.yml  postgres + backend (air) + frontend (vite)
  Makefile
  CLAUDE.md               see §14
  .claude/settings.json   skillOverrides hiding the Bhookly tenant skills (§12)
```

**Deployment:** one Railway project per store: Postgres + backend (Dockerfile) + frontend (nginx, Dockerfile). The frontend container proxies `/api` to the backend's private domain, exactly as the retail POS does (its `nginx.conf.template` and `docker-entrypoint.sh` are copied; the SSE locations are dropped).

**Tenancy:** one store per deployment, no tenant id in the schema. A second LPG customer is a second Railway project with its own `JWT_SECRET`.

**Workers:** one fiscal outbound worker (in-process, `ENABLE_FISCAL_WORKER=1`, ported). No other background jobs.

---

## 5. Data model

Money columns are `NUMERIC(12,2)`. Weight is `NUMERIC(14,3)` kg. Timestamps are `TIMESTAMPTZ`. Every pooled DB session opens with `TimeZone=Asia/Karachi` (port `injectBusinessTimezoneOption` from `database/connection.go`; the hourly-report drift it prevents is real).

### 5.1 users
```
id UUID PK, username VARCHAR(50) UNIQUE, email VARCHAR(100) UNIQUE NULL,
password_hash VARCHAR(255), first_name, last_name,
role VARCHAR(20) CHECK (role IN ('admin','counter')),
pin_hash VARCHAR(255) NULL,                -- bcrypt; admin only; voids, reopen day, force close
is_active BOOL DEFAULT true, token_revoked_at TIMESTAMPTZ NULL,
password_reset_token_hash TEXT NULL, password_reset_expires_at TIMESTAMPTZ NULL,
last_login_at, created_at, updated_at
```
First admin: `INITIAL_ADMIN_PASSWORD` (10–72 bytes) + optional `INITIAL_ADMIN_USERNAME`, created only while no active admin exists, never fatal (port `initial_admin.go`). No seeded users, ever.

### 5.2 products, rates
```
products: id UUID PK, name VARCHAR(120), sku VARCHAR(40) NULL UNIQUE,
  sell_by VARCHAR(8) NOT NULL DEFAULT 'weight' CHECK (sell_by IN ('weight','unit')),  -- 'unit' reserved for accessories later
  unit_label VARCHAR(12) NOT NULL DEFAULT 'kg',
  rate NUMERIC(12,2) NOT NULL,              -- current price per kg
  hs_code VARCHAR(12) NULL CHECK (hs_code ~ '^[0-9]{4}\.[0-9]{4}$'),   -- FBR DI format, e.g. 2711.1910; no default
  fbr_uom VARCHAR(32) NULL,                 -- 'KG' from the DI uom list
  sort_order INT DEFAULT 0, is_active BOOL DEFAULT true, created_at, updated_at

product_rate_history: id, product_id FK, old_rate, new_rate, changed_by FK users,
  changed_at TIMESTAMPTZ DEFAULT now(), note TEXT NULL
```
Every rate change, from any screen, writes a history row in the same transaction. No `effective_from` staging in v1.

### 5.3 customers, ledger, receipts
```
customers: id UUID PK, name VARCHAR(120) NOT NULL, phone VARCHAR(30) NULL,
  ntn VARCHAR(20) NULL, cnic VARCHAR(15) NULL,          -- digits only; FBR buyerNTNCNIC
  buyer_registration_type VARCHAR(12) NOT NULL DEFAULT 'Unregistered' CHECK (IN ('Registered','Unregistered')),
  address TEXT NULL, province VARCHAR(40) NULL,          -- FBR province name
  credit_allowed BOOL NOT NULL DEFAULT false, credit_limit NUMERIC(12,2) NULL,
  is_active BOOL DEFAULT true, notes TEXT NULL, created_at, updated_at
  UNIQUE INDEX on lower(phone) WHERE phone IS NOT NULL

customer_receipts: id UUID PK, receipt_number VARCHAR(16) UNIQUE,   -- R-YYYYMMDD-NNN
  customer_id FK NOT NULL, amount NUMERIC(12,2) CHECK (amount > 0),
  method VARCHAR(10) CHECK (IN ('cash','card','online')), sub_method VARCHAR(30) NULL, reference VARCHAR(100) NULL,
  business_day_id FK business_days NOT NULL, business_date DATE NOT NULL,
  received_by FK users, note TEXT NULL, created_at,
  voided_at NULL, voided_by NULL, void_reason NULL

customer_ledger_entries: id UUID PK, customer_id FK NOT NULL,
  entry_type VARCHAR(16) CHECK (IN ('invoice','invoice_void','receipt','receipt_void','adjustment')),
  invoice_id FK NULL, receipt_id FK NULL,
  debit NUMERIC(12,2) NOT NULL DEFAULT 0, credit NUMERIC(12,2) NOT NULL DEFAULT 0,
  business_date DATE NOT NULL, created_by FK users, note TEXT NULL, created_at
  -- append-only trigger; balance = Σ debit − Σ credit, computed, never stored
  INDEX (customer_id, created_at)
```
A credit invoice writes `invoice` (debit = total payable). A receipt writes `receipt` (credit). A void writes the mirror entry rather than deleting. `adjustment` is admin-only with a mandatory note (opening balances, write-offs).

### 5.4 invoices, invoice_lines, void_log
```
invoices: id UUID PK, invoice_number VARCHAR(16) UNIQUE NOT NULL,   -- YYYYMMDD-NNN
  client_op_id UUID UNIQUE NULL,            -- idempotent retry key from the till
  business_day_id FK business_days NOT NULL, business_date DATE NOT NULL,
  status VARCHAR(10) CHECK (status IN ('completed','voided')),
  cashier_id FK users SET NULL, cashier_name VARCHAR(100),
  customer_id FK customers NULL,            -- required when payment_method = 'credit'
  customer_name VARCHAR(120) NULL, customer_phone, customer_ntn, customer_cnic,   -- snapshots
  subtotal, discount_amount, discount_percent NUMERIC(5,2) NULL,
  tax_rate NUMERIC(6,4) NOT NULL,           -- fraction actually applied, snapshot
  tax_amount, further_tax_amount NUMERIC(12,2) NOT NULL DEFAULT 0,
  total_amount,                             -- paisa-exact: taxable + tax + further tax
  rounding_adjustment NUMERIC(4,2) NOT NULL DEFAULT 0,   -- −0.49 … +0.50
  total_payable NUMERIC(12,0) NOT NULL,     -- whole rupees; what is collected / posted to ledger
  payment_method VARCHAR(10) CHECK (IN ('cash','card','online','credit')),
  payment_reference VARCHAR(100) NULL, payment_sub_method VARCHAR(30) NULL,
  notes TEXT NULL,
  fiscal_status VARCHAR(12) NOT NULL DEFAULT 'off' CHECK (IN ('off','pending','synced','failed')),
  fiscal_invoice_number VARCHAR(64) NULL,   -- FBR invoiceNumber, e.g. 7000007DI1747119701593
  fiscal_details JSONB NULL,                -- request/response excerpts, QR payload, credit-note ref
  created_at TIMESTAMPTZ DEFAULT now(),
  voided_at NULL, voided_by FK users NULL, void_reason TEXT NULL

invoice_lines: id, invoice_id FK CASCADE, product_id FK SET NULL,
  product_name VARCHAR(120) NOT NULL, hs_code VARCHAR(12) NULL, fbr_uom VARCHAR(32) NULL,   -- snapshots
  quantity NUMERIC(14,3) NOT NULL CHECK (quantity > 0),                -- net kg
  entered_as VARCHAR(12) NULL CHECK (IN ('kg','tonne','amount','gross_tare')),
  gross_weight NUMERIC(14,3) NULL, tare_weight NUMERIC(14,3) NULL,    -- only for gross_tare
  unit_price NUMERIC(12,2) NOT NULL,        -- rate snapshot
  line_total NUMERIC(12,2) NOT NULL,        -- round2(quantity × unit_price), server-computed
  line_discount NUMERIC(12,2) NOT NULL DEFAULT 0,   -- pro-rata share of the invoice discount (for FBR)
  line_tax NUMERIC(12,2) NOT NULL DEFAULT 0,        -- round2((line_total − line_discount) × tax_rate)
  sort_order INT

void_log: id, invoice_id FK SET NULL, invoice_number, voided_by, authorized_by,
  total_payable, reason TEXT NOT NULL, fiscal_credit_note VARCHAR(64) NULL, created_at
  -- append-only: BEFORE UPDATE OR DELETE trigger raises (port from schema_patches.go)

invoice_number_counters: business_date DATE PK, last_value INT NOT NULL CHECK (last_value >= 0)
receipt_number_counters: same shape
```
There is no `payments` table: cash/card/online invoices are settled on creation; credit invoices are settled through `customer_receipts`. Voids do not create negative rows; reports exclude `status = 'voided'`.

### 5.5 business days (day close)
```
business_days: id UUID PK, business_date DATE UNIQUE NOT NULL,
  status VARCHAR(10) CHECK (IN ('open','closed','reopened')),
  opened_at, opened_by FK users, opening_cash NUMERIC(12,2) NOT NULL DEFAULT 0, opening_notes,
  closed_at NULL, closed_by NULL,
  counted_cash, counted_card, counted_online NUMERIC(12,2) NULL,
  expected_cash, expected_card, expected_online NUMERIC(12,2) NULL,
  cash_variance, card_variance, online_variance NUMERIC(12,2) NULL,
  gross_sales, discounts, tax_collected, net_sales, on_account_sales, receipts_collected, invoice_count, void_count,
  closing_notes TEXT NULL, created_at, updated_at
  UNIQUE INDEX uniq_single_open ON ((true)) WHERE status IN ('open','reopened')

cash_drawer_movements: id, business_day_id FK CASCADE, movement_type CHECK (IN ('paid_in','paid_out')),
  amount NUMERIC(12,2) CHECK (> 0), reason VARCHAR(200) NOT NULL, notes, created_by, created_at

day_close_audit_log: id, business_day_id FK SET NULL, business_date, action VARCHAR(40),
  actor_id, actor_name, actor_role, summary TEXT NOT NULL, metadata JSONB, created_at
  -- append-only trigger
```
`staged` is dropped: close is one step (count → confirm), because one till and one owner do not need a two-person handoff.

### 5.6 fiscal (ported tables)
`fiscal_outbound_jobs` (queue with attempts, backoff, dead letter), `fiscal_invoices` (sequence ledger: FBR invoice number ↔ invoice/credit note), `fiscal_audit_events` (append-only request/response excerpts). DDL copied from the retail POS migrations 013 and 031 with `order_id → invoice_id`.

### 5.7 settings
```
settings: key VARCHAR(100) PK, value JSONB NOT NULL, updated_at
```
Keys: `business_name, business_address, business_phone, business_ntn, business_strn, business_province, day_boundary_hour (0), tax_rate_cash, tax_rate_card, tax_rate_online, tax_rate_credit (null → cash rate), further_tax_rate (0), default_hs_code (''), receipt_paper_width_mm (80), receipt_printable_area_mm (72), receipt_logo_url, receipt_header_lines[], receipt_footer_lines[], receipt_default_document ('thermal' | 'a4'), day_close_variance_threshold (100), credit_limit_enforced (false), fiscal_config (§7.2)`.

Tax rates ship as `0` until the owner enters them from the tax advisor's figures. The retail POS invariant "no silent price override" is kept verbatim: only the `tax_rate_*` values feed rate selection.

### 5.8 Migration strategy (simpler than the retail POS)

- `backend/migrations/NNN_name.sql`, `go:embed`-ed, applied in filename order at boot inside one transaction each, recorded in `schema_migrations(version, applied_at)`.
- Every file is **also idempotent** (`IF NOT EXISTS`, `DROP CONSTRAINT IF EXISTS` before re-add, `ON CONFLICT DO NOTHING`).
- Migration failure **is fatal** at boot. With one store per deploy and a health check, a loud failure beats a silently missing column.
- `cmd/migrate` applies them without booting the server.
- One contract test: every `CREATE TABLE` / `ALTER TABLE … ADD` in `migrations/` uses the idempotent form.

---

## 6. Behaviour

### 6.1 Auth
Port `middleware/auth.go` (HS256 JWT, 24 h, `Issuer`, `IssuedAt`, `X-POS-JWT` fallback header, `CheckTokenNotRevoked` fails closed), `handlers/auth.go` login / forgot / reset / change-password with the in-process `windowRateLimiter` (10 failures per 15 min per identifier+IP), and `staffpin/pin.go` (bcrypt PIN identify iterating every candidate, no short-circuit). `JWT_SECRET` is mandatory in `GIN_MODE=release`.

### 6.2 The till
- Product tiles from `GET /products?active=1`, cached 5 min.
- **Weight pad** modes, all storing net kg to 3 dp with `entered_as`:
  - **kg**: direct entry.
  - **tonne**: entry × 1000.
  - **amount**: cashier types Rs 5,000 → `qty = round3(amount ÷ rate)`; the line total is recomputed from qty and may differ from the typed amount by a paisa; the pad shows both before confirm.
  - **gross − tare** (cylinders): two fields, net = gross − tare, must be > 0; both weights stored on the line.
  - Existing lines are editable by tapping.
- One discount per invoice (amount or percent). No per-line discount entry; the invoice discount is allocated pro-rata to lines server-side for FBR (§7.4).
- **Customer picker**: optional for cash/card/online (name on invoice, FBR buyer fields), **required for credit**. Shows balance and, if `credit_limit_enforced`, blocks when `balance + total_payable > credit_limit` with error `credit_limit_exceeded` (admin PIN override).
- Tender: Cash / Card / Online (sub-methods: Easypaisa, JazzCash, bank transfer) / Credit. Card and online may take a reference.
- **Create + settle in one POST** `/invoices`. The server recomputes every line from `products.rate` and the submitted quantities, never trusting client money. Response carries the full invoice for printing, including `fiscal_status`.
- Day gate: creating an invoice requires an open business day for today (§6.7). Errors `day_not_open` (409) and `previous_day_open` (409) are shown as banners with a button to the day-close screen.
- Idempotency: `client_op_id` UUID; a repeat POST returns the existing invoice.
- After success: print (thermal or A4 per setting, with a toggle in the tender dialog), clear cart, focus search. Hotkeys: `/` search, `F2` charge, `Esc` close dialog.

### 6.3 Pricing (the one money function)
```
line_total_i    = round2(qty_i × rate_i)                          -- qty to 3 dp first
subtotal        = Σ line_total_i
discount        = percent ? round2(subtotal × pct/100) : min(amount, subtotal)
line_discount_i = largest-remainder (Hamilton) share of discount in paisa, each line capped at its own line_total
line_taxable_i  = line_total_i − line_discount_i
line_tax_i      = round2(line_taxable_i × tax_rate(tender))
tax             = Σ line_tax_i
further_tax     = round2(Σ line_taxable_i × further_tax_rate)      -- 0 unless configured (§7.5)
total_amount    = Σ line_taxable_i + tax + further_tax             -- paisa-exact, what FBR sees
total_payable   = round_half_up_to_rupee(total_amount)
rounding_adj    = total_payable − total_amount
```
Tax is computed **per line and summed**, not on the invoice subtotal, because FBR validates each line's `salesTaxApplicable` against its `rate` (error 0104). `round2` is round-half-up on the paisa; comparisons run on `int64` paisa (port `moneyPaisa`). Implemented once in Go (`pricing.ComputeTotals`) and once in TS (`lib/pricing.ts`) for the live preview, with a shared JSON fixture both suites load. Any change here is customer-visible money: compute before/after examples and confirm with the owner before merging.

### 6.4 Voids
Whole-invoice only. `POST /invoices/:id/void` with `{reason, pin}`. The PIN identifies an active admin, the invoice flips to `voided`, a `void_log` row is written, and:
- credit invoice → a `invoice_void` ledger entry (credit = total payable) in the same transaction;
- fiscalised invoice (`fiscal_status = synced`) → an FBR **credit note** job is enqueued referencing the FBR invoice number (§7.6); the void is recorded immediately, the credit note follows through the queue; until it succeeds the invoice shows "void, credit note pending".
Voided invoices stay visible, stamped VOID on reprint, excluded from every total.

### 6.5 Customers and receipts
- `POST /customers/:id/receipts {amount, method, sub_method?, reference?, note?}` requires an open business day, allocates `R-YYYYMMDD-NNN`, writes the receipt and a `receipt` ledger entry in one transaction. Receipts are against the account, not against a specific invoice (no allocation UI in v1; ageing is FIFO by invoice date).
- Receipt void: admin PIN, mirror ledger entry, `receipt_void`.
- Statement: entries in date order with running balance computed in SQL (`SUM() OVER`).
- Ageing: outstanding = balance; buckets 0–30 / 31–60 / 61–90 / 90+ by allocating receipts FIFO against credit invoices in the query.

### 6.6 Invoice numbering
`YYYYMMDD-NNN` keyed to **business date**, allocated with the retail POS's single atomic upsert on `invoice_number_counters`, in its own short transaction before the invoice transaction. Numbering day and reporting day are the same clock. The format satisfies FBR error rule 0173 (alphanumerics with a dash in between). Receipts use `R-YYYYMMDD-NNN` from their own counter.

### 6.7 Business day and day close
Ported from the retail POS's `dayops`, keeping the five-branch decision in `EnsureOpenDayForInvoice`:
1. today's day is open/reopened → use it;
2. a different date is open → `previous_day_open` (409), blocking until it is closed;
3. today is closed → reopen for late sale (status `reopened`, keeps counts, audit row);
4. no row → `day_not_open` (409). **No auto-open**: the retail POS learnt that auto-opening invents an opening float.

Open day: `opening_cash` + notes. Close day (one step): counted cash/card/online are all required (enter 0); expected cash = `opening_cash + cash invoices + cash receipts + paid_in − paid_out`; expected card/online = card/online invoices + receipts; variance = counted − expected; any |variance| > threshold requires `closing_notes`. On-account sales are reported on the Z-report as a separate line and never enter a tender expectation. Reopen and force-close need an admin PIN and write `day_close_audit_log`. `business_date = date(now in Asia/Karachi − day_boundary_hour)`, boundary default 0, configurable.

### 6.8 Reports
All under `/reports/*`, admin only, `from`/`to` as ISO dates on the wire, `DD-MM-YYYY` labels in the response. One `LoadPeriodSummary(ctx, db, range)` feeds the dashboard KPIs, the overview, the Z-report and the Excel "period pack" so they always agree.

| Report | Output |
|---|---|
| daily | per business_date: invoices, kg, gross, discount, taxable, tax, rounding, net; tender split incl. on-account; receipts collected |
| products | per product: kg, invoices, gross, share % |
| tax | by effective rate band: taxable, tax, further tax; totals reconcile to Σ `tax_amount` by construction; fiscal column: synced / pending / failed counts |
| cashiers | per cashier: invoices, gross, average, voids |
| hourly | hour × weekday heatmap (business tz); export stays hour-of-day columns |
| receivables | per customer: balance, ageing buckets, last receipt; statement drill-down |
| day closes | list of closed days with variances; Z-report reprint |
| fiscal queue | pending / failed jobs with retry |
| invoices | paginated browser + search |

Exports: CSV (`encoding/csv`) and Excel (port `writeWorkbook` + `sheetSpec` + `neutralizeXLSXCell`, keeping the formula-injection guard).

### 6.9 API conventions
`models.APIResponse{success, message, data?, error?}` on every response; `error` is a stable snake_case code (`invalid_credentials`, `insufficient_permissions`, `product_not_found`, `invalid_quantity`, `invoice_not_found`, `invoice_already_voided`, `invalid_pin`, `day_not_open`, `previous_day_open`, `customer_required`, `credit_not_allowed`, `credit_limit_exceeded`, `variance_note_required`, `fiscal_not_configured`, `rate_limited`, …). Never pass `err.Error()` to clients. Pagination via `?page&per_page` with `meta{total, total_pages}`.

---

## 7. FBR Digital Invoicing

Source: PRAL, *Technical Specification for DI API* v1.12 (30 Jun 2025), `https://download1.fbr.gov.pk/Docs/20257301172130815TechnicalDocumentationforDIAPIV1.12.pdf`, plus the retail POS's working `fiscal/fbr.go`. Scenario tables and error catalogue are from the same PDF.

### 7.1 Endpoints and auth
| Purpose | Sandbox | Production |
|---|---|---|
| Validate | `https://gw.fbr.gov.pk/di_data/v1/di/validateinvoicedata_sb` | `…/di/validateinvoicedata` |
| Post | `https://gw.fbr.gov.pk/di_data/v1/di/postinvoicedata_sb` | `…/di/postinvoicedata` |
| Reference | `https://gw.fbr.gov.pk/pdi/v1/{provinces,doctypecode,itemdesccode,sroitemcode,transtypecode,uom}` · `…/pdi/v2/SaleTypeToRate?date=DD-MMM-YYYY&transTypeId=&originationSupplier=` · `…/pdi/v2/HS_UOM?hs_code=&annexure_id=` · `…/pdi/v2/SROItem` · `…/pdi/v1/SroSchedule` | same |

Header `Authorization: Bearer <token>`. The token is generated by the taxpayer in IRIS, separately for sandbox and production. Flow is **validate → post**; keep the retail POS's rule that the sandbox validate URL is the `_sb` one (its contract test F-FBR-01 exists because a mismatch once hit production cold). Keep its sandbox-in-release refusal too: a live deployment configured with sandbox URLs refuses to fiscalise instead of filing test invoices.

### 7.2 `fiscal_config` (settings JSONB, token AES-GCM encrypted with `FISCAL_SECRETS_KEY`)
`enabled, is_sandbox, api_key_enc, seller_ntn_cnic, seller_business_name, seller_province, seller_address, scenario_id (sandbox only), rate_desc ("18%"), sale_type ("Goods at Standard Rate (default)"), trans_type_id, default_hs_code, default_uom ("KG"), buyer_registration_default ("Unregistered"), validate_url?, post_url?`. Admin API returns `api_key_set` / masked, never the token. "Test connection" runs `validateinvoicedata` with a one-line sample. "Refresh reference lists" pulls provinces, uom, SaleTypeToRate and HS_UOM for the default HS code and caches them in settings so the dropdowns work offline.

### 7.3 Product classification
- **HS code:** Pakistan Customs Tariff heading 27.11, sub-heading **2711.1910 "L.P.G."** (TIPP, Ministry of Commerce). This is the default for all gas products; the owner's tax advisor confirms it and any product-level exception before `enabled` is switched on. The DI API takes it as the string `"2711.1910"`.
- **UoM:** `"KG"` (`uoM_ID 13` in `/pdi/v1/uom`). Setup calls `HS_UOM?hs_code=2711.1910&annexure_id=3` and refuses to enable if KG is not in the returned list (FBR error 0099/0165 otherwise).
- **Quantity is decimal** (`"quantity": 1.0000` in the spec's own sample, type "Number (Decimal)"), so `12.500` kg is sent as-is. This removes the biggest unknown from the 2026-09-17 study.
- **Rate string** must equal a `ratE_DESC` from `SaleTypeToRate` (for example `"18%"`); it is configured, never derived from the numeric tax rate, and the numeric `tax_rate_*` used for pricing must agree with it (a settings-save check).
- **Scenario** (`scenarioId`, sandbox only): which scenarios FBR assigns depends on the taxpayer's registered business activity and sector. For an LPG distributor/wholesaler/retailer the standard-rate pair applies: **SN001** (registered buyer) and **SN002** (unregistered buyer); if their profile says "retailer" FBR also expects **SN026** (sale to end consumer). The app picks SN001 vs SN002 from the customer's `buyer_registration_type` in sandbox and omits `scenarioId` in production. "Gas Distribution → SN014" in the FBR table is gas to CNG stations, not LPG retail; do not use it.

### 7.4 Payload mapping (one DI invoice per POS invoice)
```
invoiceType            "Sale Invoice"
invoiceDate            business_date as YYYY-MM-DD
sellerNTNCNIC/…Name/…Province/…Address   from fiscal_config
buyerNTNCNIC           customer.ntn or cnic (digits), "" for walk-in
buyerBusinessName      customer.name or "Walk-in Customer"
buyerProvince          customer.province or seller province
buyerAddress           customer.address or "N/A"
buyerRegistrationType  customer.buyer_registration_type or default
invoiceRefNo           ""   (sale)  |  original FBR invoiceNumber (credit note)
scenarioId             sandbox only
items[] per invoice_line:
  hsCode                          line.hs_code
  productDescription              product_name + " " + quantity + " kg"
  rate                            fiscal_config.rate_desc
  uoM                             line.fbr_uom ("KG")
  quantity                        line.quantity (3 dp)
  valueSalesExcludingST           line_total − line_discount
  salesTaxApplicable              line_tax
  totalValues                     valueSalesExcludingST + salesTaxApplicable + furtherTax share
  discount                        line_discount
  furtherTax                      line share of further_tax (0 unless configured)
  fixedNotifiedValueOrRetailPrice 0, salesTaxWithheldAtSource 0, extraTax "", fedPayable 0,
  sroScheduleNo "", sroItemSerialNo "", saleType fiscal_config.sale_type
```
The rounding adjustment is **not** sent: it is not a supply. FBR's totals are paisa-exact and the receipt shows both figures.

**Response:** `invoiceNumber` (e.g. `7000007DI1747119701593`), `validationResponse.statusCode` `"00"` valid / `"01"` invalid, `invoiceStatuses[].invoiceNo` per line (`…-1`), `errorCode` + `error`. Store `invoiceNumber` as `fiscal_invoice_number`, ledger it in `fiscal_invoices`, keep excerpts in `fiscal_audit_events`. QR payload = the FBR invoice number (port `qr_display.go`).

### 7.5 Further tax
Registered sellers charging standard-rate goods to **unregistered** buyers may owe *further tax* (section 3(1A), currently 4%) except on supplies to end consumers. Whether this LPG supplier's sales attract it is the advisor's call; the app models it as `further_tax_rate` (default 0) applied only when the buyer is unregistered, carried per line into `furtherTax`. Leave at 0 unless told otherwise.

### 7.6 Queue, retry, credit notes
Port `sync.go`'s shape: inline attempt with a short deadline at invoice creation, then `fiscal_outbound_jobs` with backoff and a dead letter; before any retry consult `fiscal_invoices` by invoice id so a timed-out POST that actually succeeded is recovered, not resubmitted. Void of a synced invoice enqueues a credit note: `invoiceType: "Credit Note"`, `invoiceRefNo: <original FBR number>`, same lines. The v1.12 doc-type list shows only *Sale Invoice* and *Debit Note*, while its error catalogue (0026, 0064, 0068) clearly handles credit notes and the retail POS files them as `"Credit Note"` in production; **verify in sandbox during Phase 7 before relying on it**, and fall back to the retail POS's exact strings.

### 7.7 Receipt
When `fiscal_status = synced`: FBR DI logo, "FBR Invoice No." + number, QR (port `integratedTaxReceiptSection`, `fbrBranding.ts`). When pending: "FBR submission pending" line; reprint refreshes.

---

## 8. Frontend

### 8.1 Stack and conventions (copied from the retail POS)
`react 18.3`, `@tanstack/react-router` (file-based, generated `routeTree.gen.ts`, never hand-edited), `@tanstack/react-query 5`, `react-hook-form` + `zod`, hand-vendored shadcn `components/ui/*`, `tailwindcss 3.4` with the same HSL token scheme and dark-mode elevation ramp, `lucide-react`, `recharts`, `date-fns`, `vite 5` with `@vitejs/plugin-react-swc`, `vitest 2` in node environment (logic tests only), `tsc --noEmit` strict with `noUnusedLocals`. `api/client.ts` is the single HTTP entry: axios, `Authorization` + `X-POS-JWT`, 401 → `/login` except `missing_auth_header`, `ApiClientError{code, status}`.

### 8.2 Copied nearly verbatim
`lib/utils.ts`, `lib/formatMoney.ts`, `contexts/ThemeContext.tsx`, `hooks/useMediaQuery.ts`, `components/ui/*` (33 files), `components/counter/NumericKeypad.tsx` (one copy), `components/counter/PinEntryModal.tsx`, `components/admin/reports/{DateRangeFilter,MetricTile,ExportButton}.tsx` + `hooks/useReportRange.ts`, `components/admin/ReceiptPreview.tsx`, `components/admin/dayclose/*` (open-day dialog, tender count form, Z-report print) trimmed of staging, `routes/login.tsx` layout, sidebar + user menu shell, `lib/print/{printTransport,printPageSize,thermalPrintCss,printDebug}.ts` and the off-screen-iframe dispatcher, `lib/fbrBranding.ts` + `public/fbr-pos-invoicing-logo.png`.

### 8.3 Written fresh
- `components/pos/WeightPad.tsx`: kg / tonne / amount / gross−tare, live line total, confirm.
- `components/pos/CartRail.tsx`, `TenderDialog.tsx`, `CustomerPicker.tsx`: about 800 lines total, built from the retail `rail/*` pieces.
- `lib/print/receipt.ts` (80 mm thermal: header, invoice no., date, cashier, customer, lines as `12.500 kg × 265.00`, subtotal, discount, tax with rate, rounding, total payable, tender, on-account balance line for credit sales, FBR block, footer, VOID stamp), `lib/print/invoiceA4.ts` (A4 tax invoice with buyer NTN/CNIC block), `lib/print/zReport.ts`, `lib/print/statement.ts`.
- `lib/pricing.ts`: mirror of §6.3 with the shared fixture test.
- Dashboard, reports tabs, customers, rates, settings: plain React Query + shadcn tables.

### 8.4 Printing
Browser: `window.print()` from an off-screen iframe with the exact `@page` size locked. Silent printing needs Chrome/Edge started with `--kiosk-printing` (ship the retail POS's launcher script). The frontend feature-detects `window.elevon` from Phase 4 so that Phase 8's Electron wrapper (which prints via `printToPDF` → `pdf-to-printer`, the only reliable Windows path found) needs no frontend change.

---

## 9. Files to copy from `../POS-System-General` (rename `bhookly → elevon`)

**Backend, verbatim or near:** `internal/middleware/{auth.go,body_limit.go}`, `internal/staffpin/pin.go`, `internal/util/{timewindow.go,pg_errors.go}`, `internal/database/{connection.go,initial_admin.go}`, `internal/pricing/pricing.go` (reshaped to §6.3), `internal/config/dayops.go`, `internal/dayops/service.go` (the open / ensure-open / close / reopen / force-close paths and `writeAuditTx`, about 600 of 2,000 lines; drop staging, shifts, tab release, offline reconciliation, report email), `handlers/auth.go` (+ `windowRateLimiter`), `handlers/inventory_reports_export.go:126-222` (`sheetSpec`, `writeWorkbook`, `neutralizeXLSXCell`), `handlers/reports_tax.go` (rate-band query shape), `handlers/payments.go` `moneyPaisa`, `fiscal/{types,config,crypto,factory,fbr,queue,invoice_ledger,audit,qr_display}.go` + the `sync.go` inline-then-queue path (drop PRAL POS, PRA, local bridge, mock, DI SN002-only preset), `models.APIResponse`, `main.go` skeleton, `Dockerfile`, `Dockerfile.dev` + `.air.toml`, `railway.json`; `cmd/schemapatch` becomes `cmd/migrate`.
**Frontend:** everything in §8.2, `Dockerfile`, `nginx.conf.template` (minus SSE), `docker-entrypoint.sh`, `railway.json`, `vite.config.ts` (drop PWA), `tailwind.config.js`, `tsconfig*.json`, `index.css` token blocks, `index.html` pre-paint scripts.
**Electron (Phase 8):** the whole `electron/` tree with new `appId`, GUID, product name, releases repo, `window.elevon`.
**Repo:** `.github/workflows/backend-tests.yml` (run `cmd/migrate` instead of `psql -f`), `docker-compose.dev.yml`, `Makefile` (dev/up/down/logs/db-shell/test), `.gitignore`, `.claude/settings.json` `skillOverrides`, `.claude/skills/verify` as a template.
**Not copied:** `schema_patches.go` and its mirrors, `CounterInterface.tsx`, `printCustomerReceipt.ts`, `finance/summary.go` (rewrite ~200 lines), PRAL POS / PRA fiscal files, everything restaurant/retail-only.

---

## 10. Testing strategy

- **Backend unit:** pricing (table-driven + shared JSON fixture, including the rounding line and pro-rata discount), business date, number allocator format, xlsx cell neutraliser, DI payload builder (golden JSON for a cash walk-in, a credit registered buyer, and a credit note).
- **Backend DB-backed (new; the retail POS lacks this):** `internal/testdb` connects to `TEST_DATABASE_URL`, runs migrations once, truncates between tests. Covers: open day → invoice (lines recomputed, number allocated, business date at a boundary), `day_not_open` and `previous_day_open`, duplicate `client_op_id`, credit invoice writes the ledger and respects the limit, receipt lowers the balance, void mirrors the ledger and the trigger blocks an UPDATE on `void_log`, close day computes expected cash from invoices + receipts + movements, rate change writes history, tax report reconciles to Σ tax, fiscal job recovery consults `fiscal_invoices` before resubmitting. Skips loudly when no DSN; CI greps for `--- SKIP`.
- **Contract tests (source-text):** every route inside `RequireRoles`; `EnsureInitialAdmin` after migrations; `SetTrustedProxies(nil)`; no `err.Error()` in a JSON response; migrations idempotent; sandbox validate URL is `_sb`; `EnsureOpenDayForInvoice` has no auto-open branch.
- **Frontend:** vitest node-only: pricing mirror against the fixture, WeightPad conversions (kg/tonne/amount/gross−tare), receipt builder snapshot, invoice number/date formatting.
- **Gate for every commit:** `cd backend && go vet ./... && go test ./...`; `cd frontend && npm run type-check && npm run test`.

---

## 11. Deployment and environment

Railway project per store: `Postgres`, `backend` (root `/backend`, Dockerfile, healthcheck `/health`), `frontend` (root `/frontend`, Dockerfile, healthcheck `/health`, `BACKEND_URL=http://${{backend.RAILWAY_PRIVATE_DOMAIN}}:8080`). Deploy from `main`. Backend env:

```
DATABASE_URL            required
JWT_SECRET              required in release; unique per store
GIN_MODE                release
PORT                    8080
CORS_ORIGINS            the frontend's public URL
INITIAL_ADMIN_PASSWORD  set for first boot only, then remove
INITIAL_ADMIN_USERNAME  optional, default admin
BUSINESS_TIMEZONE       Asia/Karachi (default)
FISCAL_SECRETS_KEY      32-byte key for the DI token at rest (required once fiscal is enabled)
ENABLE_FISCAL_WORKER    1 to run the outbound worker in-process
APP_URL                 the frontend's public URL; used to build password-reset links (default http://localhost:3000)
RESEND_API_KEY, EMAIL_FROM   only if password-reset email is wanted; unset → the reset link is written to the backend log
```

Local dev: `docker compose -f docker-compose.dev.yml up` → Postgres 5432, backend 8080 with air, Vite 3000.

---

## 12. Isolation and branching rules (carried over from the retail POS)

- This repo must never reference a Bhookly or Bhookly Retail tenant, Railway project, release repo (`bhookly-pos-releases`, `bhookly-retail-releases`), appId (`com.bhookly.*`), or `*.bhookly.com` host. The user-scoped Claude skills `tenant-deploy, tenant-cleanup, railway-tenant, tenant-drift, pral-replay, tax-reconciliation, invoice-sequence-check, pre-deploy-check, review, desktop-dev` embed those; `.claude/settings.json` hides them here exactly as the retail repo does.
- Railway: this store gets its own project. Every mutating Railway action names the explicit project/environment ID and is confirmed first.
- Branching: `dev` is the working branch, `main` deploys. Merge `dev → main` only when told. No push to `main` without explicit authorisation.
- Commits: `<scope>(<subsystem>): E-NN — <imperative>` (NN = phase/commit from the plan); `Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>`; never `--no-verify`.
- Long-form findings go to `audit/<TOPIC>_<YYYY-MM-DD>.md` or `docs/`, never chat-only.
- Customer-visible money changes (rates, rounding, tax base, further tax) are stop-and-ask with before/after examples.

---

## 13. Phases

Each phase ends green (§10 gate) and is one PR from `dev`.

| Phase | Deliverable | Notes |
|---|---|---|
| **0 Scaffold** | Repo layout, CLAUDE.md, `.claude/settings.json`, CI, docker-compose, Go server with `/health` + migration runner + first migration (users, settings), React shell with login page, sidebar, theme, API client, `window.elevon` bridge stub | **DONE 2026-09-20** on `dev` (commits `212a442`…`e48cf92`, plan `docs/superpowers/plans/2026-09-19-phase-0-scaffold.md`). Login/JWT/`/auth/me` were pulled forward from Phase 1 so sign-in works end to end. Deviation: route guards use `beforeLoad` + `redirect`, not `<Navigate>` in render, because the installed TanStack Router (1.170) loops on the latter. |
| **1 Auth + users + settings** | Login/JWT/roles, initial admin from env, users CRUD, PIN set, settings API + Business/Tax/Receipt/Day-close/Credit sections | **DONE 2026-09-20** on `dev` (plan `docs/superpowers/plans/2026-09-20-phase-1-auth-users-settings.md`). Deviations: users are deactivated, never deleted; usernames are immutable; PINs are 4 digits and unique across admins; password policy 8–72 bytes for every flow; forgot-password needs an email on the account (admins reset the rest from Settings → Users); `APP_URL` added to §11. `fiscal_config` is not a known setting until Phase 7. |
| **2 Products, rates, customers** | Products CRUD (weight), Rates screen with history, Customers CRUD | §5.2, §5.3. **DONE 2026-09-20** on `dev`, plan `docs/superpowers/plans/2026-09-20-demo-path-phases-2-4.md` (tasks D1–D3). Deviations: `hs_code` is never defaulted in code (invariant 7); a customer's `credit_limit` cannot be nulled back to "no limit" through `PUT /admin/customers/:id` in v1 — set it to 0 instead; `/rates` only lists active products (no "show inactive" toggle yet). |
| **3 Day ops + invoices + ledger (backend)** | Business day open/ensure/close/reopen, drawer movements, audit log; `POST /invoices` with server-side pricing (§6.3), numbering, idempotency, credit tender + ledger + limit; receipts; voids; list/search/detail; `testdb` harness with the §10 cases | The money core. Rounding examples put to the owner before merge. **DONE 2026-09-20** on `dev`, plan `docs/superpowers/plans/2026-09-20-demo-path-phases-2-4.md` (tasks D4–D6b). Deviations: admin day routes live under `/admin/day/*`, not `/day/*` as first drafted; `net_sales` on a closed day is Σ `total_payable` (the rounded rupee figure), not Σ `total_amount`; invoice/receipt numbers accept gaps (allocated before the day gate and credit checks run); rounding is nearest-rupee, half-up, achievable range −0.49…+0.50 — put to the owner, not yet confirmed. |
| **4 Till UI + printing** | `/pos` with WeightPad (4 modes), cart rail, customer picker, tender dialog, day-gate banners; thermal receipt + A4 invoice; reprint; kiosk-printing launcher | §6.2, §8.3, §8.4. **DONE 2026-09-20** on `dev`, plan `docs/superpowers/plans/2026-09-20-demo-path-phases-2-4.md` (tasks D7–D9). Deviations: printing is browser `window.print()` only — nothing has been through a physical printer yet; the A4 invoice's buyer address was blank unless a caller passed the `Customer` explicitly — fixed in the Phase 5 polish sweep (P6), which has `printInvoice` fetch the customer itself when the invoice carries a `customer_id`; counter staff can open/close the day without a PIN, per spec §3 — not yet confirmed with the owner. |
| **5 Day close UI, customers UI, dashboard, reports** | `/day-close` (open, movements, close, reopen, Z-report), `/customers` (statement, receive payment), dashboard, report tabs, exports | §6.5, §6.7, §6.8. **DONE 2026-09-21** on `dev`, plan `docs/superpowers/plans/2026-09-20-phase-5-dashboard-reports.md` (tasks P1–P8; P7 = visual identity pass, P8 = hourly heatmap). Deviations: the tax report's Tax excludes further tax, shown as its own line (a closed day's sealed `tax_collected` is still Tax + FurtherTax, unchanged); the dashboard's `receivables_outstanding` is Σ positive balances only — an account paid in advance is a liability, not a receivable, and is not netted against the ones who owe; the dashboard's "top products" card is always the last 30 days, independent of any range picker; the tax report's fiscal-status column is deferred to Phase 7 (nothing to show until invoices fiscalise). |
| **6 First deploy** | Railway project, env, admin created, smoke test on the real printer, `docs/DEPLOY.md`, `verify` skill | Go-live candidate with fiscal off. |
| **7 FBR DI** | `fiscal/` port, settings panel with test connection + reference refresh, payload builder with golden tests, inline + queued submission, credit note on void, receipt block, fiscal queue report; **sandbox run of SN001/SN002 (and SN026 if assigned) with the tenant's token** | §7. Blocked on the tenant's IRIS sandbox token and the advisor's HS/rate confirmation. |
| **8 Desktop** | Electron wrapper (`electron/` port): silent print, auto-update from a new `Usamapuri/elevon-pos-releases` repo, own appId/GUID | Frontend unchanged. |
| Later candidates | Empty-cylinder deposits and returns, receipt-to-invoice allocation, staged rates, weighbridge serial feed, `manager` role, second store | Each is its own spec. |

---

## 14. CLAUDE.md for this repo (written in Phase 0)

Sections, mirroring the retail POS file: product definition; production state (no live store yet; branch rules); isolation rules (§12); tech stack table; key directories; critical invariants with the test that pins each: (1) every route inside `RequireRoles`, (2) `APIResponse` envelope + stable error codes, (3) `JWT_SECRET` per store, (4) invoice money is computed server-side from `products.rate` and recomputed nowhere else, (5) `pricing` Go and TS mirror share one fixture, (6) `void_log`, `customer_ledger_entries`, `day_close_audit_log`, `fiscal_audit_events` are append-only, (7) reports filter on `business_date` / `business_day_id` only, (8) every invoice has a `business_day_id` and no auto-open exists, (9) rate changes always write history, (10) no HS-code default in code; `2711.1910` is a *settings* default the advisor confirms, (11) fiscal: sandbox validate URL is `_sb`, sandbox config in release refuses, retries consult `fiscal_invoices` first, (12) customer-visible money changes are stop-and-ask; commit conventions; working norms.
