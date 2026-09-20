package database_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"elevon-backend/internal/database"
	"elevon-backend/internal/testdb"
	"elevon-backend/migrations"
)

func TestMigrationNames_SortedAndSQLOnly(t *testing.T) {
	fsys := fstest.MapFS{
		"010_later.sql":  {Data: []byte("select 1;")},
		"002_second.sql": {Data: []byte("select 1;")},
		"embed.go":       {Data: []byte("package migrations")},
		"001_first.sql":  {Data: []byte("select 1;")},
	}
	got, err := database.MigrationNames(fsys)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"001_first.sql", "002_second.sql", "010_later.sql"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}

func TestMigrationNames_RejectsUnprefixedFile(t *testing.T) {
	fsys := fstest.MapFS{"init.sql": {Data: []byte("select 1;")}}
	if _, err := database.MigrationNames(fsys); err == nil {
		t.Fatal("a migration without an NNN_ prefix must be rejected")
	}
}

func TestEmbeddedMigrations_ArePresent(t *testing.T) {
	names, err := database.MigrationNames(migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	if len(names) == 0 || names[0] != "001_init.sql" {
		t.Fatalf("expected 001_init.sql first, got %v", names)
	}
}

// DB-backed: Migrate is idempotent and records every file exactly once.
func TestMigrate_AppliesOnceAndIsIdempotent(t *testing.T) {
	db := testdb.Fresh(t) // runs Migrate once
	if err := database.Migrate(db); err != nil {
		t.Fatalf("second Migrate must be a no-op, got %v", err)
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	names, _ := database.MigrationNames(migrations.FS)
	if n != len(names) {
		t.Fatalf("schema_migrations has %d rows, want %d", n, len(names))
	}
	var settings int
	if err := db.QueryRow(`SELECT COUNT(*) FROM settings`).Scan(&settings); err != nil {
		t.Fatal(err)
	}
	if settings == 0 {
		t.Fatal("001_init must seed default settings")
	}
}

// DB-backed: void_log, customer_ledger_entries and day_close_audit_log are
// append-only — BEFORE UPDATE OR DELETE triggers must raise (spec §5.3–5.5).
func TestMigrations_AppendOnlyTriggersRaise(t *testing.T) {
	db := testdb.Fresh(t)

	if err := database.Migrate(db); err != nil {
		t.Fatalf("second Migrate must be a no-op, got %v", err)
	}

	if _, err := db.Exec(`INSERT INTO business_days (id, business_date, status) VALUES ('11111111-1111-1111-1111-111111111111', '2026-09-20', 'open')`); err != nil {
		t.Fatalf("insert business_days: %v", err)
	}

	if _, err := db.Exec(`INSERT INTO day_close_audit_log (id, business_date, action, summary) VALUES ('22222222-2222-2222-2222-222222222222', '2026-09-20', 'open', 'opened')`); err != nil {
		t.Fatalf("insert day_close_audit_log: %v", err)
	}
	if _, err := db.Exec(`UPDATE day_close_audit_log SET summary = 'x' WHERE id = '22222222-2222-2222-2222-222222222222'`); err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("day_close_audit_log UPDATE: want append-only error, got %v", err)
	}
	if _, err := db.Exec(`DELETE FROM day_close_audit_log WHERE id = '22222222-2222-2222-2222-222222222222'`); err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("day_close_audit_log DELETE: want append-only error, got %v", err)
	}

	if _, err := db.Exec(`INSERT INTO void_log (id, invoice_number, total_payable, reason) VALUES ('33333333-3333-3333-3333-333333333333', '20260920-001', 100, 'test void')`); err != nil {
		t.Fatalf("insert void_log: %v", err)
	}
	if _, err := db.Exec(`UPDATE void_log SET reason = 'x' WHERE id = '33333333-3333-3333-3333-333333333333'`); err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("void_log UPDATE: want append-only error, got %v", err)
	}
	if _, err := db.Exec(`DELETE FROM void_log WHERE id = '33333333-3333-3333-3333-333333333333'`); err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("void_log DELETE: want append-only error, got %v", err)
	}

	if _, err := db.Exec(`INSERT INTO customers (id, name) VALUES ('44444444-4444-4444-4444-444444444444', 'Test Customer')`); err != nil {
		t.Fatalf("insert customers: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO customer_ledger_entries (id, customer_id, entry_type, debit, business_date) VALUES ('55555555-5555-5555-5555-555555555555', '44444444-4444-4444-4444-444444444444', 'adjustment', 50, '2026-09-20')`); err != nil {
		t.Fatalf("insert customer_ledger_entries: %v", err)
	}
	if _, err := db.Exec(`UPDATE customer_ledger_entries SET debit = 60 WHERE id = '55555555-5555-5555-5555-555555555555'`); err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("customer_ledger_entries UPDATE: want append-only error, got %v", err)
	}
	if _, err := db.Exec(`DELETE FROM customer_ledger_entries WHERE id = '55555555-5555-5555-5555-555555555555'`); err == nil || !strings.Contains(err.Error(), "append-only") {
		t.Fatalf("customer_ledger_entries DELETE: want append-only error, got %v", err)
	}
}
