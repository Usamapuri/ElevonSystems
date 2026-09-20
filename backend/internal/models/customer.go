package models

import (
	"time"

	"github.com/google/uuid"
)

// Customer is the public shape of a customer, including its current ledger
// balance (Σ debit − Σ credit; positive means the customer owes money).
type Customer struct {
	ID                    uuid.UUID `json:"id"`
	Name                  string    `json:"name"`
	Phone                 *string   `json:"phone"`
	NTN                   *string   `json:"ntn"`
	CNIC                  *string   `json:"cnic"`
	BuyerRegistrationType string    `json:"buyer_registration_type"`
	Address               *string   `json:"address"`
	Province              *string   `json:"province"`
	CreditAllowed         bool      `json:"credit_allowed"`
	CreditLimit           *float64  `json:"credit_limit"`
	IsActive              bool      `json:"is_active"`
	Notes                 *string   `json:"notes"`
	Balance               float64   `json:"balance"`
	CreatedAt             time.Time `json:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"`
}

// CreateCustomerRequest — admin creates a customer. A blank
// BuyerRegistrationType defaults to "Unregistered" (the FBR-safe default).
type CreateCustomerRequest struct {
	Name                  string   `json:"name"`
	Phone                 string   `json:"phone"`
	NTN                   string   `json:"ntn"`
	CNIC                  string   `json:"cnic"`
	BuyerRegistrationType string   `json:"buyer_registration_type"`
	Address               string   `json:"address"`
	Province              string   `json:"province"`
	CreditAllowed         bool     `json:"credit_allowed"`
	CreditLimit           *float64 `json:"credit_limit"`
	Notes                 string   `json:"notes"`
}

// UpdateCustomerRequest — every field optional; nil means "leave as is".
// A non-nil pointer to an empty string clears a nullable text field (same
// convention as UpdateProductRequest). CreditLimit follows the same
// "nil leaves it unchanged" rule; there is no way to null out an existing
// credit limit through this endpoint in v1 — set it to 0 instead.
type UpdateCustomerRequest struct {
	Name                  *string  `json:"name"`
	Phone                 *string  `json:"phone"`
	NTN                   *string  `json:"ntn"`
	CNIC                  *string  `json:"cnic"`
	BuyerRegistrationType *string  `json:"buyer_registration_type"`
	Address               *string  `json:"address"`
	Province              *string  `json:"province"`
	CreditAllowed         *bool    `json:"credit_allowed"`
	CreditLimit           *float64 `json:"credit_limit"`
	IsActive              *bool    `json:"is_active"`
	Notes                 *string  `json:"notes"`
}
