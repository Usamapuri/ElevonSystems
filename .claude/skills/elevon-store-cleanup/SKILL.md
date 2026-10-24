---
name: elevon-store-cleanup
description: Use when wiping test sales from an Elevon POS store's database before it goes live — e.g. "clean Bright Star before launch", "wipe the test invoices on unityex". Preserves users, settings (seller details, tax rates, FBR config and tokens), products and rate history; deletes invoices, receipts, customers and their ledger, business days, voids, day-close audit, and (only when nothing reached FBR production) the fiscal tables. Dry run first, one transaction, owner says "go". Elevon-only; never for Bhookly.
argument-hint: 'elevon-store-cleanup <store>, elevon-store-cleanup <store> --keep-customers, elevon-store-cleanup <store> --preserve-fiscal'
---

# Elevon store cleanup: pre-launch test-data wipe

Standing up a store leaves residue: test sales, a demo business day, test customers with ledger entries, voids, sandbox FBR filings. Before the first real shift those go; everything deliberately configured stays.

> **Destructive but transactional.** One `BEGIN … COMMIT`. Any error rolls the whole run back. The dry run (`-v commit=0`) executes every statement and then rolls back, so the owner sees the exact effect first.

> **Derive, don't trust.** The delete order (FK walk, Step 1c) and the append-only trigger set (Step 1a) are read from the live database every run. The lists in this file are the 2026-10-22 baseline (migrations 001–007) and are only for recognising drift.

**Isolation.** Elevon stores only (the registry in `.claude/skills/elevon-store-deploy/SKILL.md`). Never a Bhookly project, even though they share the Railway workspace. Explicit project/service/environment IDs on every Railway call.

---

## Step 0: Scope

1. **Which store?** Resolve the Railway project and its Postgres service from the registry.
2. **Database access.** Pick one, and get the owner's yes for either, because both change the store's Railway setup:
   - `railway ssh --project <ID> --environment production --service Postgres -- psql -U postgres -d railway …` — needs an SSH key in `~/.ssh` registered with Railway.
   - A TCP proxy on the store's Postgres (`DATABASE_PUBLIC_URL`), used with local `psql` (installed at `C:\Users\Usama Puri\AppData\Local\Programs\PostgreSQL\18\bin\psql.exe`). Remove the proxy afterwards if the owner wants it gone.
   Keep the DSN in an env var (`$env:PGURI`) for the session only. Never write it to disk, never echo it, never put it in the script.
3. **Prove the store.** Read back and wait for the owner's "yes, that's the store":
   ```sql
   SELECT system_identifier FROM pg_control_system();
   SELECT (SELECT value FROM settings WHERE key='business_name')  AS business_name,
          (SELECT value->>'seller_ntn_cnic' FROM settings WHERE key='fiscal_config') AS seller_ntn,
          (SELECT value->>'is_sandbox'       FROM settings WHERE key='fiscal_config') AS is_sandbox,
          (SELECT value->>'enabled'          FROM settings WHERE key='fiscal_config') AS fiscal_enabled;
   ```
   The `system_identifier` goes into the script's guard.
4. **Fiscal scope — the decisive question: did anything reach FBR *production*?**
   ```sql
   SELECT (SELECT count(*) FROM fiscal_invoices)                         AS filed_docs,
          (SELECT count(*) FROM fiscal_batches)                          AS batches,
          (SELECT count(*) FROM fiscal_outbound_jobs WHERE status IN ('pending','processing')) AS jobs_active,
          (SELECT count(*) FROM invoices WHERE fiscal_status IN ('synced','covered','pending')) AS invoices_filed_or_pending,
          (SELECT count(*) FROM fiscal_audit_events WHERE phase='post' AND outcome='ok') AS posts_ok;
   ```
   - All zero, or every filing was **sandbox** (owner confirms; the store has never held a production token): **wipe-fiscal** (default).
   - Any production filing: **stop**. FBR holds that invoice number; the seller cannot reverse it through DI (Debit Note is buyer-side, error 0402), and the local row is the only record the owner needs to cancel it on the portal or list it for a Credit Note in the return. Either the owner cancels on the FBR portal first and confirms, or run **preserve-fiscal** (see the variant in Step 3).
   - Any active job: stop until the queue is idle.
5. **Customers.** Default wipes them (test customers and their ledger). `--keep-customers` keeps the customer rows but still empties their ledger and receipts, so every balance restarts at zero; say that out loud in the plan, because a real customer with an opening balance would need it re-entered as an adjustment.

Never in scope: users, settings, products, `product_rate_history`. Test rate changes in history stay (the history is the audit trail of every rate the store has shown); if the owner wants it gone, that is a separate explicit ask.

---

## Step 1: Inspect (read-only)

Start every inspection session with `SET default_transaction_read_only = on;`.

### 1a — Migrations and the append-only trigger set

```sql
SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_name='invoices' AND column_name='invoice_date') AS mig006,
       EXISTS (SELECT 1 FROM settings WHERE key='day_close_enabled') AS mig007;

SELECT t.tgrelid::regclass AS tbl, t.tgname, pg_get_triggerdef(t.oid) AS def
  FROM pg_trigger t JOIN pg_proc p ON p.oid = t.tgfoid
 WHERE p.proname = 'elevon_append_only' AND NOT t.tgisinternal
 ORDER BY 1;
```

Baseline 2026-10-22: four, all rendered by `pg_get_triggerdef` as `BEFORE DELETE OR UPDATE … FOR EACH ROW EXECUTE FUNCTION elevon_append_only()` (the migrations write `UPDATE OR DELETE`; Postgres normalises the order) —
`void_log`, `customer_ledger_entries`, `day_close_audit_log`, `fiscal_audit_events`. A list, not an assertion: a new append-only table in a later migration shows up here and must be handled. Any definition with a different shape: stop.

If the latest migration in `backend/migrations/` is not reflected on the store, stop: the backend has not redeployed.

### 1b — Counts

```sql
-- WILL DELETE
SELECT 'invoices' t, count(*) FROM invoices
UNION ALL SELECT 'invoice_lines', count(*) FROM invoice_lines
UNION ALL SELECT 'void_log', count(*) FROM void_log
UNION ALL SELECT 'customer_receipts', count(*) FROM customer_receipts
UNION ALL SELECT 'customer_ledger_entries', count(*) FROM customer_ledger_entries
UNION ALL SELECT 'customers', count(*) FROM customers
UNION ALL SELECT 'business_days', count(*) FROM business_days
UNION ALL SELECT 'cash_drawer_movements', count(*) FROM cash_drawer_movements
UNION ALL SELECT 'day_close_audit_log', count(*) FROM day_close_audit_log
UNION ALL SELECT 'fiscal_invoices', count(*) FROM fiscal_invoices
UNION ALL SELECT 'fiscal_outbound_jobs', count(*) FROM fiscal_outbound_jobs
UNION ALL SELECT 'fiscal_batches', count(*) FROM fiscal_batches
UNION ALL SELECT 'fiscal_audit_events', count(*) FROM fiscal_audit_events
UNION ALL SELECT 'invoice_number_counters', count(*) FROM invoice_number_counters
UNION ALL SELECT 'receipt_number_counters', count(*) FROM receipt_number_counters;

-- WILL PRESERVE
SELECT 'users' t, count(*) FROM users
UNION ALL SELECT 'settings', count(*) FROM settings
UNION ALL SELECT 'products', count(*) FROM products
UNION ALL SELECT 'product_rate_history', count(*) FROM product_rate_history;
```

Is the till idle?
```sql
SELECT invoice_number, created_at FROM invoices WHERE created_at > now() - interval '10 minutes';
```

Also list what the owner is about to lose, so they recognise it as test data: `SELECT invoice_number, business_date, total_payable, fiscal_status FROM invoices ORDER BY created_at;` and `SELECT name, phone, ntn FROM customers;`.

### 1c — Derive the blocking set

```sql
WITH RECURSIVE
roots(tbl) AS (SELECT unnest(ARRAY['invoices','customers','business_days','fiscal_batches','customer_receipts'])::regclass),
edges AS (
  SELECT confrelid::regclass parent, conrelid::regclass child, conname, confdeltype
    FROM pg_constraint WHERE contype='f' AND confdeltype IN ('a','r','c')
),
walk(parent, child, conname, confdeltype, depth, path) AS (
  SELECT e.parent, e.child, e.conname, e.confdeltype, 1, ARRAY[e.parent::text]
    FROM edges e JOIN roots r ON e.parent = r.tbl
  UNION ALL
  SELECT e.parent, e.child, e.conname, e.confdeltype, w.depth+1, w.path || e.parent::text
    FROM edges e JOIN walk w ON e.parent = w.child
   WHERE w.confdeltype='c' AND NOT e.parent::text = ANY(w.path) AND w.depth < 6
)
SELECT DISTINCT parent, child AS empty_first, conname,
       CASE confdeltype WHEN 'a' THEN 'NO ACTION' ELSE 'RESTRICT' END AS on_delete
  FROM walk WHERE confdeltype IN ('a','r') ORDER BY 1,2;
```

Baseline 2026-10-22:

| parent | must be emptied first |
|---|---|
| `invoices` | none blocking (lines, `fiscal_invoices`, `fiscal_outbound_jobs` cascade; `void_log`, ledger, `fiscal_audit_events` SET NULL) |
| `business_days` | `invoices`, `customer_receipts` (NO ACTION); `cash_drawer_movements` cascades |
| `customers` | `invoices`, `customer_receipts`, `customer_ledger_entries` (NO ACTION) |
| `fiscal_batches` | `fiscal_audit_events.batch_id` (NO ACTION) |

Anything else the query returns is a stop condition: add it to the delete order in the right place and name it in the plan. Any table you now empty that has an append-only trigger joins the lifted set in 3b.

---

## Step 2: Present the plan, wait for "go"

- Store: **business_name**, seller NTN, `system_identifier`, DSN host masked.
- Fiscal scope and why (the Step 0.4 numbers). Customers scope.
- Will DELETE (non-zero counts), Will PRESERVE (counts).
- Blocking set, and anything new it found.
- Append-only triggers lifted and replayed.
- Will RESET: invoice and receipt counters, so the first real sale is `YYYYMMDD-001` and the first receipt `R-YYYYMMDD-001`. No business day remains: with `day_close_enabled` true the cashier opens the first day; with it false the first sale opens it.

Never proceed without an explicit "go" / "do it".

---

## Step 3: Execute — one script, dry run first

Write `scripts/store-cleanup/cleanup_<store>_<YYYY-MM-DD>.sql`. No DSN in it.

```
psql "$env:PGURI" -v ON_ERROR_STOP=1 -v commit=0 -f scripts/store-cleanup/cleanup_<store>_<date>.sql   # dry run
psql "$env:PGURI" -v ON_ERROR_STOP=1 -v commit=1 -f scripts/store-cleanup/cleanup_<store>_<date>.sql   # live
```

Show the dry run's NOTICE output, then run live.

```sql
\if :{?commit}
\else
\set commit 0
\endif

BEGIN;

-- 3a. Guards
DO $$
DECLARE sid text; busy text;
BEGIN
  SELECT system_identifier::text INTO sid FROM pg_control_system();
  IF sid <> '<system_identifier>' THEN
    RAISE EXCEPTION 'WRONG STORE: cluster % is not <store>', sid;
  END IF;
  SELECT string_agg(invoice_number, ', ') INTO busy FROM invoices WHERE created_at > now() - interval '10 minutes';
  IF busy IS NOT NULL THEN RAISE EXCEPTION 'REFUSING: till in use -- %', busy; END IF;
  IF EXISTS (SELECT 1 FROM fiscal_outbound_jobs WHERE status IN ('pending','processing')) THEN
    RAISE EXCEPTION 'REFUSING: FBR queue not idle';
  END IF;
  -- wipe-fiscal: refuse if the store is configured for production or holds a production token
  IF (SELECT (value->>'is_sandbox')::boolean FROM settings WHERE key='fiscal_config') IS FALSE
     OR (SELECT value FROM settings WHERE key='fiscal_api_key_production_enc') <> '""'::jsonb THEN
    IF (SELECT count(*) FROM fiscal_invoices) > 0 THEN
      RAISE EXCEPTION 'REFUSING: production FBR config with filed documents -- owner decision needed';
    END IF;
  END IF;
END $$;

CREATE TEMP TABLE _preserve ON COMMIT DROP AS
SELECT 'users' t, count(*) n FROM users
UNION ALL SELECT 'settings', count(*) FROM settings
UNION ALL SELECT 'products', count(*) FROM products
UNION ALL SELECT 'product_rate_history', count(*) FROM product_rate_history;

-- 3b. Lift append-only triggers on the tables this run empties; record each definition.
CREATE TEMP TABLE _trigger_baseline ON COMMIT DROP AS
SELECT t.tgname FROM pg_trigger t JOIN pg_proc p ON p.oid=t.tgfoid
 WHERE p.proname='elevon_append_only' AND NOT t.tgisinternal;

CREATE TEMP TABLE _lifted ON COMMIT DROP AS
SELECT t.tgname, t.tgrelid::regclass::text AS tbl, pg_get_triggerdef(t.oid) AS def
  FROM pg_trigger t JOIN pg_proc p ON p.oid=t.tgfoid
 WHERE p.proname='elevon_append_only' AND NOT t.tgisinternal
   AND t.tgrelid::regclass::text = ANY (ARRAY[
     'void_log','customer_ledger_entries','day_close_audit_log','fiscal_audit_events'
     -- + any append-only table Step 1c added
   ]);

DO $$
DECLARE r record;
BEGIN
  FOR r IN SELECT * FROM _lifted LOOP
    IF r.def NOT LIKE '%BEFORE DELETE OR UPDATE%elevon_append_only()%' THEN   -- pg_get_triggerdef's spelling
      RAISE EXCEPTION 'unexpected trigger shape on %: %', r.tbl, r.def;
    END IF;
    EXECUTE format('DROP TRIGGER %I ON %s', r.tgname, r.tbl);
  END LOOP;
  RAISE NOTICE 'lifted % append-only trigger(s)', (SELECT count(*) FROM _lifted);
END $$;

-- 3c. Delete, children before parents (merge in Step 1c's additions).
DELETE FROM fiscal_audit_events;        -- append-only; NO ACTION -> fiscal_batches
DELETE FROM fiscal_outbound_jobs;
DELETE FROM fiscal_invoices;
DELETE FROM customer_ledger_entries;    -- append-only; NO ACTION -> customers
DELETE FROM void_log;                   -- append-only
DELETE FROM day_close_audit_log;        -- append-only
DELETE FROM invoices;                   -- cascades invoice_lines
DELETE FROM fiscal_batches;
DELETE FROM customer_receipts;          -- NO ACTION -> customers, business_days
DELETE FROM customers;                  -- skip with --keep-customers
DELETE FROM business_days;              -- cascades cash_drawer_movements
DELETE FROM invoice_number_counters;
DELETE FROM receipt_number_counters;

-- 3d. Replay lifted triggers from their recorded definitions.
DO $$
DECLARE r record;
BEGIN
  FOR r IN SELECT * FROM _lifted LOOP EXECUTE r.def; END LOOP;
END $$;

-- 3e. Verify inside the transaction.
DO $$
DECLARE bad text;
BEGIN
  SELECT string_agg(t||'='||n, ', ') INTO bad FROM (
    SELECT 'invoices' t, count(*) n FROM invoices
    UNION ALL SELECT 'invoice_lines', count(*) FROM invoice_lines
    UNION ALL SELECT 'void_log', count(*) FROM void_log
    UNION ALL SELECT 'customer_receipts', count(*) FROM customer_receipts
    UNION ALL SELECT 'customer_ledger_entries', count(*) FROM customer_ledger_entries
    UNION ALL SELECT 'customers', count(*) FROM customers          -- drop with --keep-customers
    UNION ALL SELECT 'business_days', count(*) FROM business_days
    UNION ALL SELECT 'cash_drawer_movements', count(*) FROM cash_drawer_movements
    UNION ALL SELECT 'day_close_audit_log', count(*) FROM day_close_audit_log
    UNION ALL SELECT 'fiscal_invoices', count(*) FROM fiscal_invoices
    UNION ALL SELECT 'fiscal_outbound_jobs', count(*) FROM fiscal_outbound_jobs
    UNION ALL SELECT 'fiscal_batches', count(*) FROM fiscal_batches
    UNION ALL SELECT 'fiscal_audit_events', count(*) FROM fiscal_audit_events
    UNION ALL SELECT 'invoice_number_counters', count(*) FROM invoice_number_counters
    UNION ALL SELECT 'receipt_number_counters', count(*) FROM receipt_number_counters
  ) q WHERE n > 0;
  IF bad IS NOT NULL THEN RAISE EXCEPTION 'cleanup incomplete: %', bad; END IF;

  SELECT string_agg(b.t||': '||b.n||' -> '||c.n, ', ') INTO bad
    FROM _preserve b JOIN (
      SELECT 'users' t, count(*) n FROM users
      UNION ALL SELECT 'settings', count(*) FROM settings
      UNION ALL SELECT 'products', count(*) FROM products
      UNION ALL SELECT 'product_rate_history', count(*) FROM product_rate_history
    ) c USING (t) WHERE c.n <> b.n;
  IF bad IS NOT NULL THEN RAISE EXCEPTION 'preserved data changed: %', bad; END IF;

  SELECT string_agg(coalesce(b.tgname, '+'||n.tgname), ', ') INTO bad
    FROM _trigger_baseline b
    FULL JOIN (SELECT t.tgname FROM pg_trigger t JOIN pg_proc p ON p.oid=t.tgfoid
                WHERE p.proname='elevon_append_only' AND NOT t.tgisinternal) n USING (tgname)
   WHERE b.tgname IS NULL OR n.tgname IS NULL;
  IF bad IS NOT NULL THEN RAISE EXCEPTION 'append-only trigger set changed: %', bad; END IF;

  RAISE NOTICE 'cleanup verified: test data gone, config preserved, % append-only triggers intact',
    (SELECT count(*) FROM _trigger_baseline);
END $$;

\if :commit
COMMIT;
\else
ROLLBACK;
\endif
```

**Preserve-fiscal variant.** Drop the four fiscal `DELETE`s and leave `fiscal_audit_events` out of the lifted array. `fiscal_invoices` and `fiscal_outbound_jobs` cascade from `invoices`, so you cannot delete a filed invoice and keep its FBR record: delete only invoices with `fiscal_status IN ('off','unfiled','failed') AND fiscal_batch_id IS NULL`, keep the filed ones and their business days and customers, and say exactly which remain. Usually the right answer is to stop and let the owner decide.

**`--keep-customers`.** Skip `DELETE FROM customers` and its verify line; add `customers` to `_preserve`.

---

## Step 4: Verify independently

New connection, re-run 1b and the 1a trigger query. The trigger set must match the baseline by name; a missing one means an audit table is now mutable — fix before handing over. Load the store in Chrome: the till opens, Invoices is empty, Reports for today show zero.

## Step 5: Pre-launch checklist (report, change nothing)

1. **Seller details** in Settings → FBR are this store's own NTN, name, province, address — never another store's.
2. **Products** carry their HS codes; no HS default in code (invariant 7). `default_hs_code` setting is empty unless the owner set it.
3. **Tax rates** (`tax_rate_cash/card/online/credit`, `further_tax_rate`) — report them. Any change is a customer-visible money change: stop-and-ask with before/after (invariant 9).
4. **FBR mode**: sandbox vs production, token slots filled, `FISCAL_ALLOW_SANDBOX` still on the backend? It comes off before production filing.
5. **Day close**: `day_close_enabled` matches how the owner runs the store.
6. **Users**: `INITIAL_ADMIN_PASSWORD` removed from the backend; no leftover test counter accounts (list them; removing one is a separate ask).
7. **Access**: if a TCP proxy was added for this run, ask whether to remove it.

## Step 6: Record

`audit/STORE_CLEANUP_<store>_<YYYY-MM-DD>.md`: scope decisions, Step 0.4 fiscal numbers, derived blocking set, triggers lifted and restored, before/after counts, dry-run and live NOTICE output, Step 5 results. Keep the `.sql` in `scripts/store-cleanup/`.

---

## Never

- Touch `users`, `settings`, `products`, `product_rate_history`.
- Erase a production FBR filing's local record without the owner's explicit decision.
- Lift a trigger on a table it is not emptying, or restore one from anything but its recorded definition.
- Assert hardcoded counts or FK lists instead of deriving them.
- Commit before "go", or run live without a dry run.
- Log or persist the DSN.

## Result ladder

```
🟢 CLEAN — wiped <N> rows; <U> users, <P> products, settings intact; <T> append-only triggers intact
🟡 CLEAN WITH NOTES — wiped, but Step 5 found items (sandbox flag on, temp password set, tax rate 0, …)
🟠 ABORTED — a guard or verification failed; rolled back; store unchanged
🔴 PARTIAL — impossible in one transaction; if seen, stop and recover by hand
```
