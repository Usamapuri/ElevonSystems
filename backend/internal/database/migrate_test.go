package database_test

import (
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
