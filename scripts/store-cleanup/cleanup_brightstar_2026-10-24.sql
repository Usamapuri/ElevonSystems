-- Bright Star LPG pre-launch cleanup, 2026-10-24 (skill elevon-store-cleanup).
-- No DSN here. Run:
--   psql "$env:PGURI" -v ON_ERROR_STOP=1 -v commit=0 -f scripts/store-cleanup/cleanup_brightstar_2026-10-24.sql   # dry run
--   psql "$env:PGURI" -v ON_ERROR_STOP=1 -v commit=1 -f scripts/store-cleanup/cleanup_brightstar_2026-10-24.sql   # live
--
-- Scope:
--   * wipe-fiscal, owner-confirmed: the store is on FBR production and holds
--     four filed sale invoices. The owner confirms all four are cancelled on
--     the FBR DI portal; their numbers are kept in
--     audit/STORE_CLEANUP_brightstar_2026-10-24.md. The guard refuses if the
--     filed set is anything other than exactly these four.
--   * customers: delete only "MUHAMMAD SANGEEN KHAN" (test customer). Keep
--     "Bright Star LPG Distributors" (J433658) as is; its ledger is empty.

\if :{?commit}
\else
\set commit 0
\endif

BEGIN;

-- 3a. Guards
DO $$
DECLARE sid text; busy text; filed text;
BEGIN
  SELECT system_identifier::text INTO sid FROM pg_control_system();
  IF sid <> '7699438549581037627' THEN
    RAISE EXCEPTION 'WRONG STORE: cluster % is not Bright Star LPG', sid;
  END IF;
  IF (SELECT value->>'seller_ntn_cnic' FROM settings WHERE key='fiscal_config') <> '6827012' THEN
    RAISE EXCEPTION 'WRONG STORE: seller NTN is not 6827012';
  END IF;
  SELECT string_agg(invoice_number, ', ') INTO busy FROM invoices WHERE created_at > now() - interval '10 minutes';
  IF busy IS NOT NULL THEN RAISE EXCEPTION 'REFUSING: till in use -- %', busy; END IF;
  IF EXISTS (SELECT 1 FROM fiscal_outbound_jobs WHERE status IN ('pending','processing')) THEN
    RAISE EXCEPTION 'REFUSING: FBR queue not idle';
  END IF;
  -- Production store: only the four owner-confirmed, portal-cancelled filings may go.
  SELECT string_agg(fbr_invoice_number, ',' ORDER BY fbr_invoice_number) INTO filed FROM fiscal_invoices;
  IF coalesce(filed, '') <> '6827012DIJJTVZ9503682,6827012DIJJVYEA928963,6827012DIM7XO0E517720,6827012DIM8MBDO900972' THEN
    RAISE EXCEPTION 'REFUSING: filed FBR set changed since the owner confirmed it -- %', filed;
  END IF;
  IF EXISTS (SELECT 1 FROM fiscal_batches) THEN
    RAISE EXCEPTION 'REFUSING: a combined FBR invoice exists that the owner has not confirmed';
  END IF;
  IF (SELECT count(*) FROM customers WHERE name = 'Bright Star LPG Distributors' AND ntn = 'J433658') <> 1 THEN
    RAISE EXCEPTION 'REFUSING: the customer to keep is not there exactly once';
  END IF;
END $$;

CREATE TEMP TABLE _preserve ON COMMIT DROP AS
SELECT 'users' t, count(*) n FROM users
UNION ALL SELECT 'settings', count(*) FROM settings
UNION ALL SELECT 'products', count(*) FROM products
UNION ALL SELECT 'product_rate_history', count(*) FROM product_rate_history;

CREATE TEMP TABLE _keep_customer ON COMMIT DROP AS
SELECT id, name, phone, ntn, cnic, buyer_registration_type, province, address, credit_allowed, credit_limit, is_active, notes
  FROM customers WHERE name = 'Bright Star LPG Distributors' AND ntn = 'J433658';

-- 3b. Lift append-only triggers on the tables this run empties.
CREATE TEMP TABLE _trigger_baseline ON COMMIT DROP AS
SELECT t.tgname FROM pg_trigger t JOIN pg_proc p ON p.oid=t.tgfoid
 WHERE p.proname='elevon_append_only' AND NOT t.tgisinternal;

CREATE TEMP TABLE _lifted ON COMMIT DROP AS
SELECT t.tgname, t.tgrelid::regclass::text AS tbl, pg_get_triggerdef(t.oid) AS def
  FROM pg_trigger t JOIN pg_proc p ON p.oid=t.tgfoid
 WHERE p.proname='elevon_append_only' AND NOT t.tgisinternal
   AND t.tgrelid::regclass::text = ANY (ARRAY['void_log','customer_ledger_entries','day_close_audit_log','fiscal_audit_events']);

DO $$
DECLARE r record;
BEGIN
  FOR r IN SELECT * FROM _lifted LOOP
    IF r.def NOT LIKE '%BEFORE DELETE OR UPDATE%elevon_append_only()%' THEN
      RAISE EXCEPTION 'unexpected trigger shape on %: %', r.tbl, r.def;
    END IF;
    EXECUTE format('DROP TRIGGER %I ON %s', r.tgname, r.tbl);
  END LOOP;
  RAISE NOTICE 'lifted % append-only trigger(s)', (SELECT count(*) FROM _lifted);
END $$;

-- 3c. Delete, children before parents.
DELETE FROM fiscal_audit_events;
DELETE FROM fiscal_outbound_jobs;
DELETE FROM fiscal_invoices;
DELETE FROM customer_ledger_entries;
DELETE FROM void_log;
DELETE FROM day_close_audit_log;
DELETE FROM invoices;                   -- cascades invoice_lines
DELETE FROM fiscal_batches;
DELETE FROM customer_receipts;
DELETE FROM customers WHERE id NOT IN (SELECT id FROM _keep_customer);   -- Sangeen
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

  -- Exactly the kept customer remains, unchanged.
  IF (SELECT count(*) FROM customers) <> 1 OR NOT EXISTS (
       SELECT 1 FROM customers c JOIN _keep_customer k USING (id)
        WHERE (c.name, c.phone, c.ntn, c.buyer_registration_type, c.province, c.credit_allowed, c.is_active)
          IS NOT DISTINCT FROM (k.name, k.phone, k.ntn, k.buyer_registration_type, k.province, k.credit_allowed, k.is_active)) THEN
    RAISE EXCEPTION 'customers: expected only Bright Star LPG Distributors, unchanged';
  END IF;

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

  RAISE NOTICE 'cleanup verified: test data gone, 1 customer kept, config preserved, % append-only triggers intact',
    (SELECT count(*) FROM _trigger_baseline);
END $$;

\if :commit
COMMIT;
\else
ROLLBACK;
\endif
