-- 004_deal_rates — per-invoice deal rates (spec 2026-10-12 §3). Every
-- statement is idempotent so a redeploy can re-run the whole file safely.

-- list_rate is products.rate at the moment of sale; unit_price stays the rate
-- actually charged. Every line written before this migration was charged at
-- the list rate, so the backfill is exact.
ALTER TABLE invoice_lines ADD COLUMN IF NOT EXISTS list_rate NUMERIC(12,2);
UPDATE invoice_lines SET list_rate = unit_price WHERE list_rate IS NULL;
ALTER TABLE invoice_lines ALTER COLUMN list_rate SET NOT NULL;

-- Who approved a rate outside the band. Null for a list-rate line and for a
-- deal rate inside the band. The name is a snapshot for reprints, on the
-- same reasoning as invoices.cashier_name.
ALTER TABLE invoice_lines ADD COLUMN IF NOT EXISTS rate_approved_by UUID REFERENCES users(id) ON DELETE SET NULL;
ALTER TABLE invoice_lines ADD COLUMN IF NOT EXISTS rate_approved_by_name VARCHAR(120);

-- Rupees per kg a cashier may move a rate without an admin PIN. 0 = always ask.
INSERT INTO settings (key, value) VALUES ('rate_override_band', '0') ON CONFLICT (key) DO NOTHING;
