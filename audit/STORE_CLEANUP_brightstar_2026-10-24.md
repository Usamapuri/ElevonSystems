# Store cleanup — Bright Star LPG — 2026-10-24

Skill `elevon-store-cleanup`. Script `scripts/store-cleanup/cleanup_brightstar_2026-10-24.sql` (no DSN). Access through a TCP proxy the owner added on the store's Postgres (`interchange.proxy.rlwy.net:48421`).

**Status: 🟡 CLEAN WITH NOTES.** Owner confirmed the four FBR cancellations and said go; run live and committed (see "Live run"). Notes: Step 5 items 4, 6, 7.

## Store proved

| | |
|---|---|
| Railway project | `bdf1d8ab-b098-4f0e-abb4-71ddfd818682` (POS-Bright Star LPG) |
| `system_identifier` | `7699438549581037627` (in the script's guard) |
| business_name / seller | BRIGHT STAR LPG MARKETING (PRIVATE) LIMITED, NTN 6827012, PUNJAB |
| FBR | **production** (`is_sandbox false`), filing on, production and sandbox tokens stored |

## FBR filings on this store (kept here because the cleanup erases the local rows)

| Invoice | Date | Rs | Buyer on the invoice | FBR invoice number |
|---|---|---|---|---|
| 20261022-001 | 2026-10-22 | 23,431 | walk-in (SN002) | 6827012DIJJTVZ9503682 |
| 20261022-002 | 2026-10-22 | 11,716 | MUHAMMAD SANGEEN KHAN, NTN **8951943** | 6827012DIJJVYEA928963 |
| 20261024-013 | 2026-10-24 | 11,776 | Bright Star LPG Distributors, J433658 | 6827012DIM7XO0E517720 |
| 20261024-014 | 2026-10-24 | 236 | Bright Star LPG Distributors, J433658 | 6827012DIM8MBDO900972 |

All four `synced` in production; no combined invoices; queue idle. 8951943 is the FBR sandbox test seller's NTN ("Elevon Test Seller"), so `-002` stands on FBR production against that registration. Each of the four must show **Cancelled** on the FBR DI portal before the local records go; any that cannot be cancelled goes on the credit-note list for the October return (tax period 202610).

## Scope

- **Fiscal:** wipe, on the owner's confirmation that all four are cancelled. The guard refuses unless the filed set is exactly these four numbers and there are no batches.
- **Customers:** delete MUHAMMAD SANGEEN KHAN; keep Bright Star LPG Distributors (phone 03185119002, NTN J433658, Registered, CAPITAL TERRITORY, credit allowed, no limit) as is — same end state as delete-and-re-add, and it keeps its id. Its ledger is empty, balance 0.
- **Preserved:** 1 user (admin), 27 settings, 2 products (LPG 1400 / LPG 1910, Rs 199.59, own HS codes), 4 rate-history rows.

## Inspection (read-only)

| Will delete | rows |
|---|---|
| invoices / invoice_lines | 4 / 4 |
| customers | 1 of 2 |
| business_days | 2 |
| day_close_audit_log | 3 |
| fiscal_invoices / fiscal_outbound_jobs / fiscal_audit_events | 4 / 4 / 10 |
| invoice_number_counters | 2 |
| void_log, receipts, ledger, drawer movements, batches, receipt counters | 0 |

Append-only triggers: the four baseline ones, all `BEFORE DELETE OR UPDATE … elevon_append_only()`. Blocking set: exactly the 2026-10-22 baseline, nothing new. Migration 008 (`backdate_limit_days`) is not on the store — expected, it is on `dev` and not deployed; it changes no table this run touches.

## Dry runs

- 10:18 and 10:19 UTC: refused by the idle guard (`20261024-014` rung at 10:09:55 UTC, inside ten minutes). Nothing changed.
- ~10:21 UTC, `commit=0`: `lifted 4 append-only trigger(s)`; `cleanup verified: test data gone, 1 customer kept, config preserved, 4 append-only triggers intact`; `ROLLBACK`. Afterwards, independently: invoices 4, customers 2, fiscal_invoices 4, append-only triggers 4 — unchanged.

## Pre-launch checklist (Step 5, reported, nothing changed)

1. Seller details are this store's own: NTN 6827012, BRIGHT STAR LPG MARKETING (PRIVATE) LIMITED, PUNJAB, "Lahore Punjab".
2. Products carry their own HS codes. `default_hs_code` is set to `2711.1910` (owner-set).
3. Tax: cash, card, online and credit 18 %, further tax 0. Not touched.
4. FBR is production with filing on. `FISCAL_ALLOW_SANDBOX=true` was set on the backend at deploy for the sandbox phase; remove it now that the store files in production.
5. `day_close_enabled` is false: the first sale opens the day.
6. Users: only `admin`. Confirm `INITIAL_ADMIN_PASSWORD` was removed from the backend after the first sign-in.
7. Access: the TCP proxy should be removed after the run.

## Live run

`commit=1`: `lifted 4 append-only trigger(s)`; `cleanup verified: test data gone, 1 customer kept, config preserved, 4 append-only triggers intact`; `COMMIT`.

Independent check on a new connection: invoices 0, invoice_lines 0, business_days 0, day_close_audit_log 0, void_log 0, fiscal_invoices 0, fiscal_outbound_jobs 0, fiscal_audit_events 0, ledger 0, receipts 0, invoice counters 0. Kept: 1 customer (Bright Star LPG Distributors | 03185119002 | J433658 | Registered | CAPITAL TERRITORY | credit allowed), 1 user, 27 settings, 2 products, 4 rate-history rows. Append-only triggers by name: `day_close_audit_append_only`, `fiscal_audit_events_append_only`, `ledger_append_only`, `void_log_append_only` — same set as before.

The next sale is `YYYYMMDD-001` and opens its own business day (day close off).
