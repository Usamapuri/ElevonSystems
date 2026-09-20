// Package settings owns the settings table: the list of known keys, the
// value rule for each, and the load/save helpers every other package uses.
// Values are JSON. Tax rates are fractions (0.18), never percentages.
package settings

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"unicode/utf8"

	"elevon-backend/internal/util"
)

// ValueError names the key that failed and why, in words for a person.
type ValueError struct {
	Key     string
	Message string
}

func (e *ValueError) Error() string { return e.Key + ": " + e.Message }

type rule func(raw json.RawMessage) error

var hsCodeRe = regexp.MustCompile(`^[0-9]{4}\.[0-9]{4}$`)

// rules covers exactly the keys seeded in migrations/001_init.sql.
var rules = map[string]rule{
	"business_name":     text(1, 120),
	"business_address":  text(0, 300),
	"business_phone":    text(0, 30),
	"business_ntn":      text(0, 20),
	"business_strn":     text(0, 30),
	"business_province": text(0, 40),
	"day_boundary_hour": integer(0, 12),

	"tax_rate_cash":    number(0, 1),
	"tax_rate_card":    number(0, 1),
	"tax_rate_online":  number(0, 1),
	"tax_rate_credit":  nullable(number(0, 1)),
	"further_tax_rate": number(0, 1),
	"default_hs_code":  hsCode,

	"receipt_paper_width_mm":    oneOfInt(58, 80),
	"receipt_printable_area_mm": integer(40, 80),
	"receipt_logo_url":          logoURL,
	"receipt_header_lines":      stringList(6, 64),
	"receipt_footer_lines":      stringList(6, 64),
	"receipt_default_document":  oneOf("thermal", "a4"),

	"day_close_variance_threshold": number(0, 1_000_000),
	"credit_limit_enforced":        boolean,
}

// Keys lists every known setting, sorted.
func Keys() []string {
	keys := make([]string, 0, len(rules))
	for k := range rules {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Known reports whether key has a rule.
func Known(key string) bool { _, ok := rules[key]; return ok }

// Validate checks one value against its key's rule.
func Validate(key string, raw json.RawMessage) error {
	r, ok := rules[key]
	if !ok {
		return &ValueError{Key: key, Message: "unknown setting"}
	}
	if err := r(raw); err != nil {
		return &ValueError{Key: key, Message: err.Error()}
	}
	return nil
}

// CheckConsistency applies the cross-key rules to a complete map.
func CheckConsistency(all map[string]json.RawMessage) error {
	var width, printable int
	if err := json.Unmarshal(all["receipt_paper_width_mm"], &width); err != nil {
		return nil // missing or invalid on its own; Validate reports that
	}
	if err := json.Unmarshal(all["receipt_printable_area_mm"], &printable); err != nil {
		return nil
	}
	if printable > width {
		return &ValueError{Key: "receipt_printable_area_mm", Message: fmt.Sprintf("must not exceed the paper width (%d mm)", width)}
	}
	return nil
}

// Load returns every row.
func Load(db *sql.DB) (map[string]json.RawMessage, error) {
	rows, err := db.Query(`SELECT key, value FROM settings ORDER BY key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]json.RawMessage{}
	for rows.Next() {
		var k string
		var v json.RawMessage
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

// Save upserts the given values in one transaction. Callers validate first.
func Save(db *sql.DB, values map[string]json.RawMessage) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for k, v := range values {
		if _, err := tx.Exec(`INSERT INTO settings (key, value, updated_at) VALUES ($1, $2::jsonb, now())
			ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()`, k, string(v)); err != nil {
			return fmt.Errorf("save %s: %w", k, err)
		}
	}
	return tx.Commit()
}

// LoadDayBoundaryHour applies settings.day_boundary_hour to util so
// BusinessDate() agrees with the owner's setting. Called at boot and after
// every save of that key. Silently keeps the current value on any error.
func LoadDayBoundaryHour(db *sql.DB) {
	var n int
	if err := db.QueryRow(`SELECT (value #>> '{}')::int FROM settings WHERE key = 'day_boundary_hour'`).Scan(&n); err == nil {
		util.SetDayBoundaryHour(n)
	}
}

// ── rule constructors ────────────────────────────────────────────────────

func text(min, max int) rule {
	return func(raw json.RawMessage) error {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return errors.New("must be text")
		}
		n := utf8.RuneCountInString(strings.TrimSpace(s))
		if n < min {
			return errors.New("is required")
		}
		if n > max {
			return fmt.Errorf("must be at most %d characters", max)
		}
		return nil
	}
}

func integer(lo, hi int) rule {
	return func(raw json.RawMessage) error {
		var n int
		if err := json.Unmarshal(raw, &n); err != nil {
			return errors.New("must be a whole number")
		}
		if n < lo || n > hi {
			return fmt.Errorf("must be between %d and %d", lo, hi)
		}
		return nil
	}
}

func number(lo, hi float64) rule {
	return func(raw json.RawMessage) error {
		var f float64
		if err := json.Unmarshal(raw, &f); err != nil {
			return errors.New("must be a number")
		}
		if f < lo || f > hi {
			return fmt.Errorf("must be between %g and %g", lo, hi)
		}
		return nil
	}
}

func nullable(inner rule) rule {
	return func(raw json.RawMessage) error {
		if strings.TrimSpace(string(raw)) == "null" {
			return nil
		}
		return inner(raw)
	}
}

func boolean(raw json.RawMessage) error {
	var b bool
	if err := json.Unmarshal(raw, &b); err != nil {
		return errors.New("must be true or false")
	}
	return nil
}

func oneOf(vals ...string) rule {
	return func(raw json.RawMessage) error {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return errors.New("must be text")
		}
		for _, v := range vals {
			if s == v {
				return nil
			}
		}
		return fmt.Errorf("must be one of %s", strings.Join(vals, ", "))
	}
}

func oneOfInt(vals ...int) rule {
	return func(raw json.RawMessage) error {
		var n int
		if err := json.Unmarshal(raw, &n); err != nil {
			return errors.New("must be a whole number")
		}
		for _, v := range vals {
			if n == v {
				return nil
			}
		}
		return fmt.Errorf("must be one of %v", vals)
	}
}

func stringList(maxItems, maxLen int) rule {
	return func(raw json.RawMessage) error {
		var items []string
		if err := json.Unmarshal(raw, &items); err != nil {
			return errors.New("must be a list of text lines")
		}
		if len(items) > maxItems {
			return fmt.Errorf("at most %d lines", maxItems)
		}
		for _, it := range items {
			if utf8.RuneCountInString(it) > maxLen {
				return fmt.Errorf("each line at most %d characters", maxLen)
			}
		}
		return nil
	}
}

func hsCode(raw json.RawMessage) error {
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return errors.New("must be text")
	}
	if s == "" || hsCodeRe.MatchString(s) {
		return nil
	}
	return errors.New("must look like 2711.1910")
}

// logoURL accepts blank, an https URL, or an inline image data URI (kept
// small: 400 000 characters ≈ 300 KB of PNG).
func logoURL(raw json.RawMessage) error {
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return errors.New("must be text")
	}
	switch {
	case s == "", strings.HasPrefix(s, "https://"), strings.HasPrefix(s, "data:image/"):
	default:
		return errors.New("must be blank, an https:// URL or an uploaded image")
	}
	if len(s) > 400_000 {
		return errors.New("image is too large; use one under 300 KB")
	}
	return nil
}
