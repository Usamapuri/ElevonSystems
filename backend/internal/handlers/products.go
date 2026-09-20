package handlers

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"elevon-backend/internal/middleware"
	"elevon-backend/internal/models"
	"elevon-backend/internal/util"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// ProductsHandler administers products and their rates. Reads are for any
// staff (the till needs the price list); writes are admin only (routes.go).
type ProductsHandler struct{ db *sql.DB }

// NewProductsHandler builds a ProductsHandler.
func NewProductsHandler(db *sql.DB) *ProductsHandler { return &ProductsHandler{db: db} }

var hsCodeRe = regexp.MustCompile(`^[0-9]{4}\.[0-9]{4}$`)

// validProductName reports whether s (already trimmed) is 1–120 characters.
func validProductName(s string) bool {
	n := utf8.RuneCountInString(s)
	return n >= 1 && n <= 120
}

// normaliseSKU trims s; blank becomes (nil, true). A non-blank SKU over 40
// characters is rejected — the column is VARCHAR(40) and we never want a
// raw driver error reaching the client.
func normaliseSKU(s string) (*string, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, true
	}
	if utf8.RuneCountInString(s) > 40 {
		return nil, false
	}
	return &s, true
}

// validHSCode trims s; blank becomes (nil, true). A non-blank value must be
// the FBR DI format 0000.0000 (spec §5.2, §7.3).
func validHSCode(s string) (*string, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, true
	}
	if !hsCodeRe.MatchString(s) {
		return nil, false
	}
	return &s, true
}

// optionalText trims s; blank becomes nil.
func optionalText(s string) *string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return &s
}

// validRate reports whether r is zero or more, finite, within the
// NUMERIC(12,2) range and has at most 2 decimal places. Comparisons on
// float64 money are done at paisa precision with a small epsilon because
// JSON numbers are not exact decimals.
func validRate(r float64) bool {
	if math.IsNaN(r) || math.IsInf(r, 0) || r < 0 || r >= 1e10 {
		return false
	}
	paisa := r * 100
	return math.Abs(paisa-math.Round(paisa)) < 1e-6
}

// ratesEqual compares two rates at paisa precision.
func ratesEqual(a, b float64) bool {
	return math.Round(a*100) == math.Round(b*100)
}

const productColumns = `id, name, sku, sell_by, unit_label, rate::float8, hs_code, fbr_uom, sort_order, is_active, created_at, updated_at`

func scanProduct(row interface{ Scan(dest ...interface{}) error }) (models.Product, error) {
	var p models.Product
	err := row.Scan(&p.ID, &p.Name, &p.SKU, &p.SellBy, &p.UnitLabel, &p.Rate, &p.HSCode, &p.FBRUoM, &p.SortOrder, &p.IsActive, &p.CreatedAt, &p.UpdatedAt)
	return p, err
}

// queryer is satisfied by *sql.DB and *sql.Tx, so listing can run inside or
// outside a transaction with the same code.
type queryer interface {
	Query(query string, args ...any) (*sql.Rows, error)
}

// listProducts returns every product ordered sort_order, name. activeOnly
// nil means no filter; otherwise it is is_active = *activeOnly.
func listProducts(q queryer, activeOnly *bool) ([]models.Product, error) {
	where := "true"
	args := []any{}
	if activeOnly != nil {
		args = append(args, *activeOnly)
		where = "is_active = $1"
	}
	rows, err := q.Query(`SELECT `+productColumns+` FROM products WHERE `+where+` ORDER BY sort_order, name`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	products := []models.Product{}
	for rows.Next() {
		p, err := scanProduct(rows)
		if err != nil {
			return nil, err
		}
		products = append(products, p)
	}
	return products, rows.Err()
}

// List returns products, optionally filtered by ?active=1|0|true|false.
// Used by the till (active only) and the /rates screen (unfiltered, so an
// admin can also reactivate a product).
func (h *ProductsHandler) List(c *gin.Context) {
	var activeOnly *bool
	if a := c.Query("active"); a != "" {
		v := a == "1" || a == "true"
		activeOnly = &v
	}
	products, err := listProducts(h.db, activeOnly)
	if err != nil {
		log.Printf("products list: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not load products", "internal_error"))
		return
	}
	c.JSON(http.StatusOK, models.OK("OK", products))
}

// Create adds a product. Every creation writes a product_rate_history row
// (old_rate = 0, new_rate = rate) in the same transaction, so the rate a
// product launches at is part of its history from day one.
func (h *ProductsHandler) Create(c *gin.Context) {
	actorID, _, _, ok := middleware.UserFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, models.Fail("Not signed in", "auth_required"))
		return
	}
	var req models.CreateProductRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.Fail("Invalid request body", "invalid_request"))
		return
	}
	name := strings.TrimSpace(req.Name)
	if !validProductName(name) {
		c.JSON(http.StatusBadRequest, models.Fail("Name is required and at most 120 characters", "invalid_name"))
		return
	}
	sku, ok := normaliseSKU(req.SKU)
	if !ok {
		c.JSON(http.StatusBadRequest, models.Fail("SKU is at most 40 characters", "invalid_sku"))
		return
	}
	if !validRate(req.Rate) {
		c.JSON(http.StatusBadRequest, models.Fail("Rate must be zero or more with at most 2 decimal places", "invalid_rate"))
		return
	}
	hsCode, ok := validHSCode(req.HSCode)
	if !ok {
		c.JSON(http.StatusBadRequest, models.Fail("HS code must be blank or in the form 0000.0000", "invalid_hs_code"))
		return
	}
	fbrUoM := optionalText(req.FBRUoM)

	tx, err := h.db.Begin()
	if err != nil {
		log.Printf("products create: begin: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not create the product", "internal_error"))
		return
	}
	defer tx.Rollback()

	p, err := scanProduct(tx.QueryRow(`INSERT INTO products (name, sku, rate, hs_code, fbr_uom, sort_order)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING `+productColumns, name, sku, req.Rate, hsCode, fbrUoM, req.SortOrder))
	switch {
	case util.IsUniqueViolation(err, "uniq_products_name_lower"):
		c.JSON(http.StatusConflict, models.Fail("A product with that name already exists", "product_name_taken"))
		return
	case util.IsUniqueViolation(err, "uniq_products_sku"):
		c.JSON(http.StatusConflict, models.Fail("That SKU is already used by another product", "sku_taken"))
		return
	case err != nil:
		log.Printf("products create: insert: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not create the product", "internal_error"))
		return
	}
	if _, err := tx.Exec(`INSERT INTO product_rate_history (product_id, old_rate, new_rate, changed_by, note)
		VALUES ($1, 0, $2, $3, NULL)`, p.ID, req.Rate, actorID); err != nil {
		log.Printf("products create: history: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not create the product", "internal_error"))
		return
	}
	if err := tx.Commit(); err != nil {
		log.Printf("products create: commit: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not create the product", "internal_error"))
		return
	}
	c.JSON(http.StatusCreated, models.OK("Product created", p))
}

// Update changes name, SKU, HS code, FBR UoM, sort order or active flag.
// Rate is never touched here — see UpdateRates.
func (h *ProductsHandler) Update(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, models.Fail("Product not found", "product_not_found"))
		return
	}
	var req models.UpdateProductRequest
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
		if !validProductName(name) {
			c.JSON(http.StatusBadRequest, models.Fail("Name is required and at most 120 characters", "invalid_name"))
			return
		}
		add("name", name)
	}
	if req.SKU != nil {
		sku, ok := normaliseSKU(*req.SKU)
		if !ok {
			c.JSON(http.StatusBadRequest, models.Fail("SKU is at most 40 characters", "invalid_sku"))
			return
		}
		if sku == nil {
			sets = append(sets, "sku = NULL")
		} else {
			add("sku", *sku)
		}
	}
	if req.HSCode != nil {
		hs, ok := validHSCode(*req.HSCode)
		if !ok {
			c.JSON(http.StatusBadRequest, models.Fail("HS code must be blank or in the form 0000.0000", "invalid_hs_code"))
			return
		}
		if hs == nil {
			sets = append(sets, "hs_code = NULL")
		} else {
			add("hs_code", *hs)
		}
	}
	if req.FBRUoM != nil {
		if v := optionalText(*req.FBRUoM); v == nil {
			sets = append(sets, "fbr_uom = NULL")
		} else {
			add("fbr_uom", *v)
		}
	}
	if req.SortOrder != nil {
		add("sort_order", *req.SortOrder)
	}
	if req.IsActive != nil {
		add("is_active", *req.IsActive)
	}
	if len(sets) == 0 {
		c.JSON(http.StatusBadRequest, models.Fail("Nothing to update", "no_changes"))
		return
	}
	sets = append(sets, "updated_at = now()")
	args = append(args, id)
	p, err := scanProduct(h.db.QueryRow(fmt.Sprintf(`UPDATE products SET %s WHERE id = $%d RETURNING `+productColumns,
		strings.Join(sets, ", "), len(args)), args...))
	switch {
	case errors.Is(err, sql.ErrNoRows):
		c.JSON(http.StatusNotFound, models.Fail("Product not found", "product_not_found"))
		return
	case util.IsUniqueViolation(err, "uniq_products_name_lower"):
		c.JSON(http.StatusConflict, models.Fail("A product with that name already exists", "product_name_taken"))
		return
	case util.IsUniqueViolation(err, "uniq_products_sku"):
		c.JSON(http.StatusConflict, models.Fail("That SKU is already used by another product", "sku_taken"))
		return
	case err != nil:
		log.Printf("products update: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not update the product", "internal_error"))
		return
	}
	c.JSON(http.StatusOK, models.OK("Product updated", p))
}

// UpdateRates applies a batch of rate changes atomically: every change is
// validated before anything is written, so one invalid entry leaves every
// row untouched. Only rows whose rate actually differs get an UPDATE and a
// product_rate_history row; unchanged rates are silently skipped.
func (h *ProductsHandler) UpdateRates(c *gin.Context) {
	actorID, _, _, ok := middleware.UserFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, models.Fail("Not signed in", "auth_required"))
		return
	}
	var req models.UpdateRatesRequest
	if err := c.ShouldBindJSON(&req); err != nil || len(req.Changes) == 0 {
		c.JSON(http.StatusBadRequest, models.Fail("Send at least one rate change", "invalid_request"))
		return
	}
	for _, ch := range req.Changes {
		if !validRate(ch.Rate) {
			c.JSON(http.StatusBadRequest, models.Fail("Rate must be zero or more with at most 2 decimal places", "invalid_rate"))
			return
		}
	}
	note := optionalText(req.Note)

	tx, err := h.db.Begin()
	if err != nil {
		log.Printf("rates update: begin: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not update rates", "internal_error"))
		return
	}
	defer tx.Rollback()

	updated := 0
	for _, ch := range req.Changes {
		var current float64
		err := tx.QueryRow(`SELECT rate::float8 FROM products WHERE id = $1 FOR UPDATE`, ch.ProductID).Scan(&current)
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, models.Fail("Product not found", "product_not_found"))
			return
		}
		if err != nil {
			log.Printf("rates update: lock: %v", err)
			c.JSON(http.StatusInternalServerError, models.Fail("Could not update rates", "internal_error"))
			return
		}
		if ratesEqual(current, ch.Rate) {
			continue
		}
		if _, err := tx.Exec(`UPDATE products SET rate = $1, updated_at = now() WHERE id = $2`, ch.Rate, ch.ProductID); err != nil {
			log.Printf("rates update: update: %v", err)
			c.JSON(http.StatusInternalServerError, models.Fail("Could not update rates", "internal_error"))
			return
		}
		if _, err := tx.Exec(`INSERT INTO product_rate_history (product_id, old_rate, new_rate, changed_by, note)
			VALUES ($1, $2, $3, $4, $5)`, ch.ProductID, current, ch.Rate, actorID, note); err != nil {
			log.Printf("rates update: history: %v", err)
			c.JSON(http.StatusInternalServerError, models.Fail("Could not update rates", "internal_error"))
			return
		}
		updated++
	}

	products, err := listProducts(tx, nil)
	if err != nil {
		log.Printf("rates update: reload: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not update rates", "internal_error"))
		return
	}
	if err := tx.Commit(); err != nil {
		log.Printf("rates update: commit: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not update rates", "internal_error"))
		return
	}
	c.JSON(http.StatusOK, models.OK("Rates updated", models.UpdateRatesResponse{Updated: updated, Products: products}))
}

// RateHistory returns the most recent rate changes, newest first, optionally
// filtered to one product.
func (h *ProductsHandler) RateHistory(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	if limit < 1 || limit > 500 {
		limit = 50
	}
	where := []string{"true"}
	args := []any{}
	if pid := c.Query("product_id"); pid != "" {
		id, err := uuid.Parse(pid)
		if err != nil {
			c.JSON(http.StatusBadRequest, models.Fail("Invalid product id", "invalid_request"))
			return
		}
		args = append(args, id)
		where = append(where, fmt.Sprintf("h.product_id = $%d", len(args)))
	}
	args = append(args, limit)
	rows, err := h.db.Query(`
		SELECT h.id, h.product_id, p.name, h.old_rate::float8, h.new_rate::float8,
		       NULLIF(TRIM(CONCAT(u.first_name, ' ', u.last_name)), ''), h.changed_at, h.note
		FROM product_rate_history h
		JOIN products p ON p.id = h.product_id
		LEFT JOIN users u ON u.id = h.changed_by
		WHERE `+strings.Join(where, " AND ")+`
		ORDER BY h.changed_at DESC
		LIMIT $`+strconv.Itoa(len(args)), args...)
	if err != nil {
		log.Printf("rate history: query: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not load rate history", "internal_error"))
		return
	}
	defer rows.Close()
	entries := []models.RateHistoryEntry{}
	for rows.Next() {
		var e models.RateHistoryEntry
		if err := rows.Scan(&e.ID, &e.ProductID, &e.ProductName, &e.OldRate, &e.NewRate, &e.ChangedByName, &e.ChangedAt, &e.Note); err != nil {
			log.Printf("rate history: scan: %v", err)
			c.JSON(http.StatusInternalServerError, models.Fail("Could not load rate history", "internal_error"))
			return
		}
		entries = append(entries, e)
	}
	if err := rows.Err(); err != nil {
		log.Printf("rate history: rows: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not load rate history", "internal_error"))
		return
	}
	c.JSON(http.StatusOK, models.OK("OK", entries))
}
