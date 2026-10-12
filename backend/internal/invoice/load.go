package invoice

import (
	"database/sql"

	"elevon-backend/internal/models"

	"github.com/google/uuid"
)

// Querier is the slice of database/sql the loaders need. *sql.DB and *sql.Tx
// both satisfy it, so a caller can read inside its own transaction.
type Querier interface {
	QueryRow(query string, args ...any) *sql.Row
	Query(query string, args ...any) (*sql.Rows, error)
}

// RowScanner is what *sql.Row and *sql.Rows have in common, so one scanner
// serves a single-row read and a cursor over many.
type RowScanner interface {
	Scan(dest ...interface{}) error
}

// Columns is the invoices projection every read shares — a single list so a
// new column cannot be added to one query and forgotten in another. Money
// columns are cast to float8 on the way out so lib/pq hands back numbers
// rather than NUMERIC strings; total_payable is cast to bigint because it is
// whole rupees and scans into an int64.
const Columns = `id, invoice_number, client_op_id, business_day_id, business_date, invoice_date, status,
	cashier_id, cashier_name, customer_id, customer_name, customer_phone, customer_ntn, customer_cnic,
	subtotal::float8, discount_amount::float8, discount_percent::float8, tax_rate::float8,
	tax_amount::float8, further_tax_amount::float8, total_amount::float8, rounding_adjustment::float8,
	total_payable::bigint, payment_method, payment_reference, payment_sub_method, notes,
	fiscal_status, fiscal_invoice_number, fiscal_void_status, fiscal_debit_note_number, fiscal_batch_id,
	created_at, voided_at, voided_by, void_reason`

// LineColumns is the invoice_lines projection, on the same rule as Columns.
const LineColumns = `id, invoice_id, product_id, product_name, hs_code, fbr_uom,
	quantity::float8, entered_as, gross_weight::float8, tare_weight::float8,
	unit_price::float8, line_total::float8, line_discount::float8, line_tax::float8, sort_order,
	list_rate::float8, rate_approved_by, rate_approved_by_name`

// ScanInvoice reads one Columns row.
func ScanInvoice(row RowScanner) (models.Invoice, error) {
	var inv models.Invoice
	err := row.Scan(&inv.ID, &inv.InvoiceNumber, &inv.ClientOpID, &inv.BusinessDayID, &inv.BusinessDate, &inv.InvoiceDate, &inv.Status,
		&inv.CashierID, &inv.CashierName, &inv.CustomerID, &inv.CustomerName, &inv.CustomerPhone, &inv.CustomerNTN, &inv.CustomerCNIC,
		&inv.Subtotal, &inv.DiscountAmount, &inv.DiscountPercent, &inv.TaxRate,
		&inv.TaxAmount, &inv.FurtherTaxAmount, &inv.TotalAmount, &inv.RoundingAdjustment,
		&inv.TotalPayable, &inv.PaymentMethod, &inv.PaymentReference, &inv.PaymentSubMethod, &inv.Notes,
		&inv.FiscalStatus, &inv.FiscalInvoiceNumber, &inv.FiscalVoidStatus, &inv.FiscalDebitNoteNumber, &inv.FiscalBatchID,
		&inv.CreatedAt, &inv.VoidedAt, &inv.VoidedBy, &inv.VoidReason)
	return inv, err
}

// ScanLine reads one LineColumns row.
func ScanLine(row RowScanner) (models.InvoiceLine, error) {
	var l models.InvoiceLine
	err := row.Scan(&l.ID, &l.InvoiceID, &l.ProductID, &l.ProductName, &l.HSCode, &l.FBRUoM,
		&l.Quantity, &l.EnteredAs, &l.GrossWeight, &l.TareWeight,
		&l.UnitPrice, &l.LineTotal, &l.LineDiscount, &l.LineTax, &l.SortOrder,
		&l.ListRate, &l.RateApprovedBy, &l.RateApprovedByName)
	return l, err
}

// LoadLines reads one invoice's lines in cart order.
func LoadLines(q Querier, invoiceID uuid.UUID) ([]models.InvoiceLine, error) {
	rows, err := q.Query(`SELECT `+LineColumns+` FROM invoice_lines WHERE invoice_id = $1 ORDER BY sort_order, id`, invoiceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	lines := []models.InvoiceLine{}
	for rows.Next() {
		l, err := ScanLine(rows)
		if err != nil {
			return nil, err
		}
		lines = append(lines, l)
	}
	return lines, rows.Err()
}

// LoadWithLines reads a whole invoice. sql.ErrNoRows means no such id.
//
// This lives here rather than in handlers because more than the HTTP layer
// needs it: internal/fiscal builds its FBR payload from the same rows, and
// fiscal must not import handlers. This package depends only on models, so
// anything may depend on it.
func LoadWithLines(q Querier, id uuid.UUID) (models.Invoice, error) {
	inv, err := ScanInvoice(q.QueryRow(`SELECT `+Columns+` FROM invoices WHERE id = $1`, id))
	if err != nil {
		return models.Invoice{}, err
	}
	lines, err := LoadLines(q, id)
	if err != nil {
		return models.Invoice{}, err
	}
	inv.Lines = lines
	return inv, nil
}
