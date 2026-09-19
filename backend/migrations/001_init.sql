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
