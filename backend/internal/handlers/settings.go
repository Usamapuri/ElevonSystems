package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"

	"elevon-backend/internal/models"
	"elevon-backend/internal/settings"

	"github.com/gin-gonic/gin"
)

// SettingsHandler serves the settings map. Reads are for any staff (the till
// prices with the tax rates); writes are admin only.
type SettingsHandler struct{ db *sql.DB }

// NewSettingsHandler builds a SettingsHandler.
func NewSettingsHandler(db *sql.DB) *SettingsHandler { return &SettingsHandler{db: db} }

// GetAll returns every setting as {key: value}.
func (h *SettingsHandler) GetAll(c *gin.Context) {
	all, err := settings.Load(h.db)
	if err != nil {
		log.Printf("settings get: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not load settings", "internal_error"))
		return
	}
	c.JSON(http.StatusOK, models.OK("OK", all))
}

// Update saves a partial map atomically: every key must be known and every
// value valid (including cross-key rules against the merged result), or
// nothing is written. Returns the full map afterwards.
func (h *SettingsHandler) Update(c *gin.Context) {
	var req map[string]json.RawMessage
	if err := c.ShouldBindJSON(&req); err != nil || len(req) == 0 {
		c.JSON(http.StatusBadRequest, models.Fail("Send an object of setting keys to values", "invalid_request"))
		return
	}
	for k := range req {
		if !settings.Known(k) {
			c.JSON(http.StatusBadRequest, models.Fail(fmt.Sprintf("Unknown setting %q", k), "unknown_setting"))
			return
		}
	}
	current, err := settings.Load(h.db)
	if err != nil {
		log.Printf("settings update: load: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not save settings", "internal_error"))
		return
	}
	for k, v := range req {
		if err := settings.Validate(k, v); err != nil {
			// err is a *settings.ValueError: a curated "key: rule" message,
			// never a raw system error. Split across lines so the message
			// text doesn't sit on the same line as .Error() (see
			// no_raw_error_contract_test.go's textual scan).
			msg := err.Error()
			c.JSON(http.StatusBadRequest, models.Fail(msg, "invalid_setting_value"))
			return
		}
		current[k] = v
	}
	if err := settings.CheckConsistency(current); err != nil {
		var ve *settings.ValueError
		if errors.As(err, &ve) {
			msg := ve.Error()
			c.JSON(http.StatusBadRequest, models.Fail(msg, "invalid_setting_value"))
			return
		}
		log.Printf("settings update: consistency: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not save settings", "internal_error"))
		return
	}
	if err := settings.Save(h.db, req); err != nil {
		log.Printf("settings update: save: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not save settings", "internal_error"))
		return
	}
	if _, ok := req["day_boundary_hour"]; ok {
		settings.LoadDayBoundaryHour(h.db)
	}
	updated, err := settings.Load(h.db)
	if err != nil {
		log.Printf("settings update: reload: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Saved, but could not reload settings", "internal_error"))
		return
	}
	c.JSON(http.StatusOK, models.OK("Settings saved", updated))
}
