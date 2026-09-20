-- 002_trading — products, rates, customers, ledger, business days, invoices (spec §5.2–5.5)

CREATE TABLE IF NOT EXISTS products (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  name VARCHAR(120) NOT NULL,
  sku VARCHAR(40),
  sell_by VARCHAR(8) NOT NULL DEFAULT 'weight',
  unit_label VARCHAR(12) NOT NULL DEFAULT 'kg',
  rate NUMERIC(12,2) NOT NULL,
  hs_code VARCHAR(12),
  fbr_uom VARCHAR(32),
  sort_order INT NOT NULL DEFAULT 0,
  is_active BOOLEAN NOT NULL DEFAULT true,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
ALTER TABLE products DROP CONSTRAINT IF EXISTS products_sell_by_check;
ALTER TABLE products ADD CONSTRAINT products_sell_by_check CHECK (sell_by IN ('weight','unit'));
ALTER TABLE products DROP CONSTRAINT IF EXISTS products_hs_code_check;
ALTER TABLE products ADD CONSTRAINT products_hs_code_check CHECK (hs_code IS NULL OR hs_code ~ '^[0-9]{4}\.[0-9]{4}$');
ALTER TABLE products DROP CONSTRAINT IF EXISTS products_rate_check;
ALTER TABLE products ADD CONSTRAINT products_rate_check CHECK (rate >= 0);
CREATE UNIQUE INDEX IF NOT EXISTS uniq_products_sku ON products (sku) WHERE sku IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS uniq_products_name_lower ON products (lower(name));

CREATE TABLE IF NOT EXISTS product_rate_history (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  product_id UUID NOT NULL REFERENCES products(id) ON DELETE CASCADE,
  old_rate NUMERIC(12,2) NOT NULL,
  new_rate NUMERIC(12,2) NOT NULL,
  changed_by UUID REFERENCES users(id) ON DELETE SET NULL,
  changed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  note TEXT
);
CREATE INDEX IF NOT EXISTS idx_rate_history_product ON product_rate_history (product_id, changed_at DESC);

CREATE TABLE IF NOT EXISTS customers (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  name VARCHAR(120) NOT NULL,
  phone VARCHAR(30),
  ntn VARCHAR(20),
  cnic VARCHAR(15),
  buyer_registration_type VARCHAR(12) NOT NULL DEFAULT 'Unregistered',
  address TEXT,
  province VARCHAR(40),
  credit_allowed BOOLEAN NOT NULL DEFAULT false,
  credit_limit NUMERIC(12,2),
  is_active BOOLEAN NOT NULL DEFAULT true,
  notes TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
ALTER TABLE customers DROP CONSTRAINT IF EXISTS customers_buyer_type_check;
ALTER TABLE customers ADD CONSTRAINT customers_buyer_type_check CHECK (buyer_registration_type IN ('Registered','Unregistered'));
CREATE UNIQUE INDEX IF NOT EXISTS uniq_customers_phone_lower ON customers (lower(phone)) WHERE phone IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_customers_name_lower ON customers (lower(name));

CREATE TABLE IF NOT EXISTS business_days (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  business_date DATE NOT NULL UNIQUE,
  status VARCHAR(10) NOT NULL,
  opened_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  opened_by UUID REFERENCES users(id) ON DELETE SET NULL,
  opening_cash NUMERIC(12,2) NOT NULL DEFAULT 0,
  opening_notes TEXT,
  closed_at TIMESTAMPTZ,
  closed_by UUID REFERENCES users(id) ON DELETE SET NULL,
  counted_cash NUMERIC(12,2), counted_card NUMERIC(12,2), counted_online NUMERIC(12,2),
  expected_cash NUMERIC(12,2), expected_card NUMERIC(12,2), expected_online NUMERIC(12,2),
  cash_variance NUMERIC(12,2), card_variance NUMERIC(12,2), online_variance NUMERIC(12,2),
  gross_sales NUMERIC(12,2), discounts NUMERIC(12,2), tax_collected NUMERIC(12,2), net_sales NUMERIC(12,2),
  on_account_sales NUMERIC(12,2), receipts_collected NUMERIC(12,2),
  invoice_count INT, void_count INT,
  closing_notes TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
ALTER TABLE business_days DROP CONSTRAINT IF EXISTS business_days_status_check;
ALTER TABLE business_days ADD CONSTRAINT business_days_status_check CHECK (status IN ('open','closed','reopened'));
CREATE UNIQUE INDEX IF NOT EXISTS uniq_business_days_single_open ON business_days ((true)) WHERE status IN ('open','reopened');

CREATE TABLE IF NOT EXISTS cash_drawer_movements (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  business_day_id UUID NOT NULL REFERENCES business_days(id) ON DELETE CASCADE,
  movement_type VARCHAR(10) NOT NULL,
  amount NUMERIC(12,2) NOT NULL,
  reason VARCHAR(200) NOT NULL,
  notes TEXT,
  created_by UUID REFERENCES users(id) ON DELETE SET NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
ALTER TABLE cash_drawer_movements DROP CONSTRAINT IF EXISTS cash_drawer_movements_type_check;
ALTER TABLE cash_drawer_movements ADD CONSTRAINT cash_drawer_movements_type_check CHECK (movement_type IN ('paid_in','paid_out'));
ALTER TABLE cash_drawer_movements DROP CONSTRAINT IF EXISTS cash_drawer_movements_amount_check;
ALTER TABLE cash_drawer_movements ADD CONSTRAINT cash_drawer_movements_amount_check CHECK (amount > 0);

CREATE TABLE IF NOT EXISTS day_close_audit_log (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  business_day_id UUID REFERENCES business_days(id) ON DELETE SET NULL,
  business_date DATE NOT NULL,
  action VARCHAR(40) NOT NULL,
  actor_id UUID, actor_name VARCHAR(100), actor_role VARCHAR(20),
  summary TEXT NOT NULL,
  metadata JSONB,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS invoices (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  invoice_number VARCHAR(16) NOT NULL UNIQUE,
  client_op_id UUID UNIQUE,
  business_day_id UUID NOT NULL REFERENCES business_days(id),
  business_date DATE NOT NULL,
  status VARCHAR(10) NOT NULL DEFAULT 'completed',
  cashier_id UUID REFERENCES users(id) ON DELETE SET NULL,
  cashier_name VARCHAR(100) NOT NULL DEFAULT '',
  customer_id UUID REFERENCES customers(id),
  customer_name VARCHAR(120), customer_phone VARCHAR(30), customer_ntn VARCHAR(20), customer_cnic VARCHAR(15),
  subtotal NUMERIC(12,2) NOT NULL,
  discount_amount NUMERIC(12,2) NOT NULL DEFAULT 0,
  discount_percent NUMERIC(5,2),
  tax_rate NUMERIC(6,4) NOT NULL,
  tax_amount NUMERIC(12,2) NOT NULL DEFAULT 0,
  further_tax_amount NUMERIC(12,2) NOT NULL DEFAULT 0,
  total_amount NUMERIC(12,2) NOT NULL,
  rounding_adjustment NUMERIC(4,2) NOT NULL DEFAULT 0,
  total_payable NUMERIC(12,0) NOT NULL,
  payment_method VARCHAR(10) NOT NULL,
  payment_reference VARCHAR(100), payment_sub_method VARCHAR(30),
  notes TEXT,
  fiscal_status VARCHAR(12) NOT NULL DEFAULT 'off',
  fiscal_invoice_number VARCHAR(64),
  fiscal_details JSONB,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  voided_at TIMESTAMPTZ, voided_by UUID REFERENCES users(id) ON DELETE SET NULL, void_reason TEXT
);
ALTER TABLE invoices DROP CONSTRAINT IF EXISTS invoices_status_check;
ALTER TABLE invoices ADD CONSTRAINT invoices_status_check CHECK (status IN ('completed','voided'));
ALTER TABLE invoices DROP CONSTRAINT IF EXISTS invoices_payment_method_check;
ALTER TABLE invoices ADD CONSTRAINT invoices_payment_method_check CHECK (payment_method IN ('cash','card','online','credit'));
ALTER TABLE invoices DROP CONSTRAINT IF EXISTS invoices_fiscal_status_check;
ALTER TABLE invoices ADD CONSTRAINT invoices_fiscal_status_check CHECK (fiscal_status IN ('off','pending','synced','failed'));
ALTER TABLE invoices DROP CONSTRAINT IF EXISTS invoices_credit_needs_customer;
ALTER TABLE invoices ADD CONSTRAINT invoices_credit_needs_customer CHECK (payment_method <> 'credit' OR customer_id IS NOT NULL);
CREATE INDEX IF NOT EXISTS idx_invoices_business_date ON invoices (business_date, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_invoices_customer ON invoices (customer_id, created_at DESC);

CREATE TABLE IF NOT EXISTS invoice_lines (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  invoice_id UUID NOT NULL REFERENCES invoices(id) ON DELETE CASCADE,
  product_id UUID REFERENCES products(id) ON DELETE SET NULL,
  product_name VARCHAR(120) NOT NULL,
  hs_code VARCHAR(12), fbr_uom VARCHAR(32),
  quantity NUMERIC(14,3) NOT NULL,
  entered_as VARCHAR(12),
  gross_weight NUMERIC(14,3), tare_weight NUMERIC(14,3),
  unit_price NUMERIC(12,2) NOT NULL,
  line_total NUMERIC(12,2) NOT NULL,
  line_discount NUMERIC(12,2) NOT NULL DEFAULT 0,
  line_tax NUMERIC(12,2) NOT NULL DEFAULT 0,
  sort_order INT NOT NULL DEFAULT 0
);
ALTER TABLE invoice_lines DROP CONSTRAINT IF EXISTS invoice_lines_quantity_check;
ALTER TABLE invoice_lines ADD CONSTRAINT invoice_lines_quantity_check CHECK (quantity > 0);
ALTER TABLE invoice_lines DROP CONSTRAINT IF EXISTS invoice_lines_entered_as_check;
ALTER TABLE invoice_lines ADD CONSTRAINT invoice_lines_entered_as_check CHECK (entered_as IS NULL OR entered_as IN ('kg','tonne','amount','gross_tare'));
CREATE INDEX IF NOT EXISTS idx_invoice_lines_invoice ON invoice_lines (invoice_id, sort_order);

CREATE TABLE IF NOT EXISTS void_log (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  invoice_id UUID REFERENCES invoices(id) ON DELETE SET NULL,
  invoice_number VARCHAR(16) NOT NULL,
  voided_by UUID REFERENCES users(id) ON DELETE SET NULL,
  authorized_by UUID REFERENCES users(id) ON DELETE SET NULL,
  total_payable NUMERIC(12,0) NOT NULL,
  reason TEXT NOT NULL,
  fiscal_credit_note VARCHAR(64),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS customer_receipts (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  receipt_number VARCHAR(16) NOT NULL UNIQUE,
  customer_id UUID NOT NULL REFERENCES customers(id),
  amount NUMERIC(12,2) NOT NULL,
  method VARCHAR(10) NOT NULL,
  sub_method VARCHAR(30), reference VARCHAR(100),
  business_day_id UUID NOT NULL REFERENCES business_days(id),
  business_date DATE NOT NULL,
  received_by UUID REFERENCES users(id) ON DELETE SET NULL,
  note TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  voided_at TIMESTAMPTZ, voided_by UUID REFERENCES users(id) ON DELETE SET NULL, void_reason TEXT
);
ALTER TABLE customer_receipts DROP CONSTRAINT IF EXISTS customer_receipts_amount_check;
ALTER TABLE customer_receipts ADD CONSTRAINT customer_receipts_amount_check CHECK (amount > 0);
ALTER TABLE customer_receipts DROP CONSTRAINT IF EXISTS customer_receipts_method_check;
ALTER TABLE customer_receipts ADD CONSTRAINT customer_receipts_method_check CHECK (method IN ('cash','card','online'));

CREATE TABLE IF NOT EXISTS customer_ledger_entries (
  id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  customer_id UUID NOT NULL REFERENCES customers(id),
  entry_type VARCHAR(16) NOT NULL,
  invoice_id UUID REFERENCES invoices(id) ON DELETE SET NULL,
  receipt_id UUID REFERENCES customer_receipts(id) ON DELETE SET NULL,
  debit NUMERIC(12,2) NOT NULL DEFAULT 0,
  credit NUMERIC(12,2) NOT NULL DEFAULT 0,
  business_date DATE NOT NULL,
  created_by UUID REFERENCES users(id) ON DELETE SET NULL,
  note TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
ALTER TABLE customer_ledger_entries DROP CONSTRAINT IF EXISTS customer_ledger_entries_type_check;
ALTER TABLE customer_ledger_entries ADD CONSTRAINT customer_ledger_entries_type_check CHECK (entry_type IN ('invoice','invoice_void','receipt','receipt_void','adjustment'));
CREATE INDEX IF NOT EXISTS idx_ledger_customer ON customer_ledger_entries (customer_id, created_at);

CREATE TABLE IF NOT EXISTS invoice_number_counters (
  business_date DATE PRIMARY KEY,
  last_value INT NOT NULL DEFAULT 0
);
ALTER TABLE invoice_number_counters DROP CONSTRAINT IF EXISTS invoice_number_counters_last_value_check;
ALTER TABLE invoice_number_counters ADD CONSTRAINT invoice_number_counters_last_value_check CHECK (last_value >= 0);
CREATE TABLE IF NOT EXISTS receipt_number_counters (
  business_date DATE PRIMARY KEY,
  last_value INT NOT NULL DEFAULT 0
);
ALTER TABLE receipt_number_counters DROP CONSTRAINT IF EXISTS receipt_number_counters_last_value_check;
ALTER TABLE receipt_number_counters ADD CONSTRAINT receipt_number_counters_last_value_check CHECK (last_value >= 0);

-- Append-only tables: any UPDATE or DELETE raises.
CREATE OR REPLACE FUNCTION elevon_append_only() RETURNS trigger AS $$
BEGIN
  RAISE EXCEPTION '% is append-only', TG_TABLE_NAME USING ERRCODE = 'restrict_violation';
END;
$$ LANGUAGE plpgsql;
DROP TRIGGER IF EXISTS void_log_append_only ON void_log;
CREATE TRIGGER void_log_append_only BEFORE UPDATE OR DELETE ON void_log FOR EACH ROW EXECUTE FUNCTION elevon_append_only();
DROP TRIGGER IF EXISTS ledger_append_only ON customer_ledger_entries;
CREATE TRIGGER ledger_append_only BEFORE UPDATE OR DELETE ON customer_ledger_entries FOR EACH ROW EXECUTE FUNCTION elevon_append_only();
DROP TRIGGER IF EXISTS day_close_audit_append_only ON day_close_audit_log;
CREATE TRIGGER day_close_audit_append_only BEFORE UPDATE OR DELETE ON day_close_audit_log FOR EACH ROW EXECUTE FUNCTION elevon_append_only();
