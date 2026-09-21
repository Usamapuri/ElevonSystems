-- 003_fiscal — FBR Digital Invoicing (Phase 7). Every statement is
-- idempotent so a redeploy can re-run the whole file safely.

-- One row per successfully posted DI document. The retry path consults this
-- table first (CLAUDE.md invariant 7) so a resubmission can never file the
-- same invoice twice; the unique index is what makes that check reliable.
CREATE TABLE IF NOT EXISTS fiscal_invoices (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  invoice_id UUID NOT NULL REFERENCES invoices(id) ON DELETE CASCADE,
  kind VARCHAR(12) NOT NULL,                     -- sale | debit_note
  fbr_invoice_number VARCHAR(64) NOT NULL,
  response JSONB,
  submitted_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
ALTER TABLE fiscal_invoices DROP CONSTRAINT IF EXISTS fiscal_invoices_kind_check;
ALTER TABLE fiscal_invoices ADD CONSTRAINT fiscal_invoices_kind_check CHECK (kind IN ('sale','debit_note'));
CREATE UNIQUE INDEX IF NOT EXISTS fiscal_invoices_invoice_kind ON fiscal_invoices (invoice_id, kind);

-- The outbound queue. One active job per (invoice, kind); the partial unique
-- index enforces that, and idx_fiscal_jobs_dequeue serves the worker's claim.
CREATE TABLE IF NOT EXISTS fiscal_outbound_jobs (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  invoice_id UUID NOT NULL REFERENCES invoices(id) ON DELETE CASCADE,
  kind VARCHAR(12) NOT NULL,                     -- sale | debit_note
  status VARCHAR(12) NOT NULL DEFAULT 'pending', -- pending | processing | succeeded | dead
  attempt_count INT NOT NULL DEFAULT 0,
  max_attempts INT NOT NULL DEFAULT 20,
  next_run_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  last_error TEXT,
  last_error_code VARCHAR(32),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  started_at TIMESTAMPTZ,
  succeeded_at TIMESTAMPTZ
);
ALTER TABLE fiscal_outbound_jobs DROP CONSTRAINT IF EXISTS fiscal_outbound_jobs_kind_check;
ALTER TABLE fiscal_outbound_jobs ADD CONSTRAINT fiscal_outbound_jobs_kind_check CHECK (kind IN ('sale','debit_note'));
ALTER TABLE fiscal_outbound_jobs DROP CONSTRAINT IF EXISTS fiscal_outbound_jobs_status_check;
ALTER TABLE fiscal_outbound_jobs ADD CONSTRAINT fiscal_outbound_jobs_status_check CHECK (status IN ('pending','processing','succeeded','dead'));
CREATE UNIQUE INDEX IF NOT EXISTS fiscal_outbound_jobs_one_active ON fiscal_outbound_jobs (invoice_id, kind) WHERE status IN ('pending','processing');
CREATE INDEX IF NOT EXISTS idx_fiscal_jobs_dequeue ON fiscal_outbound_jobs (status, next_run_at, created_at);

-- Every conversation with PRAL, kept for the tax advisor. Append-only.
CREATE TABLE IF NOT EXISTS fiscal_audit_events (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  invoice_id UUID REFERENCES invoices(id) ON DELETE SET NULL,
  kind VARCHAR(12) NOT NULL,                     -- sale | debit_note | test | reference
  phase VARCHAR(24) NOT NULL,                    -- validate | post | ledger_recovery | refused | reference
  outcome VARCHAR(16) NOT NULL,                  -- ok | rejected | unreachable | refused
  http_status INT,
  latency_ms BIGINT,
  error_code VARCHAR(32),
  request_excerpt TEXT,
  response_excerpt TEXT,
  created_by UUID REFERENCES users(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_fiscal_audit_invoice ON fiscal_audit_events (invoice_id, created_at DESC);
-- Append-only, using the trigger function 002 defines for void_log and the
-- ledger (CREATE OR REPLACE there makes re-running both files safe).
DROP TRIGGER IF EXISTS fiscal_audit_events_append_only ON fiscal_audit_events;
CREATE TRIGGER fiscal_audit_events_append_only BEFORE UPDATE OR DELETE ON fiscal_audit_events FOR EACH ROW EXECUTE FUNCTION elevon_append_only();

-- Where a void stands with FBR: unfiled (never filed — the buyer was not
-- registered, or fiscal was off), pending, synced, failed.
--
-- This state lives on invoices, not on void_log, because void_log is
-- append-only (CLAUDE.md invariant 6) and a debit note's status has to move
-- as the filing progresses. fiscal_debit_note_number is FBR's invoiceNumber
-- for the debit note, the counterpart of invoices.fiscal_invoice_number for
-- the original sale.
ALTER TABLE invoices ADD COLUMN IF NOT EXISTS fiscal_void_status VARCHAR(12) NOT NULL DEFAULT 'unfiled';
ALTER TABLE invoices ADD COLUMN IF NOT EXISTS fiscal_debit_note_number VARCHAR(64);
ALTER TABLE invoices DROP CONSTRAINT IF EXISTS invoices_fiscal_void_status_check;
ALTER TABLE invoices ADD CONSTRAINT invoices_fiscal_void_status_check CHECK (fiscal_void_status IN ('unfiled','pending','synced','failed'));

INSERT INTO settings (key, value) VALUES
  ('fiscal_config', '{"enabled":false,"is_sandbox":true,"seller_ntn_cnic":"","seller_business_name":"","seller_province":"","seller_address":"","scenario_registered":"SN001","scenario_unregistered":"SN002","rate_desc":"18%","sale_type":"Goods at Standard Rate (default)","trans_type_id":75,"default_uom":"KG","buyer_registration_default":"Unregistered","validate_url":"","post_url":""}'),
  ('fiscal_api_key_enc', '""'),
  ('fiscal_reference', '{}')
ON CONFLICT (key) DO NOTHING;
