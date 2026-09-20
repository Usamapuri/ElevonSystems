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
	if w.Code != http.StatusOK || len(all) != 21 || string(all["tax_rate_cash"]) != "0" {
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

	w = doJSON(r, http.MethodPut, "/admin/settings", map[string]any{"fiscal_config": map[string]any{}})
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
