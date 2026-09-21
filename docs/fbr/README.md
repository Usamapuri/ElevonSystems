# FBR Digital Invoicing sandbox kit (Phase 7 pre-work)

Phase 7 of the spec (`docs/superpowers/specs/2026-09-19-elevon-lpg-pos-design.md` §7) is FBR Digital Invoicing over the PRAL DI API v1.12. The code port is blocked on the store's IRIS **sandbox** token. This folder lets the owner exercise the exact payloads the app will send before any code is written, so the unknowns in §7 are settled by FBR's own answers rather than by guesswork.

## Files

| File | Purpose |
|---|---|
| `elevon-fbr-di-sandbox.postman_collection.json` | Postman v2.1 collection: reference lists, validate, post, negative cases. Sandbox-only URLs (`_sb`). |
| `elevon-fbr-sandbox.postman_environment.template.json` | Environment template. Import it, fill in the token and seller details **in Postman**, never in this repo. |

## Setup

1. In IRIS, generate the **sandbox** DI token for the store's NTN (it is separate from the production token).
2. Postman → Import → both files.
3. Select the environment and fill in: `token`, `seller_ntn` (digits only), `seller_name`, `seller_province` (exactly as `/pdi/v1/provinces` spells it), `seller_address`. For scenario SN001 also `buyer_ntn` (any registered NTN works in sandbox; use the store's own if nothing else).
4. Collection variables already carry the app's defaults: HS code `2711.1910`, UoM `KG`, rate `18%`, sale type `Goods at Standard Rate (default)`, `trans_type_id` 18. A pre-request script sets `invoice_date` (today, Asia/Karachi) and `ref_date` (`DD-MMM-YYYY`).

## Run order and what each answer decides

**1. Reference lists.** Read-only GETs.
- `Units of measure`: confirm `KG` is id 13.
- `HS_UOM for LPG`: `KG` must be in the list for `2711.1910`. If not, the HS code or annexure is wrong and every invoice would fail with 0099/0165.
- `SaleTypeToRate`: `18%` must appear as a `ratE_DESC`. If the id for "Sale" is not 18 in `Transaction type codes`, change `trans_type_id` and re-run.
- `Document type codes`: note whether "Credit Note" is listed (see E below).

**2. Validate.** Same payloads as Post, but FBR only checks them. Every request asserts `validationResponse.statusCode == "00"`; the Console tab shows per-line `invoiceStatuses`.

| Case | What it proves |
|---|---|
| A. SN002 walk-in, one kg line | The baseline: decimal `quantity` (12.500), blank buyer NTN, `Unregistered`, rounding adjustment not sent. |
| B. SN001 registered buyer, tonne line | Registered scenario with a buyer NTN; 1000.000 kg in one line. |
| C. Two lines with Rs 500 discount | Per-line `discount` and pro-rata split (109.67 + 390.33) with tax on the discounted value. |
| D. Further tax 4 % | Only if the tax advisor says further tax applies; otherwise expect it to still validate with the field filled. |
| E. Credit Note | The doc-type string for voids. v1.12's type list shows only Sale Invoice and Debit Note, but its error catalogue handles credit notes. |

**3. Post.** Files a sandbox invoice and stores the returned `invoiceNumber` in `last_invoice_number`, which E then uses as `invoiceRefNo`. Run A before E.

**4. Negative cases.** Each expects `statusCode "01"`. The interesting one is "Tax does not match rate": the error code tells us whether the sandbox checks paisa-exact totals, which decides how strictly the app must mirror §6.3 in the payload.

## Record the outcome

Copy the Console output for each case into `audit/FBR_SANDBOX_<YYYY-MM-DD>.md` (create it), in particular:

- the `errorCode` and `error` text of any rejection,
- whether `quantity` as a decimal was accepted (spec §7.3 relies on it),
- which of `Credit Note` / `Debit Note` FBR accepted for E,
- the exact `invoiceNumber` format returned.

Those four answers close the open questions in spec §7.3 and §7.6 and let Phase 7 start.

## What is deliberately not here

- No production URLs. The collection cannot file a real invoice.
- No token or NTN in the repo. The environment template is blank on purpose.
- Nothing about the app's own settings API; the point is to test FBR's behaviour, not ours.
