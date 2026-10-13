# FBR sandbox — back-date ceiling and the first 0053 (2026-10-13)

Two findings from the Unity store's first morning on its own Railway project. Token and raw bodies are not reproduced; the validate calls below used the local sandbox token (NTN 8951943, seller `Elevon Test Seller`, PUNJAB, Lahore) from `backend/.env.sandbox.local`, validate only.

## 1. How far back FBR accepts an `invoiceDate`

The owner asked for ten more days of back-dating (the till allowed 30). The question was whether 30 was an FBR rule. It was not: the limit is the store's own guard from spec `2026-10-12-on-demand-fbr-and-deal-rates-design.md` §8.1.

`POST /di_data/v1/di/validateinvoicedata_sb`, the SN002 single-line LPG body from `scripts/fbr-sandbox-checks.http` check 3, with only `invoiceDate` changed:

| invoiceDate | Days back | Result |
|---|---|---|
| 2026-09-03 | 40 | `00 Valid` |
| 2026-08-14 | 60 | `00 Valid` |
| 2026-07-15 | 90 | `00 Valid` |

The sandbox does not refuse an old date at any distance we tried. What binds in practice is the sales tax return: a sale dated in a month whose return has already been filed has to be explained in that return, so the store's own ceiling exists to stop that happening by accident, not because the gateway would reject it.

**Change:** `backdateLimitDays` (backend) and `BACKDATE_LIMIT_DAYS` (till) raised from 30 to 40; spec §8.1 and §8.4 updated; the DB test pins "exactly 40 days → 201" and "41 → `invoice_date_too_old`"; the till test pins the bounds.

## 2. First rejection on the Unity store: 0053

Sale `20261013-001` to the new customer "Unity LPG Distributors" (NTN field `3277876318252`, buyer type Registered, province ICT), charged with FBR, failed twice:

```
0053  Provided Registration type does not match with Buyer's profile.
      Please verify and provide valid Buyer's registration type
```

The app sent what the customer card says: `buyerNTNCNIC 3277876318252`, `buyerRegistrationType Registered`, scenario SN001. FBR looked the number up and found no active sales tax registration behind it. The number is 13 digits, the shape of a CNIC, not a 7-digit company NTN.

Two correct outcomes, depending on the facts:

- The buyer is an individual or an unregistered business: move the number to the CNIC field, set buyer type **Unregistered**, save, then retry the dead job from the FBR screen. The app then files SN002 with the CNIC as the buyer identifier.
- The buyer really is sales-tax registered: enter its 7-digit NTN (digits only) in the NTN field, keep Registered, save, retry.

The job is rebuilt from the customers row at every attempt (`fiscal.loadBuyer`), so editing the card and retrying is enough; no new sale is needed.

**What the number actually was.** The owner then sent the FBR certificate: Registration No `A723262` (the NTN, with a letter), Reference No `A723262-4`, and the 13-digit number was the STRN. The customer form had refused `A723262` (digits and dashes only), so the card ended up holding `7232624`. Validate calls, seller 8951943, buyer "Unity LPG Distributors", Registered, SN001:

| buyerNTNCNIC | buyerProvince | Result |
|---|---|---|
| 7232624 | ICT | 0053 registration type does not match |
| 723262 | ICT | 0002 not in proper format |
| 3277876318252 (STRN) | ICT | 0205 scenario not valid for unregistered |
| A723262 or 0723262 | ICT | 0074 buyer province invalid |
| A723262 or 0723262 | CAPITAL TERRITORY | 00 Valid |

So FBR accepts the letter-prefixed NTN as issued, or a zero-padded 7-digit form, and the buyer province has to be one of the seven names from `/pdi/v1/provinces` (`ICT` is not one). The STRN plays no part in Digital Invoicing. The owner saved `0723262` + `CAPITAL TERRITORY` and the retry filed.

**Follow-up, 13:00.** FBR's POS Invoice Verification shows the buyer from its own registry: for registration number `0723262` it shows **ALI BILAL, CNIC 3520228922089**, an individual, not the AOP. The zero-padded form validates because it is somebody's real NTN. All six production sales on the Unity store (`20261013-001` … `-006`, all to this customer, all on credit) therefore stand on FBR in the wrong buyer's name. The company's own number is `A723262` as printed.

**Reversal attempt, 13:02.** `20261013-001` was voided with the admin PIN so the app would file a Debit Note against `3792106DI6CQ6AV596379`. Production refused it:

```
0402  Unauthorized access: Provided buyer registration number is not 13 digits
      (CNIC) or 7 digits (NTN) or the authorized token does not exist against
      buyer registration number
```

The buyer number sent was 7 digits, so the second clause applies: FBR expects the Debit Note to be filed under a token registered to the **buyer**. A seller cannot reverse its own filed sale invoice through Digital Invoicing. The sandbox never proved this path (0106 on unregistered buyers on 2026-09-21; today a seller-issued Debit Note against a freshly posted SN001 sale to PRAL's sample buyer 0786909 answered 0510 "Exception occurred"). Stopped there: the other five sales were not voided, the customer card still says `0723262`, and nothing was re-rung, because a second sale invoice would double the output tax on FBR while the first still stands.

State left behind: `20261013-001` is voided locally (ledger credit 2,655,933 posted, balance 14,594,160), `fiscal_void_status = failed`, its Debit Note job dead. Open for the tax advisor or PRAL helpdesk: how a seller corrects a sale invoice filed against the wrong buyer (a credit note in the monthly return, or cancellation by PRAL). The app's void path must stop presenting the Debit Note as a reversal until that is answered.

**Re-filing, 13:12–13:20, on the owner's go-ahead.** With PRAL unreachable the owner chose the return-filing route: the six wrong invoices stay on FBR and are reversed by Credit Notes declared in the monthly sales tax return (rule 20, within 180 days); the sales were re-filed under `A723262`. Sequence: the remaining five voided in the app (each Debit Note died with 0402, as expected); customer card set to NTN `A723262`, Registered, CAPITAL TERRITORY (the first production save of a letter-prefixed NTN, on the build pushed to `main` at 12:57); six new sales rung through `POST /invoices` with `file_fbr`, same product, kg, list rate 198.57, credit, same invoice date. All six synced on the first attempt. Customer balance returned to 17,250,093, exactly the original six.

| Voided (wrong buyer, still on FBR) | FBR number to credit-note | Replacement | FBR number (A723262) | Invoice date | kg | Total |
|---|---|---|---|---|---|---|
| 20261013-001 | 3792106DI6CQ6AV596379 | 20261013-007 | 3792106DI6EMO4C799644 | 2026-09-17 | 11,335 LPG 1910 | 2,655,933 |
| 20261013-002 | 3792106DI6DJC5A870972 | 20261013-011 | 3792106DI6EOJAK076056 | 2026-09-13 | 13,545 LPG 1910 | 3,173,764 |
| 20261013-003 | 3792106DI6DK4VI389824 | 20261013-008 | 3792106DI6ENRE8215315 | 2026-10-13 | 11,335 LPG 1400 | 2,655,933 |
| 20261013-004 | 3792106DI6DKTHR478419 | 20261013-009 | 3792106DI6ENSD5415046 | 2026-10-13 | 13,035 LPG 1400 | 3,054,265 |
| 20261013-005 | 3792106DI6DVWIR931898 | 20261013-012 | 3792106DI6EOK7W067174 | 2026-09-04 | 11,335 LPG 1400 | 2,655,933 |
| 20261013-006 | 3792106DI6DXSK0332055 | 20261013-010 | 3792106DI6ENTNM018156 | 2026-09-30 | 13,035 LPG 1400 | 3,054,265 |

The left-hand column is the list for whoever files Unity Exploration's returns: six Credit Notes, 17,250,093 in all, tax periods 202609 (four) and 202610 (two). Until they are declared FBR holds twelve sale invoices where there should be six.

**Cancelled on the portal, ~13:40.** The FBR DI web portal (seller login) lists the seller's invoices with a **Cancel Invoice** action. The owner cancelled the six Ali Bilal invoices there; the portal shows them `Cancelled` and the replacements `Valid` with buyer **Unity LPG Distributors**, which also confirms `A723262` resolves to the AOP. A cancelled DI invoice should not flow into Annex-C, so the credit-note list above is a fallback only: check the September and October returns' Annex-C in IRIS before filing and raise a credit note only if a cancelled number still appears. The API has no cancel; the portal does.

**Product consequences (open):** the void path must stop presenting a Debit Note as an FBR reversal, since the gateway only accepts one from the buyer's token; the till should warn when a Registered buyer's NTN does not match the certificate shape; the sandbox-only "0723262 validates" trap belongs in the FBR README.

**Product fix (same day).** The customer form, the handler and the payload builder now agree with FBR: an NTN may carry one leading letter and is stored upper case; the payload's `registrationNumber` keeps the letter, drops the certificate's `-N` suffix (`A723262-4` → `A723262`, `3792106-1` → `3792106`) and strips dashes from a CNIC; the province is a dropdown of the seven FBR names (`fiscal.Provinces`, mirrored in `lib/customerSchemas.ts`), the handler canonicalises case and refuses anything else with `invalid_province`, and a row saved before the list existed is mapped onto it when the dialog opens. A warning when the NTN looks like a CNIC under Registered is still not done.
