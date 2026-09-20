package handlers

import (
	"database/sql"
	"net/http"
	"testing"

	"elevon-backend/internal/dayops"
	"elevon-backend/internal/models"
	"elevon-backend/internal/testdb"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

// dayRouter mounts the same paths routes.go does, so a route conflict between
// /day/current and /day/:id/z would show up here rather than at boot.
func dayRouter(h *DayHandler, a actor) *gin.Engine {
	r := gin.New()
	staff := r.Group("", asActor(a))
	staff.GET("/day/current", h.Current)
	staff.POST("/day/open", h.Open)
	staff.POST("/day/movements", h.AddMovement)
	staff.POST("/day/close", h.Close)
	staff.GET("/day/:id/z", h.ZReport)
	admin := r.Group("/admin", asActor(a))
	admin.POST("/day/reopen", h.Reopen)
	admin.POST("/day/force-close", h.ForceClose)
	admin.GET("/day/history", h.History)
	return r
}

func setPin(t *testing.T, db *sql.DB, userID uuid.UUID, pin string) {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(pin), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE users SET pin_hash = $1 WHERE id = $2`, string(hash), userID); err != nil {
		t.Fatal(err)
	}
}

func money(v float64) *float64 { return &v }

func TestDay_OpenCurrentMovementsAndClose(t *testing.T) {
	db := testdb.Fresh(t)
	ownerID := seedUser(t, db, "owner", "admin", "owner-pass-1", nil)
	r := dayRouter(NewDayHandler(db), actor{id: ownerID, username: "owner", role: "admin"})

	// Nothing open yet.
	w := doJSON(r, http.MethodGet, "/day/current", nil)
	var current models.CurrentDayResponse
	dataAs(t, decodeEnvelope(t, w), &current)
	if w.Code != http.StatusOK || current.Day != nil || current.Expected != nil || len(current.Movements) != 0 {
		t.Fatalf("no day yet: %d %s", w.Code, w.Body.String())
	}

	// A movement before the day is open is refused.
	w = doJSON(r, http.MethodPost, "/day/movements", models.CashMovementRequest{Type: "paid_in", Amount: 100, Reason: "float"})
	if w.Code != http.StatusConflict || errCode(decodeEnvelope(t, w)) != "day_not_open" {
		t.Fatalf("movement before open: %d %s", w.Code, w.Body.String())
	}
	// So is a close.
	w = doJSON(r, http.MethodPost, "/day/close", models.CloseDayRequest{CountedCash: money(0), CountedCard: money(0), CountedOnline: money(0)})
	if w.Code != http.StatusConflict || errCode(decodeEnvelope(t, w)) != "day_not_open" {
		t.Fatalf("close before open: %d %s", w.Code, w.Body.String())
	}

	// The opening float has to be stated, not defaulted.
	w = doJSON(r, http.MethodPost, "/day/open", map[string]any{"notes": "no float given"})
	if w.Code != http.StatusBadRequest || errCode(decodeEnvelope(t, w)) != "invalid_opening_cash" {
		t.Fatalf("open without opening_cash: %d %s", w.Code, w.Body.String())
	}

	w = doJSON(r, http.MethodPost, "/day/open", models.OpenDayRequest{OpeningCash: money(1000), Notes: " counted at 8am "})
	if w.Code != http.StatusCreated {
		t.Fatalf("open: %d %s", w.Code, w.Body.String())
	}
	var day dayops.Day
	dataAs(t, decodeEnvelope(t, w), &day)
	if day.Status != dayops.StatusOpen || day.OpeningCash != 1000 || day.OpeningNotes == nil || *day.OpeningNotes != "counted at 8am" {
		t.Fatalf("opened day: %+v", day)
	}

	// Second open is a conflict, with the code the till's banner branches on.
	w = doJSON(r, http.MethodPost, "/day/open", models.OpenDayRequest{OpeningCash: money(50)})
	if w.Code != http.StatusConflict || errCode(decodeEnvelope(t, w)) != "day_already_open" {
		t.Fatalf("second open: %d %s", w.Code, w.Body.String())
	}

	w = doJSON(r, http.MethodPost, "/day/movements", models.CashMovementRequest{Type: "paid_in", Amount: 200, Reason: "change float top-up"})
	if w.Code != http.StatusCreated {
		t.Fatalf("paid_in: %d %s", w.Code, w.Body.String())
	}
	for _, bad := range []models.CashMovementRequest{
		{Type: "transfer", Amount: 100, Reason: "nope"},
		{Type: "paid_out", Amount: 0, Reason: "nothing"},
		{Type: "paid_out", Amount: 100, Reason: "  "},
	} {
		w := doJSON(r, http.MethodPost, "/day/movements", bad)
		if w.Code != http.StatusBadRequest || errCode(decodeEnvelope(t, w)) != "invalid_movement" {
			t.Errorf("%+v: %d %s", bad, w.Code, w.Body.String())
		}
	}

	w = doJSON(r, http.MethodGet, "/day/current", nil)
	dataAs(t, decodeEnvelope(t, w), &current)
	if current.Day == nil || current.Day.ID != day.ID || current.Expected == nil || current.Expected.Cash != 1200 || len(current.Movements) != 1 {
		t.Fatalf("current: %s", w.Body.String())
	}

	// All three tenders are required.
	w = doJSON(r, http.MethodPost, "/day/close", models.CloseDayRequest{CountedCash: money(1200), CountedCard: money(0)})
	if w.Code != http.StatusBadRequest || errCode(decodeEnvelope(t, w)) != "tender_count_required" {
		t.Fatalf("partial count: %d %s", w.Code, w.Body.String())
	}

	// 700 short against a threshold of 100, with nothing to explain it.
	w = doJSON(r, http.MethodPost, "/day/close", models.CloseDayRequest{
		CountedCash: money(500), CountedCard: money(0), CountedOnline: money(0)})
	if w.Code != http.StatusBadRequest || errCode(decodeEnvelope(t, w)) != "variance_note_required" {
		t.Fatalf("over-threshold close: %d %s", w.Code, w.Body.String())
	}

	w = doJSON(r, http.MethodPost, "/day/close", models.CloseDayRequest{
		CountedCash: money(500), CountedCard: money(0), CountedOnline: money(0),
		ClosingNotes: "Rs 700 taken to the bank, slip in the drawer"})
	if w.Code != http.StatusOK {
		t.Fatalf("close: %d %s", w.Code, w.Body.String())
	}
	var closed dayops.Day
	dataAs(t, decodeEnvelope(t, w), &closed)
	if closed.Status != dayops.StatusClosed || closed.CashVariance == nil || *closed.CashVariance != -700 {
		t.Fatalf("closed: %+v", closed)
	}

	// A closed day still renders on the screen, as closed.
	w = doJSON(r, http.MethodGet, "/day/current", nil)
	dataAs(t, decodeEnvelope(t, w), &current)
	if current.Day == nil || current.Day.Status != dayops.StatusClosed {
		t.Fatalf("current after close: %s", w.Body.String())
	}

	// Z-report for the sealed day.
	w = doJSON(r, http.MethodGet, "/day/"+day.ID.String()+"/z", nil)
	var z dayops.ZReport
	dataAs(t, decodeEnvelope(t, w), &z)
	if w.Code != http.StatusOK || z.Day.ID != day.ID || len(z.Movements) != 1 || z.Expected.Cash != 1200 {
		t.Fatalf("z: %d %s", w.Code, w.Body.String())
	}
	w = doJSON(r, http.MethodGet, "/day/not-a-uuid/z", nil)
	if w.Code != http.StatusNotFound || errCode(decodeEnvelope(t, w)) != "day_not_found" {
		t.Fatalf("z for a bad id: %d %s", w.Code, w.Body.String())
	}

	// History lists it.
	w = doJSON(r, http.MethodGet, "/admin/day/history?limit=30", nil)
	var history []dayops.Day
	dataAs(t, decodeEnvelope(t, w), &history)
	if w.Code != http.StatusOK || len(history) != 1 || history[0].ID != day.ID {
		t.Fatalf("history: %d %s", w.Code, w.Body.String())
	}
}

func TestDay_ReopenAndForceCloseNeedAnAdminPin(t *testing.T) {
	db := testdb.Fresh(t)
	ownerID := seedUser(t, db, "owner", "admin", "owner-pass-1", nil)
	setPin(t, db, ownerID, "1234")
	r := dayRouter(NewDayHandler(db), actor{id: ownerID, username: "owner", role: "admin"})

	w := doJSON(r, http.MethodPost, "/day/open", models.OpenDayRequest{OpeningCash: money(1000)})
	if w.Code != http.StatusCreated {
		t.Fatalf("open: %d %s", w.Code, w.Body.String())
	}
	var day dayops.Day
	dataAs(t, decodeEnvelope(t, w), &day)

	// Force close needs a reason and a real PIN.
	w = doJSON(r, http.MethodPost, "/admin/day/force-close", models.ForceCloseDayRequest{Pin: "1234", Reason: "x"})
	if w.Code != http.StatusBadRequest || errCode(decodeEnvelope(t, w)) != "reason_required" {
		t.Fatalf("force close without a reason: %d %s", w.Code, w.Body.String())
	}
	w = doJSON(r, http.MethodPost, "/admin/day/force-close", models.ForceCloseDayRequest{Pin: "0000", Reason: "nobody counted the drawer"})
	if w.Code != http.StatusUnauthorized || errCode(decodeEnvelope(t, w)) != "invalid_pin" {
		t.Fatalf("force close with a wrong PIN: %d %s", w.Code, w.Body.String())
	}

	// Close it properly, then reopen.
	w = doJSON(r, http.MethodPost, "/day/close", models.CloseDayRequest{
		CountedCash: money(1000), CountedCard: money(0), CountedOnline: money(0)})
	if w.Code != http.StatusOK {
		t.Fatalf("close: %d %s", w.Code, w.Body.String())
	}

	w = doJSON(r, http.MethodPost, "/admin/day/reopen", models.ReopenDayRequest{Pin: "9999"})
	if w.Code != http.StatusUnauthorized || errCode(decodeEnvelope(t, w)) != "invalid_pin" {
		t.Fatalf("reopen with a wrong PIN: %d %s", w.Code, w.Body.String())
	}
	w = doJSON(r, http.MethodPost, "/admin/day/reopen", models.ReopenDayRequest{Pin: ""})
	if w.Code != http.StatusUnauthorized || errCode(decodeEnvelope(t, w)) != "invalid_pin" {
		t.Fatalf("reopen with no PIN: %d %s", w.Code, w.Body.String())
	}

	w = doJSON(r, http.MethodPost, "/admin/day/reopen", models.ReopenDayRequest{Pin: "1234"})
	if w.Code != http.StatusOK {
		t.Fatalf("reopen: %d %s", w.Code, w.Body.String())
	}
	var reopened dayops.Day
	dataAs(t, decodeEnvelope(t, w), &reopened)
	if reopened.ID != day.ID || reopened.Status != dayops.StatusReopened {
		t.Fatalf("reopened: %+v", reopened)
	}

	// Now force-close the reopened day: counted matches expected exactly and
	// the note says plainly that nobody counted.
	w = doJSON(r, http.MethodPost, "/admin/day/force-close", models.ForceCloseDayRequest{
		Pin: "1234", Reason: "shop shut before anyone counted the drawer"})
	if w.Code != http.StatusOK {
		t.Fatalf("force close: %d %s", w.Code, w.Body.String())
	}
	var forced dayops.Day
	dataAs(t, decodeEnvelope(t, w), &forced)
	if forced.Status != dayops.StatusClosed || forced.CashVariance == nil || *forced.CashVariance != 0 ||
		forced.CountedCash == nil || *forced.CountedCash != 1000 || forced.ClosingNotes == nil {
		t.Fatalf("force closed: %+v", forced)
	}

	// Nothing is open now, so a force close with no day_id has no target.
	w = doJSON(r, http.MethodPost, "/admin/day/force-close", models.ForceCloseDayRequest{
		Pin: "1234", Reason: "nothing left to close"})
	if w.Code != http.StatusConflict || errCode(decodeEnvelope(t, w)) != "day_not_open" {
		t.Fatalf("force close with nothing open: %d %s", w.Code, w.Body.String())
	}
}
