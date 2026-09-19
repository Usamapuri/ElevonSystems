// Command migrate applies the embedded migrations to DATABASE_URL (or
// --database-url) without booting the API. Used by CI and local resets.
package main

import (
	"flag"
	"log"
	"os"

	"elevon-backend/internal/database"
)

func main() {
	dsn := flag.String("database-url", os.Getenv("DATABASE_URL"), "Postgres DSN (default $DATABASE_URL)")
	flag.Parse()
	if *dsn == "" {
		log.Fatal("DATABASE_URL or --database-url is required")
	}
	db, err := database.OpenPostgres(*dsn)
	if err != nil {
		log.Fatalf("open: %v", err)
	}
	defer db.Close()
	if err := database.Migrate(db); err != nil {
		log.Fatalf("migrate: %v", err)
	}
	log.Println("migrations up to date")
}
