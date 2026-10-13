package dayops

import (
	"database/sql"
	"errors"
	"testing"
	"time"

	"elevon-backend/internal/testdb"
)

// Spec §6.7 addendum (2026-10-13): with settings.day_close_enabled = false
// the invoice path opens the day itself. The owner has said the drawer is
// never counted, so there is no float to invent and no cash figure to
// falsify — the only thing the old rule did for this store was block the
// till. With the switch on (the seeded default) nothing changes.

func setDayClose(t *testing.T, db *sql.DB, enabled bool) {
	t.Helper()
	v := "false"
	if enabled {
		v = "true"
	}
	if _, err := db.Exec(`INSERT INTO settings (key, value) VALUES ('day_close_enabled', $1::jsonb)
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`, v); err != nil {
		t.Fatal(err)
	}
}

func TestEnsureOpenDayForInvoice_DayCloseOff_OpensTodayItself(t *testing.T) {
	db := testdb.Fresh(t)
	actor, actorID := admin(t, db, "1234")
	setDayClose(t, db, false)

	day, err := ensureInTx(t, db, actor, time.Now())
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if day.DateKey() != todayKey() || day.Status != StatusOpen {
		t.Fatalf("today should now be open: %s %s", day.DateKey(), day.Status)
	}
	if day.OpeningCash != 0 {
		t.Fatalf("an automatic open declares no float: %v", day.OpeningCash)
	}
	if day.OpenedBy == nil || *day.OpenedBy != actorID {
		t.Fatalf("opened_by should be the cashier whose sale opened it: %v", day.OpenedBy)
	}
	if n := countRows(t, db, "business_days"); n != 1 {
		t.Fatalf("rows: %d", n)
	}
	if got := auditActions(t, db); len(got) != 1 || got[0] != "auto_open" {
		t.Fatalf("audit trail: %v", got)
	}

	// A second sale the same day finds the row that was just opened.
	again, err := ensureInTx(t, db, actor, time.Now())
	if err != nil || again.ID != day.ID {
		t.Fatalf("second sale: %v %s vs %s", err, again.ID, day.ID)
	}
	if n := countRows(t, db, "business_days"); n != 1 {
		t.Fatalf("rows after second sale: %d", n)
	}
}

func TestEnsureOpenDayForInvoice_DayCloseOff_SealsAStaleDayFirst(t *testing.T) {
	db := testdb.Fresh(t)
	actor, _ := admin(t, db, "1234")
	setDayClose(t, db, false)
	staleID := seedDay(t, db, daysAgoKey(1), StatusOpen, 500)

	day, err := ensureInTx(t, db, actor, time.Now())
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if day.DateKey() != todayKey() || day.Status != StatusOpen {
		t.Fatalf("today should now be open: %s %s", day.DateKey(), day.Status)
	}

	stale, err := Get(db, staleID)
	if err != nil {
		t.Fatal(err)
	}
	if stale.Status != StatusClosed || stale.ClosedAt == nil {
		t.Fatalf("the stale day must be sealed: %+v", stale.Status)
	}
	// Sealed like a force close: counted = expected, so the day shows no
	// variance it was never counted for. 500 float, no sales.
	if stale.CountedCash == nil || !near(*stale.CountedCash, 500) || stale.CashVariance == nil || *stale.CashVariance != 0 {
		t.Fatalf("auto seal must copy expected into counted: counted %v variance %v", stale.CountedCash, stale.CashVariance)
	}
	if stale.ClosingNotes == nil || *stale.ClosingNotes == "" {
		t.Fatalf("auto seal must say it was automatic")
	}
	if n := countRows(t, db, "business_days"); n != 2 {
		t.Fatalf("rows: %d", n)
	}
	if got := auditActions(t, db); len(got) != 2 || got[0] != "auto_close" || got[1] != "auto_open" {
		t.Fatalf("audit trail: %v", got)
	}
}

func TestEnsureOpenDayForInvoice_DayCloseOn_KeepsBlocking(t *testing.T) {
	db := testdb.Fresh(t)
	actor, _ := admin(t, db, "1234")
	setDayClose(t, db, true)

	if _, err := ensureInTx(t, db, actor, time.Now()); !errors.Is(err, ErrDayNotOpen) {
		t.Fatalf("want ErrDayNotOpen, got %v", err)
	}
	seedDay(t, db, daysAgoKey(1), StatusOpen, 1000)
	if _, err := ensureInTx(t, db, actor, time.Now()); !errors.Is(err, ErrPreviousDayOpen) {
		t.Fatalf("want ErrPreviousDayOpen, got %v", err)
	}
	if n := countRows(t, db, "business_days"); n != 1 {
		t.Fatalf("the switch on must never create or seal a day: %d rows", n)
	}
}
