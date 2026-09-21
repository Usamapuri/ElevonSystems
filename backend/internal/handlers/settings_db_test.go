package handlers

import (
	"encoding/json"
	"net/http"
	"testing"

	"elevon-backend/internal/testdb"
	"elevon-backend/internal/util"

	"github.com/gin-gonic/gin"
)

func settingsRouter(h *SettingsHandler) *gin.Engine {
	r := gin.New()
	r.GET("/settings", h.GetAll)
	r.PUT("/admin/settings", h.Update)
	return r
}

func TestSettings_GetUpdateValidateAndReloadBoundary(t *testing.T) {
	db := testdb.Fresh(t)
	t.Cleanup(func() { util.SetDayBoundaryHour(0) })
	r := settingsRouter(NewSettingsHandler(db))

	w := doJSON(r, http.MethodGet, "/settings", nil)
	var all map[string]json.RawMessage
	dataAs(t, decodeEnvelope(t, w), &all)
	// 24 keys are seeded; the private one is withheld from every response.
	if w.Code != http.StatusOK || len(all) != 23 || string(all["tax_rate_cash"]) != "0" {
		t.Fatalf("seeded settings: %d %d %s", w.Code, len(all), all["tax_rate_cash"])
	}

	// A bad value anywhere rejects the whole request; nothing is saved.
	w = doJSON(r, http.MethodPut, "/admin/settings", map[string]any{"business_name": "Gas House", "tax_rate_cash": 1.5})
	if w.Code != http.StatusBadRequest || errCode(decodeEnvelope(t, w)) != "invalid_setting_value" {
		t.Fatalf("invalid: %d %s", w.Code, w.Body.String())
	}
	dataAs(t, decodeEnvelope(t, doJSON(r, http.MethodGet, "/settings", nil)), &all)
	if string(all["business_name"]) != `"Elevon POS"` {
		t.Fatalf("partial save leaked: %s", all["business_name"])
	}

	// tax_rate_cash is not nullable — only tax_rate_credit may be null.
	w = doJSON(r, http.MethodPut, "/admin/settings", map[string]any{"tax_rate_cash": nil})
	if w.Code != http.StatusBadRequest || errCode(decodeEnvelope(t, w)) != "invalid_setting_value" {
		t.Fatalf("null tax_rate_cash: %d %s", w.Code, w.Body.String())
	}
	dataAs(t, decodeEnvelope(t, doJSON(r, http.MethodGet, "/settings", nil)), &all)
	if string(all["tax_rate_cash"]) != "0" {
		t.Fatalf("null tax_rate_cash must not be saved: %s", all["tax_rate_cash"])
	}

	w = doJSON(r, http.MethodPut, "/admin/settings", map[string]any{"no_such_setting": 1})
	if errCode(decodeEnvelope(t, w)) != "unknown_setting" {
		t.Fatalf("unknown key: %s", w.Body.String())
	}
	w = doJSON(r, http.MethodPut, "/admin/settings", map[string]any{"receipt_paper_width_mm": 58})
	if errCode(decodeEnvelope(t, w)) != "invalid_setting_value" {
		t.Fatalf("58 mm paper with the seeded 72 mm printable area must fail the cross-check: %s", w.Body.String())
	}

	w = doJSON(r, http.MethodPut, "/admin/settings", map[string]any{
		"business_name": "Gas House", "tax_rate_cash": 0.18, "tax_rate_credit": nil, "day_boundary_hour": 5,
		"receipt_header_lines": []string{"NTN 1234567-8"},
	})
	if w.Code != http.StatusOK {
		t.Fatalf("save: %d %s", w.Code, w.Body.String())
	}
	dataAs(t, decodeEnvelope(t, w), &all)
	if string(all["business_name"]) != `"Gas House"` || string(all["tax_rate_cash"]) != "0.18" || string(all["tax_rate_credit"]) != "null" {
		t.Fatalf("saved values: %s %s %s", all["business_name"], all["tax_rate_cash"], all["tax_rate_credit"])
	}
	if util.DayBoundaryHour() != 5 {
		t.Fatalf("boundary hour must be applied after save, got %d", util.DayBoundaryHour())
	}
	if w := doJSON(r, http.MethodPut, "/admin/settings", map[string]any{}); errCode(decodeEnvelope(t, w)) != "invalid_request" {
		t.Fatalf("empty body: %s", w.Body.String())
	}
}

// The encrypted FBR token is private: it is never in a settings response and
// PUT /admin/settings refuses it outright. Only PUT /admin/fiscal/token
// writes it.
func TestSettings_FiscalTokenIsPrivate(t *testing.T) {
	db := testdb.Fresh(t)
	r := settingsRouter(NewSettingsHandler(db))

	var all map[string]json.RawMessage
	dataAs(t, decodeEnvelope(t, doJSON(r, http.MethodGet, "/settings", nil)), &all)
	if _, leaked := all["fiscal_api_key_enc"]; leaked {
		t.Fatal("GET /settings must never carry the encrypted token")
	}
	if _, ok := all["fiscal_config"]; !ok {
		t.Fatal("fiscal_config is public and must be returned")
	}

	w := doJSON(r, http.MethodPut, "/admin/settings", map[string]any{"fiscal_api_key_enc": "whatever"})
	if w.Code != http.StatusBadRequest || errCode(decodeEnvelope(t, w)) != "private_setting" {
		t.Fatalf("writing the token here must be refused: %d %s", w.Code, w.Body.String())
	}
	// Refused even when it rides along with a legitimate key, and nothing
	// else in that request is saved either.
	w = doJSON(r, http.MethodPut, "/admin/settings", map[string]any{"business_name": "Gas House", "fiscal_api_key_enc": "whatever"})
	if errCode(decodeEnvelope(t, w)) != "private_setting" {
		t.Fatalf("mixed request: %s", w.Body.String())
	}
	dataAs(t, decodeEnvelope(t, doJSON(r, http.MethodGet, "/settings", nil)), &all)
	if string(all["business_name"]) != `"Elevon POS"` {
		t.Fatalf("a refused request must save nothing: %s", all["business_name"])
	}

	// The seeded row is still there, untouched, for the token route to use.
	var stored string
	if err := db.QueryRow(`SELECT value #>> '{}' FROM settings WHERE key = 'fiscal_api_key_enc'`).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored != "" {
		t.Fatalf("the seed is an empty token, got %q", stored)
	}
}

// Switching FBR filing on is gated on the rest of the settings agreeing, and
// each refusal carries its own code so the screen can place the message.
func TestSettings_EnablingFiscalIsGated(t *testing.T) {
	db := testdb.Fresh(t)
	r := settingsRouter(NewSettingsHandler(db))

	enabled := map[string]any{
		"enabled": true, "is_sandbox": true,
		"seller_ntn_cnic": "8951943", "seller_business_name": "Elevon Gas",
		"seller_province": "PUNJAB", "seller_address": "Main Bazar, Lahore",
		"scenario_registered": "SN001", "scenario_unregistered": "SN002",
		"rate_desc": "18%", "sale_type": "Goods at Standard Rate (default)",
		"trans_type_id": 75, "default_uom": "KG",
		"buyer_registration_default": "Unregistered",
		"validate_url":               "", "post_url": "",
	}

	// tax_rate_cash is still the seeded 0, so "18%" disagrees with pricing.
	w := doJSON(r, http.MethodPut, "/admin/settings", map[string]any{"fiscal_config": enabled})
	if w.Code != http.StatusBadRequest || errCode(decodeEnvelope(t, w)) != "tax_rate_mismatch" {
		t.Fatalf("rate mismatch: %d %s", w.Code, w.Body.String())
	}

	// With the rate agreeing, the missing token is next.
	w = doJSON(r, http.MethodPut, "/admin/settings", map[string]any{"fiscal_config": enabled, "tax_rate_cash": 0.18})
	if w.Code != http.StatusBadRequest || errCode(decodeEnvelope(t, w)) != "fiscal_token_missing" {
		t.Fatalf("token missing: %d %s", w.Code, w.Body.String())
	}

	// The token only reaches the table through its own route, which F2 adds;
	// stand in for it here so the reference rules can be exercised.
	if _, err := db.Exec(`UPDATE settings SET value = to_jsonb('sealed'::text) WHERE key = 'fiscal_api_key_enc'`); err != nil {
		t.Fatal(err)
	}
	w = doJSON(r, http.MethodPut, "/admin/settings", map[string]any{"fiscal_config": enabled, "tax_rate_cash": 0.18})
	if w.Code != http.StatusBadRequest || errCode(decodeEnvelope(t, w)) != "fiscal_reference_stale" {
		t.Fatalf("reference stale: %d %s", w.Code, w.Body.String())
	}

	if _, err := db.Exec(`UPDATE settings SET value = $1::jsonb WHERE key = 'fiscal_reference'`,
		`{"hs_uom":{"2711.1910":["KG"]}}`); err != nil {
		t.Fatal(err)
	}
	// default_hs_code is still "", so the reference does not cover it yet.
	w = doJSON(r, http.MethodPut, "/admin/settings", map[string]any{"fiscal_config": enabled, "tax_rate_cash": 0.18})
	if errCode(decodeEnvelope(t, w)) != "fiscal_reference_stale" {
		t.Fatalf("blank HS code: %s", w.Body.String())
	}

	// With the HS code set, a UoM FBR does not list for it is refused (0099).
	badUoM := map[string]any{}
	for k, v := range enabled {
		badUoM[k] = v
	}
	badUoM["default_uom"] = "Liter"
	w = doJSON(r, http.MethodPut, "/admin/settings",
		map[string]any{"fiscal_config": badUoM, "tax_rate_cash": 0.18, "default_hs_code": "2711.1910"})
	if w.Code != http.StatusBadRequest || errCode(decodeEnvelope(t, w)) != "fiscal_uom_not_allowed" {
		t.Fatalf("uom not allowed: %d %s", w.Code, w.Body.String())
	}

	// Everything agreeing: the switch goes on.
	w = doJSON(r, http.MethodPut, "/admin/settings",
		map[string]any{"fiscal_config": enabled, "tax_rate_cash": 0.18, "default_hs_code": "2711.1910"})
	if w.Code != http.StatusOK {
		t.Fatalf("complete setup must save: %d %s", w.Code, w.Body.String())
	}
	var all map[string]json.RawMessage
	dataAs(t, decodeEnvelope(t, w), &all)
	var cfg struct {
		Enabled     bool   `json:"enabled"`
		TransTypeID int    `json:"trans_type_id"`
		RateDesc    string `json:"rate_desc"`
	}
	if err := json.Unmarshal(all["fiscal_config"], &cfg); err != nil {
		t.Fatal(err)
	}
	if !cfg.Enabled || cfg.TransTypeID != 75 || cfg.RateDesc != "18%" {
		t.Fatalf("saved fiscal_config: %+v", cfg)
	}
	if _, leaked := all["fiscal_api_key_enc"]; leaked {
		t.Fatal("the PUT response must not carry the token either")
	}

	// A malformed config is still the generic per-key refusal.
	w = doJSON(r, http.MethodPut, "/admin/settings", map[string]any{"fiscal_config": map[string]any{"nope": 1}})
	if errCode(decodeEnvelope(t, w)) != "invalid_setting_value" {
		t.Fatalf("unknown field: %s", w.Body.String())
	}
}
