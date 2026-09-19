# Phase 0 — Scaffold Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A bootable Elevon POS skeleton: Go backend with health check, embedded migrations, first admin from env, JWT login; React shell with login page, role-guarded layout, sidebar with placeholder screens, theme, API client and the desktop print-bridge stub; Docker/Railway/CI wiring; CLAUDE.md and Claude settings.

**Architecture:** Two deployables (`backend/` Go + Gin + raw SQL on Postgres, `frontend/` React + Vite served by nginx) in one repo, copied selectively from `../POS-System-General` (the Bhookly Retail checkout, referred to below as **RETAIL**) with every `bhookly` identifier renamed to `elevon`. Migrations are embedded SQL applied in order and recorded in `schema_migrations`; failure is fatal. Every API route is registered inside `RequireRoles`.

**Tech Stack:** Go 1.24, Gin 1.9, lib/pq, golang-jwt/v5, bcrypt; React 18.3, TypeScript 5.6 strict, Vite 5, TanStack Router 1.57 (file-based) + Query 5, shadcn/ui (vendored), Tailwind 3.4, vitest 2 (node env); Postgres 15/16; Docker; Railway; GitHub Actions.

**Spec:** `docs/superpowers/specs/2026-09-19-elevon-lpg-pos-design.md` (§4 architecture, §5.1 users, §5.7 settings, §5.8 migrations, §6.1 auth, §6.9 API conventions, §8.1 frontend stack, §9 copy list, §11 env, §12 isolation, §14 CLAUDE.md).

## Global Constraints

- Go module name `elevon-backend`; Go `1.24`. Frontend package name `elevon-pos-frontend`.
- Brand strings: app name `Elevon POS`, company `Elevon Systems`, desktop bridge `window.elevon`, localStorage keys `elevon_token`, `elevon_user`, `elevon-theme`.
- Roles are exactly `admin` and `counter` (`backend/internal/util/roles.go` ↔ `frontend/src/lib/roles.ts`).
- Every response is `models.APIResponse{success, message, data?, error?}`; `error` is a stable snake_case code; never `err.Error()` in a response.
- Every non-`/auth/login` route in `routes.go` is registered on a group that carries `middleware.RequireRoles`.
- Migrations: `backend/migrations/NNN_name.sql`, idempotent DDL, applied in name order, fatal on failure.
- `JWT_SECRET` required when `GIN_MODE=release`. `INITIAL_ADMIN_PASSWORD` 10–72 bytes.
- No reference anywhere to Bhookly tenants, `*.bhookly.com`, `com.bhookly.*`, `bhookly-pos-releases`, `bhookly-retail-releases`, or a Railway project ID.
- Commit format `<scope>(<subsystem>): E-00 — <imperative>` ending with `Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>`. Work on branch `dev`. Never push to `main`.
- Green gate: `cd backend && go vet ./... && go test ./...`; `cd frontend && npm run type-check && npm run test`.
- RETAIL path used in copy commands: `../POS-System-General` relative to the ElevonSystems repo root.

---

## File structure

```
ElevonSystems/
  .gitignore  .env.example  README.md  CLAUDE.md  Makefile  docker-compose.dev.yml
  .claude/settings.json
  .github/workflows/backend-tests.yml  frontend-checks.yml
  backend/
    go.mod  go.sum  main.go  boot_order_contract_test.go
    Dockerfile  Dockerfile.dev  .air.toml  railway.json  .env.example
    cmd/migrate/main.go
    migrations/embed.go  migrations/001_init.sql
    internal/
      util/timewindow.go  timewindow_test.go  roles.go
      models/response.go  user.go
      database/connection.go  connection_test.go  migrate.go  migrate_test.go
               migrations_idempotent_contract_test.go  initial_admin.go  initial_admin_test.go
      testdb/testdb.go
      middleware/auth.go  auth_test.go
      handlers/auth.go
      api/routes.go  routes_role_gates_contract_test.go
  frontend/
    package.json  vite.config.ts  tsconfig.json  tsconfig.node.json  tailwind.config.js
    postcss.config.js  index.html  Dockerfile  nginx.conf.template  docker-entrypoint.sh
    railway.json  .env.example
    src/
      main.tsx  index.css  vite-env.d.ts  routeTree.gen.ts (generated)
      types/index.ts
      api/client.ts  api/client.test.ts
      lib/utils.ts  lib/money.ts  lib/roles.ts  lib/roles.test.ts  lib/print/transport.ts
      hooks/use-toast.ts  hooks/useMediaQuery.ts
      contexts/ThemeContext.tsx
      components/ui/*.tsx (copied)  components/shell/Sidebar.tsx  UserMenu.tsx  PlaceholderPage.tsx
      routes/__root.tsx  index.tsx  login.tsx  _app.tsx
      routes/_app/pos.tsx  day-close.tsx  customers.tsx  dashboard.tsx  invoices.tsx  reports.tsx  rates.tsx  settings.tsx
```

---

### Task 1: Repo hygiene, Claude settings, CLAUDE.md

**Files:**
- Create: `.gitignore`, `.env.example`, `README.md`, `CLAUDE.md`, `.claude/settings.json`

**Interfaces:**
- Produces: the isolation rules and test commands every later task follows.

- [ ] **Step 1: Write `.gitignore`**

```gitignore
# env
.env
.env.*
!.env.example
backend/.env
frontend/.env

# build / deps
node_modules/
frontend/dist/
backend/tmp/
backend/main
backend/main.exe
electron/dist/
electron/build/
*.log
coverage/

# editor / OS
.vscode/
.idea/
.DS_Store
Thumbs.db

# local Claude settings (project settings.json IS committed)
.claude/settings.local*.json
.claude/scheduled_tasks.lock

# Vite
frontend/vite.config.ts.timestamp-*.mjs

# credentials — never commit
*.p8
*.p12
*.pem
*.cer
*.key
```

- [ ] **Step 2: Write `.env.example`** (root; read by docker-compose)

```dotenv
# docker-compose.dev.yml reads this file. Copy to .env and edit.
DB_USER=postgres
DB_PASSWORD=postgres123
DB_NAME=elevon_pos
POSTGRES_HOST_PORT=5432

# Backend (dev defaults; production values live on Railway)
GIN_MODE=debug
JWT_SECRET=dev-only-change-me
CORS_ORIGINS=http://localhost:3000,http://127.0.0.1:3000
# First boot only: creates the first admin while no active admin exists. Remove after sign-in.
INITIAL_ADMIN_PASSWORD=
INITIAL_ADMIN_USERNAME=admin

# Frontend dev server
VITE_API_URL=http://localhost:8080/api/v1
```

- [ ] **Step 3: Write `.claude/settings.json`**

```json
{
  "$comment": "Elevon POS is a sibling of the Bhookly / Bhookly Retail products, never a fork of their deployments. The user-scoped skills below embed Bhookly Railway project IDs, repos and *.bhookly.com tenants; they are hidden here so they can never run against that fleet from this repo. Railway is allowed, but every mutating action must name the explicit Elevon project/environment ID and be confirmed first.",
  "skillOverrides": {
    "tenant-deploy": "off",
    "tenant-cleanup": "off",
    "railway-tenant": "off",
    "tenant-drift": "off",
    "pral-replay": "off",
    "tax-reconciliation": "off",
    "invoice-sequence-check": "off",
    "pre-deploy-check": "off",
    "review": "off",
    "desktop-dev": "off"
  }
}
```

- [ ] **Step 4: Write `README.md`**

```markdown
# Elevon POS

Point-of-sale for an LPG supplier: weight-based till, customer credit ledger, day close, reports, FBR Digital Invoicing.

- Design spec: `docs/superpowers/specs/2026-09-19-elevon-lpg-pos-design.md`
- Plans: `docs/superpowers/plans/`
- Local dev: `cp .env.example .env`, set `INITIAL_ADMIN_PASSWORD`, then `docker compose -f docker-compose.dev.yml up`. Frontend http://localhost:3000, API http://localhost:8080/health.
- Tests: `cd backend && go vet ./... && go test ./...`; `cd frontend && npm run type-check && npm run test`.
```

- [ ] **Step 5: Write `CLAUDE.md`**

```markdown
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
```

- [ ] **Step 6: Commit**

```bash
git add .gitignore .env.example README.md CLAUDE.md .claude/settings.json
git commit -m "chore(repo): E-00 — repo hygiene, Claude settings and CLAUDE.md

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 2: Backend module, business-time util, response model, DB connection, `/health`

**Files:**
- Create: `backend/go.mod`, `backend/internal/util/timewindow.go`, `backend/internal/util/timewindow_test.go`, `backend/internal/util/roles.go`, `backend/internal/models/response.go`, `backend/internal/database/connection.go`, `backend/internal/database/connection_test.go`, `backend/main.go`

**Interfaces:**
- Produces: `util.BusinessDate(t time.Time) time.Time`, `util.BusinessTimezoneName() string`, `util.SetDayBoundaryHour(int)`, `util.RoleAdmin`, `util.RoleCounter`, `util.AllRoles []string`, `util.ValidRole(string) bool`, `models.APIResponse`, `models.StringPtr(string) *string`, `database.OpenPostgres(dsn string) (*sql.DB, error)`, `database.Connect(Config)`.

- [ ] **Step 1: Init the module**

```bash
cd backend
go mod init elevon-backend
go get github.com/gin-gonic/gin@v1.9.1 github.com/gin-contrib/cors@v1.5.0 github.com/lib/pq@v1.10.9 github.com/golang-jwt/jwt/v5@v5.2.0 github.com/google/uuid@v1.5.0 github.com/joho/godotenv@v1.5.1 golang.org/x/crypto@v0.48.0
```
Then edit `go.mod` so the first lines read `module elevon-backend` / `go 1.24.0`.

- [ ] **Step 2: Write the failing timewindow test** — `backend/internal/util/timewindow_test.go`

```go
package util

import (
	"testing"
	"time"
)

func TestBusinessDate_BoundaryHourShiftsEarlyMorningToPreviousDay(t *testing.T) {
	SetDayBoundaryHour(2)
	defer SetDayBoundaryHour(0)
	loc := BusinessLocation()
	at := time.Date(2026, 9, 19, 0, 30, 0, 0, loc) // 00:30 with a 2 AM boundary
	got := BusinessDate(at)
	want := time.Date(2026, 9, 18, 0, 0, 0, 0, loc)
	if !got.Equal(want) {
		t.Fatalf("BusinessDate = %v, want %v", got, want)
	}
}

func TestBusinessDate_DefaultBoundaryIsMidnight(t *testing.T) {
	SetDayBoundaryHour(0)
	loc := BusinessLocation()
	at := time.Date(2026, 9, 19, 0, 30, 0, 0, loc)
	if got := BusinessDate(at); got.Day() != 19 {
		t.Fatalf("with boundary 0, 00:30 must stay on the 19th, got %v", got)
	}
}

func TestSetDayBoundaryHour_RejectsOutOfRange(t *testing.T) {
	SetDayBoundaryHour(0)
	SetDayBoundaryHour(13)
	if DayBoundaryHour() != 0 {
		t.Fatalf("out-of-range hour must be ignored, got %d", DayBoundaryHour())
	}
}

func TestBusinessTimezoneName(t *testing.T) {
	if BusinessTimezoneName() != "Asia/Karachi" {
		t.Fatal("business timezone must be Asia/Karachi")
	}
}
```

- [ ] **Step 3: Run to verify it fails**

Run: `cd backend && go test ./internal/util/`
Expected: FAIL (package has no source files / undefined symbols).

- [ ] **Step 4: Write `backend/internal/util/timewindow.go`**

```go
// Package util holds small, dependency-free helpers used across the backend.
package util

import (
	"sync"
	"time"
)

// BusinessTimezone is the only place this string lives. The DB connection
// layer pins every Postgres session to the same zone (database.connection.go)
// so SQL casts and Go formatting agree on what "midnight" means.
const BusinessTimezone = "Asia/Karachi"

var (
	businessLocOnce sync.Once
	businessLoc     *time.Location

	dayBoundaryMu   sync.RWMutex
	dayBoundaryHour = 0 // LPG shop default: midnight. Overridden from settings.day_boundary_hour at boot.
)

// BusinessLocation returns the business *time.Location, cached. Falls back to
// a fixed UTC+5 zone if the container has no tzdata.
func BusinessLocation() *time.Location {
	businessLocOnce.Do(func() {
		if loc, err := time.LoadLocation(BusinessTimezone); err == nil {
			businessLoc = loc
			return
		}
		businessLoc = time.FixedZone("PKT", 5*60*60)
	})
	return businessLoc
}

// BusinessTimezoneName returns the IANA name in use.
func BusinessTimezoneName() string { return BusinessTimezone }

// SetDayBoundaryHour sets the hour (0–12) at which a new business day starts.
// Out-of-range values are ignored so a bad setting can never shift reports.
func SetDayBoundaryHour(hour int) {
	if hour < 0 || hour > 12 {
		return
	}
	dayBoundaryMu.Lock()
	dayBoundaryHour = hour
	dayBoundaryMu.Unlock()
}

// DayBoundaryHour returns the configured boundary hour.
func DayBoundaryHour() int {
	dayBoundaryMu.RLock()
	defer dayBoundaryMu.RUnlock()
	return dayBoundaryHour
}

// BusinessNow is time.Now() in the business timezone.
func BusinessNow() time.Time { return time.Now().In(BusinessLocation()) }

// BusinessDate returns the business day (midnight, business tz) that t falls
// in: convert to business time, subtract the boundary hour, truncate.
func BusinessDate(t time.Time) time.Time {
	local := t.In(BusinessLocation())
	shifted := local.Add(-time.Duration(DayBoundaryHour()) * time.Hour)
	return time.Date(shifted.Year(), shifted.Month(), shifted.Day(), 0, 0, 0, 0, BusinessLocation())
}
```

- [ ] **Step 5: Run to verify it passes**

Run: `cd backend && go test ./internal/util/`
Expected: PASS (4 tests).

- [ ] **Step 6: Write `backend/internal/util/roles.go`**

```go
package util

// Canonical staff roles. Mirrors frontend/src/lib/roles.ts and the
// users_role_check constraint in migrations/001_init.sql.
const (
	RoleAdmin   = "admin"
	RoleCounter = "counter"
)

// AllRoles lists every role, for route groups every signed-in user may reach.
var AllRoles = []string{RoleAdmin, RoleCounter}

// ValidRole reports whether s is a known role.
func ValidRole(s string) bool {
	for _, r := range AllRoles {
		if r == s {
			return true
		}
	}
	return false
}
```

- [ ] **Step 7: Write `backend/internal/models/response.go`**

```go
// Package models holds request/response DTOs shared by handlers.
package models

// APIResponse is the envelope on every response. Message is what a person
// reads; Error is a stable snake_case code the frontend branches on.
type APIResponse struct {
	Success bool        `json:"success"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
	Error   *string     `json:"error,omitempty"`
}

// PaginatedResponse wraps a list with paging metadata.
type PaginatedResponse struct {
	Success bool        `json:"success"`
	Message string      `json:"message"`
	Data    interface{} `json:"data"`
	Meta    MetaData    `json:"meta"`
}

// MetaData is pagination metadata.
type MetaData struct {
	CurrentPage int `json:"current_page"`
	PerPage     int `json:"per_page"`
	Total       int `json:"total"`
	TotalPages  int `json:"total_pages"`
}

// StringPtr returns a pointer to s, for APIResponse.Error.
func StringPtr(s string) *string { return &s }

// Fail builds a failure envelope with a stable error code.
func Fail(message, code string) APIResponse {
	return APIResponse{Success: false, Message: message, Error: StringPtr(code)}
}

// OK builds a success envelope.
func OK(message string, data interface{}) APIResponse {
	return APIResponse{Success: true, Message: message, Data: data}
}
```

- [ ] **Step 8: Write the failing connection test** — `backend/internal/database/connection_test.go`

```go
package database

import (
	"strings"
	"testing"
)

func TestInjectBusinessTimezoneOption_URLForm(t *testing.T) {
	got := injectBusinessTimezoneOption("postgres://u:p@host:5432/db?sslmode=disable")
	if !strings.Contains(got, "options=-c+TimeZone%3DAsia%2FKarachi") {
		t.Fatalf("URL DSN must carry the TimeZone option, got %s", got)
	}
}

func TestInjectBusinessTimezoneOption_KeywordForm(t *testing.T) {
	got := injectBusinessTimezoneOption("host=h user=u dbname=d")
	if !strings.Contains(got, "options='-c TimeZone=Asia/Karachi'") {
		t.Fatalf("keyword DSN must carry the TimeZone option, got %s", got)
	}
}

func TestInjectBusinessTimezoneOption_RespectsOperatorOptions(t *testing.T) {
	in := "postgres://u:p@host/db?options=-c%20TimeZone%3DUTC"
	if got := injectBusinessTimezoneOption(in); got != in {
		t.Fatalf("operator-set options must not be clobbered, got %s", got)
	}
}
```

- [ ] **Step 9: Run to verify it fails**

Run: `cd backend && go test ./internal/database/`
Expected: FAIL, `injectBusinessTimezoneOption` undefined.

- [ ] **Step 10: Write `backend/internal/database/connection.go`**

```go
// Package database owns the Postgres pool, migrations and first-admin bootstrap.
package database

import (
	"database/sql"
	"fmt"
	"log"
	"net/url"
	"strings"
	"time"

	_ "github.com/lib/pq"

	"elevon-backend/internal/util"
)

// Config is the discrete-variable form of a connection (local dev).
type Config struct {
	Host, Port, User, Password, DBName, SSLMode string
}

// Connect opens a pool from discrete variables.
func Connect(c Config) (*sql.DB, error) {
	dsn := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		c.Host, c.Port, c.User, c.Password, c.DBName, c.SSLMode)
	return open(injectBusinessTimezoneOption(dsn))
}

// OpenPostgres opens a pool from a libpq string or postgres:// URL (Railway DATABASE_URL).
func OpenPostgres(dsn string) (*sql.DB, error) {
	return open(injectBusinessTimezoneOption(dsn))
}

func open(dsn string) (*sql.DB, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(5 * time.Minute)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	log.Println("database connection established")
	return db, nil
}

// injectBusinessTimezoneOption appends `options=-c TimeZone=<business tz>` so
// every pooled session opens in business time. Postgres resolves
// `timestamp = timestamptz` comparisons with the SESSION TimeZone; Railway
// defaults to UTC, which shifted hourly report buckets by exactly 5 hours in
// the retail product. An operator-supplied options= is left untouched.
func injectBusinessTimezoneOption(dsn string) string {
	tz := util.BusinessTimezoneName()
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		u, err := url.Parse(dsn)
		if err != nil {
			return dsn
		}
		q := u.Query()
		if q.Get("options") != "" {
			return dsn
		}
		q.Set("options", "-c TimeZone="+tz)
		u.RawQuery = q.Encode()
		return u.String()
	}
	if strings.Contains(dsn, "options=") {
		return dsn
	}
	return dsn + fmt.Sprintf(" options='-c TimeZone=%s'", tz)
}
```

- [ ] **Step 11: Run to verify it passes**

Run: `cd backend && go test ./internal/database/`
Expected: PASS (3 tests).

- [ ] **Step 12: Write `backend/main.go`** (health only for now; Task 3–5 add migrate, admin, routes)

```go
package main

import (
	"database/sql"
	"log"
	"os"
	"strings"

	"elevon-backend/internal/database"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
)

func main() {
	if err := godotenv.Load(); err != nil {
		log.Println("no .env file, using environment")
	}

	db, err := openDB()
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	defer db.Close()

	gin.SetMode(getEnv("GIN_MODE", "release"))
	router := gin.New()
	// Default-deny proxy headers so c.ClientIP() is the TCP peer and IP-keyed
	// rate limits cannot be spoofed via X-Forwarded-For.
	if err := router.SetTrustedProxies(nil); err != nil {
		log.Fatalf("trusted proxies: %v", err)
	}
	router.Use(gin.Logger(), gin.Recovery())
	router.Use(cors.New(corsConfig()))

	router.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "healthy", "message": "Elevon POS API is running"})
	})

	port := getEnv("PORT", "8080")
	log.Printf("listening on :%s", port)
	if err := router.Run(":" + port); err != nil {
		log.Fatalf("server: %v", err)
	}
}

func openDB() (*sql.DB, error) {
	if dsn := os.Getenv("DATABASE_URL"); dsn != "" {
		return database.OpenPostgres(dsn)
	}
	return database.Connect(database.Config{
		Host:     getEnv("DB_HOST", "postgres"),
		Port:     getEnv("DB_PORT", "5432"),
		User:     getEnv("DB_USER", "postgres"),
		Password: getEnv("DB_PASSWORD", "postgres123"),
		DBName:   getEnv("DB_NAME", "elevon_pos"),
		SSLMode:  getEnv("DB_SSLMODE", "disable"),
	})
}

func corsConfig() cors.Config {
	origins := strings.Split(getEnv("CORS_ORIGINS", "http://localhost:3000,http://127.0.0.1:3000"), ",")
	for i := range origins {
		origins[i] = strings.TrimSpace(origins[i])
	}
	return cors.Config{
		AllowOrigins:     origins,
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Content-Length", "Accept", "Authorization", "X-POS-JWT", "Cache-Control", "X-Requested-With"},
		ExposeHeaders:    []string{"Content-Disposition"}, // report downloads read the server filename
		AllowCredentials: true,
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
```

- [ ] **Step 13: Build and vet**

Run: `cd backend && go mod tidy && go vet ./... && go build ./...`
Expected: no output, exit 0.

- [ ] **Step 14: Commit**

```bash
git add backend
git commit -m "backend(boot): E-00 — Go module, business-time util, DB pool with session timezone, /health

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 3: Embedded migrations runner, first migration, DB test harness, `cmd/migrate`

**Files:**
- Create: `backend/migrations/embed.go`, `backend/migrations/001_init.sql`, `backend/internal/database/migrate.go`, `backend/internal/database/migrate_test.go`, `backend/internal/database/migrations_idempotent_contract_test.go`, `backend/internal/testdb/testdb.go`, `backend/cmd/migrate/main.go`
- Modify: `backend/main.go` (call `database.Migrate` after connect)

**Interfaces:**
- Consumes: `database.OpenPostgres`.
- Produces: `migrations.FS embed.FS`, `database.Migrate(db *sql.DB) error`, `database.MigrationNames(fsys fs.FS) ([]string, error)`, `testdb.Fresh(t *testing.T) *sql.DB` (fresh schema, migrated, skips when `TEST_DATABASE_URL` unset).

- [ ] **Step 1: Write `backend/migrations/embed.go`**

```go
// Package migrations embeds the ordered SQL migration files. go:embed cannot
// reach outside its package directory, which is why this tiny package exists.
package migrations

import "embed"

// FS holds every NNN_name.sql in this directory.
//
//go:embed *.sql
var FS embed.FS
```

- [ ] **Step 2: Write `backend/migrations/001_init.sql`**

```sql
-- 001_init — users and settings. Every statement is idempotent so a
-- half-applied deploy can re-run safely (spec §5.8).

CREATE TABLE IF NOT EXISTS users (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  username VARCHAR(50) NOT NULL,
  email VARCHAR(100),
  password_hash VARCHAR(255) NOT NULL,
  first_name VARCHAR(50) NOT NULL DEFAULT '',
  last_name VARCHAR(50) NOT NULL DEFAULT '',
  role VARCHAR(20) NOT NULL,
  pin_hash VARCHAR(255),
  is_active BOOLEAN NOT NULL DEFAULT true,
  token_revoked_at TIMESTAMPTZ,
  password_reset_token_hash TEXT,
  password_reset_expires_at TIMESTAMPTZ,
  last_login_at TIMESTAMPTZ,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
ALTER TABLE users DROP CONSTRAINT IF EXISTS users_role_check;
ALTER TABLE users ADD CONSTRAINT users_role_check CHECK (role IN ('admin', 'counter'));
CREATE UNIQUE INDEX IF NOT EXISTS uniq_users_username_lower ON users (lower(username));
CREATE UNIQUE INDEX IF NOT EXISTS uniq_users_email_lower ON users (lower(email)) WHERE email IS NOT NULL;

CREATE TABLE IF NOT EXISTS settings (
  key VARCHAR(100) PRIMARY KEY,
  value JSONB NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO settings (key, value) VALUES
  ('business_name', '"Elevon POS"'),
  ('business_address', '""'),
  ('business_phone', '""'),
  ('business_ntn', '""'),
  ('business_strn', '""'),
  ('business_province', '""'),
  ('day_boundary_hour', '0'),
  ('tax_rate_cash', '0'),
  ('tax_rate_card', '0'),
  ('tax_rate_online', '0'),
  ('tax_rate_credit', 'null'),
  ('further_tax_rate', '0'),
  ('default_hs_code', '""'),
  ('receipt_paper_width_mm', '80'),
  ('receipt_printable_area_mm', '72'),
  ('receipt_logo_url', '""'),
  ('receipt_header_lines', '[]'),
  ('receipt_footer_lines', '["Thank you for your business"]'),
  ('receipt_default_document', '"thermal"'),
  ('day_close_variance_threshold', '100'),
  ('credit_limit_enforced', 'false')
ON CONFLICT (key) DO NOTHING;
```

- [ ] **Step 3: Write the failing unit test for ordering** — `backend/internal/database/migrate_test.go`

```go
package database_test

import (
	"testing"
	"testing/fstest"

	"elevon-backend/internal/database"
	"elevon-backend/internal/testdb"
	"elevon-backend/migrations"
)

func TestMigrationNames_SortedAndSQLOnly(t *testing.T) {
	fsys := fstest.MapFS{
		"010_later.sql":  {Data: []byte("select 1;")},
		"002_second.sql": {Data: []byte("select 1;")},
		"embed.go":       {Data: []byte("package migrations")},
		"001_first.sql":  {Data: []byte("select 1;")},
	}
	got, err := database.MigrationNames(fsys)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"001_first.sql", "002_second.sql", "010_later.sql"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}

func TestMigrationNames_RejectsUnprefixedFile(t *testing.T) {
	fsys := fstest.MapFS{"init.sql": {Data: []byte("select 1;")}}
	if _, err := database.MigrationNames(fsys); err == nil {
		t.Fatal("a migration without an NNN_ prefix must be rejected")
	}
}

func TestEmbeddedMigrations_ArePresent(t *testing.T) {
	names, err := database.MigrationNames(migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) == 0 || names[0] != "001_init.sql" {
		t.Fatalf("expected 001_init.sql first, got %v", names)
	}
}

// DB-backed: Migrate is idempotent and records every file exactly once.
func TestMigrate_AppliesOnceAndIsIdempotent(t *testing.T) {
	db := testdb.Fresh(t) // runs Migrate once
	if err := database.Migrate(db); err != nil {
		t.Fatalf("second Migrate must be a no-op, got %v", err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	names, _ := database.MigrationNames(migrations.FS)
	if n != len(names) {
		t.Fatalf("schema_migrations has %d rows, want %d", n, len(names))
	}
	var settings int
	if err := db.QueryRow(`SELECT COUNT(*) FROM settings`).Scan(&settings); err != nil {
		t.Fatal(err)
	}
	if settings == 0 {
		t.Fatal("001_init must seed default settings")
	}
}
```

- [ ] **Step 4: Run to verify it fails**

Run: `cd backend && go test ./internal/database/`
Expected: FAIL to compile (`database.MigrationNames`, `testdb` undefined).

- [ ] **Step 5: Write `backend/internal/database/migrate.go`**

```go
package database

import (
	"database/sql"
	"fmt"
	"io/fs"
	"log"
	"regexp"
	"sort"
	"strings"

	"elevon-backend/migrations"
)

var migrationNameRe = regexp.MustCompile(`^[0-9]{3}_[a-z0-9_]+\.sql$`)

// MigrationNames returns the .sql files in fsys sorted by name. Every file
// must be NNN_name.sql so the order is explicit.
func MigrationNames(fsys fs.FS) ([]string, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("read migrations: %w", err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		if !migrationNameRe.MatchString(e.Name()) {
			return nil, fmt.Errorf("migration %q must be named NNN_name.sql", e.Name())
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names, nil
}

// Migrate applies every embedded migration not yet recorded in
// schema_migrations, in name order, each in its own transaction. The caller
// treats an error as fatal: with one store per deploy and a health check, a
// loud boot failure beats a silently missing column.
func Migrate(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version TEXT PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}
	names, err := MigrationNames(migrations.FS)
	if err != nil {
		return err
	}
	for _, name := range names {
		var applied bool
		if err := db.QueryRow(`SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`, name).Scan(&applied); err != nil {
			return fmt.Errorf("check %s: %w", name, err)
		}
		if applied {
			continue
		}
		body, err := fs.ReadFile(migrations.FS, name)
		if err != nil {
			return fmt.Errorf("read %s: %w", name, err)
		}
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("begin %s: %w", name, err)
		}
		// No bind parameters, so lib/pq uses the simple protocol and runs the
		// whole multi-statement file in one round trip.
		if _, err := tx.Exec(string(body)); err != nil {
			tx.Rollback()
			return fmt.Errorf("apply %s: %w", name, err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations (version) VALUES ($1)`, name); err != nil {
			tx.Rollback()
			return fmt.Errorf("record %s: %w", name, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit %s: %w", name, err)
		}
		log.Printf("migration applied: %s", name)
	}
	return nil
}
```

- [ ] **Step 6: Write `backend/internal/testdb/testdb.go`**

```go
// Package testdb gives DB-backed tests a fresh, migrated Postgres schema.
// Tests skip loudly when TEST_DATABASE_URL is unset; CI fails the job if the
// skip message appears, so money-path tests can never rot silently.
package testdb

import (
	"database/sql"
	"os"
	"testing"

	"elevon-backend/internal/database"
)

// EnvVar names the DSN for DB-backed tests.
const EnvVar = "TEST_DATABASE_URL"

// Fresh drops and recreates the public schema, runs every migration, and
// returns the pool. The database must be disposable.
func Fresh(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv(EnvVar)
	if dsn == "" {
		t.Skipf("%s not set — skipping DB-backed test", EnvVar)
	}
	db, err := database.OpenPostgres(dsn)
	if err != nil {
		t.Fatalf("testdb: open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`DROP SCHEMA public CASCADE; CREATE SCHEMA public;`); err != nil {
		t.Fatalf("testdb: reset schema: %v", err)
	}
	if err := database.Migrate(db); err != nil {
		t.Fatalf("testdb: migrate: %v", err)
	}
	return db
}
```

- [ ] **Step 7: Run to verify it passes** (unit tests run everywhere; the DB test needs a local Postgres)

Run: `cd backend && go test ./internal/database/ -v`
Expected: three `PASS`, and either `PASS` or `--- SKIP: TestMigrate_AppliesOnceAndIsIdempotent ... TEST_DATABASE_URL not set`.
To run it for real locally: `docker run -d --name elevon-test-pg -e POSTGRES_PASSWORD=postgres -e POSTGRES_DB=elevon_test -p 5433:5432 postgres:16` then `TEST_DATABASE_URL=postgres://postgres:postgres@localhost:5433/elevon_test?sslmode=disable go test ./internal/database/ -v` → all PASS.

- [ ] **Step 8: Write the idempotent-DDL contract test** — `backend/internal/database/migrations_idempotent_contract_test.go`

```go
package database_test

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"

	"elevon-backend/internal/database"
	"elevon-backend/migrations"
)

// Spec §5.8: every migration must be re-runnable. This pins the forms we
// accept; anything else fails the build rather than a deploy.
func TestMigrations_UseIdempotentDDL(t *testing.T) {
	names, err := database.MigrationNames(migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	addConstraint := regexp.MustCompile(`(?i)ADD CONSTRAINT\s+(\w+)`)
	for _, name := range names {
		body, _ := fs.ReadFile(migrations.FS, name)
		src := string(body)
		upper := strings.ToUpper(src)
		for i, raw := range strings.Split(src, "\n") {
			line := strings.ToUpper(strings.TrimSpace(raw))
			switch {
			case strings.HasPrefix(line, "CREATE TABLE") && !strings.Contains(line, "IF NOT EXISTS"):
				t.Errorf("%s:%d CREATE TABLE without IF NOT EXISTS", name, i+1)
			case (strings.HasPrefix(line, "CREATE INDEX") || strings.HasPrefix(line, "CREATE UNIQUE INDEX")) && !strings.Contains(line, "IF NOT EXISTS"):
				t.Errorf("%s:%d CREATE INDEX without IF NOT EXISTS", name, i+1)
			case strings.Contains(line, "ADD COLUMN") && !strings.Contains(line, "IF NOT EXISTS"):
				t.Errorf("%s:%d ADD COLUMN without IF NOT EXISTS", name, i+1)
			case strings.HasPrefix(line, "INSERT INTO") && !strings.Contains(upper, "ON CONFLICT"):
				t.Errorf("%s:%d INSERT without ON CONFLICT", name, i+1)
			}
		}
		for _, m := range addConstraint.FindAllStringSubmatch(src, -1) {
			if !strings.Contains(upper, "DROP CONSTRAINT IF EXISTS "+strings.ToUpper(m[1])) {
				t.Errorf("%s: ADD CONSTRAINT %s without a preceding DROP CONSTRAINT IF EXISTS", name, m[1])
			}
		}
	}
}
```

- [ ] **Step 9: Run to verify it passes**

Run: `cd backend && go test ./internal/database/ -run Idempotent -v`
Expected: PASS.

- [ ] **Step 10: Write `backend/cmd/migrate/main.go`**

```go
// Command migrate applies the embedded migrations to DATABASE_URL (or
// --database-url) without booting the API. Used by CI and local resets.
package main

import (
	"flag"
	"log"
	"os"

	"elevon-backend/internal/database"
)

func main() {
	dsn := flag.String("database-url", os.Getenv("DATABASE_URL"), "Postgres DSN (default $DATABASE_URL)")
	flag.Parse()
	if *dsn == "" {
		log.Fatal("DATABASE_URL or --database-url is required")
	}
	db, err := database.OpenPostgres(*dsn)
	if err != nil {
		log.Fatalf("open: %v", err)
	}
	defer db.Close()
	if err := database.Migrate(db); err != nil {
		log.Fatalf("migrate: %v", err)
	}
	log.Println("migrations up to date")
}
```

- [ ] **Step 11: Wire `Migrate` into `main.go`** — insert directly after `defer db.Close()`:

```go
	if err := database.Migrate(db); err != nil {
		log.Fatalf("migrations: %v", err)
	}
```

- [ ] **Step 12: Vet, test, commit**

Run: `cd backend && go vet ./... && go test ./...`
Expected: PASS (DB test skips without DSN).

```bash
git add backend
git commit -m "backend(database): E-00 — embedded ordered migrations, 001_init (users, settings), testdb harness, cmd/migrate

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 4: First admin from env + boot-order contract test

**Files:**
- Create: `backend/internal/database/initial_admin.go`, `backend/internal/database/initial_admin_test.go`, `backend/boot_order_contract_test.go`
- Modify: `backend/main.go` (call `database.EnsureInitialAdmin(db)` after `Migrate`)

**Interfaces:**
- Produces: `database.EnsureInitialAdmin(db *sql.DB)` (never fatal), `database.InitialAdminPasswordEnv`, `database.InitialAdminUsernameEnv`.

- [ ] **Step 1: Write the failing tests** — `backend/internal/database/initial_admin_test.go`

```go
package database

import (
	"strings"
	"testing"
)

func TestInitialAdminCredentials_DefaultsUsername(t *testing.T) {
	u, err := initialAdminCredentials("", "correct-horse-battery")
	if err != nil || u != "admin" {
		t.Fatalf("got %q, %v", u, err)
	}
}

func TestInitialAdminCredentials_RejectsShortPassword(t *testing.T) {
	if _, err := initialAdminCredentials("admin", "short"); err == nil {
		t.Fatal("password under 10 bytes must be rejected")
	}
}

func TestInitialAdminCredentials_RejectsOver72Bytes(t *testing.T) {
	if _, err := initialAdminCredentials("admin", strings.Repeat("x", 73)); err == nil {
		t.Fatal("bcrypt truncates past 72 bytes; must refuse, not truncate")
	}
}

func TestInitialAdminCredentials_NormalisesAndValidatesUsername(t *testing.T) {
	u, err := initialAdminCredentials("  Owner.1 ", "correct-horse-battery")
	if err != nil || u != "owner.1" {
		t.Fatalf("got %q, %v", u, err)
	}
	if _, err := initialAdminCredentials("a", "correct-horse-battery"); err == nil {
		t.Fatal("username shorter than 3 chars must be rejected")
	}
}
```

- [ ] **Step 2: Run to verify it fails**

Run: `cd backend && go test ./internal/database/ -run InitialAdmin`
Expected: FAIL, `initialAdminCredentials` undefined.

- [ ] **Step 3: Write `backend/internal/database/initial_admin.go`**

```go
package database

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

const (
	// InitialAdminPasswordEnv is read only while no active admin exists.
	InitialAdminPasswordEnv = "INITIAL_ADMIN_PASSWORD"
	// InitialAdminUsernameEnv optionally overrides the username (default "admin").
	InitialAdminUsernameEnv = "INITIAL_ADMIN_USERNAME"

	initialAdminMinPasswordLen = 10
	initialAdminMaxPasswordLen = 72 // bcrypt ignores bytes past 72: refuse rather than truncate
)

var initialAdminUsernameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{2,49}$`)

func initialAdminCredentials(rawUsername, password string) (string, error) {
	username := strings.ToLower(strings.TrimSpace(rawUsername))
	if username == "" {
		username = "admin"
	}
	if !initialAdminUsernameRe.MatchString(username) {
		return "", fmt.Errorf("%s %q must be 3-50 characters of a-z, 0-9, dot, dash or underscore", InitialAdminUsernameEnv, username)
	}
	switch {
	case password == "":
		return "", fmt.Errorf("%s is not set", InitialAdminPasswordEnv)
	case len(password) < initialAdminMinPasswordLen:
		return "", fmt.Errorf("%s must be at least %d characters", InitialAdminPasswordEnv, initialAdminMinPasswordLen)
	case len(password) > initialAdminMaxPasswordLen:
		return "", fmt.Errorf("%s must be at most %d bytes", InitialAdminPasswordEnv, initialAdminMaxPasswordLen)
	}
	return username, nil
}

// EnsureInitialAdmin creates the first admin from env while the store has no
// active admin. Once one exists it does nothing, so real admins are never
// touched and the env var is inert. Never fatal: a missing password must not
// crash-loop the deploy that would let the operator set it.
func EnsureInitialAdmin(db *sql.DB) {
	var admins int
	if err := db.QueryRow(`SELECT COUNT(*) FROM users WHERE role = 'admin' AND is_active = true`).Scan(&admins); err != nil {
		log.Printf("initial admin: count admins: %v", err)
		return
	}
	if admins > 0 {
		return
	}
	username, err := initialAdminCredentials(os.Getenv(InitialAdminUsernameEnv), os.Getenv(InitialAdminPasswordEnv))
	if err != nil {
		log.Printf("WARNING: no active admin exists and %v — nobody can sign in. Set %s (optionally %s) and redeploy.", err, InitialAdminPasswordEnv, InitialAdminUsernameEnv)
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(os.Getenv(InitialAdminPasswordEnv)), bcrypt.DefaultCost)
	if err != nil {
		log.Printf("initial admin: hash: %v", err)
		return
	}
	res, err := db.Exec(`
		INSERT INTO users (username, email, password_hash, first_name, last_name, role, is_active)
		VALUES ($1, NULL, $2, 'Store', 'Admin', 'admin', true)
		ON CONFLICT DO NOTHING`, username, string(hash))
	if err != nil {
		log.Printf("initial admin: create %q: %v", username, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		log.Printf("WARNING: username %q already exists (inactive?) — set %s to another name and redeploy.", username, InitialAdminUsernameEnv)
		return
	}
	log.Printf("initial admin: created %q from %s — sign in, change the password, then remove the env var", username, InitialAdminPasswordEnv)
}
```

- [ ] **Step 4: Run to verify it passes**

Run: `cd backend && go test ./internal/database/ -run InitialAdmin -v`
Expected: PASS (4 tests).

- [ ] **Step 5: Wire into `main.go`** — after the `Migrate` block add:

```go
	database.EnsureInitialAdmin(db)
```

- [ ] **Step 6: Write the boot-order contract test** — `backend/boot_order_contract_test.go`

```go
package main

import (
	"os"
	"strings"
	"testing"
)

// Pins two boot facts that types cannot express:
//  1. migrations run before the first-admin bootstrap (the INSERT needs users);
//  2. SetTrustedProxies(nil) stays, so c.ClientIP() is the TCP peer.
func TestMain_BootOrderAndTrustedProxies(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	s := string(src)
	mig := strings.Index(s, "database.Migrate(db)")
	adm := strings.Index(s, "database.EnsureInitialAdmin(db)")
	if mig < 0 || adm < 0 {
		t.Fatal("main.go must call database.Migrate(db) and database.EnsureInitialAdmin(db)")
	}
	if adm < mig {
		t.Fatal("EnsureInitialAdmin must run after Migrate")
	}
	if !strings.Contains(s, "router.SetTrustedProxies(nil)") {
		t.Fatal("main.go must keep router.SetTrustedProxies(nil)")
	}
}
```

- [ ] **Step 7: Vet, test, commit**

Run: `cd backend && go vet ./... && go test ./...`
Expected: PASS.

```bash
git add backend
git commit -m "backend(auth): E-00 — first admin from INITIAL_ADMIN_PASSWORD; boot-order contract test

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 5: JWT middleware, login + me handlers, routes with role gates

**Files:**
- Create: `backend/internal/models/user.go`, `backend/internal/middleware/auth.go`, `backend/internal/middleware/auth_test.go`, `backend/internal/handlers/auth.go`, `backend/internal/api/routes.go`, `backend/internal/api/routes_role_gates_contract_test.go`
- Modify: `backend/main.go` (mount `/api/v1`)

**Interfaces:**
- Consumes: `models.APIResponse`, `models.Fail`, `models.OK`, `util.AllRoles`.
- Produces: `middleware.GenerateToken(userID uuid.UUID, username, role string) (string, error)`, `middleware.ValidateToken(string) (*Claims, error)`, `middleware.AuthMiddleware(db *sql.DB) gin.HandlerFunc`, `middleware.RequireRoles(roles []string) gin.HandlerFunc`, `middleware.UserFromContext(c) (uuid.UUID, string, string, bool)`, `models.User`, `models.LoginRequest`, `models.LoginResponse`, `handlers.NewAuthHandler(db)`, `api.SetupRoutes(r *gin.RouterGroup, db *sql.DB, auth gin.HandlerFunc)`. Endpoints: `POST /api/v1/auth/login`, `GET /api/v1/auth/me`.

- [ ] **Step 1: Write `backend/internal/models/user.go`**

```go
package models

import (
	"time"

	"github.com/google/uuid"
)

// User is the public shape of a staff account. Never carries the hash.
type User struct {
	ID          uuid.UUID  `json:"id"`
	Username    string     `json:"username"`
	Email       *string    `json:"email"`
	FirstName   string     `json:"first_name"`
	LastName    string     `json:"last_name"`
	Role        string     `json:"role"`
	IsActive    bool       `json:"is_active"`
	HasPin      bool       `json:"has_pin"`
	LastLoginAt *time.Time `json:"last_login_at"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// LoginRequest accepts a username or an email in the username field.
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// LoginResponse is the payload of a successful login.
type LoginResponse struct {
	Token string `json:"token"`
	User  User   `json:"user"`
}
```

- [ ] **Step 2: Write the failing middleware tests** — `backend/internal/middleware/auth_test.go`

```go
package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func TestGenerateAndValidateToken_RoundTrip(t *testing.T) {
	id := uuid.New()
	tok, err := GenerateToken(id, "owner", "admin")
	if err != nil {
		t.Fatal(err)
	}
	claims, err := ValidateToken(tok)
	if err != nil {
		t.Fatal(err)
	}
	if claims.UserID != id || claims.Username != "owner" || claims.Role != "admin" || claims.IssuedAt == nil {
		t.Fatalf("claims mismatch: %+v", claims)
	}
}

func TestValidateToken_RejectsGarbage(t *testing.T) {
	if _, err := ValidateToken("not-a-token"); err == nil {
		t.Fatal("garbage must not validate")
	}
}

func TestAuthMiddleware_MissingHeaderIs401WithCode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/x", AuthMiddleware(nil), func(c *gin.Context) { c.Status(200) })
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))
	if w.Code != 401 || !contains(w.Body.String(), `"error":"missing_auth_header"`) {
		t.Fatalf("got %d %s", w.Code, w.Body.String())
	}
}

func TestAuthMiddleware_AcceptsFallbackHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tok, _ := GenerateToken(uuid.New(), "till", "counter")
	r := gin.New()
	r.GET("/x", AuthMiddleware(nil), func(c *gin.Context) { c.String(200, c.GetString("role")) })
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set(FallbackJWTHeader, tok)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 200 || w.Body.String() != "counter" {
		t.Fatalf("got %d %s", w.Code, w.Body.String())
	}
}

func TestRequireRoles_ForbidsOtherRole(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tok, _ := GenerateToken(uuid.New(), "till", "counter")
	r := gin.New()
	r.GET("/admin-only", AuthMiddleware(nil), RequireRoles([]string{"admin"}), func(c *gin.Context) { c.Status(200) })
	req := httptest.NewRequest(http.MethodGet, "/admin-only", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 403 || !contains(w.Body.String(), `"error":"insufficient_permissions"`) {
		t.Fatalf("got %d %s", w.Code, w.Body.String())
	}
}

func contains(s, sub string) bool { return len(s) >= len(sub) && (func() bool { return indexOf(s, sub) >= 0 })() }
func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
```

- [ ] **Step 3: Run to verify it fails**

Run: `cd backend && go test ./internal/middleware/`
Expected: FAIL to compile.

- [ ] **Step 4: Write `backend/internal/middleware/auth.go`**

```go
// Package middleware holds Gin middleware: JWT auth and role gates.
package middleware

import (
	"database/sql"
	"errors"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"elevon-backend/internal/models"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// jwtSecret is per store and mandatory in release mode: a shared default would
// let a token minted on one store validate on another.
var jwtSecret = func() []byte {
	if s := os.Getenv("JWT_SECRET"); s != "" {
		return []byte(s)
	}
	if os.Getenv("GIN_MODE") == "release" {
		log.Fatal("JWT_SECRET must be set when GIN_MODE=release (generate one with: openssl rand -base64 48)")
	}
	log.Println("WARNING: JWT_SECRET unset — using dev fallback. Never run like this in production.")
	return []byte("dev-only-insecure-secret")
}()

// FallbackJWTHeader duplicates the JWT for proxies that strip Authorization.
const FallbackJWTHeader = "X-POS-JWT"

// Claims are the JWT claims for a staff session.
type Claims struct {
	UserID   uuid.UUID `json:"user_id"`
	Username string    `json:"username"`
	Role     string    `json:"role"`
	jwt.RegisteredClaims
}

// GenerateToken mints a 24-hour HS256 token. IssuedAt is always set because
// revocation compares against it.
func GenerateToken(userID uuid.UUID, username, role string) (string, error) {
	now := time.Now()
	claims := &Claims{
		UserID:   userID,
		Username: username,
		Role:     role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(now),
			Issuer:    "elevon-pos",
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(jwtSecret)
}

// ValidateToken parses and verifies a token.
func ValidateToken(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, jwt.ErrSignatureInvalid
		}
		return jwtSecret, nil
	})
	if err != nil {
		return nil, err
	}
	if claims, ok := token.Claims.(*Claims); ok && token.Valid {
		return claims, nil
	}
	return nil, jwt.ErrInvalidKey
}

var (
	// ErrTokenRevoked: users.token_revoked_at is at or after the token's iat.
	ErrTokenRevoked = errors.New("token_revoked")
	// ErrUserInactive: user missing or deactivated (same error so a deleted
	// user cannot be told apart from a deactivated one).
	ErrUserInactive = errors.New("user_inactive")
)

// CheckTokenNotRevoked verifies a validated token against the live user row.
// One PK query per request. Fails closed on DB error. db may be nil in tests.
func CheckTokenNotRevoked(db *sql.DB, userID uuid.UUID, issuedAt time.Time) error {
	if db == nil {
		return nil
	}
	var revokedAt sql.NullTime
	var isActive bool
	err := db.QueryRow(`SELECT token_revoked_at, is_active FROM users WHERE id = $1`, userID).Scan(&revokedAt, &isActive)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrUserInactive
	}
	if err != nil {
		return err
	}
	if !isActive {
		return ErrUserInactive
	}
	// iat is whole-second; reject unless strictly after the revoke instant.
	if revokedAt.Valid && !issuedAt.After(revokedAt.Time) {
		return ErrTokenRevoked
	}
	return nil
}

// AuthMiddleware authenticates every request except CORS preflight.
func AuthMiddleware(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method == http.MethodOptions {
			c.Next()
			return
		}
		header := strings.TrimSpace(c.GetHeader("Authorization"))
		if header == "" {
			if alt := strings.TrimSpace(c.GetHeader(FallbackJWTHeader)); alt != "" {
				header = "Bearer " + alt
			}
		}
		if header == "" {
			abort(c, http.StatusUnauthorized, "Authorization header is required", "missing_auth_header")
			return
		}
		if !strings.HasPrefix(header, "Bearer ") {
			abort(c, http.StatusUnauthorized, "Invalid authorization header format", "invalid_auth_format")
			return
		}
		claims, err := ValidateToken(strings.TrimPrefix(header, "Bearer "))
		if err != nil || claims.IssuedAt == nil {
			abort(c, http.StatusUnauthorized, "Invalid or expired token", "invalid_token")
			return
		}
		if err := CheckTokenNotRevoked(db, claims.UserID, claims.IssuedAt.Time); err != nil {
			switch {
			case errors.Is(err, ErrTokenRevoked):
				abort(c, http.StatusUnauthorized, "Session ended. Please sign in again.", "token_revoked")
			case errors.Is(err, ErrUserInactive):
				abort(c, http.StatusUnauthorized, "Account is not active.", "user_inactive")
			default:
				log.Printf("auth: revocation check failed for %s: %v", claims.UserID, err)
				abort(c, http.StatusUnauthorized, "Authentication check failed", "auth_check_failed")
			}
			return
		}
		c.Set("user_id", claims.UserID)
		c.Set("username", claims.Username)
		c.Set("role", claims.Role)
		c.Next()
	}
}

// RequireRoles allows the request only when the caller's role is listed.
func RequireRoles(roles []string) gin.HandlerFunc {
	return func(c *gin.Context) {
		role, ok := c.Get("role")
		if !ok {
			abort(c, http.StatusForbidden, "Role information not found", "missing_role")
			return
		}
		r, _ := role.(string)
		for _, allowed := range roles {
			if r == allowed {
				c.Next()
				return
			}
		}
		abort(c, http.StatusForbidden, "Insufficient permissions", "insufficient_permissions")
	}
}

// UserFromContext returns the authenticated user's id, username and role.
func UserFromContext(c *gin.Context) (uuid.UUID, string, string, bool) {
	id, ok1 := c.Get("user_id")
	name, ok2 := c.Get("username")
	role, ok3 := c.Get("role")
	if !ok1 || !ok2 || !ok3 {
		return uuid.Nil, "", "", false
	}
	uid, a := id.(uuid.UUID)
	n, b := name.(string)
	r, d := role.(string)
	if !a || !b || !d {
		return uuid.Nil, "", "", false
	}
	return uid, n, r, true
}

func abort(c *gin.Context, status int, message, code string) {
	c.JSON(status, models.Fail(message, code))
	c.Abort()
}
```

- [ ] **Step 5: Run to verify it passes**

Run: `cd backend && go test ./internal/middleware/ -v`
Expected: PASS (5 tests).

- [ ] **Step 6: Write `backend/internal/handlers/auth.go`**

```go
// Package handlers holds HTTP handlers. Each handler returns models.APIResponse.
package handlers

import (
	"database/sql"
	"errors"
	"log"
	"net/http"
	"strings"

	"elevon-backend/internal/middleware"
	"elevon-backend/internal/models"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

// AuthHandler serves login and the current-user lookup.
type AuthHandler struct{ db *sql.DB }

// NewAuthHandler builds an AuthHandler.
func NewAuthHandler(db *sql.DB) *AuthHandler { return &AuthHandler{db: db} }

const userColumns = `id, username, email, first_name, last_name, role, is_active,
	(pin_hash IS NOT NULL) AS has_pin, last_login_at, created_at, updated_at`

func scanUser(row interface{ Scan(dest ...interface{}) error }) (models.User, error) {
	var u models.User
	err := row.Scan(&u.ID, &u.Username, &u.Email, &u.FirstName, &u.LastName, &u.Role, &u.IsActive,
		&u.HasPin, &u.LastLoginAt, &u.CreatedAt, &u.UpdatedAt)
	return u, err
}

// Login accepts a username or email plus password and returns a JWT.
func (h *AuthHandler) Login(c *gin.Context) {
	var req models.LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.Fail("Username and password are required", "missing_credentials"))
		return
	}
	ident := strings.ToLower(strings.TrimSpace(req.Username))
	if ident == "" || req.Password == "" {
		c.JSON(http.StatusBadRequest, models.Fail("Username and password are required", "missing_credentials"))
		return
	}
	var hash string
	row := h.db.QueryRow(`SELECT `+userColumns+`, password_hash FROM users
		WHERE (lower(username) = $1 OR lower(email) = $1) AND is_active = true`, ident)
	var u models.User
	err := row.Scan(&u.ID, &u.Username, &u.Email, &u.FirstName, &u.LastName, &u.Role, &u.IsActive,
		&u.HasPin, &u.LastLoginAt, &u.CreatedAt, &u.UpdatedAt, &hash)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.Password)) != nil) {
		c.JSON(http.StatusUnauthorized, models.Fail("Invalid username or password", "invalid_credentials"))
		return
	}
	if err != nil {
		log.Printf("login: query: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not sign in right now", "internal_error"))
		return
	}
	token, err := middleware.GenerateToken(u.ID, u.Username, u.Role)
	if err != nil {
		log.Printf("login: token: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not sign in right now", "internal_error"))
		return
	}
	if _, err := h.db.Exec(`UPDATE users SET last_login_at = now() WHERE id = $1`, u.ID); err != nil {
		log.Printf("login: last_login_at: %v", err) // best effort
	}
	c.JSON(http.StatusOK, models.OK("Signed in", models.LoginResponse{Token: token, User: u}))
}

// Me returns the authenticated user's record.
func (h *AuthHandler) Me(c *gin.Context) {
	id, _, _, ok := middleware.UserFromContext(c)
	if !ok || id == uuid.Nil {
		c.JSON(http.StatusUnauthorized, models.Fail("Not signed in", "auth_required"))
		return
	}
	u, err := scanUser(h.db.QueryRow(`SELECT `+userColumns+` FROM users WHERE id = $1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		c.JSON(http.StatusUnauthorized, models.Fail("Account not found", "user_inactive"))
		return
	}
	if err != nil {
		log.Printf("me: query: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not load your account", "internal_error"))
		return
	}
	c.JSON(http.StatusOK, models.OK("OK", u))
}
```

- [ ] **Step 7: Write `backend/internal/api/routes.go`**

```go
// Package api registers every HTTP route. Every route except POST /auth/login
// is registered on a group that carries middleware.RequireRoles — pinned by
// routes_role_gates_contract_test.go.
package api

import (
	"database/sql"

	"elevon-backend/internal/handlers"
	"elevon-backend/internal/middleware"
	"elevon-backend/internal/util"

	"github.com/gin-gonic/gin"
)

// SetupRoutes mounts the API under r (normally /api/v1).
func SetupRoutes(r *gin.RouterGroup, db *sql.DB, auth gin.HandlerFunc) {
	authH := handlers.NewAuthHandler(db)

	public := r.Group("/auth")
	public.POST("/login", authH.Login)

	// Any signed-in staff member.
	staff := r.Group("", auth, middleware.RequireRoles(util.AllRoles))
	staff.GET("/auth/me", authH.Me)

	// Admin only (populated from Phase 1).
	admin := r.Group("/admin", auth, middleware.RequireRoles([]string{util.RoleAdmin}))
	_ = admin
}
```

- [ ] **Step 8: Write the role-gate contract test** — `backend/internal/api/routes_role_gates_contract_test.go`

```go
package api

import (
	_ "embed"
	"regexp"
	"strings"
	"testing"
)

//go:embed routes.go
var routesSource string

// Every handler registration must hang off `staff.` or `admin.` (both groups
// carry RequireRoles). The single exception is POST /auth/login.
func TestRoutes_EveryRouteIsRoleGated(t *testing.T) {
	reg := regexp.MustCompile(`^\s*(\w+)\.(GET|POST|PUT|PATCH|DELETE)\(`)
	if !strings.Contains(routesSource, `staff := r.Group("", auth, middleware.RequireRoles(util.AllRoles))`) {
		t.Fatal("staff group must be defined with auth + RequireRoles(util.AllRoles)")
	}
	if !strings.Contains(routesSource, `admin := r.Group("/admin", auth, middleware.RequireRoles([]string{util.RoleAdmin}))`) {
		t.Fatal("admin group must be defined with auth + RequireRoles(admin)")
	}
	for i, line := range strings.Split(routesSource, "\n") {
		m := reg.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		group := m[1]
		if strings.Contains(line, `public.POST("/login"`) {
			continue
		}
		if group != "staff" && group != "admin" {
			t.Errorf("routes.go:%d registers a route on group %q, which carries no RequireRoles: %s", i+1, group, strings.TrimSpace(line))
		}
	}
}
```

- [ ] **Step 9: Mount the API in `main.go`** — add imports `"elevon-backend/internal/api"` and `"elevon-backend/internal/middleware"`, and after the `/health` route:

```go
	api.SetupRoutes(router.Group("/api/v1"), db, middleware.AuthMiddleware(db))
```

- [ ] **Step 10: Vet, test, smoke**

Run: `cd backend && go vet ./... && go test ./...`
Expected: PASS.
Smoke (needs the local Postgres from Task 3 step 7): `DATABASE_URL=postgres://postgres:postgres@localhost:5433/elevon_test?sslmode=disable INITIAL_ADMIN_PASSWORD=correct-horse-battery GIN_MODE=debug go run .` then in another shell:
```bash
curl -s localhost:8080/health
curl -s -X POST localhost:8080/api/v1/auth/login -H 'Content-Type: application/json' -d '{"username":"admin","password":"correct-horse-battery"}'
```
Expected: `{"status":"healthy",...}` and `{"success":true,"message":"Signed in","data":{"token":"...","user":{...}}}`. Then `curl -s localhost:8080/api/v1/auth/me -H "Authorization: Bearer <token>"` → the user; without the header → 401 `missing_auth_header`.

- [ ] **Step 11: Commit**

```bash
git add backend
git commit -m "backend(auth): E-00 — JWT middleware, RequireRoles, login/me, role-gated routes with contract test

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 6: Backend Docker, air, Railway config, CI workflow

**Files:**
- Create: `backend/Dockerfile`, `backend/Dockerfile.dev`, `backend/.air.toml`, `backend/railway.json`, `backend/.env.example`, `.github/workflows/backend-tests.yml`

**Interfaces:**
- Produces: image that runs `./main`; CI that runs migrations against a Postgres service and fails if DB tests skip.

- [ ] **Step 1: Copy Dockerfiles and air config from RETAIL** (they are product-neutral)

```bash
cp ../POS-System-General/backend/Dockerfile backend/Dockerfile
cp ../POS-System-General/backend/Dockerfile.dev backend/Dockerfile.dev
cp ../POS-System-General/backend/.air.toml backend/.air.toml
```

- [ ] **Step 2: Write `backend/railway.json`**

```json
{
  "$schema": "https://railway.com/railway.schema.json",
  "build": {
    "builder": "DOCKERFILE",
    "dockerfilePath": "Dockerfile",
    "watchPatterns": ["backend/**"]
  },
  "deploy": {
    "startCommand": "./main",
    "restartPolicyType": "ON_FAILURE",
    "restartPolicyMaxRetries": 3,
    "healthcheckPath": "/health",
    "healthcheckTimeout": 30
  }
}
```

- [ ] **Step 3: Write `backend/.env.example`**

```dotenv
DATABASE_URL=postgres://postgres:postgres123@localhost:5432/elevon_pos?sslmode=disable
JWT_SECRET=                # required when GIN_MODE=release; openssl rand -base64 48
GIN_MODE=debug             # release on Railway
PORT=8080
CORS_ORIGINS=http://localhost:3000
INITIAL_ADMIN_PASSWORD=    # first boot only, 10–72 bytes; remove after sign-in
INITIAL_ADMIN_USERNAME=admin
TEST_DATABASE_URL=         # DB-backed tests; a disposable database
```

- [ ] **Step 4: Write `.github/workflows/backend-tests.yml`**

```yaml
name: Backend tests

on:
  push:
    branches: [main, dev]
    paths: ['backend/**', '.github/workflows/backend-tests.yml']
  pull_request:
    paths: ['backend/**', '.github/workflows/backend-tests.yml']

jobs:
  test:
    runs-on: ubuntu-latest
    services:
      postgres:
        image: postgres:16
        env:
          POSTGRES_USER: postgres
          POSTGRES_PASSWORD: postgres
          POSTGRES_DB: elevon_test
        ports: ['5432:5432']
        options: >-
          --health-cmd pg_isready --health-interval 10s --health-timeout 5s --health-retries 5
    env:
      TEST_DATABASE_URL: postgres://postgres:postgres@localhost:5432/elevon_test?sslmode=disable
    defaults:
      run:
        working-directory: backend
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version: '1.24'
          cache-dependency-path: backend/go.sum
      - name: Migrations apply cleanly
        run: go run ./cmd/migrate --database-url "$TEST_DATABASE_URL"
      - name: go vet
        run: go vet ./...
      - name: go test (fail if DB-backed tests skipped)
        run: |
          set -o pipefail
          go test ./... -v 2>&1 | tee /tmp/go-test.log
          if grep -q -- 'TEST_DATABASE_URL not set' /tmp/go-test.log; then
            echo "::error::DB-backed tests skipped — the Postgres service is not reachable."
            exit 1
          fi
```

- [ ] **Step 5: Build the image locally to prove the Dockerfile**

Run: `cd backend && docker build -t elevon-backend:dev .`
Expected: image builds; `docker run --rm -e GIN_MODE=debug -e DATABASE_URL=... elevon-backend:dev` logs `migration applied: 001_init.sql` then `listening on :8080` (use `host.docker.internal:5433` for the test Postgres on Windows/macOS).

- [ ] **Step 6: Commit**

```bash
git add backend .github
git commit -m "backend(deploy): E-00 — Dockerfiles, air, Railway config, CI with Postgres service

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 7: Frontend scaffold — tooling, tokens, vendored shadcn kit, utils

**Files:**
- Create: `frontend/package.json`, `frontend/vite.config.ts`, `frontend/tsconfig.json`, `frontend/tsconfig.node.json`, `frontend/tailwind.config.js`, `frontend/postcss.config.js`, `frontend/index.html`, `frontend/src/vite-env.d.ts`, `frontend/src/index.css`, `frontend/src/lib/utils.ts`, `frontend/src/lib/money.ts`, `frontend/src/lib/money.test.ts`, `frontend/src/hooks/use-toast.ts`, `frontend/src/hooks/useMediaQuery.ts`, `frontend/src/components/ui/*.tsx` (copied set)

**Interfaces:**
- Produces: `cn()`, date helpers from `@/lib/utils`, `formatMoney(n: number): string` (`Rs 1,234`), `toast()`, the shadcn primitives `Button, Input, Label, Card*, Dialog*, DropdownMenu*, Select*, Switch, Tabs*, Table*, Badge, Skeleton, Tooltip*, Popover*, Sheet*, Textarea, Checkbox, LoadingSpinner, Toaster`.

- [ ] **Step 1: Write `frontend/package.json`**

```json
{
  "name": "elevon-pos-frontend",
  "version": "0.1.0",
  "private": true,
  "type": "module",
  "scripts": {
    "dev": "vite --port 3000 --host 0.0.0.0",
    "build": "vite build",
    "preview": "vite preview",
    "type-check": "tsc --noEmit",
    "test": "vitest run",
    "test:watch": "vitest"
  },
  "dependencies": {
    "@hookform/resolvers": "^3.10.0",
    "@radix-ui/react-dialog": "^1.1.1",
    "@radix-ui/react-dropdown-menu": "^2.1.16",
    "@radix-ui/react-label": "^2.1.7",
    "@radix-ui/react-popover": "^1.1.15",
    "@radix-ui/react-select": "^2.2.6",
    "@radix-ui/react-slot": "^1.2.3",
    "@radix-ui/react-switch": "^1.2.6",
    "@radix-ui/react-tabs": "^1.1.13",
    "@radix-ui/react-toast": "^1.2.15",
    "@radix-ui/react-tooltip": "^1.2.8",
    "@tanstack/react-query": "^5.56.2",
    "@tanstack/react-router": "^1.57.15",
    "axios": "^1.7.7",
    "class-variance-authority": "^0.7.1",
    "clsx": "^2.1.1",
    "date-fns": "^4.1.0",
    "lucide-react": "^0.441.0",
    "react": "^18.3.1",
    "react-dom": "^18.3.1",
    "react-hook-form": "^7.62.0",
    "tailwind-merge": "^2.5.2",
    "zod": "^3.25.76"
  },
  "devDependencies": {
    "@tailwindcss/container-queries": "^0.1.1",
    "@tanstack/router-plugin": "^1.57.15",
    "@types/react": "^18.3.11",
    "@types/react-dom": "^18.3.0",
    "@vitejs/plugin-react-swc": "^3.7.1",
    "autoprefixer": "^10.4.20",
    "postcss": "^8.4.47",
    "tailwindcss": "^3.4.13",
    "typescript": "^5.6.2",
    "vite": "^5.4.8",
    "vite-tsconfig-paths": "^5.0.1",
    "vitest": "^2.1.9"
  }
}
```

- [ ] **Step 2: Copy the neutral config files from RETAIL and write the two that change**

```bash
cp ../POS-System-General/frontend/tsconfig.json frontend/tsconfig.json
cp ../POS-System-General/frontend/tsconfig.node.json frontend/tsconfig.node.json
cp ../POS-System-General/frontend/postcss.config.js frontend/postcss.config.js
cp ../POS-System-General/frontend/tailwind.config.js frontend/tailwind.config.js
```
Edit `frontend/tailwind.config.js`: delete the `bhk: {...}` colour block, the `backgroundImage` block and the `boxShadow` block (all Bhookly login styling). Keep everything else.

`frontend/vite.config.ts`:
```ts
import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react-swc'
import { TanStackRouterVite } from '@tanstack/router-plugin/vite'
import tsconfigPaths from 'vite-tsconfig-paths'
import path from 'path'

export default defineConfig({
  plugins: [react(), TanStackRouterVite(), tsconfigPaths()],
  resolve: { alias: { '@': path.resolve(__dirname, './src') } },
  server: {
    host: '0.0.0.0',
    port: 3000,
    // Docker bind mounts do not always propagate fs events; poll so HMR keeps working.
    watch: { usePolling: true, interval: 300 },
    proxy: {
      '/api': { target: process.env.VITE_API_URL?.replace(/\/api\/v1$/, '') || 'http://localhost:8080', changeOrigin: true },
    },
  },
  build: {
    outDir: 'dist',
    sourcemap: true,
    rollupOptions: {
      output: {
        manualChunks: {
          vendor: ['react', 'react-dom'],
          router: ['@tanstack/react-router'],
          query: ['@tanstack/react-query'],
        },
      },
    },
  },
})
```

`frontend/src/vite-env.d.ts`:
```ts
/// <reference types="vite/client" />
```

- [ ] **Step 3: Write `frontend/index.html`**

```html
<!doctype html>
<html lang="en">
  <head>
    <meta charset="UTF-8" />
    <meta name="viewport" content="width=device-width, initial-scale=1.0, viewport-fit=cover" />
    <title>Elevon POS</title>
    <meta name="theme-color" content="#0f172a" />
    <link rel="preconnect" href="https://fonts.googleapis.com" />
    <link rel="preconnect" href="https://fonts.gstatic.com" crossorigin />
    <link href="https://fonts.googleapis.com/css2?family=Inter:wght@400;500;600;700&display=swap" rel="stylesheet" />
    <script>
      // Apply the saved theme before first paint so the app never flashes light then dark.
      (function () {
        try {
          var theme = localStorage.getItem('elevon-theme');
          if (theme === 'dark') document.documentElement.classList.add('dark');
          if (theme === 'high-contrast') document.documentElement.classList.add('high-contrast');
        } catch (e) {}
      })();
    </script>
  </head>
  <body>
    <div id="root"></div>
    <script type="module" src="/src/main.tsx"></script>
  </body>
</html>
```

- [ ] **Step 4: Copy the theme tokens** — `frontend/src/index.css` is lines 1–293 of RETAIL's `frontend/src/index.css` (Tailwind directives, `:root` / `.dark` / `.high-contrast` HSL tokens, the dark shadow ramp, the base layer). Nothing after line 293 (the Bhookly login styling) is copied.

```bash
sed -n '1,293p' ../POS-System-General/frontend/src/index.css > frontend/src/index.css
grep -n -i "bhookly\|bhk" frontend/src/index.css   # must print nothing
```

- [ ] **Step 5: Copy the shadcn kit and hooks**

```bash
mkdir -p frontend/src/components/ui frontend/src/hooks frontend/src/lib
for f in badge button card checkbox dialog dropdown-menu input label loading-spinner popover select sheet skeleton switch table tabs textarea toast toaster tooltip; do
  cp "../POS-System-General/frontend/src/components/ui/$f.tsx" frontend/src/components/ui/
done
cp ../POS-System-General/frontend/src/hooks/use-toast.ts frontend/src/hooks/
cp ../POS-System-General/frontend/src/hooks/useMediaQuery.ts frontend/src/hooks/
grep -rn "@/lib/\|@/hooks/\|@/types\|@/api" frontend/src/components/ui frontend/src/hooks
```
The grep must show only imports of `@/lib/utils`, `@/components/ui/*`, `@/hooks/use-toast`. If any copied file imports something else (for example `@/lib/toast-helpers`), remove that import and the code that uses it.

- [ ] **Step 6: Write `frontend/src/lib/utils.ts`** — copy RETAIL's `frontend/src/lib/utils.ts`, then: delete the `import { formatMoney } from '@/lib/currency'` line; delete the functions `formatCurrency`, `getOrderStatusColor`, `getPaymentStatusColor`, `calculateOrderTotals`, `getPreparationTimeDisplay`, `generateOrderNumber`. Keep `cn`, every date helper, `BUSINESS_TIMEZONE`, `hourInBusinessTimezone`, `formatRelative`, `formatDate`, `formatTime`, `debounce`.

```bash
cp ../POS-System-General/frontend/src/lib/utils.ts frontend/src/lib/utils.ts
```

- [ ] **Step 7: Write the failing money test** — `frontend/src/lib/money.test.ts`

```ts
import { describe, expect, it } from 'vitest'
import { formatMoney, formatKg } from './money'

describe('formatMoney', () => {
  it('formats whole rupees with thousands separators', () => {
    expect(formatMoney(1234567)).toBe('Rs 1,234,567')
  })
  it('keeps paisa when present', () => {
    expect(formatMoney(12.5)).toBe('Rs 12.50')
  })
  it('handles negatives', () => {
    expect(formatMoney(-300)).toBe('-Rs 300')
  })
})

describe('formatKg', () => {
  it('always shows three decimals and the unit', () => {
    expect(formatKg(12.5)).toBe('12.500 kg')
    expect(formatKg(1250)).toBe('1,250.000 kg')
  })
})
```

- [ ] **Step 8: Run to verify it fails**

Run: `cd frontend && npm install && npx vitest run src/lib/money.test.ts`
Expected: FAIL, cannot resolve `./money`.

- [ ] **Step 9: Write `frontend/src/lib/money.ts`**

```ts
/** Rupee display. Whole rupees show no decimals; paisa show two. */
export function formatMoney(amount: number): string {
  const abs = Math.abs(amount)
  const whole = Number.isInteger(Math.round(abs * 100) / 100) && Math.round(abs * 100) % 100 === 0
  const body = abs.toLocaleString('en-PK', {
    minimumFractionDigits: whole ? 0 : 2,
    maximumFractionDigits: 2,
  })
  return `${amount < 0 ? '-' : ''}Rs ${body}`
}

/** Weight display: always 3 decimals, kg unit. */
export function formatKg(kg: number): string {
  return `${kg.toLocaleString('en-PK', { minimumFractionDigits: 3, maximumFractionDigits: 3 })} kg`
}
```

- [ ] **Step 10: Run tests and type-check**

Run: `cd frontend && npx vitest run && npm run type-check`
Expected: money tests PASS; type-check passes (there is no `src/main.tsx` yet, which is fine for `tsc --noEmit` on the `src` include).

- [ ] **Step 11: Commit**

```bash
git add frontend
git commit -m "frontend(scaffold): E-00 — Vite/TS/Tailwind tooling, theme tokens, vendored shadcn kit, utils

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 8: Types, API client, roles, print-bridge stub

**Files:**
- Create: `frontend/src/types/index.ts`, `frontend/src/api/client.ts`, `frontend/src/api/client.test.ts`, `frontend/src/lib/roles.ts`, `frontend/src/lib/roles.test.ts`, `frontend/src/lib/print/transport.ts`

**Interfaces:**
- Consumes: backend `POST /auth/login`, `GET /auth/me`.
- Produces: `apiClient.login(req)`, `apiClient.getCurrentUser()`, `apiClient.setAuth(token, user)`, `apiClient.clearAuth()`, `apiClient.isAuthenticated()`, `apiClient.getStoredUser(): User | null`, `ApiClientError{code,status,isNetworkError}`, `resolveApiBaseUrl(raw?: string)`, `ROLES`, `isRole`, `canAccess(role, pathname)`, `defaultPath(role)`, `roleLabel(role)`, `NAV_ITEMS`, `desktop()`, `desktopPrint(html, opts)`, `window.elevon` type.

- [ ] **Step 1: Write `frontend/src/types/index.ts`**

```ts
export interface APIResponse<T = unknown> {
  success: boolean
  message: string
  data?: T
  error?: string
}

export interface PaginatedResponse<T> {
  success: boolean
  message: string
  data: T[]
  meta: { current_page: number; per_page: number; total: number; total_pages: number }
}

export type Role = 'admin' | 'counter'
export type ThemePreference = 'light' | 'dark' | 'high-contrast'

export interface User {
  id: string
  username: string
  email: string | null
  first_name: string
  last_name: string
  role: Role
  is_active: boolean
  has_pin: boolean
  last_login_at: string | null
  created_at: string
  updated_at: string
}

/** username may be the staff username or their email. */
export interface LoginRequest {
  username: string
  password: string
}

export interface LoginResponse {
  token: string
  user: User
}
```

- [ ] **Step 2: Write the failing client test** — `frontend/src/api/client.test.ts`

```ts
import { describe, expect, it } from 'vitest'
import { resolveApiBaseUrl } from './client'

describe('resolveApiBaseUrl', () => {
  it('defaults to localhost /api/v1', () => {
    expect(resolveApiBaseUrl(undefined)).toBe('http://localhost:8080/api/v1')
  })
  it('appends /api/v1 when missing', () => {
    expect(resolveApiBaseUrl('https://pos.example.com/')).toBe('https://pos.example.com/api/v1')
  })
  it('keeps a relative /api/v1 (nginx-proxied production build)', () => {
    expect(resolveApiBaseUrl('/api/v1')).toBe('/api/v1')
  })
})
```

- [ ] **Step 3: Run to verify it fails**

Run: `cd frontend && npx vitest run src/api`
Expected: FAIL, cannot resolve `./client`.

- [ ] **Step 4: Write `frontend/src/api/client.ts`**

```ts
import axios, { type AxiosInstance, type AxiosRequestConfig } from 'axios'
import type { APIResponse, LoginRequest, LoginResponse, User } from '@/types'

export const TOKEN_KEY = 'elevon_token'
export const USER_KEY = 'elevon_user'

/** Thrown on every non-2xx response. Callers branch on `code`, never on message text. */
export class ApiClientError extends Error {
  code?: string
  status?: number
  /** No HTTP response at all (offline, DNS, timeout). */
  isNetworkError?: boolean
  constructor(message: string) {
    super(message)
    this.name = 'ApiClientError'
  }
}

/** Requests use `/auth/...`, `/admin/...`; the base must end in /api/v1. */
export function resolveApiBaseUrl(raw: string | undefined): string {
  const trimmed = typeof raw === 'string' ? raw.trim() : ''
  const base = (trimmed || 'http://localhost:8080/api/v1').replace(/\/+$/, '')
  return /\/api\/v1$/i.test(base) ? base : `${base}/api/v1`
}

class APIClient {
  private client: AxiosInstance

  constructor() {
    this.client = axios.create({
      baseURL: resolveApiBaseUrl(import.meta.env?.VITE_API_URL),
      timeout: 30000,
      headers: { 'Content-Type': 'application/json' },
    })
    this.client.interceptors.request.use((config) => {
      const token = localStorage.getItem(TOKEN_KEY)
      if (token) {
        config.headers.Authorization = `Bearer ${token}`
        config.headers['X-POS-JWT'] = token // survives proxies that strip Authorization
      }
      return config
    })
    this.client.interceptors.response.use(
      (r) => r,
      (error) => {
        if (error.response?.status === 401) {
          const code = (error.response?.data as APIResponse | undefined)?.error ?? ''
          // A stripped header is a proxy problem, not an expired session.
          if (code !== 'missing_auth_header') {
            this.clearAuth()
            if (window.location.pathname !== '/login') window.location.href = '/login'
          }
        }
        return Promise.reject(error)
      },
    )
  }

  private async request<T>(config: AxiosRequestConfig): Promise<APIResponse<T>> {
    try {
      const res = await this.client.request<APIResponse<T>>(config)
      return res.data
    } catch (err) {
      if (axios.isAxiosError(err)) {
        const data = err.response?.data as APIResponse | undefined
        const e = new ApiClientError(data?.message || err.message || 'Request failed')
        e.code = data?.error
        e.status = err.response?.status
        e.isNetworkError = !err.response
        throw e
      }
      throw err
    }
  }

  // ── Auth ──────────────────────────────────────────────────────────────
  login(req: LoginRequest) {
    return this.request<LoginResponse>({ method: 'POST', url: '/auth/login', data: req })
  }
  getCurrentUser() {
    return this.request<User>({ method: 'GET', url: '/auth/me' })
  }

  // ── Local session ─────────────────────────────────────────────────────
  setAuth(token: string, user: User): void {
    localStorage.setItem(TOKEN_KEY, token)
    localStorage.setItem(USER_KEY, JSON.stringify(user))
  }
  clearAuth(): void {
    localStorage.removeItem(TOKEN_KEY)
    localStorage.removeItem(USER_KEY)
  }
  isAuthenticated(): boolean {
    return !!localStorage.getItem(TOKEN_KEY)
  }
  getStoredUser(): User | null {
    try {
      const raw = localStorage.getItem(USER_KEY)
      return raw ? (JSON.parse(raw) as User) : null
    } catch {
      this.clearAuth()
      return null
    }
  }
}

export const apiClient = new APIClient()
export default apiClient
```

- [ ] **Step 5: Run to verify it passes**

Run: `cd frontend && npx vitest run src/api`
Expected: PASS (3 tests). Note: constructing the singleton at import time touches `localStorage` only inside interceptors, so the node test environment is fine.

- [ ] **Step 6: Write the failing roles test** — `frontend/src/lib/roles.test.ts`

```ts
import { describe, expect, it } from 'vitest'
import { canAccess, defaultPath, isRole, NAV_ITEMS, roleLabel } from './roles'

describe('roles', () => {
  it('knows exactly admin and counter', () => {
    expect(isRole('admin')).toBe(true)
    expect(isRole('counter')).toBe(true)
    expect(isRole('manager')).toBe(false)
  })
  it('admin can open everything', () => {
    for (const item of NAV_ITEMS) expect(canAccess('admin', item.to)).toBe(true)
  })
  it('counter opens till, day close, customers, invoices only', () => {
    expect(canAccess('counter', '/pos')).toBe(true)
    expect(canAccess('counter', '/day-close')).toBe(true)
    expect(canAccess('counter', '/customers/abc')).toBe(true)
    expect(canAccess('counter', '/invoices')).toBe(true)
    expect(canAccess('counter', '/reports')).toBe(false)
    expect(canAccess('counter', '/settings')).toBe(false)
    expect(canAccess('counter', '/rates')).toBe(false)
    expect(canAccess('counter', '/dashboard')).toBe(false)
  })
  it('lands admin on dashboard and counter on the till', () => {
    expect(defaultPath('admin')).toBe('/dashboard')
    expect(defaultPath('counter')).toBe('/pos')
  })
  it('labels roles', () => {
    expect(roleLabel('counter')).toBe('Counter')
  })
})
```

- [ ] **Step 7: Run to verify it fails**

Run: `cd frontend && npx vitest run src/lib/roles.test.ts`
Expected: FAIL, cannot resolve `./roles`.

- [ ] **Step 8: Write `frontend/src/lib/roles.ts`**

```ts
import { BarChart3, BookUser, CalendarCheck, FileText, LayoutDashboard, Scale, Settings, ShoppingCart } from 'lucide-react'

/** Mirrors backend/internal/util/roles.go and the users_role_check constraint. */
export const ROLES = ['admin', 'counter'] as const
export type Role = (typeof ROLES)[number]

export function isRole(value: string): value is Role {
  return (ROLES as readonly string[]).includes(value)
}

export interface NavItem {
  id: string
  label: string
  to: string
  icon: typeof ShoppingCart
  /** Roles that may open it. */
  roles: readonly Role[]
}

/** Sidebar order. `to` is the URL prefix used for access checks. */
export const NAV_ITEMS: readonly NavItem[] = [
  { id: 'pos', label: 'Till', to: '/pos', icon: ShoppingCart, roles: ['admin', 'counter'] },
  { id: 'day-close', label: 'Day close', to: '/day-close', icon: CalendarCheck, roles: ['admin', 'counter'] },
  { id: 'customers', label: 'Customers', to: '/customers', icon: BookUser, roles: ['admin', 'counter'] },
  { id: 'invoices', label: 'Invoices', to: '/invoices', icon: FileText, roles: ['admin', 'counter'] },
  { id: 'dashboard', label: 'Dashboard', to: '/dashboard', icon: LayoutDashboard, roles: ['admin'] },
  { id: 'reports', label: 'Reports', to: '/reports', icon: BarChart3, roles: ['admin'] },
  { id: 'rates', label: 'Rates', to: '/rates', icon: Scale, roles: ['admin'] },
  { id: 'settings', label: 'Settings', to: '/settings', icon: Settings, roles: ['admin'] },
]

/** Whether the role may open the URL (exact or prefix match on a nav item). */
export function canAccess(role: string, pathname: string): boolean {
  if (!isRole(role)) return false
  return NAV_ITEMS.some(
    (item) => item.roles.includes(role) && (pathname === item.to || pathname.startsWith(`${item.to}/`)),
  )
}

/** First screen after login. */
export function defaultPath(role: string): string {
  return role === 'admin' ? '/dashboard' : '/pos'
}

export function roleLabel(role: string): string {
  switch (role) {
    case 'admin':
      return 'Admin'
    case 'counter':
      return 'Counter'
    default:
      return role
  }
}
```

- [ ] **Step 9: Run to verify it passes**

Run: `cd frontend && npx vitest run src/lib/roles.test.ts`
Expected: PASS (5 tests).

- [ ] **Step 10: Write `frontend/src/lib/print/transport.ts`** (the desktop seam, adapted from RETAIL's `printTransport.ts`)

```ts
/**
 * Single seam between the web app and the Elevon desktop (Electron) shell.
 * In a browser, printing goes through an off-screen iframe + window.print().
 * Inside the desktop app, window.elevon prints silently to a named printer.
 * Callers feature-detect via desktop()/desktopPrint() so the same bundle runs
 * in both with no build-time branching. The Electron side lands in Phase 8.
 */
export interface DesktopPrintResult {
  ok: boolean
  error?: string
}

export interface DesktopBridge {
  isDesktop: true
  appVersion(): Promise<string>
  print(req: { html: string; deviceName?: string; paperWidthMm?: number; printableAreaMm?: number }): Promise<DesktopPrintResult>
  getPrinters(): Promise<{ name: string; isDefault: boolean }[]>
  minimize(): Promise<void>
}

declare global {
  interface Window {
    elevon?: DesktopBridge
  }
}

/** The Electron bridge when running inside the desktop app, otherwise null. */
export function desktop(): DesktopBridge | null {
  if (typeof window === 'undefined') return null
  const b = window.elevon
  return b && b.isDesktop ? b : null
}

/**
 * Print finished HTML silently via the desktop app. Returns true if the bridge
 * took the job; false means "not in the desktop app, use window.print()".
 */
export function desktopPrint(
  html: string,
  opts: { deviceName?: string; paperWidthMm?: number; printableAreaMm?: number } = {},
): boolean {
  const d = desktop()
  if (!d) return false
  d.print({ html, deviceName: opts.deviceName || undefined, paperWidthMm: opts.paperWidthMm, printableAreaMm: opts.printableAreaMm })
    .then((r) => {
      if (!r.ok) console.error('[print] desktop print failed:', r.error)
    })
    .catch((e) => console.error('[print] desktop print threw:', e))
  return true
}
```

- [ ] **Step 11: Type-check, test, commit**

Run: `cd frontend && npm run type-check && npm run test`
Expected: PASS.

```bash
git add frontend
git commit -m "frontend(core): E-00 — types, API client with auth session, roles, desktop print-bridge stub

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 9: Theme, routes, login page, guarded shell with sidebar and placeholders

**Files:**
- Create: `frontend/src/contexts/ThemeContext.tsx`, `frontend/src/components/ui/theme-switcher.tsx`, `frontend/src/components/shell/UserMenu.tsx`, `frontend/src/components/shell/Sidebar.tsx`, `frontend/src/components/shell/PlaceholderPage.tsx`, `frontend/src/main.tsx`, `frontend/src/routes/__root.tsx`, `frontend/src/routes/index.tsx`, `frontend/src/routes/login.tsx`, `frontend/src/routes/_app.tsx`, `frontend/src/routes/_app/{pos,day-close,customers,invoices,dashboard,reports,rates,settings}.tsx`

**Interfaces:**
- Consumes: `apiClient`, `NAV_ITEMS`, `canAccess`, `defaultPath`, `isRole`, `roleLabel`, shadcn primitives, `useMediaQuery`.
- Produces: `useTheme()`, `<ThemeProvider>`, `<Sidebar user>`, `<UserMenu user>`, `<PlaceholderPage title phase>`; URLs `/login`, `/pos`, `/day-close`, `/customers`, `/invoices`, `/dashboard`, `/reports`, `/rates`, `/settings`.

- [ ] **Step 1: Write `frontend/src/contexts/ThemeContext.tsx`** (localStorage only; server sync is not in scope)

```tsx
import { createContext, useContext, useEffect, useState, type ReactNode } from 'react'
import type { ThemePreference } from '@/types'

const STORAGE_KEY = 'elevon-theme'
const DEFAULT_THEME: ThemePreference = 'light' // never the OS preference: a dark laptop still opens the till bright
const VALID: ThemePreference[] = ['light', 'dark', 'high-contrast']

function isTheme(v: unknown): v is ThemePreference {
  return typeof v === 'string' && (VALID as string[]).includes(v)
}

function applyTheme(theme: ThemePreference) {
  const root = document.documentElement
  root.classList.remove('dark', 'high-contrast')
  if (theme !== 'light') root.classList.add(theme)
}

interface ThemeContextValue {
  theme: ThemePreference
  setTheme: (t: ThemePreference) => void
}

const ThemeContext = createContext<ThemeContextValue | undefined>(undefined)

export function ThemeProvider({ children }: { children: ReactNode }) {
  const [theme, setThemeState] = useState<ThemePreference>(() => {
    try {
      const stored = localStorage.getItem(STORAGE_KEY)
      return isTheme(stored) ? stored : DEFAULT_THEME
    } catch {
      return DEFAULT_THEME
    }
  })

  useEffect(() => applyTheme(theme), [theme])

  const setTheme = (t: ThemePreference) => {
    if (!isTheme(t)) return
    setThemeState(t)
    try {
      localStorage.setItem(STORAGE_KEY, t)
    } catch {
      /* private mode: in-memory only */
    }
  }

  return <ThemeContext.Provider value={{ theme, setTheme }}>{children}</ThemeContext.Provider>
}

export function useTheme() {
  const ctx = useContext(ThemeContext)
  if (!ctx) throw new Error('useTheme must be used within ThemeProvider')
  return ctx
}
```

- [ ] **Step 2: Copy and adapt the theme switcher**

```bash
cp ../POS-System-General/frontend/src/components/ui/theme-switcher.tsx frontend/src/components/ui/theme-switcher.tsx
```
No edits needed: it imports `useTheme` from `@/contexts/ThemeContext` and `ThemePreference` from `@/types`, both of which now exist with the same shape.

- [ ] **Step 3: Write `frontend/src/components/shell/UserMenu.tsx`**

```tsx
import { LogOut, User as UserIcon } from 'lucide-react'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { Button } from '@/components/ui/button'
import { ThemeSwitcher } from '@/components/ui/theme-switcher'
import apiClient from '@/api/client'
import { roleLabel } from '@/lib/roles'
import type { User } from '@/types'

export function UserMenu({ user, collapsed = false }: { user: User; collapsed?: boolean }) {
  const logout = () => {
    apiClient.clearAuth()
    window.location.href = '/login'
  }
  return (
    <DropdownMenu>
      <DropdownMenuTrigger asChild>
        <Button variant="ghost" className={collapsed ? 'h-auto rounded-full p-1.5' : 'h-auto w-full justify-start rounded-lg bg-muted/30 p-3 hover:bg-muted'}>
          <div className="flex w-full min-w-0 items-center gap-3">
            <div className="flex h-8 w-8 shrink-0 items-center justify-center rounded-full bg-primary">
              <UserIcon className="h-4 w-4 text-primary-foreground" />
            </div>
            {!collapsed && (
              <div className="min-w-0 flex-1 text-left">
                <p className="truncate text-sm font-medium">{user.first_name} {user.last_name}</p>
                <p className="truncate text-xs text-muted-foreground">{roleLabel(user.role)}</p>
              </div>
            )}
          </div>
        </Button>
      </DropdownMenuTrigger>
      <DropdownMenuContent align="end" side={collapsed ? 'right' : 'top'} className="z-[70] w-64">
        <DropdownMenuLabel className="font-normal">
          <p className="text-[15px] font-medium leading-none">{user.username}</p>
          <p className="mt-1 text-[13px] leading-none text-muted-foreground">{user.email ?? roleLabel(user.role)}</p>
        </DropdownMenuLabel>
        <DropdownMenuSeparator />
        <div className="px-2 py-1.5">
          <p className="px-1 pb-1.5 text-[11px] font-medium uppercase tracking-wide text-muted-foreground">Appearance</p>
          <ThemeSwitcher />
        </div>
        <DropdownMenuSeparator />
        <DropdownMenuItem onClick={logout} className="text-red-600 focus:text-red-600">
          <LogOut className="mr-2 h-4 w-4" />
          Log out
        </DropdownMenuItem>
      </DropdownMenuContent>
    </DropdownMenu>
  )
}
```

- [ ] **Step 4: Write `frontend/src/components/shell/Sidebar.tsx`**

```tsx
import { Link, useLocation } from '@tanstack/react-router'
import { Flame } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Sheet, SheetContent } from '@/components/ui/sheet'
import { Tooltip, TooltipContent, TooltipTrigger } from '@/components/ui/tooltip'
import { UserMenu } from '@/components/shell/UserMenu'
import { NAV_ITEMS } from '@/lib/roles'
import { cn } from '@/lib/utils'
import type { User } from '@/types'

interface SidebarProps {
  user: User
  isNarrowViewport: boolean
  drawerOpen: boolean
  onDrawerOpenChange: (open: boolean) => void
}

export function navTitleFromPath(pathname: string): string {
  return NAV_ITEMS.find((i) => pathname === i.to || pathname.startsWith(`${i.to}/`))?.label ?? 'Elevon POS'
}

export function Sidebar({ user, isNarrowViewport, drawerOpen, onDrawerOpenChange }: SidebarProps) {
  const { pathname } = useLocation()
  const items = NAV_ITEMS.filter((i) => i.roles.includes(user.role))

  const nav = (collapsed: boolean) => (
    <nav className="flex flex-1 flex-col gap-1 p-2">
      {items.map((item) => {
        const active = pathname === item.to || pathname.startsWith(`${item.to}/`)
        const button = (
          <Link key={item.id} to={item.to} className="block">
            <Button variant={active ? 'default' : 'ghost'} className={cn('w-full justify-start gap-3', collapsed && 'justify-center px-0')}>
              <item.icon className="h-5 w-5 shrink-0" />
              {!collapsed && <span>{item.label}</span>}
            </Button>
          </Link>
        )
        return collapsed ? (
          <Tooltip key={item.id}>
            <TooltipTrigger asChild>{button}</TooltipTrigger>
            <TooltipContent side="right">{item.label}</TooltipContent>
          </Tooltip>
        ) : (
          button
        )
      })}
    </nav>
  )

  const brand = (
    <div className="flex items-center gap-2 px-4 py-4">
      <Flame className="h-6 w-6 text-primary" />
      <span className="text-lg font-bold tracking-tight">Elevon POS</span>
    </div>
  )

  if (isNarrowViewport) {
    return (
      <Sheet open={drawerOpen} onOpenChange={onDrawerOpenChange}>
        <SheetContent side="left" className="flex w-72 flex-col p-0">
          {brand}
          {nav(false)}
          <div className="p-2">
            <UserMenu user={user} />
          </div>
        </SheetContent>
      </Sheet>
    )
  }

  return (
    <aside className="flex h-full w-60 flex-col border-r border-border bg-card">
      {brand}
      {nav(false)}
      <div className="p-2">
        <UserMenu user={user} />
      </div>
    </aside>
  )
}
```

- [ ] **Step 5: Write `frontend/src/components/shell/PlaceholderPage.tsx`**

```tsx
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'

export function PlaceholderPage({ title, phase }: { title: string; phase: number }) {
  return (
    <div className="p-6">
      <Card>
        <CardHeader>
          <CardTitle>{title}</CardTitle>
        </CardHeader>
        <CardContent className="text-muted-foreground">This screen arrives in Phase {phase}.</CardContent>
      </Card>
    </div>
  )
}
```

- [ ] **Step 6: Write `frontend/src/main.tsx`**

```tsx
import { StrictMode } from 'react'
import ReactDOM from 'react-dom/client'
import { RouterProvider, createRouter } from '@tanstack/react-router'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { Toaster } from '@/components/ui/toaster'
import { TooltipProvider } from '@/components/ui/tooltip'
import { ThemeProvider } from '@/contexts/ThemeContext'
import { routeTree } from './routeTree.gen'
import './index.css'

const router = createRouter({ routeTree })
declare module '@tanstack/react-router' {
  interface Register {
    router: typeof router
  }
}

const queryClient = new QueryClient({
  defaultOptions: { queries: { staleTime: 5 * 60 * 1000, retry: 1 } },
})

ReactDOM.createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <ThemeProvider>
        <TooltipProvider delayDuration={200} skipDelayDuration={0}>
          <RouterProvider router={router} />
        </TooltipProvider>
        <Toaster />
      </ThemeProvider>
    </QueryClientProvider>
  </StrictMode>,
)
```

- [ ] **Step 7: Write the routes**

`frontend/src/routes/__root.tsx`:
```tsx
import { createRootRoute, Outlet } from '@tanstack/react-router'

export const Route = createRootRoute({
  component: () => (
    <div className="min-h-screen bg-background">
      <Outlet />
    </div>
  ),
})
```

`frontend/src/routes/index.tsx`:
```tsx
import { createFileRoute, Navigate } from '@tanstack/react-router'
import apiClient from '@/api/client'
import { defaultPath, isRole } from '@/lib/roles'

export const Route = createFileRoute('/')({ component: Home })

function Home() {
  const user = apiClient.getStoredUser()
  if (!apiClient.isAuthenticated() || !user || !isRole(user.role)) return <Navigate to="/login" />
  return <Navigate to={defaultPath(user.role)} replace />
}
```

`frontend/src/routes/login.tsx`:
```tsx
import { createFileRoute, Navigate, useRouter } from '@tanstack/react-router'
import { useState } from 'react'
import { useMutation } from '@tanstack/react-query'
import { Eye, EyeOff, Flame, Loader2, Lock, Scale, User as UserIcon, FileCheck2, BookUser } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import apiClient, { ApiClientError } from '@/api/client'
import { defaultPath, isRole } from '@/lib/roles'
import type { LoginRequest } from '@/types'

export const Route = createFileRoute('/login')({ component: LoginPage })

const FEATURES = [
  { icon: Scale, text: 'Weigh it, price it, print it — kg, tonnes or a rupee amount.' },
  { icon: BookUser, text: 'Credit customers with a running ledger and receipts.' },
  { icon: FileCheck2, text: 'FBR Digital Invoicing the moment it is configured.' },
]

function LoginPage() {
  const router = useRouter()
  const [form, setForm] = useState<LoginRequest>({ username: '', password: '' })
  const [showPassword, setShowPassword] = useState(false)
  const [error, setError] = useState('')

  const login = useMutation({
    mutationFn: (req: LoginRequest) => apiClient.login(req),
    onSuccess: (res) => {
      if (res.success && res.data) {
        apiClient.setAuth(res.data.token, res.data.user)
        router.navigate({ to: defaultPath(res.data.user.role) })
      } else {
        setError(res.message || 'Sign-in failed')
      }
    },
    onError: (err: unknown) => {
      if (err instanceof ApiClientError && err.isNetworkError) setError('Cannot reach the server. Check the connection and try again.')
      else if (err instanceof ApiClientError && err.code === 'invalid_credentials') setError('Wrong username or password.')
      else setError(err instanceof Error ? err.message : 'Sign-in failed')
    },
  })

  const stored = apiClient.getStoredUser()
  if (apiClient.isAuthenticated() && stored && isRole(stored.role)) return <Navigate to={defaultPath(stored.role)} />

  return (
    <div className="grid min-h-screen lg:grid-cols-[3fr_2fr]">
      <section className="hidden flex-col justify-between bg-slate-900 p-10 text-slate-50 lg:flex">
        <div className="flex items-center gap-2 text-xl font-bold">
          <Flame className="h-7 w-7 text-orange-400" /> Elevon POS
        </div>
        <div className="max-w-md space-y-6">
          <h1 className="text-4xl font-semibold leading-tight">The till for an LPG counter.</h1>
          <ul className="space-y-3 text-slate-300">
            {FEATURES.map((f) => (
              <li key={f.text} className="flex items-start gap-3">
                <f.icon className="mt-0.5 h-5 w-5 shrink-0 text-orange-400" />
                <span>{f.text}</span>
              </li>
            ))}
          </ul>
        </div>
        <p className="text-xs text-slate-400">Elevon Systems</p>
      </section>

      <section className="flex items-center justify-center p-6">
        <form
          className="w-full max-w-sm space-y-5"
          onSubmit={(e) => {
            e.preventDefault()
            setError('')
            login.mutate(form)
          }}
        >
          <div className="space-y-1">
            <h2 className="text-2xl font-semibold">Sign in</h2>
            <p className="text-sm text-muted-foreground">Use your staff username or email.</p>
          </div>
          <div className="space-y-2">
            <Label htmlFor="username">Username or email</Label>
            <div className="relative">
              <UserIcon className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
              <Input id="username" className="pl-9" autoComplete="username" autoFocus value={form.username}
                onChange={(e) => setForm({ ...form, username: e.target.value })} />
            </div>
          </div>
          <div className="space-y-2">
            <Label htmlFor="password">Password</Label>
            <div className="relative">
              <Lock className="absolute left-3 top-1/2 h-4 w-4 -translate-y-1/2 text-muted-foreground" />
              <Input id="password" type={showPassword ? 'text' : 'password'} className="pl-9 pr-10" autoComplete="current-password"
                value={form.password} onChange={(e) => setForm({ ...form, password: e.target.value })} />
              <button type="button" tabIndex={-1} aria-label={showPassword ? 'Hide password' : 'Show password'}
                onClick={() => setShowPassword((s) => !s)}
                className="absolute right-2 top-1/2 -translate-y-1/2 rounded p-1 text-muted-foreground hover:text-foreground">
                {showPassword ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
              </button>
            </div>
          </div>
          {error && <div className="rounded-md border border-red-200 bg-red-50 px-3 py-2 text-sm text-red-800 dark:border-red-900 dark:bg-red-950 dark:text-red-200">{error}</div>}
          <Button type="submit" className="w-full" disabled={login.isPending || !form.username || !form.password}>
            {login.isPending ? <Loader2 className="h-4 w-4 animate-spin" /> : 'Sign in'}
          </Button>
        </form>
      </section>
    </div>
  )
}
```

`frontend/src/routes/_app.tsx` (pathless layout: guard + shell):
```tsx
import { createFileRoute, Navigate, Outlet, useLocation } from '@tanstack/react-router'
import { useEffect, useState } from 'react'
import { PanelLeft } from 'lucide-react'
import apiClient from '@/api/client'
import { Button } from '@/components/ui/button'
import { Sidebar, navTitleFromPath } from '@/components/shell/Sidebar'
import { useMediaQuery } from '@/hooks/useMediaQuery'
import { canAccess, defaultPath, isRole } from '@/lib/roles'

export const Route = createFileRoute('/_app')({ component: AppLayout })

function AppLayout() {
  const { pathname } = useLocation()
  const isNarrow = useMediaQuery('(max-width: 767px)')
  const [drawerOpen, setDrawerOpen] = useState(false)
  const user = apiClient.getStoredUser()

  useEffect(() => setDrawerOpen(false), [pathname])

  if (!apiClient.isAuthenticated() || !user || !isRole(user.role)) return <Navigate to="/login" />
  if (!canAccess(user.role, pathname)) return <Navigate to={defaultPath(user.role)} replace />

  return (
    <div className="flex h-[100dvh] overflow-hidden bg-background">
      <div className={isNarrow ? 'w-0 shrink-0' : 'shrink-0'}>
        <Sidebar user={user} isNarrowViewport={isNarrow} drawerOpen={drawerOpen} onDrawerOpenChange={setDrawerOpen} />
      </div>
      <main className="flex min-w-0 flex-1 flex-col overflow-hidden">
        <header className="flex h-14 shrink-0 items-center gap-3 border-b border-border px-3 md:hidden">
          <Button variant="ghost" size="icon" aria-label="Open navigation" onClick={() => setDrawerOpen((o) => !o)}>
            <PanelLeft className="h-6 w-6" />
          </Button>
          <span className="truncate text-base font-semibold">{navTitleFromPath(pathname)}</span>
        </header>
        <div className="min-h-0 flex-1 overflow-y-auto">
          <Outlet />
        </div>
      </main>
    </div>
  )
}
```

Eight placeholder routes, one file each under `frontend/src/routes/_app/`. The pattern (file `pos.tsx`):
```tsx
import { createFileRoute } from '@tanstack/react-router'
import { PlaceholderPage } from '@/components/shell/PlaceholderPage'

export const Route = createFileRoute('/_app/pos')({
  component: () => <PlaceholderPage title="Till" phase={4} />,
})
```
Repeat with: `day-close.tsx` → `'/_app/day-close'`, "Day close", 5 · `customers.tsx` → "Customers", 5 · `invoices.tsx` → "Invoices", 5 · `dashboard.tsx` → "Dashboard", 5 · `reports.tsx` → "Reports", 5 · `rates.tsx` → "Rates", 2 · `settings.tsx` → "Settings", 1.

- [ ] **Step 8: Generate the route tree, type-check, test**

Run: `cd frontend && npx vite build` (the router plugin writes `src/routeTree.gen.ts` during build) then `npm run type-check && npm run test`
Expected: build succeeds, type-check clean, tests PASS. Fix any unused-import errors (`noUnusedLocals` is on).

- [ ] **Step 9: Manual verification against the backend from Task 5**

Run: `cd frontend && VITE_API_URL=http://localhost:8080/api/v1 npm run dev` with the backend running.
Check in the browser at http://localhost:3000: `/` redirects to `/login`; wrong password shows "Wrong username or password."; correct login as admin lands on `/dashboard` with the sidebar showing all eight items; `/settings` as admin opens the placeholder; the user menu switches theme and logs out; `/reports` after logging out redirects to `/login`.

- [ ] **Step 10: Commit** (`routeTree.gen.ts` is committed; it is generated but must exist for `tsc`)

```bash
git add frontend
git commit -m "frontend(shell): E-00 — theme, login page, role-guarded layout, sidebar, placeholder screens

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

### Task 10: Frontend Docker/nginx, Railway config, compose, Makefile, frontend CI

**Files:**
- Create: `frontend/Dockerfile`, `frontend/nginx.conf.template`, `frontend/docker-entrypoint.sh`, `frontend/railway.json`, `frontend/.env.example`, `docker-compose.dev.yml`, `Makefile`, `.github/workflows/frontend-checks.yml`

**Interfaces:**
- Produces: `docker compose -f docker-compose.dev.yml up` → Postgres 5432, API 8080, Vite 3000; production frontend image serving `dist/` and proxying `/api` to `$BACKEND_URL`.

- [ ] **Step 1: Copy the neutral files from RETAIL**

```bash
cp ../POS-System-General/frontend/Dockerfile frontend/Dockerfile
cp ../POS-System-General/frontend/docker-entrypoint.sh frontend/docker-entrypoint.sh
cp ../POS-System-General/frontend/railway.json frontend/railway.json
```

- [ ] **Step 2: Write `frontend/nginx.conf.template`** (RETAIL's template minus the two SSE locations and the `/q/`, `/p/`, PWA blocks)

```nginx
events { worker_connections 1024; }

http {
    include       /etc/nginx/mime.types;
    default_type  application/octet-stream;
    access_log /var/log/nginx/access.log;
    error_log  /var/log/nginx/error.log warn;

    sendfile on; tcp_nopush on; tcp_nodelay on; keepalive_timeout 65; types_hash_max_size 2048;

    gzip on; gzip_vary on; gzip_min_length 1024; gzip_proxied any; gzip_comp_level 6;
    gzip_types application/javascript application/json text/css text/javascript text/plain image/svg+xml font/woff2;

    # Generous proxy buffers: the platform edge stacks headers on top of ours and the
    # nginx default 4k overflows into a 502 on long responses.
    proxy_buffer_size 64k; proxy_buffers 8 64k; proxy_busy_buffers_size 128k;

    server {
        listen ${PORT};
        server_name _;
        root /usr/share/nginx/html;
        index index.html;

        add_header X-Frame-Options "SAMEORIGIN" always;
        add_header X-Content-Type-Options "nosniff" always;
        add_header Referrer-Policy "no-referrer-when-downgrade" always;

        location ~* \.(js|css|png|jpg|jpeg|gif|ico|svg|woff|woff2|ttf|eot)$ {
            expires 1y;
            add_header Cache-Control "public, immutable";
            try_files $uri =404;
        }

        location /api {
            # Resolver from /etc/resolv.conf via docker-entrypoint.sh: 127.0.0.11 in
            # Compose, Railway's internal IPv6 resolver in production (*.railway.internal
            # only resolves over IPv6). proxy_pass through a variable forces runtime DNS.
            resolver ${NGINX_RESOLVER} valid=30s;
            set $backend_upstream ${BACKEND_URL};
            proxy_pass $backend_upstream;
            proxy_http_version 1.1;
            proxy_ssl_server_name on;
            proxy_set_header Host ${BACKEND_HOST};
            proxy_set_header X-Real-IP $remote_addr;
            proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
            proxy_set_header X-Forwarded-Proto $scheme;
            proxy_read_timeout 300s;
            proxy_connect_timeout 75s;
        }

        location / { try_files $uri $uri/ /index.html; }

        location /health { access_log off; add_header Content-Type text/plain; return 200 "healthy\n"; }
    }
}
```

- [ ] **Step 3: Write `frontend/.env.example`**

```dotenv
# Dev only — production bakes VITE_API_URL=/api/v1 in the Dockerfile.
VITE_API_URL=http://localhost:8080/api/v1
# nginx container (Railway): proxy target for /api
BACKEND_URL=http://backend:8080
PORT=3000
```

- [ ] **Step 4: Write `docker-compose.dev.yml`**

```yaml
# Reads ./.env (copy .env.example). Set POSTGRES_HOST_PORT if 5432 is taken.
services:
  postgres:
    image: postgres:16-alpine
    container_name: elevon-postgres-dev
    environment:
      POSTGRES_DB: ${DB_NAME:-elevon_pos}
      POSTGRES_USER: ${DB_USER:-postgres}
      POSTGRES_PASSWORD: ${DB_PASSWORD:-postgres123}
    ports:
      - "${POSTGRES_HOST_PORT:-5432}:5432"
    volumes:
      - postgres_dev_data:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U ${DB_USER:-postgres} -d ${DB_NAME:-elevon_pos}"]
      interval: 3s
      timeout: 5s
      retries: 15

  backend:
    build:
      context: ./backend
      dockerfile: Dockerfile.dev
      target: development
    container_name: elevon-backend-dev
    env_file: .env
    environment:
      DATABASE_URL: postgres://${DB_USER:-postgres}:${DB_PASSWORD:-postgres123}@postgres:5432/${DB_NAME:-elevon_pos}?sslmode=disable
      PORT: 8080
      GIN_MODE: ${GIN_MODE:-debug}
      CORS_ORIGINS: ${CORS_ORIGINS:-http://localhost:3000,http://127.0.0.1:3000}
    ports:
      - "8080:8080"
    depends_on:
      postgres:
        condition: service_healthy
    volumes:
      - ./backend:/app

  frontend:
    image: node:20-alpine
    container_name: elevon-frontend-dev
    working_dir: /app
    command: sh -c "npm ci && npm run dev"
    environment:
      VITE_API_URL: ${VITE_API_URL:-http://localhost:8080/api/v1}
    ports:
      - "3000:3000"
    depends_on:
      - backend
    volumes:
      - ./frontend:/app
      - frontend_node_modules:/app/node_modules

volumes:
  postgres_dev_data:
  frontend_node_modules:
```

- [ ] **Step 5: Write `Makefile`**

```makefile
COMPOSE = docker compose -f docker-compose.dev.yml

.PHONY: help dev up down logs db-shell test test-backend test-frontend

help:
	@echo "dev            start postgres + backend (air) + frontend (vite)"
	@echo "down           stop the stack"
	@echo "logs           tail all logs"
	@echo "db-shell       psql into the dev database"
	@echo "test           run backend and frontend suites"

dev up:
	$(COMPOSE) up --build

down:
	$(COMPOSE) down

logs:
	$(COMPOSE) logs -f

db-shell:
	$(COMPOSE) exec postgres psql -U $${DB_USER:-postgres} -d $${DB_NAME:-elevon_pos}

test: test-backend test-frontend

test-backend:
	cd backend && go vet ./... && go test ./...

test-frontend:
	cd frontend && npm run type-check && npm run test
```

- [ ] **Step 6: Write `.github/workflows/frontend-checks.yml`**

```yaml
name: Frontend checks

on:
  push:
    branches: [main, dev]
    paths: ['frontend/**', '.github/workflows/frontend-checks.yml']
  pull_request:
    paths: ['frontend/**', '.github/workflows/frontend-checks.yml']

jobs:
  check:
    runs-on: ubuntu-latest
    defaults:
      run:
        working-directory: frontend
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-node@v4
        with:
          node-version: '20'
          cache: npm
          cache-dependency-path: frontend/package-lock.json
      - run: npm ci
      - run: npm run type-check
      - run: npm run test
      - run: npm run build
```

- [ ] **Step 7: End-to-end smoke through compose**

```bash
cp .env.example .env   # then set INITIAL_ADMIN_PASSWORD=correct-horse-battery
docker compose -f docker-compose.dev.yml up --build
```
Expected: backend log shows `migration applied: 001_init.sql` and `initial admin: created "admin"`; http://localhost:8080/health returns healthy; http://localhost:3000 shows the login page; signing in as `admin` lands on `/dashboard`. Then `docker build -t elevon-frontend:dev frontend` succeeds and `docker run --rm -p 3001:3000 -e BACKEND_URL=http://host.docker.internal:8080 elevon-frontend:dev` serves the built app at http://localhost:3001 with `/api` proxied.

- [ ] **Step 8: Grep sweep for isolation, then commit**

```bash
grep -rni "bhookly\|chaikhana\|Usamapuri/POS-System\|railway.internal:8080" --exclude-dir=node_modules --exclude-dir=.git --exclude-dir=docs . ; echo "exit=$?"
```
Expected: no matches (exit 1). The spec under `docs/` legitimately names Bhookly Retail as the source, which is why `docs` is excluded.

```bash
git add frontend docker-compose.dev.yml Makefile .github
git commit -m "frontend(deploy): E-00 — nginx image, Railway config, dev compose stack, Makefile, frontend CI

Co-Authored-By: Claude Fable 5.1 <noreply@anthropic.com>"
```

---

## Self-review

**Spec coverage (Phase 0 row of §13):** repo layout ✔ (Task 1, 7, 10) · CLAUDE.md ✔ (Task 1) · `.claude/settings.json` ✔ (Task 1) · CI ✔ (Tasks 6, 10) · docker-compose ✔ (Task 10) · Go server with `/health` ✔ (Task 2) · migration runner + first migration (users, settings) ✔ (Task 3) · React shell with login page ✔ (Task 9) · sidebar ✔ · theme ✔ · API client ✔ (Task 8) · `window.elevon` bridge stub ✔ (Task 8). Login actually works end-to-end because the JWT middleware and login/me handlers (§6.1's core) are pulled into Phase 0 (Task 5); forgot/reset/change-password, the rate limiter, PIN identify and users CRUD stay in Phase 1.

**Placeholder scan:** none. Every copy step names the exact source path and what to delete.

**Type consistency:** `middleware.GenerateToken(userID, username, role)` is what `handlers/auth.go` calls; `models.Fail`/`models.OK` are used by both middleware and handlers; `apiClient.getStoredUser()` is used by `index.tsx`, `login.tsx`, `_app.tsx`; `NAV_ITEMS` entries carry `roles` and `Sidebar` filters on `user.role` which is typed `Role`; `navTitleFromPath` is exported by `Sidebar.tsx` and imported by `_app.tsx`; `ThemePreference` lives in `@/types` for both `ThemeContext` and the copied `theme-switcher`.
