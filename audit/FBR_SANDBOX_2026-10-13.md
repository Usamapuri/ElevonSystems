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

**Product fix (same day).** The customer form, the handler and the payload builder now agree with FBR: an NTN may carry one leading letter and is stored upper case; the payload's `registrationNumber` keeps the letter, drops the certificate's `-N` suffix (`A723262-4` → `A723262`, `3792106-1` → `3792106`) and strips dashes from a CNIC; the province is a dropdown of the seven FBR names (`fiscal.Provinces`, mirrored in `lib/customerSchemas.ts`), the handler canonicalises case and refuses anything else with `invalid_province`, and a row saved before the list existed is mapped onto it when the dialog opens. A warning when the NTN looks like a CNIC under Registered is still not done.
