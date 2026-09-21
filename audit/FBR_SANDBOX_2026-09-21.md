# FBR DI sandbox run — 2026-09-21

First contact with the PRAL Digital Invoicing sandbox using the store's IRIS sandbox token (NTN 8951943). Run from the Postman kit in `docs/fbr/` via curl. Token and full responses are not reproduced here; the payloads are the collection's cases verbatim with `sellerNTNCNIC 8951943`, `sellerProvince PUNJAB`, placeholder seller name/address.

## Reference lists

| Call | Result | Consequence |
|---|---|---|
| `/pdi/v1/provinces` | 7 rows, upper-case names: `PUNJAB`, `SINDH`, `KHYBER PAKHTUNKHWA`, `BALOCHISTAN`, `CAPITAL TERRITORY`, `AZAD JAMMU AND KASHMIR`, `GILGIT BALTISTAN` | `fiscal_config.seller_province` and `customers.province` must be stored/sent in this exact casing. The settings dropdown should be fed from this list, not typed. |
| `/pdi/v1/uom` | `KG` is `uoM_ID 13` (also `Kilogram` 63, `MT` 3/120) | Spec §7.3 confirmed. Send `"KG"`. |
| `/pdi/v2/HS_UOM?hs_code=2711.1910&annexure_id=3` | exactly `[{13, KG}]` | HS code 2711.1910 with KG is accepted by FBR. Advisor still confirms the classification, but the API will not reject it. |
| `/pdi/v1/transtypecode` | 26 rows. **`Goods at standard rate (default)` = id 75.** Id 18 is `Services`. `Gas to CNG stations` = 77 (not ours). | Spec §7.2 `trans_type_id` default must be **75**. The Postman kit shipped with 18 and has been corrected. |
| `/pdi/v2/SaleTypeToRate?date=21-Sep-2026&transTypeId=75&originationSupplier=1` | `[{ratE_ID 728, "18%", 18}]` | `rate_desc "18%"` is valid for goods at standard rate. (With id 18 the list is the services set: 0%, Exempt, 5%, 15%, 16%, 17%, 18.5%, … and has no 18%.) |
| `/pdi/v1/doctypecode` | `Sale Invoice` (4), `Debit Note` (9) only | No `Credit Note` type exists. See below. |

## Validate (`validateinvoicedata_sb`)

| Case | Payload | Result |
|---|---|---|
| A | SN002, walk-in, `quantity 12.5`, excl 3312.50, ST 596.25 | **Valid (00)**, line 1 valid. Decimal quantity is accepted; spec §7.3 stands. |
| B | SN001, buyer NTN = seller NTN | Invalid, line error **0058** "Buyer and Seller Registration number are same. Self invoicing is not allowed." |
| B' | SN001, buyer NTN `1234567` | Invalid, **0205** "Provided scenario not valid for unregistered user". The sandbox checks the buyer NTN against the sales-tax register; a real registered NTN is needed to exercise SN001. |
| C | SN002, two lines, Rs 500 discount split 109.67 / 390.33, ST on the discounted value | **Valid (00)**, both lines valid. The §6.3 pro-rata discount and per-line `discount` field pass. |
| D | SN002, `furtherTax 132.50` (4 %), totalValues 4041.25 | **Valid (00)**. |
| E1 | `invoiceType "Credit Note"`, `invoiceRefNo` = posted A | Invalid, **0003** "Provided invoice type is not valid". |
| E2 | `invoiceType "Debit Note"`, same ref | Invalid, **0027** "Reason is mandatory requirement for debit note". |
| E3 | Debit Note + `"reason": "Sale voided at the till"` | Moves past 0027 to **0106** "The Buyer is not registered for sales tax. Please provide a valid Registration No./NTN." Field bisected: only `reason` is recognised (`reasonRemarks`, `reasonId`, `debitNoteReason`, `dnReason`, `remarks` all still give 0027). |
| E4 | Debit Note + reason, buyer = 13-digit CNIC | Still **0106**. |

## Post (`postinvoicedata_sb`)

Case A posted once. Response: `invoiceNumber "6110180871403DIAJGEJ4031308"`, line `invoiceNo "…-1"`, `dated "2026-09-21 05:57:45"`. The number format is `<13 digits>DI<letters/digits>`, longer than the spec's example; store it as an opaque string (spec §7.4 already does).

## Negative cases

| Case | Error |
|---|---|
| UoM `Liter` on 2711.1910 | **0099** "Provided UoM is not allowed against the provided HS Code" |
| ST 500.00 instead of 596.25 | **0104** "Provided sales tax amount does not match the calculated sales tax amount" — the sandbox recomputes tax from `valueSalesExcludingST` × rate, so the payload must carry §6.3's exact per-line figures. |
| HS `0000.0000` | **0044** "HS Code cannot be empty" |
| SN001 with blank buyer NTN | **0205** "Provided scenario not valid for unregistered user" |

Rejections come back as HTTP 200 with `statusCode "01"`; invoice-level errors (`0003`, `0027`, `0106`, `0205`) sit on `validationResponse` with `invoiceStatuses: null`, line-level errors (`0058`, `0099`, `0104`, `0044`) sit on `invoiceStatuses[i]`. The client must read both.

## Decisions for Phase 7

1. **Doc types are `Sale Invoice` and `Debit Note` only.** Spec §7.6's "Credit Note" fallback is dead; the void path files a **Debit Note** with `invoiceRefNo` = the original FBR number and a mandatory top-level `reason` string (the void reason the cashier typed).
2. **A debit note needs a registered buyer (0106).** Voids of walk-in / unregistered sales cannot be reversed through DI in the sandbox. Open question for the tax advisor: how are voided unregistered sales to be handled (not filed at all, or a sales return in the monthly return). Until answered the app should void locally, mark `fiscal_status = void_unfiled`, and file a debit note only when the original buyer was registered.
3. **`trans_type_id` = 75**, `rate_desc "18%"`, provinces upper-case from the reference list, `uoM "KG"`, HS `2711.1910` all confirmed against the API.
4. **Tax is recomputed server-side by FBR (0104)**, so `salesTaxApplicable` must be `round2(valueSalesExcludingST × 18 %)` per line, which is exactly §6.3's `line_tax`. No tolerance observed; keep the paisa-exact pricing package as the only source of these numbers.
5. Still untested for lack of a registered buyer NTN: SN001, and the full Debit Note happy path. The owner should supply the NTN of any registered customer to close both.
