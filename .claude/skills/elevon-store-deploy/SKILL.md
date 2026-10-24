---
name: elevon-store-deploy
description: Use when standing up a new Elevon POS store (a new LPG tenant) on Railway — the owner has created a Railway project with Postgres, backend and frontend services and a custom domain, and wants it configured, booted, given a first admin and seeded with a catalog copied from an existing Elevon store. Elevon-only; never for Bhookly.
---

# Elevon store deploy

One Railway project per store: `Postgres` + `backend` (root `/backend`) + `frontend` (root `/frontend`), source repo `Usamapuri/ElevonSystems`. The owner creates the project, the three services and the domain. This skill configures the rest.

**Isolation.** Only Elevon projects (`POS-<Store>` projects listed in the store registry below or named by the owner). Never touch a Bhookly project even though they share the Railway workspace. Pass explicit project, service and environment IDs on every call; never rely on the linked project.

**Source store is read-only.** Copying from an existing store (catalog, HS codes, variable shape) means reads only: `list_variables`, `get_service_config`, and `SELECT` through `railway ssh` into its Postgres. Never set a variable, redeploy, or write SQL on the source store.

## Store registry

| Store | Project ID | Domain |
|---|---|---|
| Unity Explorations | `1714e6fd-04b2-4886-8571-7d5805d6b1ef` | unityex.elevonsystems.com |
| Bright Star LPG | `bdf1d8ab-b098-4f0e-abb4-71ddfd818682` | brightstar.elevonsystems.com |

Add the new store's row when it is live.

## Checklist

1. **Discover.** `list_projects`; `list_services` and `environment_status` for the target and the source. Record project, service and environment IDs. A fresh backend crash-looping on `password authentication failed` or `JWT_SECRET unset` is expected: it has no variables yet.
2. **Read the source shape.** `list_variables` on the source backend and frontend. Copy the *keys and non-secret values* only.
3. **Generate fresh secrets** locally, never reused from another store:
   - `JWT_SECRET`: 48 random bytes, base64.
   - `FISCAL_SECRETS_KEY`: exactly 32 random bytes, base64 (24 bytes is rejected and shows the "no key" banner).
   - `INITIAL_ADMIN_PASSWORD`: 10–72 bytes, random, printable.
   ```powershell
   $b=[byte[]]::new(32); [Security.Cryptography.RandomNumberGenerator]::Fill($b); [Convert]::ToBase64String($b)
   ```
   (PowerShell 5.1: `[Security.Cryptography.RandomNumberGenerator]::Create().GetBytes($b)`.)
4. **Backend variables** (`set_variables`, target backend):

   | Key | Value |
   |---|---|
   | `DATABASE_URL` | `${{Postgres.DATABASE_URL}}` (reference, not a pasted string) |
   | `GIN_MODE` | `release` |
   | `PORT` | `8080` |
   | `BUSINESS_TIMEZONE` | `Asia/Karachi` |
   | `APP_URL`, `CORS_ORIGINS` | `https://<store domain>` |
   | `JWT_SECRET`, `FISCAL_SECRETS_KEY` | fresh, step 3 |
   | `INITIAL_ADMIN_USERNAME` | `admin` |
   | `INITIAL_ADMIN_PASSWORD` | fresh, step 3; removed after first sign-in |
   | `FISCAL_ALLOW_SANDBOX` | `true` only while this seller runs its FBR sandbox scenarios; remove before production filing |

5. **Frontend variables**: `BACKEND_URL=http://backend.railway.internal:8080`, `PORT=8080`, `VITE_API_URL=/api/v1`. `BACKEND_HOST` and `NGINX_RESOLVER` are derived by `docker-entrypoint.sh`; do not set them. If the domain's target port differs from `PORT`, the site returns 502 — check the domain's port.
6. **Boot.** Wait for both deploys to reach `SUCCESS`. Backend log must show migrations applied and the initial admin created; no `JWT_SECRET unset`. Check `https://<domain>/health` and `https://<domain>/api/v1/auth/login` (a bad-credentials POST returns the JSON envelope, not nginx HTML).
7. **Copy the catalog** from the source store's Postgres, read-only:
   ```sh
   railway ssh --project <SRC_PROJECT> --environment production --service Postgres -- \
     psql -U postgres -d railway -At -F '|' -c "SELECT name, sku, sell_by, default_uom, fbr_uom, hs_code, rate, active FROM products ORDER BY name"
   ```
   Insert into the target with the app API as the new admin (`POST /api/v1/products`), so `product_rate_history` and validation run exactly as in the app. Rates are customer-visible money (invariant 9): show the owner the copied rates before inserting, and say that they are the source store's current rates.
8. **Not copied, ever:** users, customers, ledger, invoices, business days, seller NTN/name/address, FBR tokens, fiscal settings. Each store files under its own NTN; the token is bound to it. The owner enters seller details and pastes tokens in Settings → FBR, then runs Refresh from FBR.
9. **Hand over.** Give the owner the domain, the admin username and the one-time password. After their first sign-in and password change, remove `INITIAL_ADMIN_PASSWORD` from the backend.
10. **Record.** Write `audit/STORE_DEPLOY_<STORE>_<YYYY-MM-DD>.md`: IDs, variable keys set (never values), catalog copied, smoke results, open items. Add the store to the registry above.

## Confirmations

The owner asking for a store to be deployed authorises steps 1–7 on *that* project. Anything on another project, any deletion, any change to a live store's variables, and pushing to `main` each need their own confirmation.
