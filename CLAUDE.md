# Elevon POS

Single-store point-of-sale for an LPG supplier in Pakistan: a weight-based till (kg / tonne / rupee-amount / gross−tare entry), customers with a credit ledger, day close for cash/card/online, dashboard and reports, weekly rate changes with history, and FBR Digital Invoicing that activates once configured. One Railway project per store (Postgres + Go backend + nginx-served React frontend).

This repo is a **sibling** of Bhookly Retail (`ArtyReal/POS-System-General`, local checkout `../POS-System-General`): files were copied selectively with `bhookly → elevon` renames, nothing else is shared. The design is `docs/superpowers/specs/2026-09-19-elevon-lpg-pos-design.md`; read it before planning work and update it when a phase lands. Phase plans live in `docs/superpowers/plans/`.

## Production state

- **No live store yet.** `dev` is the working branch; `main` deploys. Merge `dev → main` only when explicitly told. Never push to `main` without explicit authorisation.
- Fresh deploy: migrations run at boot (fatal on failure), then `database.EnsureInitialAdmin` creates the first admin from `INITIAL_ADMIN_PASSWORD` (10–72 bytes, optional `INITIAL_ADMIN_USERNAME`) only while no active admin exists. Remove the var after first sign-in. No seeded users, ever.
- Desktop app (Phase 8) will publish to a new `elevon-pos-releases` repo with its own appId and NSIS GUID.

### ⛔ Isolation from Bhookly — non-negotiable
- Never reference a Bhookly or Bhookly Retail tenant, Railway project, release repo (`bhookly-pos-releases`, `bhookly-retail-releases`), appId (`com.bhookly.*`) or `*.bhookly.com` host anywhere in this repo.
- The user-scoped skills `tenant-deploy, tenant-cleanup, railway-tenant, tenant-drift, pral-replay, tax-reconciliation, invoice-sequence-check, pre-deploy-check, review, desktop-dev` are hidden by `.claude/settings.json`; never run them here.
- Railway: only ever target this store's own project, pass explicit project/environment IDs, and confirm every mutating action.

## Tech stack
| Layer | Tech |
|---|---|
| Backend | Go 1.24, Gin, `database/sql` + lib/pq (raw SQL, no ORM), golang-jwt/v5, bcrypt |
| Frontend | React 18 + TypeScript strict, Vite 5, TanStack Router (file-based) + Query, shadcn/ui (vendored), Tailwind 3, RHF + Zod, recharts, vitest (node env, logic tests only) |
| Database | Postgres, one per store; every pooled session opens with `TimeZone=Asia/Karachi` |
| Fiscal | FBR Digital Invoicing (PRAL DI API v1.12) — Phase 7 |
| Deploy | Railway: backend Dockerfile, frontend nginx Dockerfile proxying `/api` to the backend's private domain |

## Key directories
```
backend/main.go                     boot: env → DB → Migrate → EnsureInitialAdmin → router
backend/migrations/NNN_*.sql        embedded, applied in order, each idempotent; cmd/migrate applies without booting
backend/internal/api/routes.go      ALL route registration; every route inside RequireRoles
backend/internal/middleware/auth.go JWT (24h, X-POS-JWT fallback), CheckTokenNotRevoked fails closed, RequireRoles
backend/internal/util/roles.go      canonical roles ↔ frontend/src/lib/roles.ts
backend/internal/util/timewindow.go business timezone + boundary-hour BusinessDate()
backend/internal/testdb/            DB-backed test harness (TEST_DATABASE_URL; skips loudly)
frontend/src/api/client.ts          the only place that talks HTTP
frontend/src/lib/print/transport.ts window.elevon desktop bridge seam (browser falls back to window.print)
frontend/src/routes/                file-based routes; routeTree.gen.ts is generated — never hand-edit
```

## Critical invariants (DO NOT regress) — each pinned by a test
1. Every route in `routes.go` sits inside `RequireRoles` except `POST /auth/login` (`routes_role_gates_contract_test.go`).
2. `APIResponse` envelope on every response; `error` codes are stable snake_case; no `err.Error()` reaches a client.
3. `JWT_SECRET` is per store and mandatory in release mode; never hardcoded.
4. Migrations are idempotent DDL (`migrations_idempotent_contract_test.go`) and `Migrate` runs before `EnsureInitialAdmin` (`boot_order_contract_test.go`).
5. `SetTrustedProxies(nil)` stays (`boot_order_contract_test.go`).
6. (from Phase 3) invoice money is computed server-side from `products.rate`; Go and TS pricing share one fixture; `void_log`, `customer_ledger_entries`, `day_close_audit_log`, `fiscal_audit_events` are append-only; reports filter on `business_date` only; every invoice has a `business_day_id` and there is no auto-open.
7. (from Phase 7) no HS-code default in code; sandbox validate URL is `_sb`; sandbox config in release refuses; retries consult `fiscal_invoices` first.
8. Customer-visible money changes (rates, rounding, tax base, further tax) are stop-and-ask: compute before/after and put it to the owner first.

## Commit conventions
`<scope>(<subsystem>): E-NN — <short imperative>` (NN = phase). Sign Claude-authored commits with `Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>`. Never `--no-verify`, never amend a published commit.

## Working norms
- Run the suites before calling anything merge-ready: `cd backend && go vet ./... && go test ./...`; `cd frontend && npm run type-check && npm run test`.
- Persist long-form findings in `audit/<TOPIC>_<YYYY-MM-DD>.md` or `docs/`, never chat-only.
- Risky operations (push to `main`, `git reset`, `rm -rf`, destructive SQL, anything touching Railway) need explicit per-scope confirmation.
- The user is the engineer/owner: frame explanations operationally.
