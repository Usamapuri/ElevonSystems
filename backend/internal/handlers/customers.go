package handlers

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"elevon-backend/internal/dayops"
	"elevon-backend/internal/invoice"
	"elevon-backend/internal/ledger"
	"elevon-backend/internal/models"
	"elevon-backend/internal/staffpin"
	"elevon-backend/internal/util"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// CustomersHandler administers customers and serves their ledger balance
// and statement. Reads (List, Get, Statement) are for any staff — the till's
// customer picker and the /customers screen both need them; writes (Create,
// Update) are admin only (routes.go). Receipts and receiving payment are
// Task D6's job — nothing here writes a ledger entry.
type CustomersHandler struct{ db *sql.DB }

// NewCustomersHandler builds a CustomersHandler.
func NewCustomersHandler(db *sql.DB) *CustomersHandler { return &CustomersHandler{db: db} }

var (
	customerPhoneRe = regexp.MustCompile(`^[0-9+ ]{1,30}$`)
	customerNTNRe   = regexp.MustCompile(`^[0-9-]{1,20}$`)
	customerCNICRe  = regexp.MustCompile(`^[0-9]{1,15}$`)
)

// validCustomerName reports whether s (already trimmed) is 1–120 characters.
func validCustomerName(s string) bool {
	n := utf8.RuneCountInString(s)
	return n >= 1 && n <= 120
}

// normalisePhone trims s; blank becomes (nil, true). A non-blank phone must
// be digits, '+' or spaces, at most 30 characters (the column is VARCHAR(30)
// and the unique index is on lower(phone)).
func normalisePhone(s string) (*string, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, true
	}
	if !customerPhoneRe.MatchString(s) {
		return nil, false
	}
	return &s, true
}

// normaliseNTN trims s; blank becomes (nil, true). A non-blank NTN is
// digits or dashes, at most 20 characters (VARCHAR(20)).
func normaliseNTN(s string) (*string, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, true
	}
	if !customerNTNRe.MatchString(s) {
		return nil, false
	}
	return &s, true
}

// normaliseCNIC trims s; blank becomes (nil, true). A non-blank CNIC is
// digits only, at most 15 characters (VARCHAR(15), FBR buyerNTNCNIC).
func normaliseCNIC(s string) (*string, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, true
	}
	if !customerCNICRe.MatchString(s) {
		return nil, false
	}
	return &s, true
}

// validBuyerRegistrationType matches the customers_buyer_type_check constraint.
func validBuyerRegistrationType(s string) bool {
	return s == "Registered" || s == "Unregistered"
}

// normaliseProvince trims s; blank becomes (nil, true). A non-blank value is
// at most 40 characters (VARCHAR(40)).
func normaliseProvince(s string) (*string, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, true
	}
	if utf8.RuneCountInString(s) > 40 {
		return nil, false
	}
	return &s, true
}

// customerColumns is deliberately unprefixed: it is reused as-is by Create,
// Get and Update (no join, no alias needed) and by List, whose JOIN adds a
// balance subquery whose own columns (customer_id, balance) never collide
// with a customers column name.
const customerColumns = `id, name, phone, ntn, cnic, buyer_registration_type, address, province,
	credit_allowed, credit_limit::float8, is_active, notes, created_at, updated_at`

func scanCustomer(row interface {
	Scan(dest ...interface{}) error
}) (models.Customer, error) {
	var cu models.Customer
	err := row.Scan(&cu.ID, &cu.Name, &cu.Phone, &cu.NTN, &cu.CNIC, &cu.BuyerRegistrationType, &cu.Address, &cu.Province,
		&cu.CreditAllowed, &cu.CreditLimit, &cu.IsActive, &cu.Notes, &cu.CreatedAt, &cu.UpdatedAt)
	return cu, err
}

// scanCustomerWithBalance reads a customerColumns row plus a trailing
// balance column, as produced by List's join query.
func scanCustomerWithBalance(row interface {
	Scan(dest ...interface{}) error
}) (models.Customer, error) {
	var cu models.Customer
	err := row.Scan(&cu.ID, &cu.Name, &cu.Phone, &cu.NTN, &cu.CNIC, &cu.BuyerRegistrationType, &cu.Address, &cu.Province,
		&cu.CreditAllowed, &cu.CreditLimit, &cu.IsActive, &cu.Notes, &cu.CreatedAt, &cu.UpdatedAt, &cu.Balance)
	return cu, err
}

// List returns a page of customers with their ledger balance (a customer
// with no ledger entries at all gets 0, via COALESCE — the LEFT JOIN would
// otherwise drop the balance to NULL rather than 0). Filters: search
// (name/phone), active=true|false.
func (h *CustomersHandler) List(c *gin.Context) {
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
	if active := c.Query("active"); active != "" {
		args = append(args, active == "true" || active == "1")
		where = append(where, fmt.Sprintf("is_active = $%d", len(args)))
	}
	if s := strings.TrimSpace(c.Query("search")); s != "" {
		args = append(args, "%"+s+"%")
		n := len(args)
		where = append(where, fmt.Sprintf("(name ILIKE $%d OR phone ILIKE $%d)", n, n))
	}
	cond := strings.Join(where, " AND ")

	var total int
	if err := h.db.QueryRow(`SELECT COUNT(*) FROM customers WHERE `+cond, args...).Scan(&total); err != nil {
		log.Printf("customers list: count: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not load customers", "internal_error"))
		return
	}

	args = append(args, perPage, (page-1)*perPage)
	rows, err := h.db.Query(`
		SELECT `+customerColumns+`, COALESCE(l.balance, 0)::float8
		FROM customers
		LEFT JOIN (
			SELECT customer_id, SUM(debit) - SUM(credit) AS balance
			FROM customer_ledger_entries
			GROUP BY customer_id
		) l ON l.customer_id = customers.id
		WHERE `+cond+
		fmt.Sprintf(` ORDER BY name LIMIT $%d OFFSET $%d`, len(args)-1, len(args)), args...)
	if err != nil {
		log.Printf("customers list: query: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not load customers", "internal_error"))
		return
	}
	defer rows.Close()
	customers := []models.Customer{}
	for rows.Next() {
		cu, err := scanCustomerWithBalance(rows)
		if err != nil {
			log.Printf("customers list: scan: %v", err)
			c.JSON(http.StatusInternalServerError, models.Fail("Could not load customers", "internal_error"))
			return
		}
		customers = append(customers, cu)
	}
	if err := rows.Err(); err != nil {
		log.Printf("customers list: rows: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not load customers", "internal_error"))
		return
	}
	c.JSON(http.StatusOK, models.PaginatedResponse{
		Success: true, Message: "OK", Data: customers,
		Meta: models.MetaData{CurrentPage: page, PerPage: perPage, Total: total, TotalPages: (total + perPage - 1) / perPage},
	})
}

// Get returns one customer with its current balance.
func (h *CustomersHandler) Get(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, models.Fail("Customer not found", "customer_not_found"))
		return
	}
	cu, err := scanCustomer(h.db.QueryRow(`SELECT `+customerColumns+` FROM customers WHERE id = $1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		c.JSON(http.StatusNotFound, models.Fail("Customer not found", "customer_not_found"))
		return
	}
	if err != nil {
		log.Printf("customers get: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not load the customer", "internal_error"))
		return
	}
	balance, err := ledger.Balance(h.db, id)
	if err != nil {
		log.Printf("customers get: balance: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not load the customer", "internal_error"))
		return
	}
	cu.Balance = balance
	c.JSON(http.StatusOK, models.OK("OK", cu))
}

// parseDateQuery parses a YYYY-MM-DD query parameter in the business
// timezone. A blank string is "no filter" — (nil, true).
func parseDateQuery(s string) (*time.Time, bool) {
	if s == "" {
		return nil, true
	}
	t, err := time.ParseInLocation("2006-01-02", s, util.BusinessLocation())
	if err != nil {
		return nil, false
	}
	return &t, true
}

// Statement returns a customer's ledger entries with running balance,
// optionally narrowed to a business_date range (?from=YYYY-MM-DD&to=YYYY-MM-DD).
func (h *CustomersHandler) Statement(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, models.Fail("Customer not found", "customer_not_found"))
		return
	}
	var exists bool
	if err := h.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM customers WHERE id = $1)`, id).Scan(&exists); err != nil {
		log.Printf("customers statement: exists: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not load the statement", "internal_error"))
		return
	}
	if !exists {
		c.JSON(http.StatusNotFound, models.Fail("Customer not found", "customer_not_found"))
		return
	}
	from, ok := parseDateQuery(c.Query("from"))
	if !ok {
		c.JSON(http.StatusBadRequest, models.Fail("from must be YYYY-MM-DD", "invalid_request"))
		return
	}
	to, ok := parseDateQuery(c.Query("to"))
	if !ok {
		c.JSON(http.StatusBadRequest, models.Fail("to must be YYYY-MM-DD", "invalid_request"))
		return
	}
	rows, err := ledger.Statement(h.db, id, from, to)
	if err != nil {
		log.Printf("customers statement: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not load the statement", "internal_error"))
		return
	}
	c.JSON(http.StatusOK, models.OK("OK", rows))
}

// Create adds a customer. Balance is always 0 on creation — there is no way
// to post a ledger entry from this endpoint.
func (h *CustomersHandler) Create(c *gin.Context) {
	var req models.CreateCustomerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.Fail("Invalid request body", "invalid_request"))
		return
	}
	name := strings.TrimSpace(req.Name)
	if !validCustomerName(name) {
		c.JSON(http.StatusBadRequest, models.Fail("Name is required and at most 120 characters", "invalid_name"))
		return
	}
	phone, ok := normalisePhone(req.Phone)
	if !ok {
		c.JSON(http.StatusBadRequest, models.Fail("Phone must be at most 30 characters of digits, + or spaces", "invalid_phone"))
		return
	}
	ntn, ok := normaliseNTN(req.NTN)
	if !ok {
		c.JSON(http.StatusBadRequest, models.Fail("NTN must be at most 20 characters of digits or dashes", "invalid_ntn"))
		return
	}
	cnic, ok := normaliseCNIC(req.CNIC)
	if !ok {
		c.JSON(http.StatusBadRequest, models.Fail("CNIC must be digits only, at most 15 characters", "invalid_cnic"))
		return
	}
	buyerType := strings.TrimSpace(req.BuyerRegistrationType)
	if buyerType == "" {
		buyerType = "Unregistered"
	}
	if !validBuyerRegistrationType(buyerType) {
		c.JSON(http.StatusBadRequest, models.Fail("Buyer registration type must be Registered or Unregistered", "invalid_buyer_registration_type"))
		return
	}
	province, ok := normaliseProvince(req.Province)
	if !ok {
		c.JSON(http.StatusBadRequest, models.Fail("Province is at most 40 characters", "invalid_province"))
		return
	}
	if req.CreditLimit != nil && !validRate(*req.CreditLimit) {
		c.JSON(http.StatusBadRequest, models.Fail("Credit limit must be zero or more with at most 2 decimal places", "invalid_credit_limit"))
		return
	}
	address := optionalText(req.Address)
	notes := optionalText(req.Notes)

	cu, err := scanCustomer(h.db.QueryRow(`
		INSERT INTO customers (name, phone, ntn, cnic, buyer_registration_type, address, province, credit_allowed, credit_limit, notes)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING `+customerColumns,
		name, phone, ntn, cnic, buyerType, address, province, req.CreditAllowed, req.CreditLimit, notes))
	switch {
	case util.IsUniqueViolation(err, "uniq_customers_phone_lower"):
		c.JSON(http.StatusConflict, models.Fail("A customer with that phone number already exists", "phone_taken"))
		return
	case err != nil:
		log.Printf("customers create: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not create the customer", "internal_error"))
		return
	}
	c.JSON(http.StatusCreated, models.OK("Customer created", cu))
}

// Update changes any subset of a customer's fields. CreditLimit follows the
// same convention as every other optional field here: nil (omitted or an
// explicit JSON null — Go cannot tell those apart on a *float64) leaves it
// unchanged; there is no way to null out an existing limit through this
// endpoint in v1 — set it to 0 instead.
func (h *CustomersHandler) Update(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, models.Fail("Customer not found", "customer_not_found"))
		return
	}
	var req models.UpdateCustomerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.Fail("Invalid request body", "invalid_request"))
		return
	}
	sets := []string{}
	args := []any{}
	add := func(col string, v any) {
		args = append(args, v)
		sets = append(sets, fmt.Sprintf("%s = $%d", col, len(args)))
	}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if !validCustomerName(name) {
			c.JSON(http.StatusBadRequest, models.Fail("Name is required and at most 120 characters", "invalid_name"))
			return
		}
		add("name", name)
	}
	if req.Phone != nil {
		phone, ok := normalisePhone(*req.Phone)
		if !ok {
			c.JSON(http.StatusBadRequest, models.Fail("Phone must be at most 30 characters of digits, + or spaces", "invalid_phone"))
			return
		}
		if phone == nil {
			sets = append(sets, "phone = NULL")
		} else {
			add("phone", *phone)
		}
	}
	if req.NTN != nil {
		ntn, ok := normaliseNTN(*req.NTN)
		if !ok {
			c.JSON(http.StatusBadRequest, models.Fail("NTN must be at most 20 characters of digits or dashes", "invalid_ntn"))
			return
		}
		if ntn == nil {
			sets = append(sets, "ntn = NULL")
		} else {
			add("ntn", *ntn)
		}
	}
	if req.CNIC != nil {
		cnic, ok := normaliseCNIC(*req.CNIC)
		if !ok {
			c.JSON(http.StatusBadRequest, models.Fail("CNIC must be digits only, at most 15 characters", "invalid_cnic"))
			return
		}
		if cnic == nil {
			sets = append(sets, "cnic = NULL")
		} else {
			add("cnic", *cnic)
		}
	}
	if req.BuyerRegistrationType != nil {
		bt := strings.TrimSpace(*req.BuyerRegistrationType)
		if !validBuyerRegistrationType(bt) {
			c.JSON(http.StatusBadRequest, models.Fail("Buyer registration type must be Registered or Unregistered", "invalid_buyer_registration_type"))
			return
		}
		add("buyer_registration_type", bt)
	}
	if req.Address != nil {
		if v := optionalText(*req.Address); v == nil {
			sets = append(sets, "address = NULL")
		} else {
			add("address", *v)
		}
	}
	if req.Province != nil {
		province, ok := normaliseProvince(*req.Province)
		if !ok {
			c.JSON(http.StatusBadRequest, models.Fail("Province is at most 40 characters", "invalid_province"))
			return
		}
		if province == nil {
			sets = append(sets, "province = NULL")
		} else {
			add("province", *province)
		}
	}
	if req.CreditAllowed != nil {
		add("credit_allowed", *req.CreditAllowed)
	}
	if req.CreditLimit != nil {
		if !validRate(*req.CreditLimit) {
			c.JSON(http.StatusBadRequest, models.Fail("Credit limit must be zero or more with at most 2 decimal places", "invalid_credit_limit"))
			return
		}
		add("credit_limit", *req.CreditLimit)
	}
	if req.IsActive != nil {
		add("is_active", *req.IsActive)
	}
	if req.Notes != nil {
		if v := optionalText(*req.Notes); v == nil {
			sets = append(sets, "notes = NULL")
		} else {
			add("notes", *v)
		}
	}
	if len(sets) == 0 {
		c.JSON(http.StatusBadRequest, models.Fail("Nothing to update", "no_changes"))
		return
	}
	sets = append(sets, "updated_at = now()")
	args = append(args, id)
	cu, err := scanCustomer(h.db.QueryRow(fmt.Sprintf(`UPDATE customers SET %s WHERE id = $%d RETURNING `+customerColumns,
		strings.Join(sets, ", "), len(args)), args...))
	switch {
	case errors.Is(err, sql.ErrNoRows):
		c.JSON(http.StatusNotFound, models.Fail("Customer not found", "customer_not_found"))
		return
	case util.IsUniqueViolation(err, "uniq_customers_phone_lower"):
		c.JSON(http.StatusConflict, models.Fail("A customer with that phone number already exists", "phone_taken"))
		return
	case err != nil:
		log.Printf("customers update: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not update the customer", "internal_error"))
		return
	}
	balance, err := ledger.Balance(h.db, id)
	if err != nil {
		log.Printf("customers update: balance: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not update the customer", "internal_error"))
		return
	}
	cu.Balance = balance
	c.JSON(http.StatusOK, models.OK("Customer updated", cu))
}

// ── receipts (spec §6.5) ─────────────────────────────────────────────────

// receiptColumns is unprefixed: every read below is a plain select from
// customer_receipts with no join. The customer's name and the cashier's name
// are filled in by the caller, which already has both.
const receiptColumns = `id, receipt_number, customer_id, amount::float8, method, sub_method, reference,
	business_day_id, business_date, received_by, note, created_at, voided_at, voided_by, void_reason`

func scanReceipt(row interface {
	Scan(dest ...interface{}) error
}) (models.Receipt, error) {
	var r models.Receipt
	err := row.Scan(&r.ID, &r.ReceiptNumber, &r.CustomerID, &r.Amount, &r.Method, &r.SubMethod, &r.Reference,
		&r.BusinessDayID, &r.BusinessDate, &r.ReceivedBy, &r.Note, &r.CreatedAt, &r.VoidedAt, &r.VoidedBy, &r.VoidReason)
	return r, err
}

// receiptColumnsPrefixed is the same list aliased to r, for the one read that
// joins the customer and the cashier in to fill the two display names.
const receiptColumnsPrefixed = `r.id, r.receipt_number, r.customer_id, r.amount::float8, r.method, r.sub_method, r.reference,
	r.business_day_id, r.business_date, r.received_by, r.note, r.created_at, r.voided_at, r.voided_by, r.void_reason`

// scanReceiptWithNames reads a receiptColumnsPrefixed row plus the trailing
// customer name and cashier name that the joined read adds.
func scanReceiptWithNames(row interface {
	Scan(dest ...interface{}) error
}) (models.Receipt, error) {
	var r models.Receipt
	err := row.Scan(&r.ID, &r.ReceiptNumber, &r.CustomerID, &r.Amount, &r.Method, &r.SubMethod, &r.Reference,
		&r.BusinessDayID, &r.BusinessDate, &r.ReceivedBy, &r.Note, &r.CreatedAt, &r.VoidedAt, &r.VoidedBy, &r.VoidReason,
		&r.CustomerName, &r.ReceivedByName)
	return r, err
}

// validReceiptMethod matches the customer_receipts_method_check constraint.
// Credit is not a method here: a receipt is money arriving.
func validReceiptMethod(s string) bool {
	return s == "cash" || s == "card" || s == "online"
}

// CreateReceipt takes money against a customer's account (spec §6.5).
//
// It goes through the same day gate as a sale, because the money lands in the
// same drawer and the same tender count: a receipt taken while no day is open
// would be cash nobody expected at close. The number comes from its own
// counter, allocated outside the transaction for the same reason invoice
// numbers are (see package invoice).
//
// Receipts are against the account, not against a specific invoice — there is
// no allocation UI in v1 and ageing is FIFO by invoice date in the report.
func (h *CustomersHandler) CreateReceipt(c *gin.Context) {
	customerID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, models.Fail("Customer not found", "customer_not_found"))
		return
	}
	var req models.CreateReceiptRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.Fail("Invalid request body", "invalid_request"))
		return
	}
	if !validRate(req.Amount) || req.Amount <= 0 {
		c.JSON(http.StatusBadRequest, models.Fail("Amount must be more than zero, with at most 2 decimal places", "invalid_amount"))
		return
	}
	method := strings.TrimSpace(req.Method)
	if !validReceiptMethod(method) {
		c.JSON(http.StatusBadRequest, models.Fail("Method must be cash, card or online", "invalid_method"))
		return
	}
	subMethod := optionalText(req.SubMethod)
	if subMethod != nil && utf8.RuneCountInString(*subMethod) > 30 {
		c.JSON(http.StatusBadRequest, models.Fail("Sub-method is at most 30 characters", "invalid_request"))
		return
	}
	reference := optionalText(req.Reference)
	if reference != nil && utf8.RuneCountInString(*reference) > 100 {
		c.JSON(http.StatusBadRequest, models.Fail("Reference is at most 100 characters", "invalid_request"))
		return
	}
	note := optionalText(req.Note)
	if note != nil && utf8.RuneCountInString(*note) > maxNotesLen {
		c.JSON(http.StatusBadRequest, models.Fail(fmt.Sprintf("Note is at most %d characters", maxNotesLen), "invalid_request"))
		return
	}

	// Check the account before burning a receipt number on it.
	var customerName string
	var isActive bool
	err = h.db.QueryRow(`SELECT name, is_active FROM customers WHERE id = $1`, customerID).Scan(&customerName, &isActive)
	if errors.Is(err, sql.ErrNoRows) {
		c.JSON(http.StatusNotFound, models.Fail("Customer not found", "customer_not_found"))
		return
	}
	if err != nil {
		log.Printf("receipt create: customer: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not record the receipt", "internal_error"))
		return
	}
	if !isActive {
		c.JSON(http.StatusConflict, models.Fail("That customer is no longer active", "customer_inactive"))
		return
	}

	actor := invoiceActor(c)
	now := time.Now()
	number, err := invoice.AllocateReceiptNumber(h.db, util.BusinessDate(now))
	if err != nil {
		log.Printf("receipt create: allocate number: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not allocate a receipt number", "internal_error"))
		return
	}

	tx, err := h.db.Begin()
	if err != nil {
		log.Printf("receipt create: begin: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not record the receipt", "internal_error"))
		return
	}
	defer tx.Rollback()

	day, err := dayops.EnsureOpenDayForInvoice(tx, actor, now)
	if err != nil {
		if failDayGate(c, err) {
			return
		}
		log.Printf("receipt create: day gate: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not record the receipt", "internal_error"))
		return
	}

	receipt, err := scanReceipt(tx.QueryRow(`
		INSERT INTO customer_receipts (receipt_number, customer_id, amount, method, sub_method, reference,
			business_day_id, business_date, received_by, note)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8::date, $9, $10)
		RETURNING `+receiptColumns,
		number, customerID, req.Amount, method, subMethod, reference,
		day.ID, day.BusinessDate.Format(dayops.DateLayout), actorOrNil(actor.ID), note))
	if err != nil {
		log.Printf("receipt create: insert: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not record the receipt", "internal_error"))
		return
	}

	// The credit entry and the receipt row are written together: a receipt
	// that did not reach the ledger is money the customer paid and still owes.
	if _, err := ledger.Post(tx, ledger.Entry{
		CustomerID:   customerID,
		EntryType:    "receipt",
		ReceiptID:    &receipt.ID,
		Credit:       req.Amount,
		BusinessDate: day.BusinessDate,
		CreatedBy:    actorOrNil(actor.ID),
		Note:         strPtrOrNil(fmt.Sprintf("Receipt %s", receipt.ReceiptNumber)),
	}); err != nil {
		log.Printf("receipt create: ledger: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not record the receipt", "internal_error"))
		return
	}
	balance, err := ledger.Balance(tx, customerID)
	if err != nil {
		log.Printf("receipt create: balance: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not record the receipt", "internal_error"))
		return
	}
	if err := tx.Commit(); err != nil {
		log.Printf("receipt create: commit: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not record the receipt", "internal_error"))
		return
	}

	receipt.CustomerName = customerName
	receipt.ReceivedByName = cashierName(h.db, actor.ID, actor.Name)
	receipt.CustomerBalanceAfter = &balance
	c.JSON(http.StatusCreated, models.OK("Receipt recorded", receipt))
}

// VoidReceipt reverses a receipt against an admin PIN, mirroring the ledger
// entry with a receipt_void debit (spec §6.5). Like an invoice void it takes
// no day gate: the money already landed on its own day, and refusing to
// correct a mistyped receipt because the till is closed would be perverse.
func (h *CustomersHandler) VoidReceipt(c *gin.Context) {
	customerID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, models.Fail("Customer not found", "customer_not_found"))
		return
	}
	receiptID, err := uuid.Parse(c.Param("rid"))
	if err != nil {
		c.JSON(http.StatusNotFound, models.Fail("Receipt not found", "receipt_not_found"))
		return
	}
	var req models.VoidReceiptRequest
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
		log.Printf("receipt void: begin: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not void the receipt", "internal_error"))
		return
	}
	defer tx.Rollback()

	var (
		receiptNumber string
		amount        float64
		voidedAt      *time.Time
	)
	err = tx.QueryRow(`SELECT receipt_number, amount::float8, voided_at FROM customer_receipts
		WHERE id = $1 AND customer_id = $2 FOR UPDATE`, receiptID, customerID).Scan(&receiptNumber, &amount, &voidedAt)
	if errors.Is(err, sql.ErrNoRows) {
		c.JSON(http.StatusNotFound, models.Fail("Receipt not found", "receipt_not_found"))
		return
	}
	if err != nil {
		log.Printf("receipt void: lock: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not void the receipt", "internal_error"))
		return
	}
	if voidedAt != nil {
		c.JSON(http.StatusConflict, models.Fail("That receipt is already voided", "receipt_already_voided"))
		return
	}

	if _, err := staffpin.Identify(tx, pin, staffpin.AdminOnly); errors.Is(err, staffpin.ErrNoMatch) {
		c.JSON(http.StatusUnauthorized, models.Fail("That PIN does not match an active admin", "invalid_pin"))
		return
	} else if err != nil {
		log.Printf("receipt void: pin: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not void the receipt", "internal_error"))
		return
	}

	receipt, err := scanReceipt(tx.QueryRow(`
		UPDATE customer_receipts
		SET voided_at = now(), voided_by = $2, void_reason = $3
		WHERE id = $1 AND voided_at IS NULL
		RETURNING `+receiptColumns, receiptID, actorOrNil(actor.ID), reason))
	if err != nil {
		log.Printf("receipt void: update: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not void the receipt", "internal_error"))
		return
	}

	// The mirror debit puts the money back on the account, dated to the day
	// the void was made (see the invoice void for why it is not backdated).
	if _, err := ledger.Post(tx, ledger.Entry{
		CustomerID:   customerID,
		EntryType:    "receipt_void",
		ReceiptID:    &receiptID,
		Debit:        amount,
		BusinessDate: util.BusinessDate(time.Now()),
		CreatedBy:    actorOrNil(actor.ID),
		Note:         strPtrOrNil(fmt.Sprintf("Void of receipt %s: %s", receiptNumber, reason)),
	}); err != nil {
		log.Printf("receipt void: ledger: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not void the receipt", "internal_error"))
		return
	}
	balance, err := ledger.Balance(tx, customerID)
	if err != nil {
		log.Printf("receipt void: balance: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not void the receipt", "internal_error"))
		return
	}
	var customerName string
	if err := tx.QueryRow(`SELECT name FROM customers WHERE id = $1`, customerID).Scan(&customerName); err != nil {
		log.Printf("receipt void: customer name: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not void the receipt", "internal_error"))
		return
	}
	if err := tx.Commit(); err != nil {
		log.Printf("receipt void: commit: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not void the receipt", "internal_error"))
		return
	}

	receipt.CustomerName = customerName
	receipt.ReceivedByName = cashierName(h.db, actor.ID, actor.Name)
	receipt.CustomerBalanceAfter = &balance
	c.JSON(http.StatusOK, models.OK("Receipt voided", receipt))
}

// ListReceipts returns a customer's receipts, newest first — the account
// screen's "payments taken" panel and the reprint path for a receipt slip.
func (h *CustomersHandler) ListReceipts(c *gin.Context) {
	customerID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, models.Fail("Customer not found", "customer_not_found"))
		return
	}
	rows, err := h.db.Query(`SELECT `+receiptColumnsPrefixed+`, c.name,
			COALESCE(NULLIF(TRIM(u.first_name || ' ' || u.last_name), ''), u.username, '')
		FROM customer_receipts r
		JOIN customers c ON c.id = r.customer_id
		LEFT JOIN users u ON u.id = r.received_by
		WHERE r.customer_id = $1 ORDER BY r.created_at DESC, r.receipt_number DESC LIMIT 200`, customerID)
	if err != nil {
		log.Printf("receipts list: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not load receipts", "internal_error"))
		return
	}
	defer rows.Close()
	receipts := []models.Receipt{}
	for rows.Next() {
		r, err := scanReceiptWithNames(rows)
		if err != nil {
			log.Printf("receipts list: scan: %v", err)
			c.JSON(http.StatusInternalServerError, models.Fail("Could not load receipts", "internal_error"))
			return
		}
		receipts = append(receipts, r)
	}
	if err := rows.Err(); err != nil {
		log.Printf("receipts list: rows: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not load receipts", "internal_error"))
		return
	}
	c.JSON(http.StatusOK, models.OK("OK", receipts))
}
