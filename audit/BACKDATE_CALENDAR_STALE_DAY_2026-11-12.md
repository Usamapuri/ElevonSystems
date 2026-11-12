# Sale-date calendar capped at a stale "open" day — 2026-11-12

**Symptom.** On 2026-11-12 the till's sale-date calendar (admin back-dating, spec 2026-10-12 §8) will not accept any date after 2026-10-13. Seen on both stores.

**Root cause.** `frontend/src/routes/_app/pos.tsx:172` derives the calendar's "today" from the business day returned by `GET /day/current`:

```ts
const todayKey = dayCurrent.data?.day ? dayDateKey(dayCurrent.data.day) : businessDateKey(new Date(), ...)
```

and `invoiceDateBounds(todayKey, limitDays)` (`frontend/src/lib/invoiceDate.ts`) sets the input's `max` to that key. `dayops.Current` (`backend/internal/dayops/service.go:280`) returns whichever `business_days` row holds the open slot, whatever its date.

With `day_close_enabled = false` (migration 007, E-05 on 2026-10-13) nobody closes days by hand. A day is sealed only by the *next sale* (`autoSeal` → `autoOpen`, both inside `EnsureOpenDayForInvoice`). Between the last sale and the next one the old day stays `open` in the table. So the till is handed the day of the last sale (2026-10-13 on the store in the screenshot), treats it as "today", and the calendar's upper bound lands a month in the past. The server is not confused: `resolveInvoiceDate` (`handlers/invoices.go:316`) compares against `day.BusinessDate` *after* `EnsureOpenDayForInvoice` has sealed the stale day and opened the real one.

The stale-day read is harmless with the day-close switch **on**, because `evaluateDayGate` then shows "previous day open" and blocks the till until someone closes it. With the switch **off** the gate is forced to `ok`, so the stale day leaks into the calendar unnoticed.

**Side effects of the same line.**
- Picking the date the calendar shows as "today" (the stale 2026-10-13) counts as *not* back-dating (`invoiceDate === todayKey`), so no `invoice_date` is sent and the sale is dated the real business day. The operator sees one date and gets another.
- `checkInvoiceDate` and the earlier-tax-month warning in `CartRail.tsx` measure against the stale key, so the 40-day window is shifted a month back and the tax-period warning fires on the wrong months.
- The dashboard/day-close screens also render the stale day as "open" between sales, which is the existing E-05 design but is what makes this visible.

**Not verified live.** Direct reads of either store's Postgres were not possible from this session: the Railway MCP `list_services` call returned the linked (non-Elevon) project for both explicit project IDs, so it was abandoned per the isolation rule. The diagnosis rests on the code path above plus the screenshot (no gate banner ⇒ switch off; cap = 2026-10-13 ⇒ last sale on that store was 2026-10-13).

**Fix (applied 2026-11-12, `dev`).** `pos.tsx` now computes `todayKey` from `businessDateKey(new Date(), day_boundary_hour)` unconditionally; it already mirrors `util.BusinessDate` in Asia/Karachi. With the switch on, the gate only lets a sale through when the open day *is* today, so nothing changes there. `dayDateKey` is no longer imported by the till. Pinned by `frontend/src/components/pos/saleDateContract.test.ts`, a source-grep contract test in the style of the backend's: `todayKey` must come from `businessDateKey` and the till route may not reference `dayDateKey`. Type-check and all 345 frontend tests pass; the backend is untouched.
