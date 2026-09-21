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

// GetAll returns every setting as {key: value}, minus the private ones — the
// encrypted FBR token never leaves the server here.
func (h *SettingsHandler) GetAll(c *gin.Context) {
	all, err := settings.LoadPublic(h.db)
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
		// A private key has its own guarded route; writing it here would let
		// a plaintext FBR token into the settings table.
		if settings.Private(k) {
			c.JSON(http.StatusBadRequest, models.Fail(fmt.Sprintf("%q is not writable here", k), "private_setting"))
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
			var ve *settings.ValueError
			if !errors.As(err, &ve) {
				log.Printf("settings update: validate %s: %v", k, err)
				c.JSON(http.StatusInternalServerError, models.Fail("Could not save settings", "internal_error"))
				return
			}
			// ve.Key/ve.Message are curated, person-facing text — never a
			// raw system error — so building the message from those fields
			// is safe to send to the client.
			c.JSON(http.StatusBadRequest, models.Fail(ve.Key+": "+ve.Message, settingErrorCode(ve)))
			return
		}
		current[k] = v
	}
	if err := settings.CheckConsistency(current); err != nil {
		var ve *settings.ValueError
		if errors.As(err, &ve) {
			c.JSON(http.StatusBadRequest, models.Fail(ve.Key+": "+ve.Message, settingErrorCode(ve)))
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
	updated, err := settings.LoadPublic(h.db)
	if err != nil {
		log.Printf("settings update: reload: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Saved, but could not reload settings", "internal_error"))
		return
	}
	c.JSON(http.StatusOK, models.OK("Settings saved", updated))
}

// settingErrorCode picks the envelope's error code. Most refusals are the
// generic invalid_setting_value; the fiscal cross-key rules carry their own
// stable code so the FBR screen can show each one beside its field.
func settingErrorCode(ve *settings.ValueError) string {
	if ve.Code != "" {
		return ve.Code
	}
	return "invalid_setting_value"
}
