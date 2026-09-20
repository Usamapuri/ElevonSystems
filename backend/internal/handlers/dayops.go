package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"

	"elevon-backend/internal/dayops"
	"elevon-backend/internal/middleware"
	"elevon-backend/internal/models"
	"elevon-backend/internal/settings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// DayHandler serves the business day: open, drawer movements, the one-step
// close and the Z-report are for any staff member (one till, one person on
// it); reopen, force close and the day history are admin only, because they
// are the paths that can change a sealed number (routes.go).
type DayHandler struct{ db *sql.DB }

// NewDayHandler builds a DayHandler.
func NewDayHandler(db *sql.DB) *DayHandler { return &DayHandler{db: db} }

// dayActor is the signed-in user, as recorded on every audit row.
func dayActor(c *gin.Context) dayops.Actor {
	id, username, role, _ := middleware.UserFromContext(c)
	return dayops.Actor{ID: id, Name: username, Role: role}
}

// varianceThreshold reads settings.day_close_variance_threshold. A missing or
// unreadable setting falls back to the default rather than blocking a close:
// the note gate exists to make a person explain a real discrepancy, and a
// busted setting is not a reason to hold the till shut.
func varianceThreshold(db *sql.DB) float64 {
	all, err := settings.Load(db)
	if err != nil {
		log.Printf("day close: load threshold: %v", err)
		return dayops.DefaultVarianceThreshold
	}
	raw, ok := all["day_close_variance_threshold"]
	if !ok {
		return dayops.DefaultVarianceThreshold
	}
	var v float64
	if err := json.Unmarshal(raw, &v); err != nil || v < 0 {
		return dayops.DefaultVarianceThreshold
	}
	return v
}

// failDay maps a dayops error to its stable snake_case code. Anything
// unrecognised is logged and answered as internal_error — no driver or
// service string ever reaches a client (spec §6.9).
func failDay(c *gin.Context, err error, what string) {
	switch {
	case errors.Is(err, dayops.ErrDayAlreadyOpen):
		c.JSON(http.StatusConflict, models.Fail("A business day has already been started for today", "day_already_open"))
	case errors.Is(err, dayops.ErrPreviousDayOpen):
		c.JSON(http.StatusConflict, models.Fail("An earlier business day is still open — close it first", "previous_day_open"))
	case errors.Is(err, dayops.ErrDayNotOpen):
		c.JSON(http.StatusConflict, models.Fail("No business day is open — start the day first", "day_not_open"))
	case errors.Is(err, dayops.ErrDayNotFound):
		c.JSON(http.StatusNotFound, models.Fail("Business day not found", "day_not_found"))
	case errors.Is(err, dayops.ErrDayClosed):
		c.JSON(http.StatusConflict, models.Fail("That business day is already closed", "day_closed"))
	case errors.Is(err, dayops.ErrVarianceNoteRequired):
		c.JSON(http.StatusBadRequest, models.Fail("A tender is over the variance threshold — add a closing note explaining it", "variance_note_required"))
	case errors.Is(err, dayops.ErrInvalidPin):
		c.JSON(http.StatusUnauthorized, models.Fail("That PIN does not match an active admin", "invalid_pin"))
	case errors.Is(err, dayops.ErrReasonRequired):
		c.JSON(http.StatusBadRequest, models.Fail("A written reason of 4 to 500 characters is required", "reason_required"))
	case errors.Is(err, dayops.ErrInvalidMovement):
		c.JSON(http.StatusBadRequest, models.Fail("Check the movement type, amount and reason", "invalid_movement"))
	default:
		log.Printf("day %s: %v", what, err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not complete that day operation", "internal_error"))
	}
}

// Current returns the day the till is working against, with what should be in
// each tender and the drawer movements so far. Falls back to today's row when
// nothing is open, so a sealed day still renders as "closed" rather than
// disappearing from the screen.
func (h *DayHandler) Current(c *gin.Context) {
	day, err := dayops.Current(h.db)
	if err != nil {
		log.Printf("day current: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not load the business day", "internal_error"))
		return
	}
	if day == nil {
		day, err = dayops.Today(h.db)
		if err != nil {
			log.Printf("day current: today: %v", err)
			c.JSON(http.StatusInternalServerError, models.Fail("Could not load the business day", "internal_error"))
			return
		}
	}
	resp := models.CurrentDayResponse{Movements: []dayops.Movement{}}
	if day != nil {
		expected, err := dayops.ComputeExpected(h.db, day.ID)
		if err != nil {
			log.Printf("day current: expected: %v", err)
			c.JSON(http.StatusInternalServerError, models.Fail("Could not load the business day", "internal_error"))
			return
		}
		movements, err := dayops.ListMovements(h.db, day.ID)
		if err != nil {
			log.Printf("day current: movements: %v", err)
			c.JSON(http.StatusInternalServerError, models.Fail("Could not load the business day", "internal_error"))
			return
		}
		resp.Day = day
		resp.Expected = &expected
		resp.Movements = movements
	}
	c.JSON(http.StatusOK, models.OK("OK", resp))
}

// Open starts today's day against a declared cash float.
func (h *DayHandler) Open(c *gin.Context) {
	var req models.OpenDayRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.Fail("Invalid request body", "invalid_request"))
		return
	}
	if req.OpeningCash == nil || !validRate(*req.OpeningCash) {
		c.JSON(http.StatusBadRequest, models.Fail("Enter the cash in the drawer — zero is fine, but it has to be stated", "invalid_opening_cash"))
		return
	}
	day, err := dayops.Open(h.db, dayActor(c), *req.OpeningCash, optionalText(req.Notes))
	if err != nil {
		failDay(c, err, "open")
		return
	}
	c.JSON(http.StatusCreated, models.OK("Business day started", day))
}

// AddMovement records a paid-in or paid-out against the open day.
func (h *DayHandler) AddMovement(c *gin.Context) {
	var req models.CashMovementRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.Fail("Invalid request body", "invalid_request"))
		return
	}
	if req.Type != "paid_in" && req.Type != "paid_out" {
		c.JSON(http.StatusBadRequest, models.Fail("Type must be paid_in or paid_out", "invalid_movement"))
		return
	}
	if !validRate(req.Amount) || req.Amount <= 0 {
		c.JSON(http.StatusBadRequest, models.Fail("Amount must be more than zero, with at most 2 decimal places", "invalid_movement"))
		return
	}
	reason := strings.TrimSpace(req.Reason)
	if reason == "" || len(reason) > 200 {
		c.JSON(http.StatusBadRequest, models.Fail("A reason of 1 to 200 characters is required", "invalid_movement"))
		return
	}
	day, err := dayops.Current(h.db)
	if err != nil {
		failDay(c, err, "movement")
		return
	}
	if day == nil {
		failDay(c, dayops.ErrDayNotOpen, "movement")
		return
	}
	movement, err := dayops.AddMovement(h.db, dayActor(c), day.ID, req.Type, req.Amount, reason, optionalText(req.Notes))
	if err != nil {
		failDay(c, err, "movement")
		return
	}
	c.JSON(http.StatusCreated, models.OK("Cash movement recorded", movement))
}

// Close counts the open day and seals it.
func (h *DayHandler) Close(c *gin.Context) {
	var req models.CloseDayRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.Fail("Invalid request body", "invalid_request"))
		return
	}
	if req.CountedCash == nil || req.CountedCard == nil || req.CountedOnline == nil {
		c.JSON(http.StatusBadRequest, models.Fail("Count all three tenders — enter 0 for one that took nothing", "tender_count_required"))
		return
	}
	for _, v := range []float64{*req.CountedCash, *req.CountedCard, *req.CountedOnline} {
		if !validRate(v) {
			c.JSON(http.StatusBadRequest, models.Fail("Counted amounts must be zero or more, with at most 2 decimal places", "invalid_counted_amount"))
			return
		}
	}
	day, err := dayops.Current(h.db)
	if err != nil {
		failDay(c, err, "close")
		return
	}
	if day == nil {
		failDay(c, dayops.ErrDayNotOpen, "close")
		return
	}
	counted := dayops.Counted{Cash: *req.CountedCash, Card: *req.CountedCard, Online: *req.CountedOnline}
	closed, err := dayops.Close(h.db, dayActor(c), day.ID, counted, optionalText(req.ClosingNotes), varianceThreshold(h.db))
	if err != nil {
		failDay(c, err, "close")
		return
	}
	c.JSON(http.StatusOK, models.OK("Business day closed", closed))
}

// resolveDayID reads an optional day_id from a request body, falling back to
// the supplied default. A malformed id is reported as day_not_found rather
// than a parse error: from the caller's side it names no day.
func resolveDayID(raw *string, fallback *uuid.UUID) (uuid.UUID, error) {
	if raw != nil && strings.TrimSpace(*raw) != "" {
		id, err := uuid.Parse(strings.TrimSpace(*raw))
		if err != nil {
			return uuid.Nil, dayops.ErrDayNotFound
		}
		return id, nil
	}
	if fallback == nil {
		return uuid.Nil, dayops.ErrDayNotFound
	}
	return *fallback, nil
}

// Reopen unseals a day against an admin PIN. Without day_id it means today.
func (h *DayHandler) Reopen(c *gin.Context) {
	var req models.ReopenDayRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.Fail("Invalid request body", "invalid_request"))
		return
	}
	if strings.TrimSpace(req.Pin) == "" {
		c.JSON(http.StatusUnauthorized, models.Fail("That PIN does not match an active admin", "invalid_pin"))
		return
	}
	today, err := dayops.Today(h.db)
	if err != nil {
		failDay(c, err, "reopen")
		return
	}
	var fallback *uuid.UUID
	if today != nil {
		fallback = &today.ID
	}
	dayID, err := resolveDayID(req.DayID, fallback)
	if err != nil {
		failDay(c, err, "reopen")
		return
	}
	day, err := dayops.Reopen(h.db, dayActor(c), dayID, req.Pin)
	if err != nil {
		failDay(c, err, "reopen")
		return
	}
	c.JSON(http.StatusOK, models.OK("Business day reopened", day))
}

// ForceClose seals a day nobody counted. Without day_id it means whichever
// day is holding the open slot — normally a forgotten previous day.
func (h *DayHandler) ForceClose(c *gin.Context) {
	var req models.ForceCloseDayRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.Fail("Invalid request body", "invalid_request"))
		return
	}
	if strings.TrimSpace(req.Pin) == "" {
		c.JSON(http.StatusUnauthorized, models.Fail("That PIN does not match an active admin", "invalid_pin"))
		return
	}
	current, err := dayops.Current(h.db)
	if err != nil {
		failDay(c, err, "force close")
		return
	}
	var fallback *uuid.UUID
	if current != nil {
		fallback = &current.ID
	}
	if req.DayID == nil && current == nil {
		failDay(c, dayops.ErrDayNotOpen, "force close")
		return
	}
	dayID, err := resolveDayID(req.DayID, fallback)
	if err != nil {
		failDay(c, err, "force close")
		return
	}
	day, err := dayops.ForceClose(h.db, dayActor(c), dayID, req.Pin, req.Reason)
	if err != nil {
		failDay(c, err, "force close")
		return
	}
	c.JSON(http.StatusOK, models.OK("Business day force-closed", day))
}

// ZReport returns everything the printable Z slip needs for one day.
func (h *DayHandler) ZReport(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, models.Fail("Business day not found", "day_not_found"))
		return
	}
	z, err := dayops.ZData(h.db, id)
	if err != nil {
		failDay(c, err, "z report")
		return
	}
	c.JSON(http.StatusOK, models.OK("OK", z))
}

// History lists the most recently closed days with their variances.
func (h *DayHandler) History(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "30"))
	days, err := dayops.History(h.db, limit)
	if err != nil {
		failDay(c, err, "history")
		return
	}
	c.JSON(http.StatusOK, models.OK("OK", days))
}
