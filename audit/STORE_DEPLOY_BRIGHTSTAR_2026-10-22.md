# Store deploy — Bright Star LPG — 2026-10-22

Run with the repo skill `.claude/skills/elevon-store-deploy`. Source store for shape and catalog: Unity Explorations (read-only).

## IDs

| | Bright Star LPG |
|---|---|
| Project | `bdf1d8ab-b098-4f0e-abb4-71ddfd818682` (POS-Bright Star LPG) |
| Environment | `6941c691-de58-42fd-96f1-5bcadf7223a3` (production) |
| Postgres | `11dcec1a-27e5-45bc-bc74-cc7786c5842c` |
| backend | `66b18af2-035d-41a8-a389-7d29f81d3b6f` (root `/backend`) |
| frontend | `ee34f381-3655-420a-ac3b-2c1c69b2dcf1` (root `/frontend`) |
| Domain | https://brightstar.elevonsystems.com |

## Variables set (keys only)

- **backend:** `DATABASE_URL` (reference `${{Postgres.DATABASE_URL}}`), `GIN_MODE=release`, `PORT=8080`, `BUSINESS_TIMEZONE=Asia/Karachi`, `APP_URL` and `CORS_ORIGINS` = the domain, `JWT_SECRET` (fresh 48 bytes), `FISCAL_SECRETS_KEY` (fresh 32 bytes), `INITIAL_ADMIN_USERNAME=admin`, `INITIAL_ADMIN_PASSWORD` (fresh, one-time), `FISCAL_ALLOW_SANDBOX=true` (same as Unity; remove before production filing).
- **frontend:** `BACKEND_URL=http://backend.railway.internal:8080`, `PORT=8080`, `VITE_API_URL=/api/v1`.

No secret was copied from Unity.

## Boot (12:38 UTC)

Before: backend crash-looping (`password authentication failed`, `JWT_SECRET unset`), no variables. After: migrations 001–007 applied, initial admin `admin` created, listening on :8080. `/health` 200, `/` serves Elevon POS, `POST /api/v1/auth/login` with bad credentials returns 401 through the nginx proxy.

## Catalog — not yet created

Unity's catalog, read from its Rates screen:

| Name | SKU | HS code | Rate (Rs/kg) | FBR UoM |
|---|---|---|---|---|
| LPG 1910 | 2711.1910 | 2711.1910 | 198.57 | default (KG) |
| LPG 1400 | 2711.1400 | 2711.1400 | 198.57 | default (KG) |

Creating them on Bright Star through `POST /api/v1/products` was refused by the session's permission guard. The owner adds them in Rates → Add product, or allows the API call.

## Open items

1. Create the two products above.
2. Sign in as `admin`, change the password, then remove `INITIAL_ADMIN_PASSWORD` from the backend.
3. Settings → FBR: Bright Star's own seller NTN, name, province, address; paste its sandbox token; Refresh from FBR; run the sandbox scenarios. Never Unity's NTN 3792106.
4. `RESEND_API_KEY`/`EMAIL_FROM` unset: password-reset links go to the log.
5. Production token later; drop `FISCAL_ALLOW_SANDBOX` at that point.
