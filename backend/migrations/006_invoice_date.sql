-- 006_invoice_date — back-dated invoices (spec 2026-10-12 §8). Every
-- statement is idempotent so a redeploy can re-run the whole file safely.

-- The date printed on the document and sent to FBR. business_date stays the
-- day the sale is booked, drawered and reported on; the two differ only when
-- an admin back-dated the sale.
ALTER TABLE invoices ADD COLUMN IF NOT EXISTS invoice_date DATE;
UPDATE invoices SET invoice_date = business_date WHERE invoice_date IS NULL;

-- A writer that omits invoice_date gets the business date, so no path (and
-- no test seed) can leave a document undated.
CREATE OR REPLACE FUNCTION elevon_default_invoice_date() RETURNS trigger AS $$
BEGIN
  IF NEW.invoice_date IS NULL THEN
    NEW.invoice_date := NEW.business_date;
  END IF;
  RETURN NEW;
END;
$$ LANGUAGE plpgsql;
DROP TRIGGER IF EXISTS invoices_default_invoice_date ON invoices;
CREATE TRIGGER invoices_default_invoice_date BEFORE INSERT ON invoices FOR EACH ROW EXECUTE FUNCTION elevon_default_invoice_date();

ALTER TABLE invoices ALTER COLUMN invoice_date SET NOT NULL;

-- What the File with FBR screen and the pending summary scan (by document
-- date, not booking date).
CREATE INDEX IF NOT EXISTS idx_invoices_fiscal_unfiled_invoice_date ON invoices (invoice_date) WHERE fiscal_status IN ('unfiled','failed');
