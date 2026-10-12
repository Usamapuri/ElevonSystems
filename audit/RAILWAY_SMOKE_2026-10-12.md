# Railway smoke run — 2026-10-12

First-hand check of `main` at `f03a871` on the store's Railway environment
(`fbrpos.up.railway.app`), done in Chrome after the owner signed in. Both
services had already rebuilt from the push (backend: new fiscal routes
answer 401; frontend bundle carries the FBR screen, Charge + FBR and the
rate band).

## Blocker found and fixed: the previous app's service worker

The host served a Workbox PWA before Elevon. A browser that used it still
held that worker (`/sw.js`, precache `workbox-precache-v2-…`) and showed its
cached "Not Found" shell instead of Elevon on every visit; Elevon registers
no worker, so nothing ever replaced it. The same browsers carry the old
app's `pos_token`/`pos_user` keys, so an "already signed in" browser from
the old app is signed out of Elevon.

- Cleared by hand in the test tab (unregister + `caches.delete`), after
  which the Elevon sign-in page loaded.
- Fixed for every device by commit `d1dd617` on `dev`: `frontend/public/sw.js`
  is a self-destructing worker served `no-cache` by nginx, pinned by
  `src/lib/legacyServiceWorker.test.ts`. **Not on `main` yet.**

## What was verified

| Area | Result |
|---|---|
| Settings → FBR | Loads; banner says the server has no `FISCAL_SECRETS_KEY` and tokens cannot be stored; both Sandbox and Production slots render with their test panels. Migrations 004/005 applied (status route answers). |
| Settings → Tax | "Rate change without PIN (Rs per kg)" present, value 0. |
| Day close | Closed the stale 21 Sep 2026 demo day (counted = expected) and opened today with a Rs 5,000 float. |
| Till, deal rate | 12.5 kg LPG, rate changed 300 → 290; cart shows struck 300.00 and @ 290.00, Rs 3,625. |
| Band = 0 enforcement | Charge refused by the server, PIN dialog shown. Wrong PIN → "That PIN does not match an active admin", dialog stays. Correct PIN → sale `20261012-004` completed. |
| Invoice drawer | Line shows Rs 290, "list Rs 300 · approved by Store Admin". No "File with FBR" button (filing is off). |
| File with FBR | Business date defaults to the server's 2026-10-12; "Nothing unfiled on this date" (the sale is `off`, not `unfiled`). No combined invoices. |
| Reports → Products | Deal variance −Rs 125 on 12.5 kg. |
| Reports → Tax | Filed / Unfiled / Rejected columns present, 0 · Rs 0 each. |
| Reports → FBR queue | Empty. |
| Dashboard | Renders for 2026-10-12. |

Printing: the till prints after every sale; in a browser that is
`window.print()`, which opens Chrome's print preview and freezes the tab. It
was stubbed in the test tab. Not a defect — the desktop bridge handles it in
production.

## Not verified (blocked on environment)

- Saving a token, Run test, Refresh from FBR, Charge + FBR, File with FBR
  and the FBR block on receipts need `FISCAL_SECRETS_KEY` and
  `FISCAL_ALLOW_SANDBOX=true` on the backend service. The Railway CLI
  session on the dev machine had expired, so neither variable could be set
  or even read from here.
- Registered-buyer (SN001) filing still needs a real registered NTN.

## Observation

Today's first completed sale is numbered `-004`: the three refused charge
attempts (no PIN, wrong PIN, and one cancelled tender) each consumed a daily
number. Numbering has accepted gaps since D6 and FBR does not see local
numbers, so this is by design, but a cashier who retries a PIN will see
gaps.
