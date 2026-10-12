# FBR DI sandbox run — 2026-10-12

The checks spec `2026-10-12-on-demand-fbr-and-deal-rates-design.md` §5 asked for, run against the PRAL sandbox with the store's IRIS sandbox token (NTN 8951943, seller `Elevon Test Seller`, PUNJAB, Lahore). **Validate only** — nothing was posted. Token and full bodies are not reproduced; the runnable checks are `scripts/fbr-sandbox-checks.http` and the session script that built the many-item payloads.

## Reference lists

| Call | Result | Consequence |
|---|---|---|
| `/pdi/v2/HS_UOM?hs_code=2711.1400&annexure_id=3` | exactly `[{13, KG}]` | The store's second HS code takes **KG only**, the same as 2711.1910. A product on 2711.1400 keeps `fbr_uom` KG (or null → `default_uom`). |
| `/pdi/v2/HS_UOM?hs_code=2711.1910&annexure_id=3` | `[{13, KG}]` | Unchanged since 2026-09-21. |
| `/pdi/v2/SaleTypeToRate?date=12-Oct-2026&transTypeId=75&originationSupplier=1` | `[{728, "18%", 18}]` | `rate_desc "18%"` still the only rate for goods at standard rate. |

## Validate (`validateinvoicedata_sb`), all SN002 walk-in

| Case | Payload | Result |
|---|---|---|
| HS 2711.1400, KG, 12.5 kg, excl 3312.50, ST 596.25 | one line | **Valid (00)**, line valid. 2711.1400 is usable at 18% with KG. |
| Combined invoice, **50 items** on 2711.1910, descriptions `20261012-NNN LPG bulk 12.500 kg` | 50 lines | **Valid (00)**, all 50 lines valid. |
| Combined invoice, **300 items** | 300 lines | **Valid (00)**, all 300 lines valid. |
| Combined invoice dated **2026-10-11** (−1 day) | one line | **Valid (00)**. |
| Combined invoice dated **2026-10-05** (−7 days) | one line | **Valid (00)**. |

## Decisions

1. **`2711.1400` is cleared for use** with UoM `KG` at `18%`. No code change: the HS code is set per product (`products.hs_code`), never defaulted in code (invariant 7), and `Refresh` now caches `HS_UOM` for every product's code so `CheckConsistency` can refuse a wrong unit before filing is enabled.
2. **No item-count cap was met at 300.** Plan B9 step 3's `fiscal_batch_too_large` guard is **not added**; a day with more than 300 lines has not been seen at this store. If FBR ever rejects a large batch, the operator unticks invoices and files in two batches — the screen already supports that.
3. **Back-dated `invoiceDate` is accepted** at least 7 days back. The File with FBR screen's date picker stands as designed; no change.

## Still open

- A **registered buyer** (SN001) still needs a real sales-tax-registered NTN to exercise; the sandbox rejects placeholders with 0205 (as on 2026-09-21). The till's Charge + FBR path for a registered customer is covered by the handler tests against a fake FBR, not by the sandbox.
- The smoke run through the app (plan B9 step 4) is recorded below once done.

## Smoke run through the app

Local dev stack (`docker-compose.dev.yml`, `GIN_MODE=debug`), driven first-hand in Chrome; the three plain sales and the Charge + FBR sale after the first were rung through `POST /invoices` because Chrome's print preview held the till tab.

| Step | Result |
|---|---|
| Settings → FBR: seller profile saved; sandbox token saved (shown `****264a`); **Run test** (sandbox) | **Passed.** Validate then post; FBR number `6110180871403DI4QDHGM740718`, QR rendered, outbound body and both raw responses shown. Two `fiscal_audit_events` rows of kind `test`; nothing in `fiscal_invoices`. |
| Refresh from FBR | Provinces and UoM lists cached; `hs_uom` covers `2711.1910`; province and UoM fields became selects. |
| Filing switched on, saved | Accepted (token present, reference fresh, `rate_desc` = cash rate). |
| Till: LPG Bulk 12.5 kg, rate tapped, 264.50 set (band Rs 1) | Line shows ~~265.00~~ @ 264.50; plain **Charge** → `20261012-001` unfiled, `unit_price 264.50`, `list_rate 265.00`, no approver. |
| **Charge + FBR**, 12.5 kg | `20261012-002` **synced**, FBR `6110180871403DI4QHCQ6547615`, filed inline. |
| Three plain sales (5 / 7.25 / 20 kg) | `-003`, `-004`, `-005` unfiled. `GET /fiscal/pending`: 4 · 44.750 kg · Rs 13,986; the till rail and the dashboard card show the same line. |
| File with FBR: 4 ticked by default, `-005` unticked, **File now** | **Filed.** Combined invoice `6110180871403DI4QIFIV174886` covers 3 sales (24.750 kg, Rs 7,731.96); `-001/-003/-004` → `covered`; the batch row lists the three. |
| Tax report | 18 % band: Filed 4 · Rs 11,640.71 · Unfiled 1 · Rs 6,254 · Rejected 0. FBR queue tab: empty (every filing completed inline). |
| Invoice drawer, `-003` | "FBR: Filed (combined) · 6110…174886 (combined invoice)". **Void** with admin PIN → voided; `fiscal_status` stays `covered`, `fiscal_void_status` `unfiled`; the batch's count and total unchanged; no job, no debit note. |

Not exercised here: a registered buyer's Charge + FBR and its Debit Note on void (needs a real registered NTN in the sandbox — covered by the handler tests against a fake FBR), and the queue path (the sandbox answered every call; covered by `queue_db_test.go`).

## Scenario sign-off (evening)

The sandbox profile for seller UNITY EXPLORATION (PRIVATE) LIMITED (NTN 3792106) listed nine scenarios: SN002, SN001, SN005, SN006, SN007, SN014, SN016, SN017, SN024. SN002 was already Successful from the app's own filings. The other eight were posted once each from `docs/fbr/elevon-fbr-di-scenarios.postman_collection.json` (values from PRAL's reference lists, all validated 00 first; registered buyer MUHAMMAD SANGEEN KHAN, NTN 8951943). **The owner reports all nine Successful on 2026-10-12.** The profile is ready for the production token request.

Findings worth keeping: the bearer token is bound to the seller NTN (401 otherwise); SN017 accepts only petroleum HS codes and computes tax as 18 % of `fixedNotifiedValueOrRetailPrice` + Rs 80/litre; the SRO 297 sale-type string contains a vertical bar.
