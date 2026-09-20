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

	"elevon-backend/internal/ledger"
	"elevon-backend/internal/models"
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
