// Package database owns the Postgres pool, migrations and first-admin bootstrap.
package database

import (
	"database/sql"
	"fmt"
	"log"
	"net/url"
	"strings"
	"time"

	_ "github.com/lib/pq"

	"elevon-backend/internal/util"
)

// Config is the discrete-variable form of a connection (local dev).
type Config struct {
	Host, Port, User, Password, DBName, SSLMode string
}

// Connect opens a pool from discrete variables.
func Connect(c Config) (*sql.DB, error) {
	dsn := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		c.Host, c.Port, c.User, c.Password, c.DBName, c.SSLMode)
	return open(injectBusinessTimezoneOption(dsn))
}

// OpenPostgres opens a pool from a libpq string or postgres:// URL (Railway DATABASE_URL).
func OpenPostgres(dsn string) (*sql.DB, error) {
	return open(injectBusinessTimezoneOption(dsn))
}

func open(dsn string) (*sql.DB, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(5 * time.Minute)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	log.Println("database connection established")
	return db, nil
}

// injectBusinessTimezoneOption appends `options=-c TimeZone=<business tz>` so
// every pooled session opens in business time. Postgres resolves
// `timestamp = timestamptz` comparisons with the SESSION TimeZone; Railway
// defaults to UTC, which shifted hourly report buckets by exactly 5 hours in
// the retail product. An operator-supplied options= is left untouched.
func injectBusinessTimezoneOption(dsn string) string {
	tz := util.BusinessTimezoneName()
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		u, err := url.Parse(dsn)
		if err != nil {
			return dsn
		}
		q := u.Query()
		if q.Get("options") != "" {
			return dsn
		}
		q.Set("options", "-c TimeZone="+tz)
		u.RawQuery = q.Encode()
		return u.String()
	}
	if strings.Contains(dsn, "options=") {
		return dsn
	}
	return dsn + fmt.Sprintf(" options='-c TimeZone=%s'", tz)
}
