// Command seed-demo inserts a small, fixed set of products and customers for
// the 2026-09-21 owner demo/rehearsal. It refuses to run when
// GIN_MODE=release — this is rehearsal data, never for a live store — and is
// safe to run more than once: every insert is ON CONFLICT DO NOTHING against
// the same unique indexes products.go and customers.go already enforce
// (uniq_products_name_lower, uniq_customers_phone_lower), so a repeat run
// changes nothing.
//
// No hs_code is set on any product — there is no HS-code default in code
// (CLAUDE.md invariant 7); the advisor confirms it later via the settings
// default or per-product field.
package main

import (
	"database/sql"
	"log"
	"os"

	"elevon-backend/internal/database"
)

type demoProduct struct {
	name string
	rate float64
}

type demoCustomer struct {
	name                  string
	phone                 string
	ntn                   string
	buyerRegistrationType string
	creditAllowed         bool
	creditLimit           *float64
}

func main() {
	if os.Getenv("GIN_MODE") == "release" {
		log.Fatal("seed-demo refuses to run when GIN_MODE=release — this is rehearsal data, never for a live store")
	}

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Fatal("DATABASE_URL is required")
	}

	db, err := database.OpenPostgres(dsn)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer db.Close()

	for _, p := range []demoProduct{
		{name: "LPG Bulk", rate: 265.00},
		{name: "LPG 11.8kg Cylinder Fill", rate: 268.00},
		{name: "LPG 45kg Cylinder Fill", rate: 262.00},
	} {
		seedProduct(db, p)
	}

	karachiAutoGasLimit := 50000.00
	bismillahHotelLimit := 20000.00

	for _, c := range []demoCustomer{
		{
			name:                  "Karachi Auto Gas",
			phone:                 "03001234567",
			ntn:                   "1234567-8",
			buyerRegistrationType: "Registered",
			creditAllowed:         true,
			creditLimit:           &karachiAutoGasLimit,
		},
		{
			name:                  "Bismillah Hotel",
			phone:                 "03211234567",
			buyerRegistrationType: "Unregistered",
			creditAllowed:         true,
			creditLimit:           &bismillahHotelLimit,
		},
		{
			name:                  "Cash customer Ahmed",
			phone:                 "03451234567",
			buyerRegistrationType: "Unregistered",
			creditAllowed:         false,
		},
	} {
		seedCustomer(db, c)
	}

	log.Println("seed-demo: done")
}

func seedProduct(db *sql.DB, p demoProduct) {
	var id string
	err := db.QueryRow(
		`INSERT INTO products (name, rate)
		 VALUES ($1, $2)
		 ON CONFLICT (lower(name)) DO NOTHING
		 RETURNING id`,
		p.name, p.rate,
	).Scan(&id)
	if err == sql.ErrNoRows {
		log.Printf("product %q already exists, skipped", p.name)
		return
	}
	if err != nil {
		log.Fatalf("insert product %q: %v", p.name, err)
	}
	if _, err := db.Exec(
		`INSERT INTO product_rate_history (product_id, old_rate, new_rate, note)
		 VALUES ($1, 0, $2, 'seed-demo')`,
		id, p.rate,
	); err != nil {
		log.Fatalf("rate history for %q: %v", p.name, err)
	}
	log.Printf("product %q created at Rs %.2f/kg", p.name, p.rate)
}

func seedCustomer(db *sql.DB, c demoCustomer) {
	var ntn any
	if c.ntn != "" {
		ntn = c.ntn
	}
	var id string
	err := db.QueryRow(
		`INSERT INTO customers (name, phone, ntn, buyer_registration_type, credit_allowed, credit_limit)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 ON CONFLICT (lower(phone)) WHERE phone IS NOT NULL DO NOTHING
		 RETURNING id`,
		c.name, c.phone, ntn, c.buyerRegistrationType, c.creditAllowed, c.creditLimit,
	).Scan(&id)
	if err == sql.ErrNoRows {
		log.Printf("customer %q already exists, skipped", c.name)
		return
	}
	if err != nil {
		log.Fatalf("insert customer %q: %v", c.name, err)
	}
	log.Printf("customer %q created (phone %s)", c.name, c.phone)
}
