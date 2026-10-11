-- 005_fiscal_on_demand — filing becomes the operator's action (spec
-- 2026-10-12 §2). Every statement is idempotent so a redeploy can re-run
-- the whole file safely.

-- unfiled: fiscal on, not filed. covered: filed inside a combined invoice.
ALTER TABLE invoices DROP CONSTRAINT IF EXISTS invoices_fiscal_status_check;
ALTER TABLE invoices ADD CONSTRAINT invoices_fiscal_status_check
  CHECK (fiscal_status IN ('off','unfiled','pending','synced','covered','failed'));

-- One combined FBR invoice. Totals are a snapshot of what was sent; they are
-- never edited afterwards, including when a covered invoice is later voided.
CREATE TABLE IF NOT EXISTS fiscal_batches (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  business_date DATE NOT NULL,
  status VARCHAR(12) NOT NULL DEFAULT 'pending',   -- pending | synced | failed
  fbr_invoice_number VARCHAR(64),
  invoice_count INT NOT NULL,
  total_kg NUMERIC(14,3) NOT NULL,
  value_excl_tax NUMERIC(14,2) NOT NULL,
  tax_amount NUMERIC(14,2) NOT NULL,
  further_tax_amount NUMERIC(14,2) NOT NULL,
  total_amount NUMERIC(14,2) NOT NULL,
  last_error_code VARCHAR(32),
  last_error TEXT,
  created_by UUID REFERENCES users(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  settled_at TIMESTAMPTZ
);
ALTER TABLE fiscal_batches DROP CONSTRAINT IF EXISTS fiscal_batches_status_check;
ALTER TABLE fiscal_batches ADD CONSTRAINT fiscal_batches_status_check CHECK (status IN ('pending','synced','failed'));
CREATE INDEX IF NOT EXISTS idx_fiscal_batches_date ON fiscal_batches (business_date DESC, created_at DESC);

ALTER TABLE invoices ADD COLUMN IF NOT EXISTS fiscal_batch_id UUID REFERENCES fiscal_batches(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS idx_invoices_fiscal_batch ON invoices (fiscal_batch_id) WHERE fiscal_batch_id IS NOT NULL;
-- What "pending to file" and the eligible list scan.
CREATE INDEX IF NOT EXISTS idx_invoices_fiscal_unfiled ON invoices (business_date) WHERE fiscal_status IN ('unfiled','failed');

-- The ledger, the queue and the audit trail each learn about batches. A row
-- belongs to exactly one of an invoice or a batch.
ALTER TABLE fiscal_invoices ADD COLUMN IF NOT EXISTS batch_id UUID REFERENCES fiscal_batches(id) ON DELETE CASCADE;
ALTER TABLE fiscal_invoices ALTER COLUMN invoice_id DROP NOT NULL;
ALTER TABLE fiscal_invoices DROP CONSTRAINT IF EXISTS fiscal_invoices_one_owner;
ALTER TABLE fiscal_invoices ADD CONSTRAINT fiscal_invoices_one_owner CHECK ((invoice_id IS NULL) <> (batch_id IS NULL));
ALTER TABLE fiscal_invoices DROP CONSTRAINT IF EXISTS fiscal_invoices_kind_check;
ALTER TABLE fiscal_invoices ADD CONSTRAINT fiscal_invoices_kind_check CHECK (kind IN ('sale','debit_note','batch'));
CREATE UNIQUE INDEX IF NOT EXISTS fiscal_invoices_batch_kind ON fiscal_invoices (batch_id, kind) WHERE batch_id IS NOT NULL;

ALTER TABLE fiscal_outbound_jobs ADD COLUMN IF NOT EXISTS batch_id UUID REFERENCES fiscal_batches(id) ON DELETE CASCADE;
ALTER TABLE fiscal_outbound_jobs ALTER COLUMN invoice_id DROP NOT NULL;
ALTER TABLE fiscal_outbound_jobs DROP CONSTRAINT IF EXISTS fiscal_outbound_jobs_one_owner;
ALTER TABLE fiscal_outbound_jobs ADD CONSTRAINT fiscal_outbound_jobs_one_owner CHECK ((invoice_id IS NULL) <> (batch_id IS NULL));
ALTER TABLE fiscal_outbound_jobs DROP CONSTRAINT IF EXISTS fiscal_outbound_jobs_kind_check;
ALTER TABLE fiscal_outbound_jobs ADD CONSTRAINT fiscal_outbound_jobs_kind_check CHECK (kind IN ('sale','debit_note','batch'));
CREATE UNIQUE INDEX IF NOT EXISTS fiscal_outbound_jobs_one_active_batch ON fiscal_outbound_jobs (batch_id, kind)
  WHERE batch_id IS NOT NULL AND status IN ('pending','processing');

-- The audit trail is append-only by trigger, so this FK cannot SET NULL on a
-- batch delete; batches are never deleted, so it is plain RESTRICT.
ALTER TABLE fiscal_audit_events ADD COLUMN IF NOT EXISTS batch_id UUID REFERENCES fiscal_batches(id);
CREATE INDEX IF NOT EXISTS idx_fiscal_audit_batch ON fiscal_audit_events (batch_id, created_at DESC) WHERE batch_id IS NOT NULL;

-- Two token slots. A token saved under the old single key was a sandbox
-- token (no production token has ever been issued to a store).
INSERT INTO settings (key, value)
  SELECT 'fiscal_api_key_sandbox_enc', value FROM settings WHERE key = 'fiscal_api_key_enc'
  ON CONFLICT (key) DO NOTHING;
INSERT INTO settings (key, value) VALUES
  ('fiscal_api_key_sandbox_enc', '""'), ('fiscal_api_key_production_enc', '""')
  ON CONFLICT (key) DO NOTHING;
DELETE FROM settings WHERE key = 'fiscal_api_key_enc';
