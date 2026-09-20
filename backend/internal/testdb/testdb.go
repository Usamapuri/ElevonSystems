// Package testdb gives DB-backed tests a fresh, migrated Postgres schema.
// Tests skip loudly when TEST_DATABASE_URL is unset; CI fails the job if the
// skip message appears, so money-path tests can never rot silently.
//
// go test ./... runs packages as separate, parallel processes. Several
// packages (internal/database, internal/handlers, internal/staffpin) call
// Fresh against the same TEST_DATABASE_URL, so without coordination their
// `DROP SCHEMA public CASCADE` / `CREATE SCHEMA public` resets race and the
// suite fails intermittently with duplicate-key errors on Postgres catalog
// tables. Fresh takes a session-level advisory lock for the life of the test
// to serialise these resets across processes.
package testdb

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"elevon-backend/internal/database"
)

// EnvVar names the DSN for DB-backed tests.
const EnvVar = "TEST_DATABASE_URL"

// testdbLockKey serialises DB-backed tests that share one TEST_DATABASE_URL
// across packages. Its value is arbitrary and only needs to be unique within
// this process' advisory-lock namespace.
const testdbLockKey int64 = 61_7331

// Fresh drops and recreates the public schema, runs every migration, and
// returns the pool. The database must be disposable.
//
// Fresh pins a session-level Postgres advisory lock on a dedicated
// connection for the life of the test so that concurrent test packages
// sharing one TEST_DATABASE_URL cannot reset the schema at the same time.
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

	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		t.Fatalf("testdb: acquire lock connection: %v", err)
	}
	t.Cleanup(func() { conn.Close() })

	if _, err := conn.ExecContext(ctx, "SELECT pg_advisory_lock($1)", testdbLockKey); err != nil {
		t.Fatalf("testdb: acquire advisory lock: %v", err)
	}
	t.Cleanup(func() {
		if _, err := conn.ExecContext(context.Background(), "SELECT pg_advisory_unlock($1)", testdbLockKey); err != nil {
			t.Logf("testdb: release advisory lock: %v", err)
		}
	})

	if _, err := db.Exec(`DROP SCHEMA public CASCADE; CREATE SCHEMA public;`); err != nil {
		t.Fatalf("testdb: reset schema: %v", err)
	}
	if err := database.Migrate(db); err != nil {
		t.Fatalf("testdb: migrate: %v", err)
	}
	return db
}
