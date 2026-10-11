# Per-invoice Deal Rates Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A cashier can charge a deal rate on any cart line; within a configured band it needs nothing, outside it an admin PIN; the server still computes every rupee and each line stores the list rate beside the charged rate.

**Architecture:** `pricing.ComputeTotals` already takes a rate per line, so pricing and its shared Go/TS fixture do not change. The invoice create handler decides which rate each line is priced at: the locked `products.rate`, or a `rate_override` it has band-checked or PIN-approved inside the invoice transaction. Migration `004` adds the list-rate snapshot and approver to `invoice_lines` and one new setting.

**Tech Stack:** Go 1.24 / Gin / lib-pq raw SQL; React 18 + TS strict + TanStack; vitest (node, logic only).

**Spec:** `docs/superpowers/specs/2026-10-12-on-demand-fbr-and-deal-rates-design.md` §3 (deal rate), §3.4 (owner-approved before/after), §6 (tests). Base design: `docs/superpowers/specs/2026-09-19-elevon-lpg-pos-design.md` §6.2, §6.3.

## Global Constraints

- Money is never taken from the request: the only new client number is a **rate** (Rs/kg, > 0, 2 dp); every total is still computed by `pricing.ComputeTotals`.
- `products.rate` and `product_rate_history` are **not written** by a deal rate.
- Band check and PIN check happen **inside the invoice transaction**, against the product rows `lockProducts` holds `FOR SHARE`.
- Band comparison is in whole paisa via `pricing.Paisa`, never float subtraction.
- New setting `rate_override_band`: Rs per kg, 0–100000, default `0` (every override needs a PIN).
- Error codes: `rate_override_pin_required` (403), `invalid_pin` (401, existing), `invalid_rate` (400, existing). `APIResponse` envelope; no `err.Error()` to a client.
- Migrations are idempotent (`migrations_idempotent_contract_test.go`).
- `pricing.go`, `lib/pricing.ts` and both `pricing_fixture.json` files must be **unchanged** by this plan.
- Frontend: TS strict, no `any`; `.tabular` on numbers; colours via tokens.
- Commits `<scope>(<subsystem>): E-09 — <imperative>`, trailer `Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>`. Gate: `cd backend && go vet ./... && go test ./...` (with `TEST_DATABASE_URL`); `cd frontend && npm run type-check && npm run test && npx vite build`.
- No Bhookly tenant/host/appId/release-repo/Railway-ID references.

## Review Focus

1. **Override equal to the list rate.** A cashier opens the rate field and confirms without changing it. Expected: treated as no override — no PIN prompt, `rate_approved_by` null. (Test in D2.)
2. **Idempotent retry after the PIN prompt.** The till re-POSTs the same `client_op_id` with a PIN after `rate_override_pin_required`. Expected: one invoice, at the deal rate. (Test in D2.)
3. **One PIN, two approvals.** A credit sale over the limit that also has an out-of-band rate. Expected: a single PIN satisfies both; bcrypt runs once. (Test in D2.)
4. **Amount mode with a deal rate.** The customer names Rs 50,000 at a deal rate. Expected: quantity = round3(amount ÷ deal rate), and changing the rate afterwards recomputes the quantity from the typed amount. (Test in D3.)
5. **Rate change racing the sale.** The list rate moves between cart and charge. Expected: the band is measured from the rate locked at charge time; the stored `list_rate` is that locked rate. (Test in D2.)

## File structure

```
backend/migrations/004_deal_rates.sql            list_rate, rate_approved_by(+_name), rate_override_band seed
backend/internal/settings/settings.go            + rate_override_band rule (settings_test key count 24 → 25)
backend/internal/models/invoice.go               InvoiceLineRequest.RateOverride; InvoiceLine.ListRate/RateApprovedBy/RateApprovedByName
backend/internal/invoice/load.go                 LineColumns + ScanLine
backend/internal/handlers/invoices.go            parse, band/PIN decision, line insert
backend/internal/handlers/invoices_db_test.go    deal-rate cases
backend/internal/reports/queries.go              Products: deal_variance
frontend/src/types/index.ts                      AppSettings.rate_override_band; line types
frontend/src/components/pos/cart.ts (+test)      CartLine.rate_override, set_rate action, toRequestLines
frontend/src/components/pos/dealRate.ts (+test)  needsPin(), parseRateInput()
frontend/src/components/pos/RateDialog.tsx       the rate field
frontend/src/components/pos/CartRail.tsx         rate display, struck list rate
frontend/src/routes/_app/pos.tsx                 PIN retry on rate_override_pin_required
frontend/src/components/settings/TaxForm.tsx     band field
frontend/src/components/invoices/InvoiceDrawer.tsx  list rate / charged rate / approver
frontend/src/components/reports/reportColumns.tsx   deal variance column
```

---

### Task D1 (E-09): Migration, setting, models

**Files:** Create `backend/migrations/004_deal_rates.sql`. Modify `backend/internal/settings/settings.go`, `backend/internal/settings/settings_test.go`, `backend/internal/models/invoice.go`, `backend/internal/invoice/load.go`.

**Interfaces — Produces:**
- `invoice_lines.list_rate NUMERIC(12,2) NOT NULL`, `rate_approved_by UUID NULL`, `rate_approved_by_name VARCHAR(120) NULL` (name snapshot, same reason as `invoices.cashier_name`; the spec lists only the id).
- `models.InvoiceLineRequest.RateOverride *float64` (`json:"rate_override"`).
- `models.InvoiceLine.ListRate float64` (`json:"list_rate"`), `RateApprovedBy *uuid.UUID` (`json:"rate_approved_by"`), `RateApprovedByName *string` (`json:"rate_approved_by_name"`).
- Setting key `rate_override_band`.

- [ ] **Step 1: Write the failing tests.** In `settings_test.go` change `TestKeys_MatchTheSeededList` to expect 25 and add `"rate_override_band"` to the `Known` loop; add:

```go
func TestRateOverrideBand_Rule(t *testing.T) {
	for raw, ok := range map[string]bool{`0`: true, `5`: true, `2.5`: true, `100000`: true, `-1`: false, `100001`: false, `null`: false, `"5"`: false} {
		err := Validate("rate_override_band", json.RawMessage(raw))
		if ok && err != nil {
			t.Errorf("%s should be accepted: %v", raw, err)
		}
		if !ok && err == nil {
			t.Errorf("%s should be refused", raw)
		}
	}
}
```

- [ ] **Step 2: Run** `cd backend && go test ./internal/settings/` — expect FAIL (`rules cover 24`, unknown key).

- [ ] **Step 3: Write the migration.**

```sql
-- 004_deal_rates — per-invoice deal rates (spec 2026-10-12 §3). Idempotent.

-- list_rate is products.rate at the moment of sale; unit_price stays the rate
-- actually charged. Every line written before this migration was charged at
-- the list rate, so the backfill is exact.
ALTER TABLE invoice_lines ADD COLUMN IF NOT EXISTS list_rate NUMERIC(12,2);
UPDATE invoice_lines SET list_rate = unit_price WHERE list_rate IS NULL;
ALTER TABLE invoice_lines ALTER COLUMN list_rate SET NOT NULL;

-- Who approved a rate outside the band. Null for a list-rate line and for a
-- deal rate inside the band. The name is a snapshot for reprints.
ALTER TABLE invoice_lines ADD COLUMN IF NOT EXISTS rate_approved_by UUID REFERENCES users(id) ON DELETE SET NULL;
ALTER TABLE invoice_lines ADD COLUMN IF NOT EXISTS rate_approved_by_name VARCHAR(120);

-- Rs per kg a cashier may move a rate without an admin PIN. 0 = always ask.
INSERT INTO settings (key, value) VALUES ('rate_override_band', '0') ON CONFLICT (key) DO NOTHING;
```

- [ ] **Step 4: Add the rule** in `settings.go` beside `further_tax_rate`: `"rate_override_band": notNull(number(0, 100000)),`.

- [ ] **Step 5: Models and projection.** Add the three `InvoiceLine` fields after `UnitPrice`, `RateOverride` to `InvoiceLineRequest`. In `invoice/load.go` append `, list_rate::float8, rate_approved_by, rate_approved_by_name` to `LineColumns` and `&l.ListRate, &l.RateApprovedBy, &l.RateApprovedByName` to `ScanLine`.

- [ ] **Step 6: Make the existing insert compile-and-pass.** In `handlers/invoices.go` the line `INSERT` gains `list_rate` with value `prod.rate` (column list and `$15`). No behaviour change yet.

- [ ] **Step 7: Run** `go vet ./... && go test ./...` — expect PASS, including `migrations_idempotent_contract_test.go`. If that contract test rejects the `UPDATE`, read its allow-list and wrap nothing — report it instead of weakening the test.

- [ ] **Step 8: Commit** `backend(migration): E-09 — 004: list rate and approver on invoice lines, rate_override_band`.

---

### Task D2 (E-09): Band and PIN decision in invoice create

**Files:** Modify `backend/internal/handlers/invoices.go`, `backend/internal/handlers/invoices_db_test.go`.

**Interfaces — Consumes:** D1's fields; `staffpin.Identify(tx, pin, staffpin.AdminOnly) (Identity{UserID, Name}, error)`, `staffpin.ErrNoMatch`; `pricing.Paisa(float64) int64`; `settingNumber(all, key)`.
**Produces:** `POST /invoices` accepts `lines[].rate_override`; response lines carry `unit_price` (charged), `list_rate`, `rate_approved_by`, `rate_approved_by_name`.

- [ ] **Step 1: Write the failing tests** in `invoices_db_test.go` (helpers `testdb.Fresh`, `seedUser`, `openBusinessDay`, `seedProductRow`, `setSetting`, `moneyRouter`, `doJSON`, `decodeEnvelope`, `errCode`, `dataAs`, `sameMoney` already exist; set an admin PIN the way the existing void tests do):

```go
func dealSale(productID uuid.UUID, qty, rate float64, extra map[string]any) map[string]any {
	body := map[string]any{
		"client_op_id": uuid.NewString(), "payment_method": "cash",
		"lines": []map[string]any{{"product_id": productID.String(), "quantity": qty, "entered_as": "kg", "rate_override": rate}},
	}
	for k, v := range extra {
		body[k] = v
	}
	return body
}

// Spec §3.4, row two: 1 000 kg at 249.50 against a 250.00 list rate.
func TestDealRate_WithinBandNeedsNoPin(t *testing.T) {
	db := testdb.Fresh(t)
	cashierID := seedUser(t, db, "counter", "staff", "counter-pass-1", nil)
	openBusinessDay(t, db, cashierID, businessToday())
	productID := seedProductRow(t, db, "LPG bulk", 250.00)
	setSetting(t, db, "tax_rate_cash", "0.18")
	setSetting(t, db, "rate_override_band", "1")
	r := moneyRouter(db, actor{id: cashierID, username: "counter", role: "staff"})

	w := doJSON(r, http.MethodPost, "/invoices", dealSale(productID, 1000, 249.50, nil))
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var inv models.Invoice
	dataAs(t, decodeEnvelope(t, w), &inv)
	if !sameMoney(inv.Subtotal, 249500) || !sameMoney(inv.TaxAmount, 44910) || !sameMoney(inv.TotalAmount, 294410) {
		t.Fatalf("priced at the deal rate: %+v", inv)
	}
	l := inv.Lines[0]
	if !sameMoney(l.UnitPrice, 249.50) || !sameMoney(l.ListRate, 250) || l.RateApprovedBy != nil {
		t.Fatalf("line snapshot: %+v", l)
	}
	var rate float64
	if err := db.QueryRow(`SELECT rate::float8 FROM products WHERE id = $1`, productID).Scan(&rate); err != nil || !sameMoney(rate, 250) {
		t.Fatalf("products.rate must not move: %v %v", rate, err)
	}
	var history int
	if err := db.QueryRow(`SELECT count(*) FROM product_rate_history WHERE product_id = $1`, productID).Scan(&history); err != nil || history != 0 {
		t.Fatalf("a deal rate writes no rate history: %d %v", history, err)
	}
}

func TestDealRate_OutsideBandWithoutPinIsRefused(t *testing.T) {
	// band 1, override 245.00 → 403 rate_override_pin_required, no invoice row.
}

func TestDealRate_OutsideBandWrongPin(t *testing.T) {
	// same, "pin": "0000" (no such admin) → 401 invalid_pin, no invoice row.
}

func TestDealRate_OutsideBandWithPinStoresApprover(t *testing.T) {
	// same, valid admin PIN → 201; line.RateApprovedBy == admin id,
	// RateApprovedByName == admin name, UnitPrice 245.00, ListRate 250.00.
}

func TestDealRate_DefaultBandZeroAlwaysAsks(t *testing.T) {
	// no rate_override_band set beyond the seed (0); override 249.99 → 403.
}

func TestDealRate_EqualToListIsNotAnOverride(t *testing.T) {
	// band 0, override 250.00 → 201 without a PIN; RateApprovedBy nil.
}

func TestDealRate_RetryWithPinMakesOneInvoice(t *testing.T) {
	// POST without pin → 403; POST the SAME client_op_id with the PIN → 201;
	// SELECT count(*) FROM invoices = 1.
}

func TestDealRate_OnePinCoversCreditLimitAndRate(t *testing.T) {
	// credit customer over limit (credit_limit_enforced true) + out-of-band
	// rate; one valid PIN → 201; notes contain "credit limit overridden by";
	// line.RateApprovedBy set.
}

func TestDealRate_BandMeasuredFromLockedRate(t *testing.T) {
	// band 1. UPDATE products SET rate = 260 after seeding at 250; override
	// 250.00 → 403 (10 away from the live 260), and with a PIN ListRate == 260.
}

func TestDealRate_InvalidRates(t *testing.T) {
	// rate_override 0, -5, 249.505 (3 dp) → 400 invalid_rate each.
}
```

Write every body in full in the file, following the first test's shape; the comments above are the exact inputs and assertions.

- [ ] **Step 2: Run** `go test ./internal/handlers/ -run TestDealRate` — expect FAIL (override ignored: subtotal 250,000).

- [ ] **Step 3: Parse.** Add `rateOverride *float64` to `parsedLine`. In `parseCreateInvoice`'s line loop:

```go
if l.RateOverride != nil {
	if *l.RateOverride <= 0 || !validRate(*l.RateOverride) {
		return parsedInvoice{}, badRequest("invalid_rate", fmt.Sprintf("Line %d has a rate that is not a positive rupee amount", i+1))
	}
	v := *l.RateOverride
	pl.rateOverride = &v
}
```

(`pl` is whatever the loop names its `parsedLine`; `validRate` is the 2-dp check in `handlers/products.go`.)

- [ ] **Step 4: One PIN lookup for the whole request.** In `Create`, after the transaction opens and before step 6, add a memoised resolver, and change the credit-limit block (step 10) to use it instead of calling `staffpin.Identify` directly:

```go
// adminByPin resolves the request's PIN at most once: bcrypt is deliberately
// slow, and one PIN may approve both a deal rate and a credit-limit override.
var pinIdentity *staffpin.Identity
var pinErr error
pinTried := false
adminByPin := func() (*staffpin.Identity, error) {
	if !pinTried {
		pinTried = true
		id, err := staffpin.Identify(tx, p.pin, staffpin.AdminOnly)
		if err != nil {
			pinErr = err
		} else {
			pinIdentity = &id
		}
	}
	return pinIdentity, pinErr
}
```

- [ ] **Step 5: Decide each line's rate** — new step 8b, after the customer block and before pricing:

```go
// 8b — deal rates. A line is priced at products.rate unless the cashier
// named another rate; that rate stands on its own inside the owner's band
// and needs an admin PIN outside it. The band is measured from the row
// lockProducts holds, so a rate change cannot widen it mid-sale.
band := pricing.Paisa(settingNumber(all, "rate_override_band"))
chargedRate := make([]float64, len(p.lines))
approved := make([]bool, len(p.lines))
needPin := false
for i, l := range p.lines {
	list := products[l.productID].rate
	chargedRate[i] = list
	if l.rateOverride == nil {
		continue
	}
	diff := pricing.Paisa(*l.rateOverride) - pricing.Paisa(list)
	if diff == 0 {
		continue
	}
	chargedRate[i] = *l.rateOverride
	if diff < 0 {
		diff = -diff
	}
	if diff > band {
		approved[i] = true
		needPin = true
	}
}
var rateApprover *staffpin.Identity
if needPin {
	if p.pin == "" {
		c.JSON(http.StatusForbidden, models.Fail("This rate is outside the allowed band — an admin PIN can approve it", "rate_override_pin_required"))
		return
	}
	identity, err := adminByPin()
	if errors.Is(err, staffpin.ErrNoMatch) {
		c.JSON(http.StatusUnauthorized, models.Fail("That PIN does not match an active admin", "invalid_pin"))
		return
	}
	if err != nil {
		log.Printf("invoice create: pin: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not record the sale", "internal_error"))
		return
	}
	rateApprover = identity
}
```

Pricing then uses `Rate: chargedRate[i]`; replace the step-9 comment with "Rates come from the locked product rows or a deal rate approved in 8b, never unchecked from the body."

- [ ] **Step 6: Store it.** The line insert writes `list_rate = prod.rate` and, when `approved[i]`, `rate_approved_by = rateApprover.UserID`, `rate_approved_by_name = rateApprover.Name` (nil otherwise).

- [ ] **Step 7: Update the package doc comments** that say "always products.rate" (`invoices.go:32`, the `Create` step list) to match invariant 6's new wording. Do **not** edit `pricing.go`.

- [ ] **Step 8: Run** `go vet ./... && go test ./...` — expect PASS. Confirm `git diff --stat` shows no change under `internal/pricing/`.

- [ ] **Step 9: Commit** `backend(invoices): E-09 — per-line deal rate, band-checked or PIN-approved in the sale transaction`.

---

### Task D3 (E-09): Till — rate field, PIN retry, band setting

**Files:** Create `frontend/src/components/pos/dealRate.ts`, `dealRate.test.ts`, `RateDialog.tsx`. Modify `frontend/src/types/index.ts`, `components/pos/cart.ts`, `cart.test.ts`, `CartRail.tsx`, `routes/_app/pos.tsx`, `components/settings/TaxForm.tsx`, `lib/settings.ts` (+ its test if it enumerates keys).

**Interfaces — Consumes:** `POST /invoices` `lines[].rate_override`; error codes `rate_override_pin_required`, `invalid_pin`; `AppSettings.rate_override_band`.
**Produces:**
```ts
// dealRate.ts
export function parseRateInput(text: string): number | null   // "249.5" → 249.5; "", "0", "-1", "1.234", "abc" → null
export function needsPin(listRate: number, dealRate: number, band: number): boolean   // integer-paisa compare, mirrors the server
export function effectiveRate(line: { rate: number; rate_override?: number }): number
// cart.ts
interface CartLine { rate_override?: number }                  // absent = list rate
type CartAction = … | { type: 'set_rate'; key: string; rate: number | null }
```

- [ ] **Step 1: Write the failing tests.**

```ts
// dealRate.test.ts
import { describe, expect, it } from 'vitest'
import { effectiveRate, needsPin, parseRateInput } from './dealRate'

describe('parseRateInput', () => {
  it('accepts a positive rupee rate to 2 dp', () => {
    expect(parseRateInput('249.5')).toBe(249.5)
    expect(parseRateInput(' 250 ')).toBe(250)
  })
  it('refuses everything else', () => {
    for (const bad of ['', '0', '-1', '1.234', 'abc', '1e3']) expect(parseRateInput(bad)).toBeNull()
  })
})

describe('needsPin', () => {
  it('is false at the list rate and inside the band', () => {
    expect(needsPin(250, 250, 0)).toBe(false)
    expect(needsPin(250, 249, 1)).toBe(false)
    expect(needsPin(250, 251, 1)).toBe(false)
  })
  it('is true one paisa past the band, either side', () => {
    expect(needsPin(250, 248.99, 1)).toBe(true)
    expect(needsPin(250, 251.01, 1)).toBe(true)
    expect(needsPin(250, 249.99, 0)).toBe(true)
  })
  it('does not trip on float representation', () => {
    expect(needsPin(0.3, 0.1 + 0.2, 0)).toBe(false)
  })
})

describe('effectiveRate', () => {
  it('prefers the deal rate', () => {
    expect(effectiveRate({ rate: 250 })).toBe(250)
    expect(effectiveRate({ rate: 250, rate_override: 249.5 })).toBe(249.5)
  })
})
```

In `cart.test.ts` add: `set_rate` stores the override; `set_rate` with the list rate or `null` clears it; on an `amount` line with `typed_amount: 50000` and list 250, `set_rate` 249.5 changes `quantity` to `round3(50000 / 249.5)` = `200.401`; the preview lines (`LineIn[]`) and the request lines carry the deal rate / `rate_override`; a line with no override sends no `rate_override` key.

- [ ] **Step 2: Run** `cd frontend && npm run test -- dealRate cart` — expect FAIL (module missing).

- [ ] **Step 3: Implement `dealRate.ts`.**

```ts
const paisa = (v: number) => Math.round(v * 100)

export function parseRateInput(text: string): number | null {
  const t = text.trim()
  if (!/^\d+(\.\d{1,2})?$/.test(t)) return null
  const v = Number(t)
  return v > 0 ? v : null
}

export function needsPin(listRate: number, dealRate: number, band: number): boolean {
  return Math.abs(paisa(dealRate) - paisa(listRate)) > paisa(band)
}

export function effectiveRate(line: { rate: number; rate_override?: number }): number {
  return line.rate_override ?? line.rate
}
```

- [ ] **Step 4: Cart reducer.** Add `rate_override?: number` to `CartLine` and the `set_rate` action: clear when `rate === null` or equal in paisa to `line.rate`; when the line is `entered_as: 'amount'` with a `typed_amount`, set `quantity = round3(typed_amount / effectiveRate)`. Wherever the module builds `LineIn` for the preview and `InvoiceLineRequest` for the POST, use `effectiveRate(line)` and add `rate_override` only when present. Update the file's header comment: the charged figure is recomputed by the server from `products.rate` or an approved deal rate.

- [ ] **Step 5: Types.** `InvoiceLineRequest.rate_override?: number`; `InvoiceLine.list_rate: number`, `rate_approved_by: string | null`, `rate_approved_by_name: string | null`; `AppSettings.rate_override_band: number` (and the settings-key count comment).

- [ ] **Step 6: `RateDialog.tsx`.** A shadcn `Dialog` in the style of `TenderDialog`: product name, list rate, one numeric input prefilled with the current effective rate, a live line "Rs X × N kg = Rs Y", a muted note "Needs an admin PIN" when `needsPin(...)`, buttons **Use list rate** and **Set rate** (disabled while `parseRateInput` is null). Props: `{ line: CartLine; band: number; open: boolean; onOpenChange(open: boolean): void; onConfirm(rate: number | null): void }`.

- [ ] **Step 7: `CartRail.tsx`.** Each line shows its rate as a button (44 px target, like the existing cart actions) that opens `RateDialog`. An overridden line shows the list rate struck through (`line-through text-muted-foreground`) before the deal rate, both `.tabular`.

- [ ] **Step 8: PIN retry in `pos.tsx`.** The page already retries with a PIN on `credit_limit_exceeded` using a stable `client_op_id`. Add `rate_override_pin_required` to the codes that open the same PIN modal with the title "Admin PIN to approve this rate"; the retry re-sends the same body plus `pin`. `invalid_pin` stays inline in the modal.

- [ ] **Step 9: Band field.** In `TaxForm.tsx` add "Rate change without PIN (Rs per kg)" bound to `rate_override_band`, 0–100000, step 0.01, helper text "0 means every rate change needs an admin PIN." Add the key to the zod schema and patch mapping in `lib/settings.ts`.

- [ ] **Step 10: Run** `npm run type-check && npm run test && npx vite build` — expect PASS.

- [ ] **Step 11: Commit** `frontend(pos): E-09 — deal rate on a cart line with band and PIN approval`.

---

### Task D4 (E-09): Invoice detail, products report, docs

**Files:** Modify `backend/internal/reports/queries.go`, `queries_db_test.go`, `export.go` (if columns are enumerated there), `frontend/src/components/invoices/InvoiceDrawer.tsx`, `frontend/src/components/reports/reportColumns.tsx`, `frontend/src/types/index.ts`, `CLAUDE.md`, `docs/superpowers/specs/2026-09-19-elevon-lpg-pos-design.md`.

**Interfaces — Produces:** the Products report row gains `deal_variance float64` (`json:"deal_variance"`): `Σ (unit_price − list_rate) × quantity` over non-void lines in the window, rounded to 2 dp; negative means sold under list.

- [ ] **Step 1: Failing test** in `queries_db_test.go`: two invoices for one product on one business date, one at list 250 × 100 kg, one at deal 249.50 × 1000 kg, plus a voided deal-rate invoice; assert the product row's `DealVariance` is `-500.00` and that a product with no deal lines reports `0`.
- [ ] **Step 2: Run** `go test ./internal/reports/ -run Products` — expect FAIL (no such field).
- [ ] **Step 3: Implement** by adding `COALESCE(ROUND(SUM((l.unit_price - l.list_rate) * l.quantity), 2), 0)::float8` to the Products query's select list and the row struct; add the column to the CSV/XLSX writer headers in the position after net sales.
- [ ] **Step 4: Frontend.** `reportColumns.tsx`: a "Deal variance" money column on the products tab (red when negative via the existing negative-money token). `InvoiceDrawer.tsx`: when `line.unit_price !== line.list_rate`, show "List Rs 250.00 → charged Rs 249.50" and, when present, "approved by {rate_approved_by_name}". Receipt and A4 builders are untouched (they already print `unit_price`); add one assertion to `receipt.test.ts` that a deal-rate line prints the charged rate and not the list rate.
- [ ] **Step 5: Docs.** `CLAUDE.md`: invariant 6's first clause becomes "Invoice money is computed server-side from `products.rate`, or from a per-line deal rate that is band-checked or PIN-approved server-side and stored beside the list rate"; "the 24 setting keys" → 25. Main spec §13: add a row "**9 Deal rates** — DONE <date>" pointing at the addendum and this plan.
- [ ] **Step 6: Run both gates** — expect PASS.
- [ ] **Step 7: Commit** `reports(products): E-09 — deal variance; invoice detail shows list and charged rate`.

---

## Self-review notes

- Spec §3.1 → D2 (server), D3 (till, band setting). §3.2 → D1. §3.3 → D2 step 8 pins pricing unchanged. §3.4 → D2's first test is the table's second row. §3.5 → D4.
- Deviation from the spec, recorded here: `rate_approved_by_name` is stored beside the id.
- The five Review Focus items each have a named test in D2 or D3.
