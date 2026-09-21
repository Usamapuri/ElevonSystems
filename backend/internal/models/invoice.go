package models

import (
	"time"

	"github.com/google/uuid"
)

// InvoiceLine is one invoice_lines row. Every money field on it was computed
// server-side by pricing.ComputeTotals from products.rate and the submitted
// quantity (spec §6.3) — none of them can be set by a client. product_name,
// hs_code and fbr_uom are snapshots taken at sale time, so renaming a product
// or fixing its HS code later never rewrites history or a filed invoice.
type InvoiceLine struct {
	ID          uuid.UUID  `json:"id"`
	InvoiceID   uuid.UUID  `json:"invoice_id"`
	ProductID   *uuid.UUID `json:"product_id"`
	ProductName string     `json:"product_name"`
	HSCode      *string    `json:"hs_code"`
	FBRUoM      *string    `json:"fbr_uom"`

	// Quantity is always net kg to 3 dp, whatever the pad mode was.
	// EnteredAs records that mode (kg / tonne / amount / gross_tare) so a
	// reprint can show the sale the way the cashier rang it up; GrossWeight
	// and TareWeight are filled in for gross_tare only.
	Quantity    float64  `json:"quantity"`
	EnteredAs   *string  `json:"entered_as"`
	GrossWeight *float64 `json:"gross_weight"`
	TareWeight  *float64 `json:"tare_weight"`

	UnitPrice    float64 `json:"unit_price"`
	LineTotal    float64 `json:"line_total"`
	LineDiscount float64 `json:"line_discount"`
	LineTax      float64 `json:"line_tax"`
	SortOrder    int     `json:"sort_order"`
}

// Invoice is one invoices row. It is also the list shape: Lines is omitted
// from a listing (omitempty) and populated on the single-invoice reads and on
// the create/void responses, so the till, the browser and the reprint path
// all decode one type.
//
// Customer name/phone/ntn/cnic and CashierName are snapshots for the same
// reason the line snapshots are: the invoice is a fiscal document and must
// keep saying what it said when it was issued.
type Invoice struct {
	ID            uuid.UUID  `json:"id"`
	InvoiceNumber string     `json:"invoice_number"`
	ClientOpID    *uuid.UUID `json:"client_op_id"`
	BusinessDayID uuid.UUID  `json:"business_day_id"`
	BusinessDate  time.Time  `json:"business_date"`
	Status        string     `json:"status"`

	CashierID   *uuid.UUID `json:"cashier_id"`
	CashierName string     `json:"cashier_name"`

	CustomerID    *uuid.UUID `json:"customer_id"`
	CustomerName  *string    `json:"customer_name"`
	CustomerPhone *string    `json:"customer_phone"`
	CustomerNTN   *string    `json:"customer_ntn"`
	CustomerCNIC  *string    `json:"customer_cnic"`

	Subtotal        float64  `json:"subtotal"`
	DiscountAmount  float64  `json:"discount_amount"`
	DiscountPercent *float64 `json:"discount_percent"`

	// TaxRate is the rate actually applied, snapshotted from settings at sale
	// time (tax_rate_<tender>), so a later settings change never restates a
	// filed invoice.
	TaxRate            float64 `json:"tax_rate"`
	TaxAmount          float64 `json:"tax_amount"`
	FurtherTaxAmount   float64 `json:"further_tax_amount"`
	TotalAmount        float64 `json:"total_amount"`
	RoundingAdjustment float64 `json:"rounding_adjustment"`

	// TotalPayable is whole rupees — the figure the customer pays and the
	// ledger carries (spec D9).
	TotalPayable int64 `json:"total_payable"`

	PaymentMethod    string  `json:"payment_method"`
	PaymentReference *string `json:"payment_reference"`
	PaymentSubMethod *string `json:"payment_sub_method"`
	Notes            *string `json:"notes"`

	FiscalStatus        string  `json:"fiscal_status"`
	FiscalInvoiceNumber *string `json:"fiscal_invoice_number"`

	// FiscalVoidStatus and FiscalDebitNoteNumber track the debit note that
	// reverses this sale with FBR. They live here rather than on void_log
	// because void_log is append-only and this state moves as the filing
	// progresses. FiscalVoidStatus is 'unfiled' until a void is filed.
	FiscalVoidStatus      string  `json:"fiscal_void_status"`
	FiscalDebitNoteNumber *string `json:"fiscal_debit_note_number"`

	CreatedAt  time.Time  `json:"created_at"`
	VoidedAt   *time.Time `json:"voided_at"`
	VoidedBy   *uuid.UUID `json:"voided_by"`
	VoidReason *string    `json:"void_reason"`

	Lines []InvoiceLine `json:"lines,omitempty"`

	// CustomerBalanceAfter is the customer's ledger balance immediately after
	// this operation, so the receipt can print "On account — balance now
	// Rs X" without a second round-trip. Set on the POST responses that move
	// a balance (a credit sale, a void of one) and null everywhere else —
	// on a later read it would be today's balance, not the balance this
	// document left behind, which is a different and misleading number.
	CustomerBalanceAfter *float64 `json:"customer_balance_after"`
}

// InvoiceLineRequest is one cart line as submitted. There is deliberately no
// money field here: unit_price, line_total, tax and totals sent by a client
// are not ignored by convention but unrepresentable — the server prices every
// line from products.rate (spec §6.2).
type InvoiceLineRequest struct {
	ProductID   string   `json:"product_id"`
	Quantity    float64  `json:"quantity"`
	EnteredAs   string   `json:"entered_as"`
	GrossWeight *float64 `json:"gross_weight"`
	TareWeight  *float64 `json:"tare_weight"`
}

// CreateInvoiceRequest is POST /invoices: create and settle in one call.
//
// ClientOpID is the till's idempotency key: a repeat POST with the same UUID
// returns the invoice already created rather than ringing the sale twice. It
// is mandatory — blank or not a UUID is refused with invalid_request, since
// an optional idempotency key is no guarantee at all.
// Pin is the admin credit-limit override and is never stored.
type CreateInvoiceRequest struct {
	ClientOpID       string               `json:"client_op_id"`
	Lines            []InvoiceLineRequest `json:"lines"`
	DiscountAmount   float64              `json:"discount_amount"`
	DiscountPercent  *float64             `json:"discount_percent"`
	CustomerID       string               `json:"customer_id"`
	PaymentMethod    string               `json:"payment_method"`
	PaymentSubMethod string               `json:"payment_sub_method"`
	PaymentReference string               `json:"payment_reference"`
	Notes            string               `json:"notes"`
	Pin              string               `json:"pin"`
}

// VoidInvoiceRequest voids a whole invoice (spec §6.4). The PIN is the
// authority — the signed-in cashier is recorded as who did it, the PIN holder
// as who allowed it.
type VoidInvoiceRequest struct {
	Reason string `json:"reason"`
	Pin    string `json:"pin"`
}

// Receipt is one customer_receipts row: money taken against an account, not
// against a specific invoice (spec §6.5).
type Receipt struct {
	ID             uuid.UUID  `json:"id"`
	ReceiptNumber  string     `json:"receipt_number"`
	CustomerID     uuid.UUID  `json:"customer_id"`
	CustomerName   string     `json:"customer_name"`
	Amount         float64    `json:"amount"`
	Method         string     `json:"method"`
	SubMethod      *string    `json:"sub_method"`
	Reference      *string    `json:"reference"`
	BusinessDayID  uuid.UUID  `json:"business_day_id"`
	BusinessDate   time.Time  `json:"business_date"`
	ReceivedBy     *uuid.UUID `json:"received_by"`
	ReceivedByName string     `json:"received_by_name"`
	Note           *string    `json:"note"`
	CreatedAt      time.Time  `json:"created_at"`
	VoidedAt       *time.Time `json:"voided_at"`
	VoidedBy       *uuid.UUID `json:"voided_by"`
	VoidReason     *string    `json:"void_reason"`

	// CustomerBalanceAfter carries the same meaning as on Invoice: the
	// balance immediately after this receipt (or its void) was posted.
	CustomerBalanceAfter *float64 `json:"customer_balance_after"`
}

// CreateReceiptRequest is POST /customers/:id/receipts. Credit is not a
// method here: a receipt is money arriving, so it can only be cash, card or
// online (customer_receipts_method_check).
type CreateReceiptRequest struct {
	Amount    float64 `json:"amount"`
	Method    string  `json:"method"`
	SubMethod string  `json:"sub_method"`
	Reference string  `json:"reference"`
	Note      string  `json:"note"`
}

// VoidReceiptRequest voids a receipt against an admin PIN, mirroring the
// ledger entry (spec §6.5).
type VoidReceiptRequest struct {
	Reason string `json:"reason"`
	Pin    string `json:"pin"`
}
