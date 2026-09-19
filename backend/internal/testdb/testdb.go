// Package testdb gives DB-backed tests a fresh, migrated Postgres schema.
// Tests skip loudly when TEST_DATABASE_URL is unset; CI fails the job if the
// skip message appears, so money-path tests can never rot silently.
package testdb

import (
	"database/sql"
	"os"
	"testing"

	"elevon-backend/internal/database"
)

// EnvVar names the DSN for DB-backed tests.
const EnvVar = "TEST_DATABASE_URL"

// Fresh drops and recreates the public schema, runs every migration, and
// returns the pool. The database must be disposable.
func Fresh(t *testing.T) *sql.DB {
	t.Helper()
	dsn := os.Getenv(EnvVar)
	if dsn == "" {
		t.Skipf("%s not set — skipping DB-backed test", EnvVar)
	}
	db, err := database.OpenPostgres(dsn)
	if err != nil {
		t.Fatalf("testdb: open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(`DROP SCHEMA public CASCADE; CREATE SCHEMA public;`); err != nil {
		t.Fatalf("testdb: reset schema: %v", err)
	}
	if err := database.Migrate(db); err != nil {
		t.Fatalf("testdb: migrate: %v", err)
	}
	return db
}
