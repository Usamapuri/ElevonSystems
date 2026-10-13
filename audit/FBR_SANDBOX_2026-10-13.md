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

Same class of mistake as yesterday's 0205 on the test store ("Sangeen Khan" saved Unregistered with an NTN): the customer form lets NTN and buyer type disagree with FBR's registry. A form warning when the NTN looks like a CNIC under Registered, or a lookup against FBR's registration check, is the product fix; not done yet.
