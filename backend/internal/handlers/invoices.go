package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"elevon-backend/internal/dayops"
	"elevon-backend/internal/invoice"
	"elevon-backend/internal/ledger"
	"elevon-backend/internal/middleware"
	"elevon-backend/internal/models"
	"elevon-backend/internal/pricing"
	"elevon-backend/internal/settings"
	"elevon-backend/internal/staffpin"
	"elevon-backend/internal/util"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// InvoicesHandler is the money core: create-and-settle, the invoice browser
// and whole-invoice voids (spec §6.2, §6.4). Everything money here is
// computed by pricing.ComputeTotals from products.rate and the submitted
// quantities — a client can submit what quantities it likes and never a
// price.
type InvoicesHandler struct{ db *sql.DB }

// NewInvoicesHandler builds an InvoicesHandler.
func NewInvoicesHandler(db *sql.DB) *InvoicesHandler { return &InvoicesHandler{db: db} }

// runner is satisfied by *sql.DB and *sql.Tx, so the same read helpers serve
// a plain GET and the inside of the create/void transactions.
type runner interface {
	Query(query string, args ...any) (*sql.Rows, error)
	QueryRow(query string, args ...any) *sql.Row
	Exec(query string, args ...any) (sql.Result, error)
}

// maxInvoiceLines caps one sale. A weight till rings a handful of lines; a
// four-figure payload is a bug or an attack, not a customer.
const maxInvoiceLines = 100

// maxNotesLen bounds the free-text note (the column is TEXT).
const maxNotesLen = 2000

// Invoice money columns are cast to float8 on the way out so lib/pq hands
// back numbers rather than NUMERIC strings; total_payable is cast to bigint
// because it is whole rupees and scans into an int64.
const invoiceColumns = `id, invoice_number, client_op_id, business_day_id, business_date, status,
	cashier_id, cashier_name, customer_id, customer_name, customer_phone, customer_ntn, customer_cnic,
	subtotal::float8, discount_amount::float8, discount_percent::float8, tax_rate::float8,
	tax_amount::float8, further_tax_amount::float8, total_amount::float8, rounding_adjustment::float8,
	total_payable::bigint, payment_method, payment_reference, payment_sub_method, notes,
	fiscal_status, fiscal_invoice_number, created_at, voided_at, voided_by, void_reason`

const invoiceLineColumns = `id, invoice_id, product_id, product_name, hs_code, fbr_uom,
	quantity::float8, entered_as, gross_weight::float8, tare_weight::float8,
	unit_price::float8, line_total::float8, line_discount::float8, line_tax::float8, sort_order`

func scanInvoice(row interface {
	Scan(dest ...interface{}) error
}) (models.Invoice, error) {
	var inv models.Invoice
	err := row.Scan(&inv.ID, &inv.InvoiceNumber, &inv.ClientOpID, &inv.BusinessDayID, &inv.BusinessDate, &inv.Status,
		&inv.CashierID, &inv.CashierName, &inv.CustomerID, &inv.CustomerName, &inv.CustomerPhone, &inv.CustomerNTN, &inv.CustomerCNIC,
		&inv.Subtotal, &inv.DiscountAmount, &inv.DiscountPercent, &inv.TaxRate,
		&inv.TaxAmount, &inv.FurtherTaxAmount, &inv.TotalAmount, &inv.RoundingAdjustment,
		&inv.TotalPayable, &inv.PaymentMethod, &inv.PaymentReference, &inv.PaymentSubMethod, &inv.Notes,
		&inv.FiscalStatus, &inv.FiscalInvoiceNumber, &inv.CreatedAt, &inv.VoidedAt, &inv.VoidedBy, &inv.VoidReason)
	return inv, err
}

func scanInvoiceLine(row interface {
	Scan(dest ...interface{}) error
}) (models.InvoiceLine, error) {
	var l models.InvoiceLine
	err := row.Scan(&l.ID, &l.InvoiceID, &l.ProductID, &l.ProductName, &l.HSCode, &l.FBRUoM,
		&l.Quantity, &l.EnteredAs, &l.GrossWeight, &l.TareWeight,
		&l.UnitPrice, &l.LineTotal, &l.LineDiscount, &l.LineTax, &l.SortOrder)
	return l, err
}

// loadInvoiceLines reads one invoice's lines in cart order.
func loadInvoiceLines(q runner, invoiceID uuid.UUID) ([]models.InvoiceLine, error) {
	rows, err := q.Query(`SELECT `+invoiceLineColumns+` FROM invoice_lines WHERE invoice_id = $1 ORDER BY sort_order, id`, invoiceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	lines := []models.InvoiceLine{}
	for rows.Next() {
		l, err := scanInvoiceLine(rows)
		if err != nil {
			return nil, err
		}
		lines = append(lines, l)
	}
	return lines, rows.Err()
}

// loadInvoiceWithLines reads a whole invoice. sql.ErrNoRows means no such id.
func loadInvoiceWithLines(q runner, id uuid.UUID) (models.Invoice, error) {
	inv, err := scanInvoice(q.QueryRow(`SELECT `+invoiceColumns+` FROM invoices WHERE id = $1`, id))
	if err != nil {
		return models.Invoice{}, err
	}
	lines, err := loadInvoiceLines(q, id)
	if err != nil {
		return models.Invoice{}, err
	}
	inv.Lines = lines
	return inv, nil
}

// ── request parsing ──────────────────────────────────────────────────────

// apiError is a fully-formed refusal: an HTTP status, a stable snake_case
// code and the sentence a person reads. Parsing returns one instead of a Go
// error so the handler never has to guess a status for it.
type apiError struct {
	status  int
	code    string
	message string
}

func badRequest(code, message string) *apiError {
	return &apiError{status: http.StatusBadRequest, code: code, message: message}
}

func (e *apiError) send(c *gin.Context) { c.JSON(e.status, models.Fail(e.message, e.code)) }

// parsedLine is one validated cart line: a real product id, a net quantity in
// kg and, for the gross−tare pad, the two weights it was derived from.
type parsedLine struct {
	productID   uuid.UUID
	quantity    float64
	enteredAs   *string
	grossWeight *float64
	tareWeight  *float64
}

// parsedInvoice is a CreateInvoiceRequest after shape validation: ids parsed,
// text trimmed and bounded, nothing touched the database yet.
type parsedInvoice struct {
	clientOpID      *uuid.UUID
	customerID      *uuid.UUID
	lines           []parsedLine
	discountAmount  float64
	discountPercent *float64
	method          string
	subMethod       *string
	reference       *string
	notes           *string
	pin             string
}

// validPaymentMethod matches the invoices_payment_method_check constraint.
func validPaymentMethod(s string) bool {
	switch s {
	case "cash", "card", "online", "credit":
		return true
	}
	return false
}

// validEnteredAs matches the invoice_lines_entered_as_check constraint.
func validEnteredAs(s string) bool {
	switch s {
	case "kg", "tonne", "amount", "gross_tare":
		return true
	}
	return false
}

// validWeight accepts a positive weight with at most 3 decimal places, within
// NUMERIC(14,3). The pad stores net kg to 3 dp in every mode, including
// amount mode (qty = round3(amount ÷ rate)), so 3 dp is the whole of the
// contract — anything finer is a client that has stopped rounding.
func validWeight(v float64) bool {
	if math.IsNaN(v) || math.IsInf(v, 0) || v <= 0 || v >= 1e9 {
		return false
	}
	scaled := v * 1000
	return math.Abs(scaled-math.Round(scaled)) < 1e-6
}

// parseCreateInvoice validates the request shape and parses its ids. Money is
// not validated here beyond the discount, because no money is accepted here.
func parseCreateInvoice(req models.CreateInvoiceRequest) (parsedInvoice, *apiError) {
	var p parsedInvoice

	// client_op_id is mandatory: the till's idempotency key is what lets a
	// retried POST (a dropped response, a doubled tap) return the sale
	// already rung instead of ringing it twice, and that guarantee only
	// holds if every submission carries one.
	id, err := uuid.Parse(strings.TrimSpace(req.ClientOpID))
	if err != nil {
		return p, badRequest("invalid_request", "client_op_id is required")
	}
	p.clientOpID = &id

	p.method = strings.TrimSpace(req.PaymentMethod)
	if !validPaymentMethod(p.method) {
		return p, badRequest("invalid_request", "Payment method must be cash, card, online or credit")
	}

	if s := strings.TrimSpace(req.CustomerID); s != "" {
		id, err := uuid.Parse(s)
		if err != nil {
			// A malformed id names no customer — the same convention the rest
			// of the API uses for an unparseable :id.
			return p, &apiError{status: http.StatusNotFound, code: "customer_not_found", message: "Customer not found"}
		}
		p.customerID = &id
	}

	if len(req.Lines) == 0 {
		return p, badRequest("invalid_request", "An invoice needs at least one line")
	}
	if len(req.Lines) > maxInvoiceLines {
		return p, badRequest("invalid_request", fmt.Sprintf("An invoice can carry at most %d lines", maxInvoiceLines))
	}
	p.lines = make([]parsedLine, 0, len(req.Lines))
	for i, l := range req.Lines {
		productID, err := uuid.Parse(strings.TrimSpace(l.ProductID))
		if err != nil {
			return p, badRequest("invalid_request", fmt.Sprintf("Line %d has no valid product", i+1))
		}
		if !validWeight(l.Quantity) {
			return p, badRequest("invalid_quantity", fmt.Sprintf("Line %d needs a quantity above zero with at most 3 decimal places", i+1))
		}
		line := parsedLine{productID: productID, quantity: l.Quantity}

		enteredAs := strings.TrimSpace(l.EnteredAs)
		if enteredAs != "" {
			if !validEnteredAs(enteredAs) {
				return p, badRequest("invalid_request", fmt.Sprintf("Line %d has an unknown entry mode", i+1))
			}
			line.enteredAs = &enteredAs
		}
		// Gross and tare are stored for the gross−tare pad only, and only
		// when they actually reconcile to the net quantity being charged for.
		// Storing them for the other modes would put two weights on a line
		// nobody weighed.
		if enteredAs == "gross_tare" {
			if l.GrossWeight == nil || l.TareWeight == nil {
				return p, badRequest("invalid_quantity", fmt.Sprintf("Line %d was entered as gross − tare but is missing a weight", i+1))
			}
			gross, tare := *l.GrossWeight, *l.TareWeight
			if !validWeight(gross) || math.IsNaN(tare) || math.IsInf(tare, 0) || tare < 0 {
				return p, badRequest("invalid_quantity", fmt.Sprintf("Line %d has an invalid gross or tare weight", i+1))
			}
			// Compared at gram precision: the client subtracts in float64 too.
			if math.Abs((gross-tare)-l.Quantity) > 0.0005 {
				return p, badRequest("invalid_quantity", fmt.Sprintf("Line %d: gross − tare does not equal the quantity being charged", i+1))
			}
			line.grossWeight, line.tareWeight = &gross, &tare
		}
		p.lines = append(p.lines, line)
	}

	if req.DiscountPercent != nil {
		pct := *req.DiscountPercent
		// discount_percent is NUMERIC(5,2): a third decimal place would price
		// against the value the cashier saw and store a different one, so it
		// gets the same 2 dp check as discount_amount, plus its own 0–100
		// range.
		if pct < 0 || pct > 100 || !validRate(pct) {
			return p, badRequest("invalid_discount", "Discount percent must be between 0 and 100 with at most 2 decimal places")
		}
		p.discountPercent = &pct
	} else {
		if !validRate(req.DiscountAmount) {
			return p, badRequest("invalid_discount", "Discount must be zero or more with at most 2 decimal places")
		}
		p.discountAmount = req.DiscountAmount
	}

	if sub := strings.TrimSpace(req.PaymentSubMethod); sub != "" {
		if utf8.RuneCountInString(sub) > 30 {
			return p, badRequest("invalid_request", "Payment sub-method is at most 30 characters")
		}
		p.subMethod = &sub
	}
	if ref := strings.TrimSpace(req.PaymentReference); ref != "" {
		if utf8.RuneCountInString(ref) > 100 {
			return p, badRequest("invalid_request", "Payment reference is at most 100 characters")
		}
		p.reference = &ref
	}
	if notes := strings.TrimSpace(req.Notes); notes != "" {
		if utf8.RuneCountInString(notes) > maxNotesLen {
			return p, badRequest("invalid_request", fmt.Sprintf("Notes are at most %d characters", maxNotesLen))
		}
		p.notes = &notes
	}
	p.pin = strings.TrimSpace(req.Pin)
	return p, nil
}

// ── settings readers ─────────────────────────────────────────────────────

// settingNumber reads a numeric setting, defaulting to 0 when it is missing,
// null or unparseable. A missing tax rate charges nothing rather than
// guessing a rate the owner never configured.
func settingNumber(all map[string]json.RawMessage, key string) float64 {
	raw, ok := all[key]
	if !ok {
		return 0
	}
	var v float64
	if err := json.Unmarshal(raw, &v); err != nil || math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
		return 0
	}
	return v
}

// settingBool reads a boolean setting, defaulting to false.
func settingBool(all map[string]json.RawMessage, key string) bool {
	raw, ok := all[key]
	if !ok {
		return false
	}
	var v bool
	if err := json.Unmarshal(raw, &v); err != nil {
		return false
	}
	return v
}

// ── locked reads inside the invoice transaction ──────────────────────────

// lockedProduct is a product as priced: its rate held still for the life of
// the transaction.
type lockedProduct struct {
	id     uuid.UUID
	name   string
	rate   float64
	hsCode *string
	fbrUoM *string
}

// lockProducts loads every requested product FOR SHARE, active only. The
// share lock is what makes "priced from products.rate" true under
// concurrency: a rate change (PUT /admin/rates) takes a row-exclusive lock,
// so it waits behind this transaction rather than landing between the price
// the customer was quoted and the row that gets written.
func lockProducts(tx *sql.Tx, lines []parsedLine) (map[uuid.UUID]lockedProduct, error) {
	seen := map[uuid.UUID]bool{}
	args := []any{}
	placeholders := []string{}
	for _, l := range lines {
		if seen[l.productID] {
			continue
		}
		seen[l.productID] = true
		args = append(args, l.productID)
		placeholders = append(placeholders, fmt.Sprintf("$%d", len(args)))
	}
	rows, err := tx.Query(`SELECT id, name, rate::float8, hs_code, fbr_uom FROM products
		WHERE id IN (`+strings.Join(placeholders, ",")+`) AND is_active = true
		ORDER BY id
		FOR SHARE`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uuid.UUID]lockedProduct{}
	for rows.Next() {
		var p lockedProduct
		if err := rows.Scan(&p.id, &p.name, &p.rate, &p.hsCode, &p.fbrUoM); err != nil {
			return nil, err
		}
		out[p.id] = p
	}
	return out, rows.Err()
}

// lockedCustomer is the customer as the sale saw them: name and tax identity
// for the snapshot, credit terms for the limit check, all held still.
type lockedCustomer struct {
	id            uuid.UUID
	name          string
	phone         *string
	ntn           *string
	cnic          *string
	buyerType     string
	creditAllowed bool
	creditLimit   *float64
	isActive      bool
}

// lockCustomer loads one customer, locked so their credit terms cannot
// change between the limit check and the ledger entry. A credit sale takes
// FOR UPDATE: the limit check reads the ledger balance and the sale it is
// about to add to it, so two credit sales on the same account racing each
// other must serialize on this row — the second one's balance read has to
// see the first one's ledger debit, or both can pass a limit that only one
// of them fits under. Every other tender only needs a stable snapshot of the
// name and tax identity for the invoice, so it takes the lighter FOR SHARE
// and stays free to run alongside another sale on the same account.
func lockCustomer(tx *sql.Tx, id uuid.UUID, method string) (lockedCustomer, error) {
	lockClause := "FOR SHARE"
	if method == "credit" {
		lockClause = "FOR UPDATE"
	}
	var cu lockedCustomer
	err := tx.QueryRow(`SELECT id, name, phone, ntn, cnic, buyer_registration_type,
		credit_allowed, credit_limit::float8, is_active
		FROM customers WHERE id = $1 `+lockClause, id).Scan(
		&cu.id, &cu.name, &cu.phone, &cu.ntn, &cu.cnic, &cu.buyerType,
		&cu.creditAllowed, &cu.creditLimit, &cu.isActive)
	return cu, err
}

// cashierName resolves the signed-in user's display name for the invoice
// snapshot, falling back to the username in the token. A name is for a human
// reading a reprint; failing a sale because a name lookup hiccuped would be
// the wrong trade.
func cashierName(q runner, userID uuid.UUID, fallback string) string {
	var name string
	err := q.QueryRow(`SELECT COALESCE(NULLIF(TRIM(first_name || ' ' || last_name), ''), username)
		FROM users WHERE id = $1`, userID).Scan(&name)
	if err != nil || strings.TrimSpace(name) == "" {
		return fallback
	}
	return name
}

// actorOrNil maps the signed-in user's id to a nullable FK value: uuid.Nil is
// not a user, and writing it would trip the foreign key.
func actorOrNil(id uuid.UUID) *uuid.UUID {
	if id == uuid.Nil {
		return nil
	}
	return &id
}

// invoiceActor is the signed-in user, as dayops records them.
func invoiceActor(c *gin.Context) dayops.Actor {
	id, username, role, _ := middleware.UserFromContext(c)
	return dayops.Actor{ID: id, Name: username, Role: role}
}

// failPricing maps a pricing error to its stable code. Most are "the request
// did not describe a sale we can price"; ErrInvalidTaxRate is the exception —
// the tax rate never comes from the client, so a rejected one means a broken
// settings row and is a 500 the owner needs to see in the log, not a 400 the
// cashier can do anything about.
func failPricing(c *gin.Context, err error) {
	switch {
	case errors.Is(err, pricing.ErrInvalidQuantity):
		c.JSON(http.StatusBadRequest, models.Fail("Every line needs a quantity above zero", "invalid_quantity"))
	case errors.Is(err, pricing.ErrInvalidDiscount):
		c.JSON(http.StatusBadRequest, models.Fail("Check the discount", "invalid_discount"))
	case errors.Is(err, pricing.ErrNoLines):
		c.JSON(http.StatusBadRequest, models.Fail("An invoice needs at least one line", "invalid_request"))
	case errors.Is(err, pricing.ErrInvalidRate):
		// A product priced at 0 or worse: a data problem, not a client one,
		// but the sale cannot proceed and the cashier has to be told why.
		c.JSON(http.StatusBadRequest, models.Fail("A product on this sale has no usable rate — set its rate first", "invalid_rate"))
	case errors.Is(err, pricing.ErrInvalidTaxRate):
		log.Printf("invoice create: pricing rejected the configured tax rate — check settings.tax_rate_*/further_tax_rate: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("The configured tax rate is not usable — check Settings", "internal_error"))
	default:
		log.Printf("invoice create: pricing: %v", err)
		c.JSON(http.StatusBadRequest, models.Fail("Could not price this sale", "invalid_request"))
	}
}

// failDayGate answers the two day-gate refusals (§6.7) and reports whether it
// handled the error. Anything else from the gate is a server fault and is
// logged by the caller, which knows which operation it was.
func failDayGate(c *gin.Context, err error) bool {
	switch {
	case errors.Is(err, dayops.ErrDayNotOpen):
		c.JSON(http.StatusConflict, models.Fail("No business day is open — start the day first", "day_not_open"))
	case errors.Is(err, dayops.ErrPreviousDayOpen):
		c.JSON(http.StatusConflict, models.Fail("An earlier business day is still open — close it first", "previous_day_open"))
	default:
		return false
	}
	return true
}

// ── POST /invoices ───────────────────────────────────────────────────────

// Create rings a sale and settles it in one call (spec §6.2), in this order:
//
//  1. validate the shape — ids, quantities, entry modes, discount; a mandatory
//     client_op_id among them;
//  2. idempotency: a client_op_id already used returns that invoice (200);
//  3. allocate the invoice number in its own short transaction, OUTSIDE the
//     one below (see package invoice for why, and for why the gap a later
//     failure leaves is acceptable);
//  4. begin the invoice transaction;
//  5. the day gate — EnsureOpenDayForInvoice, which never opens a day;
//  6. lock and load the products FOR SHARE, active only;
//  7. the tender's tax rate, further tax and limit enforcement, read from
//     settings just before the transaction opens (see the note there);
//  8. resolve the customer and the credit terms;
//  9. price everything server-side from products.rate;
//  10. the credit-limit check (it needs total_payable, so it can only run
//     after pricing) with the admin-PIN override;
//  11. insert the invoice, its lines and, for a credit sale, the ledger debit;
//  12. commit and answer 201 with the whole invoice for printing.
func (h *InvoicesHandler) Create(c *gin.Context) {
	var req models.CreateInvoiceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.Fail("Invalid request body", "invalid_request"))
		return
	}

	// 1 — shape.
	p, bad := parseCreateInvoice(req)
	if bad != nil {
		bad.send(c)
		return
	}

	// 2 — idempotency, before any transaction: a till retrying after a
	// dropped response must get the sale it already made, not a second one.
	if p.clientOpID != nil {
		existing, err := h.findByClientOp(*p.clientOpID)
		switch {
		case err == nil:
			c.JSON(http.StatusOK, models.OK("Invoice already recorded", existing))
			return
		case errors.Is(err, sql.ErrNoRows):
			// carry on
		default:
			log.Printf("invoice create: idempotency lookup: %v", err)
			c.JSON(http.StatusInternalServerError, models.Fail("Could not record the sale", "internal_error"))
			return
		}
	}

	actor := invoiceActor(c)
	// One clock for the whole request: the number, the day gate and the
	// ledger entry all key off the same business date, so a sale rung across
	// the boundary second cannot be numbered for one day and reported in
	// another.
	now := time.Now()

	// Settings are read before the transaction opens, not inside it. They
	// take no locks and nothing about them is transactional, and a query on
	// the pool from inside a transaction borrows a SECOND connection for the
	// life of that transaction — with enough concurrent sales every
	// connection in the pool would be held by a transaction waiting for a
	// connection to read settings with.
	all, err := settings.Load(h.db)
	if err != nil {
		log.Printf("invoice create: settings: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not record the sale", "internal_error"))
		return
	}
	taxRate := pricing.TaxRateFor(p.method, all)
	furtherTaxRate := settingNumber(all, "further_tax_rate")
	creditLimitEnforced := settingBool(all, "credit_limit_enforced")

	// 3 — number, outside the transaction.
	number, err := invoice.AllocateInvoiceNumber(h.db, util.BusinessDate(now))
	if err != nil {
		log.Printf("invoice create: allocate number: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not allocate an invoice number", "internal_error"))
		return
	}

	// 4 — the invoice transaction. Every early return below rolls back.
	tx, err := h.db.Begin()
	if err != nil {
		log.Printf("invoice create: begin: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not record the sale", "internal_error"))
		return
	}
	defer tx.Rollback()

	// 5 — the day gate.
	day, err := dayops.EnsureOpenDayForInvoice(tx, actor, now)
	if err != nil {
		if failDayGate(c, err) {
			return
		}
		log.Printf("invoice create: day gate: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not record the sale", "internal_error"))
		return
	}

	// 6 — products, locked.
	products, err := lockProducts(tx, p.lines)
	if err != nil {
		log.Printf("invoice create: lock products: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not record the sale", "internal_error"))
		return
	}
	for i, l := range p.lines {
		if _, ok := products[l.productID]; !ok {
			c.JSON(http.StatusNotFound, models.Fail(fmt.Sprintf("Line %d names a product that is not on sale", i+1), "product_not_found"))
			return
		}
	}

	// 7 — the tax rate, further tax and limit enforcement were read above.

	// 8 — the customer and their credit terms.
	var customer *lockedCustomer
	if p.customerID != nil {
		cu, err := lockCustomer(tx, *p.customerID, p.method)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			c.JSON(http.StatusNotFound, models.Fail("Customer not found", "customer_not_found"))
			return
		case err != nil:
			log.Printf("invoice create: customer: %v", err)
			c.JSON(http.StatusInternalServerError, models.Fail("Could not record the sale", "internal_error"))
			return
		}
		if !cu.isActive {
			c.JSON(http.StatusConflict, models.Fail("That customer is no longer active", "customer_inactive"))
			return
		}
		customer = &cu
	}
	if p.method == "credit" {
		if customer == nil {
			c.JSON(http.StatusBadRequest, models.Fail("A credit sale needs a customer account", "customer_required"))
			return
		}
		if !customer.creditAllowed {
			c.JSON(http.StatusConflict, models.Fail("That customer is not set up for credit", "credit_not_allowed"))
			return
		}
	}

	// 9 — price it. Rates come from the locked product rows, never the body.
	in := pricing.Input{
		Lines:           make([]pricing.LineIn, 0, len(p.lines)),
		DiscountAmount:  p.discountAmount,
		DiscountPercent: p.discountPercent,
		TaxRate:         taxRate,
		FurtherTaxRate:  furtherTaxRate,
		BuyerRegistered: customer != nil && customer.buyerType == "Registered",
	}
	for _, l := range p.lines {
		in.Lines = append(in.Lines, pricing.LineIn{ProductID: l.productID, Quantity: l.quantity, Rate: products[l.productID].rate})
	}
	totals, err := pricing.ComputeTotals(in)
	if err != nil {
		failPricing(c, err)
		return
	}

	// 10 — the credit limit, and the admin override that can pass it. Three
	// things have to be true before a sale can be blocked: it is on credit,
	// the owner has switched enforcement on, and this customer has a limit
	// set. A null credit_limit on a credit-approved account means no ceiling
	// was agreed, which is not the same as a ceiling of zero.
	notes := p.notes
	if p.method == "credit" && creditLimitEnforced && customer.creditLimit != nil {
		balance, err := ledger.Balance(tx, customer.id)
		if err != nil {
			log.Printf("invoice create: balance: %v", err)
			c.JSON(http.StatusInternalServerError, models.Fail("Could not record the sale", "internal_error"))
			return
		}
		// Compared in whole paisa: two float64 rupee values within a paisa of
		// the limit should not decide a sale on representation error.
		if pricing.Paisa(balance)+totals.TotalPayable*100 > pricing.Paisa(*customer.creditLimit) {
			if p.pin == "" {
				c.JSON(http.StatusConflict, models.Fail(
					fmt.Sprintf("This sale would take %s past their credit limit — an admin PIN can override it", customer.name),
					"credit_limit_exceeded"))
				return
			}
			identity, err := staffpin.Identify(tx, p.pin, staffpin.AdminOnly)
			if errors.Is(err, staffpin.ErrNoMatch) {
				// A PIN was offered and matched nobody. Answering
				// credit_limit_exceeded here would send the cashier back to
				// the customer's account when the problem is the PIN.
				c.JSON(http.StatusUnauthorized, models.Fail("That PIN does not match an active admin", "invalid_pin"))
				return
			}
			if err != nil {
				log.Printf("invoice create: pin: %v", err)
				c.JSON(http.StatusInternalServerError, models.Fail("Could not record the sale", "internal_error"))
				return
			}
			notes = appendNote(notes, fmt.Sprintf("credit limit overridden by %s", identity.Name))
		}
	}

	// 11 — write it.
	cashier := cashierName(tx, actor.ID, actor.Name)
	var customerID *uuid.UUID
	var customerNameSnapshot, customerPhone, customerNTN, customerCNIC *string
	if customer != nil {
		customerID = &customer.id
		name := customer.name
		customerNameSnapshot, customerPhone, customerNTN, customerCNIC = &name, customer.phone, customer.ntn, customer.cnic
	}

	inv, err := scanInvoice(tx.QueryRow(`
		INSERT INTO invoices (invoice_number, client_op_id, business_day_id, business_date, status,
			cashier_id, cashier_name, customer_id, customer_name, customer_phone, customer_ntn, customer_cnic,
			subtotal, discount_amount, discount_percent, tax_rate, tax_amount, further_tax_amount,
			total_amount, rounding_adjustment, total_payable, payment_method, payment_reference, payment_sub_method,
			notes, fiscal_status)
		VALUES ($1, $2, $3, $4::date, 'completed',
			$5, $6, $7, $8, $9, $10, $11,
			$12, $13, $14, $15, $16, $17,
			$18, $19, $20, $21, $22, $23,
			$24, 'off')
		RETURNING `+invoiceColumns,
		number, p.clientOpID, day.ID, day.BusinessDate.Format(dayops.DateLayout),
		actorOrNil(actor.ID), cashier, customerID, customerNameSnapshot, customerPhone, customerNTN, customerCNIC,
		totals.Subtotal, totals.DiscountAmount, p.discountPercent, totals.TaxRate, totals.TaxAmount, totals.FurtherTaxAmount,
		totals.TotalAmount, totals.RoundingAdjustment, totals.TotalPayable, p.method, p.reference, p.subMethod,
		notes))
	if err != nil {
		// Two tills retrying the same sale at the same moment: the loser of
		// the unique index gets the winner's invoice, not an error.
		if p.clientOpID != nil && util.IsUniqueViolation(err, "invoices_client_op_id_key") {
			_ = tx.Rollback()
			existing, lookupErr := h.findByClientOp(*p.clientOpID)
			if lookupErr == nil {
				c.JSON(http.StatusOK, models.OK("Invoice already recorded", existing))
				return
			}
			log.Printf("invoice create: duplicate client_op_id lookup: %v", lookupErr)
		} else {
			log.Printf("invoice create: insert: %v", err)
		}
		c.JSON(http.StatusInternalServerError, models.Fail("Could not record the sale", "internal_error"))
		return
	}

	inv.Lines = make([]models.InvoiceLine, 0, len(p.lines))
	for i, l := range p.lines {
		prod := products[l.productID]
		out := totals.Lines[i]
		line, err := scanInvoiceLine(tx.QueryRow(`
			INSERT INTO invoice_lines (invoice_id, product_id, product_name, hs_code, fbr_uom,
				quantity, entered_as, gross_weight, tare_weight,
				unit_price, line_total, line_discount, line_tax, sort_order)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
			RETURNING `+invoiceLineColumns,
			inv.ID, prod.id, prod.name, prod.hsCode, prod.fbrUoM,
			l.quantity, l.enteredAs, l.grossWeight, l.tareWeight,
			out.UnitPrice, out.LineTotal, out.LineDiscount, out.LineTax, i))
		if err != nil {
			log.Printf("invoice create: line %d: %v", i+1, err)
			c.JSON(http.StatusInternalServerError, models.Fail("Could not record the sale", "internal_error"))
			return
		}
		inv.Lines = append(inv.Lines, line)
	}

	// A credit sale is money owed: the ledger debit is part of the same
	// transaction as the invoice, so the two can never disagree.
	if p.method == "credit" {
		if _, err := ledger.Post(tx, ledger.Entry{
			CustomerID:   customer.id,
			EntryType:    "invoice",
			InvoiceID:    &inv.ID,
			Debit:        float64(totals.TotalPayable),
			BusinessDate: day.BusinessDate,
			CreatedBy:    actorOrNil(actor.ID),
			Note:         strPtrOrNil(fmt.Sprintf("Invoice %s", inv.InvoiceNumber)),
		}); err != nil {
			log.Printf("invoice create: ledger: %v", err)
			c.JSON(http.StatusInternalServerError, models.Fail("Could not record the sale", "internal_error"))
			return
		}
		balance, err := ledger.Balance(tx, customer.id)
		if err != nil {
			log.Printf("invoice create: balance after: %v", err)
			c.JSON(http.StatusInternalServerError, models.Fail("Could not record the sale", "internal_error"))
			return
		}
		inv.CustomerBalanceAfter = &balance
	}

	// 12 — commit and hand back the whole invoice for printing.
	if err := tx.Commit(); err != nil {
		log.Printf("invoice create: commit: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not record the sale", "internal_error"))
		return
	}
	c.JSON(http.StatusCreated, models.OK("Invoice recorded", inv))
}

// findByClientOp returns the invoice already written for this idempotency
// key, with its lines. sql.ErrNoRows means the key is new.
func (h *InvoicesHandler) findByClientOp(clientOpID uuid.UUID) (models.Invoice, error) {
	var id uuid.UUID
	if err := h.db.QueryRow(`SELECT id FROM invoices WHERE client_op_id = $1`, clientOpID).Scan(&id); err != nil {
		return models.Invoice{}, err
	}
	return loadInvoiceWithLines(h.db, id)
}

// appendNote adds a line to an optional note, creating it if there was none.
func appendNote(existing *string, addition string) *string {
	if existing == nil || strings.TrimSpace(*existing) == "" {
		return &addition
	}
	joined := *existing + "\n" + addition
	return &joined
}

// strPtrOrNil returns nil for a blank string.
func strPtrOrNil(s string) *string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return &s
}

// ── GET /invoices, /invoices/recent, /invoices/:id ───────────────────────

// List is the invoice browser: filters on business_date (never created_at, so
// a late sale lands on the day it was rung against), free text over the
// invoice number and the snapshotted customer name, and the obvious facets.
func (h *InvoicesHandler) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	if page < 1 {
		page = 1
	}
	perPage, _ := strconv.Atoi(c.DefaultQuery("per_page", "50"))
	if perPage < 1 || perPage > 100 {
		perPage = 50
	}

	where := []string{"true"}
	args := []any{}
	addFilter := func(clause string, v any) {
		args = append(args, v)
		where = append(where, fmt.Sprintf(clause, len(args)))
	}

	from, ok := parseDateQuery(c.Query("from"))
	if !ok {
		c.JSON(http.StatusBadRequest, models.Fail("from must be YYYY-MM-DD", "invalid_request"))
		return
	}
	if from != nil {
		addFilter("business_date >= $%d::date", from.Format(dayops.DateLayout))
	}
	to, ok := parseDateQuery(c.Query("to"))
	if !ok {
		c.JSON(http.StatusBadRequest, models.Fail("to must be YYYY-MM-DD", "invalid_request"))
		return
	}
	if to != nil {
		addFilter("business_date <= $%d::date", to.Format(dayops.DateLayout))
	}
	if s := strings.TrimSpace(c.Query("search")); s != "" {
		args = append(args, "%"+s+"%")
		n := len(args)
		where = append(where, fmt.Sprintf("(invoice_number ILIKE $%d OR customer_name ILIKE $%d)", n, n))
	}
	if v := strings.TrimSpace(c.Query("customer_id")); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			c.JSON(http.StatusBadRequest, models.Fail("customer_id must be a UUID", "invalid_request"))
			return
		}
		addFilter("customer_id = $%d", id)
	}
	if v := strings.TrimSpace(c.Query("cashier_id")); v != "" {
		id, err := uuid.Parse(v)
		if err != nil {
			c.JSON(http.StatusBadRequest, models.Fail("cashier_id must be a UUID", "invalid_request"))
			return
		}
		addFilter("cashier_id = $%d", id)
	}
	if v := strings.TrimSpace(c.Query("payment_method")); v != "" {
		if !validPaymentMethod(v) {
			c.JSON(http.StatusBadRequest, models.Fail("Payment method must be cash, card, online or credit", "invalid_request"))
			return
		}
		addFilter("payment_method = $%d", v)
	}
	if v := strings.TrimSpace(c.Query("status")); v != "" {
		if v != "completed" && v != "voided" {
			c.JSON(http.StatusBadRequest, models.Fail("Status must be completed or voided", "invalid_request"))
			return
		}
		addFilter("status = $%d", v)
	}
	cond := strings.Join(where, " AND ")

	var total int
	if err := h.db.QueryRow(`SELECT COUNT(*) FROM invoices WHERE `+cond, args...).Scan(&total); err != nil {
		log.Printf("invoices list: count: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not load invoices", "internal_error"))
		return
	}
	args = append(args, perPage, (page-1)*perPage)
	rows, err := h.db.Query(`SELECT `+invoiceColumns+` FROM invoices WHERE `+cond+
		fmt.Sprintf(` ORDER BY created_at DESC, invoice_number DESC LIMIT $%d OFFSET $%d`, len(args)-1, len(args)), args...)
	if err != nil {
		log.Printf("invoices list: query: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not load invoices", "internal_error"))
		return
	}
	defer rows.Close()
	invoices := []models.Invoice{}
	for rows.Next() {
		inv, err := scanInvoice(rows)
		if err != nil {
			log.Printf("invoices list: scan: %v", err)
			c.JSON(http.StatusInternalServerError, models.Fail("Could not load invoices", "internal_error"))
			return
		}
		invoices = append(invoices, inv)
	}
	if err := rows.Err(); err != nil {
		log.Printf("invoices list: rows: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not load invoices", "internal_error"))
		return
	}
	c.JSON(http.StatusOK, models.PaginatedResponse{
		Success: true, Message: "OK", Data: invoices,
		Meta: models.MetaData{CurrentPage: page, PerPage: perPage, Total: total, TotalPages: (total + perPage - 1) / perPage},
	})
}

// Recent is the till's "last few sales" strip: newest first, no filters, no
// paging metadata to decode.
func (h *InvoicesHandler) Recent(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))
	if limit < 1 || limit > 50 {
		limit = 10
	}
	rows, err := h.db.Query(`SELECT `+invoiceColumns+` FROM invoices ORDER BY created_at DESC, invoice_number DESC LIMIT $1`, limit)
	if err != nil {
		log.Printf("invoices recent: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not load invoices", "internal_error"))
		return
	}
	defer rows.Close()
	invoices := []models.Invoice{}
	for rows.Next() {
		inv, err := scanInvoice(rows)
		if err != nil {
			log.Printf("invoices recent: scan: %v", err)
			c.JSON(http.StatusInternalServerError, models.Fail("Could not load invoices", "internal_error"))
			return
		}
		invoices = append(invoices, inv)
	}
	if err := rows.Err(); err != nil {
		log.Printf("invoices recent: rows: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not load invoices", "internal_error"))
		return
	}
	c.JSON(http.StatusOK, models.OK("OK", invoices))
}

// Get returns one invoice with its lines — the reprint path.
func (h *InvoicesHandler) Get(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, models.Fail("Invoice not found", "invoice_not_found"))
		return
	}
	inv, err := loadInvoiceWithLines(h.db, id)
	if errors.Is(err, sql.ErrNoRows) {
		c.JSON(http.StatusNotFound, models.Fail("Invoice not found", "invoice_not_found"))
		return
	}
	if err != nil {
		log.Printf("invoices get: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not load the invoice", "internal_error"))
		return
	}
	c.JSON(http.StatusOK, models.OK("OK", inv))
}

// ── POST /invoices/:id/void ──────────────────────────────────────────────

// validReason bounds a written reason for a void, matching the force-close
// gate: long enough that "x" is not an explanation, short enough for the
// column and the printed slip.
func validReason(s string) bool {
	n := utf8.RuneCountInString(s)
	return n >= dayops.ReasonMinLen && n <= dayops.ReasonMaxLen
}

// Void voids a whole invoice against an admin PIN (spec §6.4). The signed-in
// cashier is recorded as who did it and the PIN holder as who authorised it —
// on a one-till counter those are often different people and the void_log is
// the only place that distinction survives.
//
// The invoice row stays exactly where it was, flipped to 'voided': it is
// still printable (stamped VOID), still in the browser, and excluded from
// every total by the status filter in dayops.ComputeExpected and the reports.
func (h *InvoicesHandler) Void(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, models.Fail("Invoice not found", "invoice_not_found"))
		return
	}
	var req models.VoidInvoiceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.Fail("Invalid request body", "invalid_request"))
		return
	}
	reason := strings.TrimSpace(req.Reason)
	if !validReason(reason) {
		c.JSON(http.StatusBadRequest, models.Fail(
			fmt.Sprintf("A written reason of %d to %d characters is required", dayops.ReasonMinLen, dayops.ReasonMaxLen), "reason_required"))
		return
	}
	pin := strings.TrimSpace(req.Pin)
	if pin == "" {
		c.JSON(http.StatusUnauthorized, models.Fail("That PIN does not match an active admin", "invalid_pin"))
		return
	}

	actor := invoiceActor(c)
	tx, err := h.db.Begin()
	if err != nil {
		log.Printf("invoice void: begin: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not void the invoice", "internal_error"))
		return
	}
	defer tx.Rollback()

	// Lock the invoice first: two cashiers hitting void at once must not both
	// write a void_log row and two mirroring ledger entries.
	var (
		invoiceNumber string
		status        string
		paymentMethod string
		totalPayable  int64
		customerID    *uuid.UUID
	)
	err = tx.QueryRow(`SELECT invoice_number, status, payment_method, total_payable::bigint, customer_id
		FROM invoices WHERE id = $1 FOR UPDATE`, id).Scan(
		&invoiceNumber, &status, &paymentMethod, &totalPayable, &customerID)
	if errors.Is(err, sql.ErrNoRows) {
		c.JSON(http.StatusNotFound, models.Fail("Invoice not found", "invoice_not_found"))
		return
	}
	if err != nil {
		log.Printf("invoice void: lock: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not void the invoice", "internal_error"))
		return
	}
	if status == "voided" {
		c.JSON(http.StatusConflict, models.Fail("That invoice is already voided", "invoice_already_voided"))
		return
	}

	identity, err := staffpin.Identify(tx, pin, staffpin.AdminOnly)
	if errors.Is(err, staffpin.ErrNoMatch) {
		c.JSON(http.StatusUnauthorized, models.Fail("That PIN does not match an active admin", "invalid_pin"))
		return
	}
	if err != nil {
		log.Printf("invoice void: pin: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not void the invoice", "internal_error"))
		return
	}

	var voidedID uuid.UUID
	err = tx.QueryRow(`UPDATE invoices
		SET status = 'voided', voided_at = now(), voided_by = $2, void_reason = $3
		WHERE id = $1 AND status = 'completed'
		RETURNING id`, id, actorOrNil(actor.ID), reason).Scan(&voidedID)
	if errors.Is(err, sql.ErrNoRows) {
		// Unreachable in practice — the FOR UPDATE lock taken above already
		// serializes against a concurrent void of the same row — but
		// defensive: a race here means the invoice was already voided, not a
		// server fault.
		c.JSON(http.StatusConflict, models.Fail("That invoice is already voided", "invoice_already_voided"))
		return
	}
	if err != nil {
		log.Printf("invoice void: update: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not void the invoice", "internal_error"))
		return
	}
	if _, err := tx.Exec(`INSERT INTO void_log (invoice_id, invoice_number, voided_by, authorized_by, total_payable, reason)
		VALUES ($1, $2, $3, $4, $5, $6)`,
		id, invoiceNumber, actorOrNil(actor.ID), identity.UserID, totalPayable, reason); err != nil {
		log.Printf("invoice void: void_log: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not void the invoice", "internal_error"))
		return
	}

	// A voided credit sale is money no longer owed. The mirror entry is a new
	// append-only row, never an edit of the original debit, and it is dated
	// to the day the void happened rather than back to the invoice's own
	// business date: a reversal is an event of the day it was made, and
	// backdating it would silently restate a statement the customer may
	// already be holding.
	var balanceAfter *float64
	if paymentMethod == "credit" && customerID != nil {
		if _, err := ledger.Post(tx, ledger.Entry{
			CustomerID:   *customerID,
			EntryType:    "invoice_void",
			InvoiceID:    &id,
			Credit:       float64(totalPayable),
			BusinessDate: util.BusinessDate(time.Now()),
			CreatedBy:    actorOrNil(actor.ID),
			Note:         strPtrOrNil(fmt.Sprintf("Void of invoice %s: %s", invoiceNumber, reason)),
		}); err != nil {
			log.Printf("invoice void: ledger: %v", err)
			c.JSON(http.StatusInternalServerError, models.Fail("Could not void the invoice", "internal_error"))
			return
		}
		balance, err := ledger.Balance(tx, *customerID)
		if err != nil {
			log.Printf("invoice void: balance: %v", err)
			c.JSON(http.StatusInternalServerError, models.Fail("Could not void the invoice", "internal_error"))
			return
		}
		balanceAfter = &balance
	}

	inv, err := loadInvoiceWithLines(tx, id)
	if err != nil {
		log.Printf("invoice void: reload: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not void the invoice", "internal_error"))
		return
	}
	if err := tx.Commit(); err != nil {
		log.Printf("invoice void: commit: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not void the invoice", "internal_error"))
		return
	}
	inv.CustomerBalanceAfter = balanceAfter
	c.JSON(http.StatusOK, models.OK("Invoice voided", inv))
}
