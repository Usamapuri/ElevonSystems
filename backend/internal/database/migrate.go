package database

import (
	"database/sql"
	"fmt"
	"io/fs"
	"log"
	"regexp"
	"sort"
	"strings"

	"elevon-backend/migrations"
)

var migrationNameRe = regexp.MustCompile(`^[0-9]{3}_[a-z0-9_]+\.sql$`)

// MigrationNames returns the .sql files in fsys sorted by name. Every file
// must be NNN_name.sql so the order is explicit.
func MigrationNames(fsys fs.FS) ([]string, error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("read migrations: %w", err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		if !migrationNameRe.MatchString(e.Name()) {
			return nil, fmt.Errorf("migration %q must be named NNN_name.sql", e.Name())
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names, nil
}

// Migrate applies every embedded migration not yet recorded in
// schema_migrations, in name order, each in its own transaction. The caller
// treats an error as fatal: with one store per deploy and a health check, a
// loud boot failure beats a silently missing column.
func Migrate(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version TEXT PRIMARY KEY,
		applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}
	names, err := MigrationNames(migrations.FS)
	if err != nil {
		return err
	}
	for _, name := range names {
		var applied bool
		if err := db.QueryRow(`SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`, name).Scan(&applied); err != nil {
			return fmt.Errorf("check %s: %w", name, err)
		}
		if applied {
			continue
		}
		body, err := fs.ReadFile(migrations.FS, name)
		if err != nil {
			return fmt.Errorf("read %s: %w", name, err)
		}
		tx, err := db.Begin()
		if err != nil {
			return fmt.Errorf("begin %s: %w", name, err)
		}
		// No bind parameters, so lib/pq uses the simple protocol and runs the
		// whole multi-statement file in one round trip.
		if _, err := tx.Exec(string(body)); err != nil {
			tx.Rollback()
			return fmt.Errorf("apply %s: %w", name, err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations (version) VALUES ($1)`, name); err != nil {
			tx.Rollback()
			return fmt.Errorf("record %s: %w", name, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit %s: %w", name, err)
		}
		log.Printf("migration applied: %s", name)
	}
	return nil
}
