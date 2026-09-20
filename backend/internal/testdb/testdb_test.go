package testdb

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"

	_ "github.com/lib/pq"
)

// TestFreshHoldsAdvisoryLock proves Fresh actually serialises DB-backed
// tests: a second, independent session cannot acquire testdbLockKey while
// Fresh's pool is live in a test, and can once that test's cleanup has run.
//
// This opens its own probe *sql.DB (distinct from Fresh's pool) pinned to a
// single connection, because session-level advisory locks are held per
// backend session: acquiring and releasing must happen on the same
// connection, and a plain *sql.DB pool would not guarantee that.
func TestFreshHoldsAdvisoryLock(t *testing.T) {
	dsn := os.Getenv(EnvVar)
	if dsn == "" {
		t.Skipf("%s not set — skipping DB-backed test", EnvVar)
	}

	probeDB, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("testdb: open probe: %v", err)
	}
	defer probeDB.Close()

	ctx := context.Background()
	probeConn, err := probeDB.Conn(ctx)
	if err != nil {
		t.Fatalf("testdb: acquire probe connection: %v", err)
	}
	defer probeConn.Close()

	// tryLock attempts (and immediately gives back) testdbLockKey from the
	// probe's own session, reporting whether it was free.
	tryLock := func() bool {
		t.Helper()
		var locked bool
		if err := probeConn.QueryRowContext(ctx, "SELECT pg_try_advisory_lock($1)", testdbLockKey).Scan(&locked); err != nil {
			t.Fatalf("testdb: pg_try_advisory_lock: %v", err)
		}
		if locked {
			if _, err := probeConn.ExecContext(ctx, "SELECT pg_advisory_unlock($1)", testdbLockKey); err != nil {
				t.Fatalf("testdb: pg_advisory_unlock (probe): %v", err)
			}
		}
		return locked
	}

	t.Run("held while Fresh is active", func(t *testing.T) {
		Fresh(t) // acquires testdbLockKey and registers cleanup to release it
		if tryLock() {
			t.Fatalf("advisory lock was not held while Fresh's pool was live")
		}
	})
	// t.Run only returns once the subtest above and its t.Cleanup funcs have
	// finished, so Fresh has already released the lock at this point.

	// Under `go test ./...`, internal/database, internal/handlers and
	// internal/staffpin contend for the same lock against the same
	// TEST_DATABASE_URL, so another package's Fresh can win it in the
	// instant after ours releases. Poll instead of asserting immediately:
	// this proves the lock does become available again without being
	// flaky about exactly when.
	deadline := time.Now().Add(60 * time.Second)
	for {
		if tryLock() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("advisory lock never became available after Fresh's cleanup ran")
		}
		time.Sleep(50 * time.Millisecond)
	}
}
