# Back-date limit becomes a setting — plan (2026-10-24)

**Ask.** The owner wants back-dating raised again (40 → ~60 days) and would rather the store control it. Today the limit is a constant in two places: `backdateLimitDays = 40` in `backend/internal/handlers/invoices.go` and its till mirror `BACKDATE_LIMIT_DAYS = 40` in `frontend/src/lib/invoiceDate.ts`.

**Status:** owner approved the 90-day cap (2026-10-24). Tasks 1–9 built on `dev`; task 10 (production validate probe) not run. Settings-change audit not requested. Commit scope `invoices(backdate): E-09 — …`.

## How far can it go

| Constraint | What we know | Binds? |
|---|---|---|
| FBR DI gateway | Sandbox validated sale invoices dated 40, 60 and 90 days back, all `00 Valid` (audit `FBR_SANDBOX_2026-10-13.md` §1). No refusal seen at any distance tried. Production not probed. | Not up to 90 in sandbox |
| Sales tax return | A sale dated in a month whose return is already filed must be declared through a revised return. Returns are due mid-next-month, so even today's 40 days reaches into a filed month (40 days before 24 Oct is 14 Sep; September's return was due in October). 60 days reaches into August. | Yes, operationally: tax advisor's call |
| Credit-note window | Sales Tax Rules allow a credit note within 180 days of the supply (the route used for Unity's wrong-buyer invoices). | Outer bound for corrections |
| The app | The limit is the store's own guard (spec 2026-10-12 §8.1). Reports stay on `business_date`, so a back-dated sale still lands in today's day close and reports. | Ours to set |

**Recommendation:** make it a setting, **0–90 days**, seeded at **40** so nothing changes on upgrade. 90 is the furthest FBR has been seen to accept; going past it needs a new sandbox probe first. **0 switches back-dating off** (the calendar disappears). The owner can then set 60 themselves.

## Design

- **Key `backdate_limit_days`**, integer 0–90, not private, admin-writable through `PUT /admin/settings` like every other key. Seeded by migration `008_backdate_limit.sql` (`INSERT … ON CONFLICT DO NOTHING`, value `40`).
- **Server stays the authority.** `Create` already loads settings before its transaction; `resolveInvoiceDate` takes the limit as a parameter instead of reading the constant. A missing or malformed value falls back to 40 (`defaultBackdateLimitDays`), never to "unlimited". Limit 0: any date other than the business date is refused `invoice_date_too_old` with the message "Back-dating is switched off in Settings". Error codes unchanged.
- **Till** reads `settings.backdate_limit_days` (already in the public settings payload). `invoiceDateBounds`/`checkInvoiceDate`/`invoiceDateMessage` take the limit as an argument; the constant goes. Limit 0 hides the Sale date calendar.
- **Settings screen:** Tax tab, beside "Rate change without PIN", field "Back-date sales up to (days)", hint: "Admins only. 0 turns back-dating off. A sale dated in a month whose sales tax return is already filed has to go in a revised return — check with your tax advisor before raising this."
- **Till warning (recommended):** when the chosen sale date is in an earlier calendar month than the business date, the amber line adds "Dated in September — if September's return is filed, this sale needs a revised return." No block, just the warning.
- Not a customer-visible money change (invariant 9 does not apply); no rate, tax base or rounding moves.

## Tasks

1. **Migration.** `backend/migrations/008_backdate_limit.sql` seeds the key. `migrations_idempotent_contract_test.go` covers it automatically.
2. **Settings rule.** `settings.go`: `"backdate_limit_days": notNull(integer(0, 90))` with a comment citing the sandbox ceiling. `settings_test.go`: key count 27 → 28 (and the message), `backdate_limit_days` in the known list, valid `0`/`40`/`90`, invalid `-1`/`91`/`40.5`/`null`/`"40"`.
3. **Handler.** `invoices.go`: replace the constant with `defaultBackdateLimitDays = 40` plus a `settingInt(all, key, fallback)` reader; pass the limit into `resolveInvoiceDate`; message names the actual limit, or "switched off" at 0. Update the doc comment on `models.CreateInvoiceRequest.InvoiceDate`.
4. **DB tests** (`invoices_backdate_db_test.go`): keep "40 → 201, 41 → too_old" as the default case; add setting 60 → 60 days 201, 61 refused; setting 0 → yesterday refused, today 201 for an admin and a counter; malformed stored value → behaves as 40. `settings_db_test.go`: `PUT /admin/settings {"backdate_limit_days": 91}` → 400 `invalid_setting_value`; a counter cannot write it.
5. **Till.** `lib/invoiceDate.ts` functions take `limitDays`; `routes/_app/pos.tsx` passes `settings?.backdate_limit_days ?? 40` and drops the calendar at 0; `CartRail.tsx` amber line gains the earlier-month warning. `types/index.ts` `AppSettings.backdate_limit_days: number`; print fixtures (`lib/print/printInvoice.ts`, `receipt.test.ts`) get the field.
6. **Settings form.** `lib/settings.ts` `taxSchema` gains `backdate_limit_days` (whole number 0–90), `taxFromSettings`/`taxToPatch` map it; `TaxForm.tsx` renders the field. `settings.test.ts` pins the schema bounds and the round trip.
7. **Till tests** (`invoiceDate.test.ts`): bounds and checks at limits 0, 40, 60, 90; month-boundary warning helper.
8. **Docs.** Spec `2026-10-12-on-demand-fbr-and-deal-rates-design.md` §8.1/§8.4: limit is the `backdate_limit_days` setting (default 40, max 90), with the return caveat. Main spec row 9b. CLAUDE.md "the 27 setting keys" → 28.
9. **Verify.** `cd backend && go vet ./... && go test ./...`; `cd frontend && npm run type-check && npm run test`. Then on a store, first-hand: set 60, back-date a sale 60 days, confirm 61 is refused, File with FBR for that date in sandbox.
10. **Optional, before an owner sets more than 40 on a production store:** a validate-only production call (never posts, invariant 7) with an invoice dated at the new limit, so production is proven like the sandbox was. Needs the owner's go-ahead and that store's production token.

## Open for the owner

- **Ceiling.** Is 90 days right, or should it stop at 60? A higher cap needs a fresh sandbox probe.
- **Who changes it.** Any admin, the same as tax rates. Settings changes are not audited today (only `updated_at`); if the owner wants a record of who raised the limit, that is a separate small change.
- **Tax advisor.** Confirm the store is comfortable back-dating into a month whose return is filed. The setting makes it possible, it does not make it right.
