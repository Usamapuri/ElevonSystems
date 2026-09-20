package models

import (
	"time"

	"github.com/google/uuid"
)

// Product is the public shape of a product and its current rate.
type Product struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	SKU       *string   `json:"sku"`
	SellBy    string    `json:"sell_by"`
	UnitLabel string    `json:"unit_label"`
	Rate      float64   `json:"rate"`
	HSCode    *string   `json:"hs_code"`
	FBRUoM    *string   `json:"fbr_uom"`
	SortOrder int       `json:"sort_order"`
	IsActive  bool      `json:"is_active"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// CreateProductRequest — admin creates a product. sell_by is always 'weight'
// and unit_label always 'kg' in v1, so neither is accepted from the client.
type CreateProductRequest struct {
	Name      string  `json:"name"`
	SKU       string  `json:"sku"`
	Rate      float64 `json:"rate"`
	HSCode    string  `json:"hs_code"`
	FBRUoM    string  `json:"fbr_uom"`
	SortOrder int     `json:"sort_order"`
}

// UpdateProductRequest — every field optional; nil means "leave as is". No
// rate here: rate changes go through PUT /admin/rates so every change is
// logged to product_rate_history.
type UpdateProductRequest struct {
	Name      *string `json:"name"`
	SKU       *string `json:"sku"`
	HSCode    *string `json:"hs_code"`
	FBRUoM    *string `json:"fbr_uom"`
	SortOrder *int    `json:"sort_order"`
	IsActive  *bool   `json:"is_active"`
}

// RateChange is one row of a PUT /admin/rates request.
type RateChange struct {
	ProductID uuid.UUID `json:"product_id"`
	Rate      float64   `json:"rate"`
}

// UpdateRatesRequest is an atomic bulk rate change; Note is attached to
// every history row this request writes.
type UpdateRatesRequest struct {
	Changes []RateChange `json:"changes"`
	Note    string       `json:"note"`
}

// UpdateRatesResponse reports how many rows actually changed and returns
// every product so the caller can replace its cache wholesale.
type UpdateRatesResponse struct {
	Updated  int       `json:"updated"`
	Products []Product `json:"products"`
}

// RateHistoryEntry is one row of GET /admin/rates/history.
type RateHistoryEntry struct {
	ID            uuid.UUID `json:"id"`
	ProductID     uuid.UUID `json:"product_id"`
	ProductName   string    `json:"product_name"`
	OldRate       float64   `json:"old_rate"`
	NewRate       float64   `json:"new_rate"`
	ChangedByName *string   `json:"changed_by_name"`
	ChangedAt     time.Time `json:"changed_at"`
	Note          *string   `json:"note"`
}
