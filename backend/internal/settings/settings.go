// Package settings owns the settings table: the list of known keys, the
// value rule for each, and the load/save helpers every other package uses.
// Values are JSON. Tax rates are fractions (0.18), never percentages.
package settings

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"elevon-backend/internal/fiscal"
	"elevon-backend/internal/util"
)

// ValueError names the key that failed and why, in words for a person.
//
// Code, when set, is the stable snake_case error code the handler puts in the
// envelope instead of the generic invalid_setting_value. The fiscal cross-key
// rules use it so the admin screen can show each refusal beside the field it
// belongs to. Message is always curated text, never a system error.
type ValueError struct {
	Key     string
	Code    string
	Message string
}

func (e *ValueError) Error() string { return e.Key + ": " + e.Message }

type rule func(raw json.RawMessage) error

var hsCodeRe = regexp.MustCompile(`^[0-9]{4}\.[0-9]{4}$`)

// rules covers exactly the keys seeded in migrations/001_init.sql. Every
// entry is wrapped in notNull except tax_rate_credit, the one key the spec
// allows to be null.
var rules = map[string]rule{
	"business_name":     notNull(text(1, 120)),
	"business_address":  notNull(text(0, 300)),
	"business_phone":    notNull(text(0, 30)),
	"business_ntn":      notNull(text(0, 20)),
	"business_strn":     notNull(text(0, 30)),
	"business_province": notNull(text(0, 40)),
	"day_boundary_hour": notNull(integer(0, 12)),

	"tax_rate_cash":    notNull(number(0, 1)),
	"tax_rate_card":    notNull(number(0, 1)),
	"tax_rate_online":  notNull(number(0, 1)),
	"tax_rate_credit":  nullable(number(0, 1)),
	"further_tax_rate": notNull(number(0, 1)),
	"default_hs_code":  notNull(hsCode),

	"receipt_paper_width_mm":    notNull(oneOfInt(58, 80)),
	"receipt_printable_area_mm": notNull(integer(40, 80)),
	"receipt_logo_url":          notNull(logoURL),
	"receipt_header_lines":      notNull(stringList(6, 64)),
	"receipt_footer_lines":      notNull(stringList(6, 64)),
	"receipt_default_document":  notNull(oneOf("thermal", "a4")),

	"day_close_variance_threshold": notNull(number(0, 1_000_000)),
	"credit_limit_enforced":        notNull(boolean),

	// Phase 7, seeded by migrations/003_fiscal.sql. fiscal_api_key_enc holds
	// the AES-GCM ciphertext of the FBR token and is private: it never leaves
	// the server through GET /settings and is not writable through
	// PUT /admin/settings.
	"fiscal_config":      notNull(fiscalConfig),
	"fiscal_api_key_enc": notNull(text(0, 512)),
	"fiscal_reference":   notNull(jsonObject),
}

// privateKeys never leave the server in a settings response and cannot be
// written through the general settings endpoint. Each has its own guarded
// route instead (the FBR token: PUT /admin/fiscal/token).
var privateKeys = map[string]bool{"fiscal_api_key_enc": true}

// Private reports whether key is withheld from GET /settings and refused by
// PUT /admin/settings.
func Private(key string) bool { return privateKeys[key] }

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
	if err := checkReceipt(all); err != nil {
		return err
	}
	return checkFiscal(all)
}

func checkReceipt(all map[string]json.RawMessage) error {
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

// checkFiscal is the gate on switching FBR filing on. While fiscal_config is
// disabled nothing here applies, so setup can happen in any order; the moment
// enabled goes true every other key it depends on must already agree, because
// each of these mismatches is an FBR rejection at the till:
//
//   - rate_desc against tax_rate_cash — FBR recomputes tax as
//     valueSalesExcludingST × rate and rejects with 0104 when the payload's
//     figures were priced at a different rate.
//   - a stored token — without one nothing can be filed at all.
//   - the reference lists covering default_hs_code and default_uom — FBR
//     rejects an unlisted UoM for an HS code with 0099.
//
// Each refusal carries its own code so the admin screen can put the message
// beside the right field.
func checkFiscal(all map[string]json.RawMessage) error {
	raw, ok := all[fiscal.KeyConfig]
	if !ok {
		return nil
	}
	cfg, err := fiscal.ParseConfig(raw)
	if err != nil || !cfg.Enabled {
		return nil // Validate already reports a malformed config
	}

	var cash float64
	pct, pctErr := strconv.ParseFloat(strings.TrimSuffix(cfg.RateDesc, "%"), 64)
	if err := json.Unmarshal(all["tax_rate_cash"], &cash); err != nil || pctErr != nil || math.Abs(pct-cash*100) > 1e-9 {
		return &ValueError{
			Key:     fiscal.KeyConfig,
			Code:    "tax_rate_mismatch",
			Message: "rate_desc does not match tax_rate_cash",
		}
	}

	var token string
	if err := json.Unmarshal(all[fiscal.KeyAPIKeyEnc], &token); err != nil || strings.TrimSpace(token) == "" {
		return &ValueError{
			Key:     fiscal.KeyConfig,
			Code:    "fiscal_token_missing",
			Message: "save the FBR token before switching filing on",
		}
	}

	var hsCode string
	_ = json.Unmarshal(all["default_hs_code"], &hsCode)
	var ref struct {
		HSUoM map[string][]string `json:"hs_uom"`
	}
	if err := json.Unmarshal(all[fiscal.KeyReference], &ref); err != nil || len(ref.HSUoM) == 0 {
		return &ValueError{
			Key:     fiscal.KeyConfig,
			Code:    "fiscal_reference_stale",
			Message: "refresh the FBR reference lists before switching filing on",
		}
	}
	allowed, ok := ref.HSUoM[hsCode]
	if !ok {
		return &ValueError{
			Key:     fiscal.KeyConfig,
			Code:    "fiscal_reference_stale",
			Message: "refresh the FBR reference lists for the default HS code before switching filing on",
		}
	}
	for _, u := range allowed {
		if strings.EqualFold(strings.TrimSpace(u), strings.TrimSpace(cfg.DefaultUoM)) {
			return nil
		}
	}
	return &ValueError{
		Key:     fiscal.KeyConfig,
		Code:    "fiscal_uom_not_allowed",
		Message: "FBR does not allow this unit of measure for the default HS code",
	}
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

// LoadPublic is Load without the private keys. Every path that hands settings
// to a browser uses this; server-side readers (pricing, day ops) keep Load.
func LoadPublic(db *sql.DB) (map[string]json.RawMessage, error) {
	all, err := Load(db)
	if err != nil {
		return nil, err
	}
	for k := range all {
		if Private(k) {
			delete(all, k)
		}
	}
	return all, nil
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

// notNull rejects the JSON literal null (and an empty value) before handing
// off to inner. json.Unmarshal("null", &x) silently succeeds and leaves the
// zero value, so every rule needs this guard except tax_rate_credit, the one
// key the spec allows to be null.
func notNull(inner rule) rule {
	return func(raw json.RawMessage) error {
		if s := strings.TrimSpace(string(raw)); s == "" || s == "null" {
			return errors.New("is required")
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

// jsonObject accepts any JSON object. fiscal_reference is a cache of FBR's
// own lists, written by the refresh job rather than typed, so its shape is
// the refresher's contract (internal/fiscal) and not this package's.
func jsonObject(raw json.RawMessage) error {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return errors.New("must be an object")
	}
	return nil
}

// fiscalConfig defers to internal/fiscal, which owns the shape and the rules.
// The import goes settings → fiscal and never back.
func fiscalConfig(raw json.RawMessage) error {
	cfg, err := fiscal.ParseConfig(raw)
	if err != nil {
		return err
	}
	return cfg.Validate()
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
