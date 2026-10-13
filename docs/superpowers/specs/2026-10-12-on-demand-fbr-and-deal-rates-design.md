# On-demand FBR filing and per-invoice deal rates — design addendum

**Date:** 2026-10-12
**Status:** design agreed in conversation; awaiting owner review of this file. Next step is the implementation plan.
**Amends:** `2026-09-19-elevon-lpg-pos-design.md` §5.4, §6.2, §6.3, §6.8, §7.4, §7.6, §13 and the remaining tasks (F4–F8) of `plans/2026-09-21-phase-7-fbr-digital-invoicing.md`. Where this file and those disagree, this file wins.

---

## 1. What changes and why

The store asked for three things after the demo:

1. **FBR filing is the operator's action, not automatic.** Most sales are not filed one by one. A sale can be filed on its own from the till, and the unfiled sales can be filed together as one combined FBR invoice, typically once a day.
2. **Rates are set per invoice.** LPG deals are struck per sale; a small per-kg margin is large at tonne scale, so the cashier needs to charge a deal rate without changing the product's list rate.
3. **Further tax stays off.** No change: `further_tax_rate` defaults to 0 and the sandbox accepts unregistered-buyer invoices without it (audit case A).

HS codes supplied by the store: `2711.1910` (LPG, sandbox-confirmed) and `2711.1400` (not yet checked against the sandbox — see §5).

### Decisions

| # | Decision |
|---|---|
| A1 | Nothing is filed automatically. A plain Charge leaves the invoice `unfiled`. |
| A2 | The till gets a second button, **Charge + FBR**, that files that one sale. |
| A3 | A **File with FBR** screen (admin) files a chosen set of unfiled invoices as one combined FBR invoice. All eligible invoices are ticked by default; the operator may untick any. |
| A4 | The app shows how much is unfiled (count, kg, rupees). This is information only: it never warns, blocks a sale, or blocks day close. |
| A5 | Every figure sent to FBR is copied from recorded invoice lines. There is no way to type a combined invoice's quantity or amount. |
| A6 | A line's rate can be overridden at the till. Within a configurable band any cashier may do it; outside the band it needs an admin PIN. |
| A7 | The server still computes every rupee. The line stores both the list rate and the charged rate. `products.rate` is not touched. |

---

## 2. On-demand filing

### 2.1 Fiscal status of an invoice

`invoices.fiscal_status` becomes:

| Value | Meaning |
|---|---|
| `off` | Fiscal was disabled when the sale was made. |
| `unfiled` | Fiscal enabled, not filed. Counts toward "pending to file". |
| `pending` | A filing is in flight (its own, or the batch it belongs to). |
| `synced` | Filed on its own; `fiscal_invoice_number` is its FBR number. |
| `covered` | Filed as part of a combined invoice; `fiscal_batch_id` points at it and `fiscal_invoice_number` holds the combined FBR number. |
| `failed` | Its own filing was rejected by FBR. Counts toward "pending to file" and can be retried or put in a batch. |

### 2.2 Charge + FBR (per sale)

- `POST /invoices` gains `file_fbr` (boolean, default false). The till shows the second button only when `fiscal_config.enabled`.
- Insert `fiscal_status` as `off` when fiscal is disabled, `pending` when `file_fbr`, else `unfiled`.
- When `file_fbr`: after commit, `fiscal.Submit(KindSale)` inline with an 8 s deadline. Success → `synced`. Rejected → `failed`, and the response carries `fiscal_error_code`. Unreachable → enqueue and leave `pending`. The sale is never rolled back because of FBR.
- An unfiled or failed invoice can also be filed on its own later: `POST /admin/fiscal/invoices/:id/submit` from the invoices browser.
- Available to `staff` and `admin` (it is a way of completing a sale). Later filing is admin-only.

### 2.3 Combined invoice (roll-up)

**Scope of one batch:** invoices from a single business date. The screen opens on today's business date with a date picker; `invoiceDate` in the payload is that date.

**Eligible invoices:** `fiscal_status IN ('unfiled','failed')`, not voided, that business date, the buyer is not `Registered` (registered buyers need an invoice in their own NTN, so they are filed individually), and the invoice's `tax_rate` matches `fiscal_config.rate_desc`. Ineligible unfiled invoices are listed greyed out with the reason.

**Payload:** `invoiceType "Sale Invoice"`, walk-in buyer block exactly as the single-sale builder produces it (unregistered scenario in sandbox). **One DI item per source invoice line**, built by the existing line mapper, so every item carries that line's stored `line_total − line_discount`, `line_tax` and further-tax share unchanged. Lines are not summed: FBR recomputes tax per item with no tolerance (error 0104), and a sum of per-line rounded taxes can differ from the tax on the summed value by a few paisa. `productDescription` is prefixed with the source invoice number.

**Data model (migration `004`):**

```
fiscal_batches
  id UUID PK
  business_date DATE NOT NULL
  status VARCHAR(12)            -- pending | synced | failed
  fbr_invoice_number VARCHAR(64) NULL
  invoice_count INT, total_kg NUMERIC(14,3),
  value_excl_tax, tax_amount, further_tax_amount, total_amount NUMERIC(14,2)
  last_error_code VARCHAR(32) NULL
  created_by UUID, created_at, settled_at

invoices.fiscal_batch_id UUID NULL REFERENCES fiscal_batches(id)
```

`fiscal_invoices`, `fiscal_outbound_jobs` and `fiscal_audit_events` each gain a nullable `batch_id`; on the first two `invoice_id` becomes nullable with a check that exactly one of the two is set, and a unique index on `(batch_id, kind)` mirrors the existing `(invoice_id, kind)` one. This keeps invariant 7 true for batches: a retry consults `fiscal_invoices` by batch first and cannot file the same batch twice.

**Flow — `POST /admin/fiscal/batches {business_date, invoice_ids[]}`:**

1. One transaction: lock the invoices `FOR UPDATE`, re-check eligibility for every id (any failure refuses the whole request with `fiscal_batch_ineligible` and the offending ids), insert the batch as `pending` with its totals, set the invoices to `pending` with `fiscal_batch_id`. Commit.
2. `fiscal.Submit(KindBatch)` inline with a 30 s deadline.
3. Synced → batch `synced`, its invoices `covered` with the combined number.
4. Rejected → batch `failed` with the error code, its invoices released back to `unfiled` with `fiscal_batch_id` cleared so the operator can fix and refile. The failed batch row stays as history.
5. Unreachable → enqueue; the worker settles it the same way.

An invoice is in at most one non-failed batch, enforced by the status check under the row lock.

**Other endpoints:** `GET /admin/fiscal/batches?from=&to=` (list), `GET /admin/fiscal/batches/:id` (detail with covered invoices), `GET /admin/fiscal/eligible?business_date=` (the screen's list).

### 2.4 Pending to file

`GET /fiscal/pending` (`staff`, `admin`) returns `{count, kg, total_payable}` for non-void invoices in `unfiled` or `failed`, for the current business date and for all dates. Shown as one quiet line under the cart totals on the till and as a dashboard card, only when fiscal is enabled. Nothing reads it to gate anything.

### 2.5 Voids

- `synced` and buyer `Registered` → Debit Note, as already planned.
- `synced` and buyer not registered, or `covered` → void locally, `fiscal_void_status` stays `unfiled` (FBR rejects debit notes for unregistered buyers, error 0106). The batch's figures are not altered; the batch detail shows which covered invoices were later voided.
- `pending` → refuse with `fiscal_filing_in_flight` (409) until the filing settles.
- `unfiled` / `failed` / `off` → plain void; it drops out of pending.

### 2.6 Receipts and reports

- The FBR block and QR print only on `synced` invoices. A `covered` invoice prints as an ordinary receipt.
- Tax report: per band, counts and amounts for filed (`synced` + `covered`), unfiled and failed.
- Fiscal queue tab: batches and single filings, with status, FBR number, error code and retry.

### 2.7 Settings → FBR: two tokens and a test panel

Added 2026-10-12 at the owner's request.

**Two token slots.** The single private key `fiscal_api_key_enc` is replaced by `fiscal_api_key_sandbox_enc` and `fiscal_api_key_production_enc` (both private, both AES-GCM with `FISCAL_SECRETS_KEY`; migration `004` moves an existing value into the sandbox slot). `PUT /admin/fiscal/token` and `DELETE /admin/fiscal/token` take `environment: "sandbox" | "production"`. Status returns `set` and `masked` (`****` + last 4) per slot, never a token. `fiscal_config.is_sandbox` still chooses which environment live filing uses, and the token for that environment is the one read. The production slot cannot be cleared while fiscal is enabled in production mode, and likewise for sandbox.

**Test per environment.** The tab has a Sandbox card and a Production card, each with its token field, masked state and a **Run test** button. `POST /admin/fiscal/test {environment}` builds the one-line sample sale already specified (1.000 kg, value 100.00, tax from `tax_rate_cash`, HS from `default_hs_code`, walk-in buyer) and:

| Environment | What it sends | Result |
|---|---|---|
| Sandbox | validate, then post, to the `_sb` URLs | FBR invoice number and QR |
| Production | **validate only** | valid / invalid; no number, because posting would file a real invoice against the store's NTN |

The sandbox test still obeys the sandbox-in-release guard (`sandbox_in_release` unless `FISCAL_ALLOW_SANDBOX=true`). Every test writes a `fiscal_audit_events` row of kind `test`; a sandbox test's posted number is not written to `fiscal_invoices`.

**Result dialog.** Passed / Failed badge and the environment; the URL(s) called; the exact outbound JSON body with a Download JSON button; the FBR invoice number and its QR (sandbox pass only), rendered by the same builder the receipt uses; the raw gateway response; and on failure the error code, message and offending line index. The response shape is `{ok, environment, steps: [{phase, url, status_code, error_code, error, line, raw_response}], request_body, invoice_number}`. The request body never contains the token (it travels in the header).

### 2.8 Removed from the Phase 7 plan

The "every completed sale is filed" goal and F4's unconditional create hook. The queue, worker, ledger, audit trail, Settings → FBR tab, test connection and reference refresh are unchanged.

---

## 3. Per-invoice deal rate

### 3.1 Behaviour

- Each cart line shows its rate; tapping it opens a rate field. An overridden line shows the list rate struck through beside the deal rate.
- Request lines gain optional `rate_override` (> 0, 2 dp).
- New setting `rate_override_band` (Rs per kg, 0–100000, **default 0**). With `|rate_override − products.rate| ≤ band` any cashier may charge it. Outside the band the request needs an admin PIN in the existing `pin` field; without one the server answers `rate_override_pin_required` (403), with a wrong one `invalid_pin`. Default 0 means every override needs a PIN until the owner sets a band.
- The band is checked against the product row as locked in the create transaction, so a rate change racing the sale cannot widen it.
- `products.rate` and `product_rate_history` are not written.

### 3.2 Data

`invoice_lines` gains:

```
list_rate NUMERIC(12,2) NOT NULL     -- products.rate at the time of sale; backfilled from unit_price
rate_approved_by UUID NULL           -- the admin whose PIN approved an out-of-band rate
```

`unit_price` remains the rate actually charged.

### 3.3 Pricing

`pricing.ComputeTotals` is unchanged: it already takes a rate per line. The handler passes `rate_override` when present and authorised, else the locked `products.rate`. The shared Go/TS fixture does not change. Amount mode divides by the charged rate.

**Invariant 6 is reworded:** "Invoice money is computed server-side from `products.rate`, or from a per-line deal rate that is band-checked or PIN-approved server-side and stored beside the list rate."

### 3.4 Before/after (invariant 8)

1,000 kg, 18 % tax, list rate Rs 250.00:

| Rate charged | Subtotal | Tax | Total |
|---|---|---|---|
| 250.00 (list) | 250,000.00 | 45,000.00 | 295,000.00 |
| 249.50 (deal) | 249,500.00 | 44,910.00 | 294,410.00 |
| 251.00 (deal) | 251,000.00 | 45,180.00 | 296,180.00 |

Rounding, tax base and further tax are untouched. The owner's approval of this file is the sign-off for the change.

### 3.5 Reports and print

- Receipt and A4 invoice print the charged rate only.
- Invoice detail shows list rate, charged rate and the approver.
- Products report gains a "deal variance" column: Σ (`unit_price − list_rate`) × quantity.
- FBR payloads need no change; they copy line values.

---

## 4. Error codes added

`fiscal_batch_ineligible`, `fiscal_batch_not_found`, `fiscal_filing_in_flight`, `rate_override_pin_required`. All existing fiscal codes stay.

## 5. To verify in the sandbox (with the store's token)

1. `HS_UOM` and `SaleTypeToRate` for `2711.1400`: allowed units and that the 18 % rate applies. No product uses it until this passes.
2. A combined invoice with many items (try 50 and 300) validates; if FBR caps the item count, a batch is split into several of that size.
3. A combined invoice dated a previous business date validates.

## 6. Testing

- DB tests: create with and without `file_fbr`; batch happy path, rejected batch releases its invoices, an invoice cannot join two batches (concurrent requests), registered-buyer and voided invoices refused, void rules per status, pending summary.
- Golden payload test for a batch built from two invoices with three lines.
- Ledger-first contract test extended to batches.
- Rate override: within band without PIN, outside band without PIN refused, outside band with PIN stores the approver, `list_rate` snapshot, band checked against the locked row.
- Frontend logic tests: band check mirror, struck-rate display model, pending line formatting.

## 7. Order of work

1. Deal rate (independent of fiscal; migration, handler, till, reports).
2. Fiscal: migration `004`, queue and worker with batch support, `file_fbr` hook, batch endpoints.
3. Settings → FBR tab, File with FBR screen, till button and pending line.
4. Receipt block, reports.
5. Sandbox checks in §5, then the smoke run through the app.

---

## 8. Back-dated invoices (approved 2026-10-12)

**Why.** The owner has sales made before the system went live, and the odd sale rung on paper, that still have to reach FBR. The sandbox accepts an `invoiceDate` at least 7 days back (audit `FBR_SANDBOX_2026-10-12.md`), so the filing side needs only the right date. What needed deciding is how a past-dated sale sits in the store's own books.

### 8.1 Behaviour

- Every invoice carries an **invoice date** (`invoices.invoice_date DATE NOT NULL`), the date printed on the document and sent to FBR. It defaults to the business date and is only ever different when an admin back-dates the sale.
- The sale is still **recorded on the current business day**: today's invoice number, today's drawer, today's day close and every report, which filter on `business_date` and nothing else (invariant 6). Closed days stay sealed; no day row is invented for a date before the system existed.
- **Who:** an admin signed in at the till picks the date from a calendar beside the cart before charging. A counter login cannot back-date; the server refuses with `invoice_date_requires_admin` whatever the client sends. No PIN: the signed-in role is the authority, and the date is printed on the document.
- **How far back:** at most 40 days before the current business date (`invoice_date_too_old`); never after it (`invoice_date_future`). An unparseable date is `invalid_request`. The limit is the store's own guard, not an FBR rule: the sandbox validated sale invoices dated 40, 60 and 90 days back (2026-10-13). It was 30 days until the owner asked for ten more on 2026-10-13.
- **What the customer sees:** the receipt and the A4 invoice print `Invoice date 5 Oct 2026`, then `Recorded 12 Oct 2026, 5:40 pm` and the business day, whenever the two differ. A same-day sale prints exactly as before. The invoice drawer says `Invoice date 5 Oct 2026 (back-dated)` and the invoice list carries a `dated 5 Oct 2026` chip; the till shows an amber line under the calendar while a back-date is set, and the date resets to today after every sale.

### 8.2 FBR

- A single filing (Charge + FBR, the drawer's File with FBR, the queue) sends `invoiceDate = invoice_date`.
- The File with FBR screen's date picker selects sales by **invoice date**, and a combined invoice is dated that date. A back-dated sale therefore rolls up with the other sales of its invoice date, never with today's. `fiscal_batches.business_date` keeps its name and now means "the date the combined invoice is dated"; the eligibility re-check in `CreateBatch` compares `invoice_date`.
- "Pending to file today" on the till and the dashboard counts by invoice date, so it agrees with what the screen lists for today.
- A Debit Note for a voided back-dated sale is dated like any other debit note (the day it is filed); only the sale invoice carries the past date.

### 8.3 Data and API

```
invoices.invoice_date DATE NOT NULL   -- backfilled = business_date; a BEFORE INSERT trigger
                                      -- defaults it to business_date when a writer omits it
idx_invoices_fiscal_unfiled_invoice_date (invoice_date) WHERE fiscal_status IN ('unfiled','failed')
```

`POST /invoices` accepts an optional `invoice_date` (`YYYY-MM-DD`). `GET /invoices/:id` and every invoice projection return `invoice_date`. Error codes added: `invoice_date_requires_admin` (403), `invoice_date_future` (400), `invoice_date_too_old` (400).

### 8.4 Testing

- DB: admin back-dates exactly 40 days → 201, `invoice_date` as sent, `business_date` and the invoice number are today's; counter refused; a future date and a 41-day-old date refused; omitted → equals the business date.
- Payload: a sale's `invoiceDate` is `invoice_date`, not `business_date`.
- Batches: `Eligible` and `CreateBatch` select by `invoice_date`; a sale rung today and back-dated to yesterday is listed under yesterday and refused for today. The pending summary's "today" counts by invoice date.
- Print: a back-dated invoice prints the `Invoice date` and `Recorded` rows; a same-day one prints neither.
- Till: the date helper refuses a future date and one older than 40 days, and accepts today and 40 days ago.
