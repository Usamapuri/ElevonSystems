package dayops

import (
	"database/sql"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"elevon-backend/internal/testdb"
	"elevon-backend/internal/util"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

// ── fixtures ─────────────────────────────────────────────────────────────

func todayKey() string { return util.BusinessDate(time.Now()).Format(DateLayout) }

func daysAgoKey(n int) string {
	return util.BusinessDate(time.Now()).AddDate(0, 0, -n).Format(DateLayout)
}

func seedUser(t *testing.T, db *sql.DB, username, role, pin string) uuid.UUID {
	t.Helper()
	var pinHash *string
	if pin != "" {
		h, err := bcrypt.GenerateFromPassword([]byte(pin), bcrypt.MinCost)
		if err != nil {
			t.Fatal(err)
		}
		s := string(h)
		pinHash = &s
	}
	var id uuid.UUID
	if err := db.QueryRow(`INSERT INTO users (username, password_hash, first_name, last_name, role, pin_hash, is_active)
		VALUES ($1, 'x', 'Test', $1, $2, $3, true) RETURNING id`, username, role, pinHash).Scan(&id); err != nil {
		t.Fatalf("seed user %s: %v", username, err)
	}
	return id
}

func admin(t *testing.T, db *sql.DB, pin string) (Actor, uuid.UUID) {
	t.Helper()
	id := seedUser(t, db, "owner", "admin", pin)
	return Actor{ID: id, Name: "owner", Role: "admin"}, id
}

// seedDay inserts a business_days row directly, for the states a test cannot
// reach through Open (yesterday still open, today already closed).
func seedDay(t *testing.T, db *sql.DB, dateKey, status string, openingCash float64) uuid.UUID {
	t.Helper()
	var closedAt any
	if status == StatusClosed {
		closedAt = time.Now()
	}
	var id uuid.UUID
	if err := db.QueryRow(`INSERT INTO business_days (business_date, status, opening_cash, closed_at)
		VALUES ($1::date, $2, $3, $4) RETURNING id`, dateKey, status, openingCash, closedAt).Scan(&id); err != nil {
		t.Fatalf("seed day %s/%s: %v", dateKey, status, err)
	}
	return id
}

func seedCustomer(t *testing.T, db *sql.DB, name string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := db.QueryRow(`INSERT INTO customers (name, credit_allowed) VALUES ($1, true) RETURNING id`, name).Scan(&id); err != nil {
		t.Fatalf("seed customer: %v", err)
	}
	return id
}

// seedInvoice writes an invoice row directly — the invoice API is Task D6, so
// until then the day-close arithmetic is exercised against hand-built rows.
func seedInvoice(t *testing.T, db *sql.DB, dayID uuid.UUID, dateKey, number, method, status string,
	subtotal, discount, tax, payable float64, customerID *uuid.UUID) {
	t.Helper()
	if _, err := db.Exec(`
		INSERT INTO invoices (invoice_number, business_day_id, business_date, status, customer_id,
			subtotal, discount_amount, tax_rate, tax_amount, further_tax_amount, total_amount, total_payable, payment_method)
		VALUES ($1, $2, $3::date, $4, $5, $6, $7, 0.18, $8, 0, $9, $10, $11)`,
		number, dayID, dateKey, status, customerID,
		subtotal, discount, tax, subtotal-discount+tax, payable, method); err != nil {
		t.Fatalf("seed invoice %s: %v", number, err)
	}
}

func seedReceipt(t *testing.T, db *sql.DB, dayID uuid.UUID, dateKey, number, method string,
	amount float64, customerID uuid.UUID, voided bool) {
	t.Helper()
	var voidedAt any
	if voided {
		voidedAt = time.Now()
	}
	if _, err := db.Exec(`
		INSERT INTO customer_receipts (receipt_number, customer_id, amount, method, business_day_id, business_date, voided_at)
		VALUES ($1, $2, $3, $4, $5, $6::date, $7)`,
		number, customerID, amount, method, dayID, dateKey, voidedAt); err != nil {
		t.Fatalf("seed receipt %s: %v", number, err)
	}
}

func countRows(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

func auditActions(t *testing.T, db *sql.DB) []string {
	t.Helper()
	rows, err := db.Query(`SELECT action FROM day_close_audit_log ORDER BY created_at, action`)
	if err != nil {
		t.Fatalf("audit: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var a string
		if err := rows.Scan(&a); err != nil {
			t.Fatal(err)
		}
		out = append(out, a)
	}
	return out
}

func ensureInTx(t *testing.T, db *sql.DB, actor Actor, now time.Time) (Day, error) {
	t.Helper()
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	day, ensureErr := EnsureOpenDayForInvoice(tx, actor, now)
	if ensureErr != nil {
		if err := tx.Rollback(); err != nil {
			t.Fatalf("rollback: %v", err)
		}
		return Day{}, ensureErr
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	return day, nil
}

func near(a, b float64) bool { return math.Abs(a-b) < 0.005 }

// ── tests ────────────────────────────────────────────────────────────────

func TestOpen_CurrentAndSecondOpen(t *testing.T) {
	db := testdb.Fresh(t)
	actor, actorID := admin(t, db, "1234")

	if cur, err := Current(db); err != nil || cur != nil {
		t.Fatalf("no day yet: %+v %v", cur, err)
	}

	notes := "float counted by the owner"
	day, err := Open(db, actor, 5000, &notes)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if day.Status != StatusOpen || day.DateKey() != todayKey() || !near(day.OpeningCash, 5000) {
		t.Fatalf("opened day: %+v", day)
	}
	if day.OpenedBy == nil || *day.OpenedBy != actorID || day.OpeningNotes == nil || *day.OpeningNotes != notes {
		t.Fatalf("opened by/notes: %+v", day)
	}
	if day.CountedCash != nil || day.ExpectedCash != nil || day.CashVariance != nil {
		t.Fatalf("a fresh day carries no counts: %+v", day)
	}

	cur, err := Current(db)
	if err != nil || cur == nil || cur.ID != day.ID {
		t.Fatalf("current: %+v %v", cur, err)
	}

	// A second open of the same date is refused, not silently ignored.
	if _, err := Open(db, actor, 100, nil); !errors.Is(err, ErrDayAlreadyOpen) {
		t.Fatalf("second open: want ErrDayAlreadyOpen, got %v", err)
	}
	if n := countRows(t, db, "business_days"); n != 1 {
		t.Fatalf("business_days rows after a refused second open: %d", n)
	}
	if got := auditActions(t, db); len(got) != 1 || got[0] != "open" {
		t.Fatalf("audit trail: %v", got)
	}
}

func TestOpen_RefusesWhileAnEarlierDayIsOpen(t *testing.T) {
	db := testdb.Fresh(t)
	actor, _ := admin(t, db, "1234")
	seedDay(t, db, daysAgoKey(1), StatusOpen, 1000)

	if _, err := Open(db, actor, 5000, nil); !errors.Is(err, ErrPreviousDayOpen) {
		t.Fatalf("open with yesterday still open: want ErrPreviousDayOpen, got %v", err)
	}
	if n := countRows(t, db, "business_days"); n != 1 {
		t.Fatalf("business_days rows: %d", n)
	}
}

func TestOpen_RefusesReopeningAClosedTodayThroughOpen(t *testing.T) {
	db := testdb.Fresh(t)
	actor, _ := admin(t, db, "1234")
	seedDay(t, db, todayKey(), StatusClosed, 1000)

	// A day that was sealed earlier today is reopened with an admin PIN, never
	// opened a second time — a second open would restate the float.
	if _, err := Open(db, actor, 5000, nil); !errors.Is(err, ErrDayAlreadyOpen) {
		t.Fatalf("open over a closed today: want ErrDayAlreadyOpen, got %v", err)
	}
}

func TestEnsureOpenDayForInvoice_UsesTheOpenDay(t *testing.T) {
	db := testdb.Fresh(t)
	actor, _ := admin(t, db, "1234")
	opened, err := Open(db, actor, 2000, nil)
	if err != nil {
		t.Fatal(err)
	}

	day, err := ensureInTx(t, db, actor, time.Now())
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if day.ID != opened.ID || day.Status != StatusOpen {
		t.Fatalf("ensure returned %+v, want the open day %s", day, opened.ID)
	}
	if n := countRows(t, db, "business_days"); n != 1 {
		t.Fatalf("ensure must not create a row: %d", n)
	}
}

func TestEnsureOpenDayForInvoice_PreviousDayOpenBlocks(t *testing.T) {
	db := testdb.Fresh(t)
	actor, _ := admin(t, db, "1234")
	seedDay(t, db, daysAgoKey(1), StatusOpen, 1000)

	if _, err := ensureInTx(t, db, actor, time.Now()); !errors.Is(err, ErrPreviousDayOpen) {
		t.Fatalf("want ErrPreviousDayOpen, got %v", err)
	}
	if n := countRows(t, db, "business_days"); n != 1 {
		t.Fatalf("blocked ensure must not create a row: %d", n)
	}
}

func TestEnsureOpenDayForInvoice_ReopensTodayForALateSale(t *testing.T) {
	db := testdb.Fresh(t)
	actor, _ := admin(t, db, "1234")
	closedID := seedDay(t, db, todayKey(), StatusClosed, 1500)
	// The count taken at close must survive the reopen.
	if _, err := db.Exec(`UPDATE business_days SET counted_cash = 1500, expected_cash = 1500, cash_variance = 0 WHERE id = $1`, closedID); err != nil {
		t.Fatal(err)
	}

	day, err := ensureInTx(t, db, actor, time.Now())
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if day.ID != closedID {
		t.Fatalf("a late sale must reattach to the same row: got %s want %s", day.ID, closedID)
	}
	if day.Status != StatusReopened {
		t.Fatalf("status: %s", day.Status)
	}
	if day.ClosedAt != nil {
		t.Fatalf("closed_at must be cleared on reopen: %+v", day.ClosedAt)
	}
	if day.CountedCash == nil || !near(*day.CountedCash, 1500) {
		t.Fatalf("the count taken at close must survive: %+v", day.CountedCash)
	}
	if n := countRows(t, db, "business_days"); n != 1 {
		t.Fatalf("reopen is an UPDATE, not an INSERT: %d rows", n)
	}
	if got := auditActions(t, db); len(got) != 1 || got[0] != "reopen_for_sale" {
		t.Fatalf("audit trail: %v", got)
	}
}

func TestEnsureOpenDayForInvoice_NoDayMeansNoSaleAndNoRow(t *testing.T) {
	db := testdb.Fresh(t)
	actor, _ := admin(t, db, "1234")

	if _, err := ensureInTx(t, db, actor, time.Now()); !errors.Is(err, ErrDayNotOpen) {
		t.Fatalf("want ErrDayNotOpen, got %v", err)
	}
	// §6.7: the system never opens the day by itself.
	if n := countRows(t, db, "business_days"); n != 0 {
		t.Fatalf("no business day may be created: %d rows", n)
	}
	if n := countRows(t, db, "day_close_audit_log"); n != 0 {
		t.Fatalf("nothing happened, so nothing to audit: %d rows", n)
	}
}

func TestExpected_AndCloseSnapshotsTheDay(t *testing.T) {
	db := testdb.Fresh(t)
	actor, actorID := admin(t, db, "1234")
	day, err := Open(db, actor, 5000, nil)
	if err != nil {
		t.Fatal(err)
	}
	customer := seedCustomer(t, db, "Ali Traders")
	date := day.DateKey()

	seedInvoice(t, db, day.ID, date, "20260920-001", "cash", "completed", 900, 0, 100, 1000, nil)
	seedInvoice(t, db, day.ID, date, "20260920-002", "card", "completed", 1800, 100, 300, 2000, nil)
	seedInvoice(t, db, day.ID, date, "20260920-003", "online", "completed", 450, 0, 50, 500, nil)
	seedInvoice(t, db, day.ID, date, "20260920-004", "credit", "completed", 2700, 0, 300, 3000, &customer)
	// Voided: excluded from every figure but counted as a void.
	seedInvoice(t, db, day.ID, date, "20260920-005", "cash", "voided", 400, 0, 0, 400, nil)

	seedReceipt(t, db, day.ID, date, "R-20260920-001", "cash", 250, customer, false)
	seedReceipt(t, db, day.ID, date, "R-20260920-002", "card", 100, customer, false)
	seedReceipt(t, db, day.ID, date, "R-20260920-003", "cash", 999, customer, true) // voided

	if _, err := AddMovement(db, actor, day.ID, "paid_in", 300, "change float top-up", nil); err != nil {
		t.Fatalf("paid_in: %v", err)
	}
	if _, err := AddMovement(db, actor, day.ID, "paid_out", 200, "cylinder delivery fuel", nil); err != nil {
		t.Fatalf("paid_out: %v", err)
	}

	exp, err := ComputeExpected(db, day.ID)
	if err != nil {
		t.Fatalf("expected: %v", err)
	}
	// cash = 5000 opening + 1000 sales + 250 receipts + 300 in − 200 out
	if !near(exp.Cash, 6350) || !near(exp.Card, 2100) || !near(exp.Online, 500) {
		t.Fatalf("tender expectations: cash %.2f card %.2f online %.2f", exp.Cash, exp.Card, exp.Online)
	}
	if !near(exp.OnAccountSales, 3000) {
		t.Fatalf("credit sales are on account, never a tender: %.2f", exp.OnAccountSales)
	}
	if !near(exp.ReceiptsCollected, 350) {
		t.Fatalf("voided receipts must be excluded: %.2f", exp.ReceiptsCollected)
	}
	if !near(exp.GrossSales, 5850) || !near(exp.Discounts, 100) || !near(exp.TaxCollected, 750) || !near(exp.NetSales, 6500) {
		t.Fatalf("sales summary: %+v", exp)
	}
	if exp.InvoiceCount != 4 || exp.VoidCount != 1 {
		t.Fatalf("counts: %d invoices, %d voids", exp.InvoiceCount, exp.VoidCount)
	}

	// 500 short on cash with a 100 threshold and nothing to explain it.
	short := Counted{Cash: exp.Cash - 500, Card: exp.Card, Online: exp.Online}
	if _, err := Close(db, actor, day.ID, short, nil, 100); !errors.Is(err, ErrVarianceNoteRequired) {
		t.Fatalf("want ErrVarianceNoteRequired, got %v", err)
	}
	blank := "   "
	if _, err := Close(db, actor, day.ID, short, &blank, 100); !errors.Is(err, ErrVarianceNoteRequired) {
		t.Fatalf("whitespace is not an explanation: %v", err)
	}
	if still, err := Get(db, day.ID); err != nil || still.Status != StatusOpen {
		t.Fatalf("a refused close must leave the day open: %+v %v", still, err)
	}

	note := "Rs 500 short — counted twice, giving it to the owner in the morning"
	closed, err := Close(db, actor, day.ID, short, &note, 100)
	if err != nil {
		t.Fatalf("close: %v", err)
	}
	if closed.Status != StatusClosed || closed.ClosedAt == nil || closed.ClosedBy == nil || *closed.ClosedBy != actorID {
		t.Fatalf("closed day: %+v", closed)
	}
	if closed.CashVariance == nil || !near(*closed.CashVariance, -500) {
		t.Fatalf("cash variance: %+v", closed.CashVariance)
	}
	if closed.CardVariance == nil || !near(*closed.CardVariance, 0) ||
		closed.OnlineVariance == nil || !near(*closed.OnlineVariance, 0) {
		t.Fatalf("card/online variance: %+v %+v", closed.CardVariance, closed.OnlineVariance)
	}
	if closed.ExpectedCash == nil || !near(*closed.ExpectedCash, 6350) ||
		closed.CountedCash == nil || !near(*closed.CountedCash, 5850) {
		t.Fatalf("counted/expected snapshot: %+v", closed)
	}
	if closed.OnAccountSales == nil || !near(*closed.OnAccountSales, 3000) ||
		closed.ReceiptsCollected == nil || !near(*closed.ReceiptsCollected, 350) ||
		closed.InvoiceCount == nil || *closed.InvoiceCount != 4 ||
		closed.VoidCount == nil || *closed.VoidCount != 1 {
		t.Fatalf("summary snapshot: %+v", closed)
	}
	if closed.ClosingNotes == nil || *closed.ClosingNotes != note {
		t.Fatalf("closing note: %+v", closed.ClosingNotes)
	}

	// Closing twice is refused.
	if _, err := Close(db, actor, day.ID, short, &note, 100); !errors.Is(err, ErrDayClosed) {
		t.Fatalf("second close: want ErrDayClosed, got %v", err)
	}

	// Z data carries the day, the live expectation and the movements.
	z, err := ZData(db, day.ID)
	if err != nil {
		t.Fatalf("z: %v", err)
	}
	if z.Day.ID != day.ID || len(z.Movements) != 2 || !near(z.Expected.Cash, 6350) || z.Expected.InvoiceCount != 4 {
		t.Fatalf("z report: %+v", z)
	}

	// History lists the closed day.
	hist, err := History(db, 30)
	if err != nil || len(hist) != 1 || hist[0].ID != day.ID {
		t.Fatalf("history: %+v %v", hist, err)
	}
}

func TestClose_WithinThresholdNeedsNoNote(t *testing.T) {
	db := testdb.Fresh(t)
	actor, _ := admin(t, db, "1234")
	day, err := Open(db, actor, 1000, nil)
	if err != nil {
		t.Fatal(err)
	}
	// 100 threshold, 40 short: inside the tolerance, so no explanation needed.
	closed, err := Close(db, actor, day.ID, Counted{Cash: 960, Card: 0, Online: 0}, nil, 100)
	if err != nil {
		t.Fatalf("close: %v", err)
	}
	if closed.Status != StatusClosed || closed.CashVariance == nil || !near(*closed.CashVariance, -40) {
		t.Fatalf("closed: %+v", closed)
	}
	if closed.ClosingNotes != nil {
		t.Fatalf("no note was given: %+v", closed.ClosingNotes)
	}
	if got := auditActions(t, db); len(got) != 2 || got[0] != "open" || got[1] != "close" {
		t.Fatalf("audit trail: %v", got)
	}
}

// Two tills (or one impatient person double-tapping Close) must not both
// seal the same day: the second would overwrite the first's counted figures
// and leave two 'close' rows in an append-only log that is supposed to record
// exactly what happened. Close resolves, computes and writes inside one
// transaction holding a row lock, so the loser waits and then finds the day
// already sealed.
func TestClose_IsSerialisedAndSecondCloseIsRefused(t *testing.T) {
	db := testdb.Fresh(t)
	actor, _ := admin(t, db, "1234")
	day, err := Open(db, actor, 1000, nil)
	if err != nil {
		t.Fatal(err)
	}

	type outcome struct {
		counted float64
		err     error
	}
	// Both counts are inside the threshold of the expected 1000, and both
	// carry a note, so nothing but the lock can separate them.
	note := "counted at the same moment from two devices"
	counts := []float64{1000, 950}
	results := make(chan outcome, len(counts))
	start := make(chan struct{})
	for _, counted := range counts {
		go func(c float64) {
			<-start
			_, err := Close(db, actor, day.ID, Counted{Cash: c}, &note, 100)
			results <- outcome{counted: c, err: err}
		}(counted)
	}
	close(start)

	var sealed, refused int
	var winner float64
	for range counts {
		r := <-results
		switch {
		case r.err == nil:
			sealed++
			winner = r.counted
		case errors.Is(r.err, ErrDayClosed):
			refused++
		default:
			t.Fatalf("counted %.2f: unexpected error %v", r.counted, r.err)
		}
	}
	if sealed != 1 || refused != 1 {
		t.Fatalf("exactly one close must win: %d sealed, %d refused", sealed, refused)
	}

	var closeRows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM day_close_audit_log WHERE action = 'close'`).Scan(&closeRows); err != nil {
		t.Fatal(err)
	}
	if closeRows != 1 {
		t.Fatalf("one close audit row expected, got %d", closeRows)
	}

	final, err := Get(db, day.ID)
	if err != nil {
		t.Fatal(err)
	}
	if final.Status != StatusClosed {
		t.Fatalf("status: %s", final.Status)
	}
	if final.CountedCash == nil || !near(*final.CountedCash, winner) {
		t.Fatalf("the sealed row must carry the winner's count (%.2f): %+v", winner, final.CountedCash)
	}
	if final.CashVariance == nil || !near(*final.CashVariance, winner-1000) {
		t.Fatalf("variance must match the winner's count: %+v", final.CashVariance)
	}
}

func TestMovements_NeedAnOpenDay(t *testing.T) {
	db := testdb.Fresh(t)
	actor, actorID := admin(t, db, "1234")
	day, err := Open(db, actor, 1000, nil)
	if err != nil {
		t.Fatal(err)
	}

	notes := "from the owner's pocket"
	m, err := AddMovement(db, actor, day.ID, "paid_in", 250.5, "  change float  ", &notes)
	if err != nil {
		t.Fatalf("paid_in: %v", err)
	}
	if m.MovementType != "paid_in" || !near(m.Amount, 250.5) || m.Reason != "change float" ||
		m.CreatedBy == nil || *m.CreatedBy != actorID {
		t.Fatalf("movement: %+v", m)
	}

	for _, bad := range []struct {
		kind   string
		amount float64
		reason string
	}{
		{"transfer", 100, "nope"},
		{"paid_out", 0, "nothing"},
		{"paid_out", -5, "negative"},
		{"paid_out", 100, "   "},
	} {
		if _, err := AddMovement(db, actor, day.ID, bad.kind, bad.amount, bad.reason, nil); !errors.Is(err, ErrInvalidMovement) {
			t.Errorf("%+v: want ErrInvalidMovement, got %v", bad, err)
		}
	}

	if _, err := Close(db, actor, day.ID, Counted{Cash: 1250.5}, nil, 1000); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, err := AddMovement(db, actor, day.ID, "paid_out", 50, "after close", nil); !errors.Is(err, ErrDayNotOpen) {
		t.Fatalf("movement on a closed day: want ErrDayNotOpen, got %v", err)
	}
	if _, err := AddMovement(db, actor, uuid.New(), "paid_out", 50, "unknown day", nil); !errors.Is(err, ErrDayNotFound) {
		t.Fatalf("movement on an unknown day: want ErrDayNotFound, got %v", err)
	}
}

func TestReopen_NeedsAValidAdminPin(t *testing.T) {
	db := testdb.Fresh(t)
	actor, _ := admin(t, db, "1234")
	seedUser(t, db, "till", "counter", "4321")
	day, err := Open(db, actor, 1000, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Close(db, actor, day.ID, Counted{Cash: 1000}, nil, 100); err != nil {
		t.Fatal(err)
	}

	if _, err := Reopen(db, actor, day.ID, "0000"); !errors.Is(err, ErrInvalidPin) {
		t.Fatalf("wrong PIN: want ErrInvalidPin, got %v", err)
	}
	// A counter's PIN is not an admin's.
	if _, err := Reopen(db, actor, day.ID, "4321"); !errors.Is(err, ErrInvalidPin) {
		t.Fatalf("counter PIN: want ErrInvalidPin, got %v", err)
	}
	if still, err := Get(db, day.ID); err != nil || still.Status != StatusClosed {
		t.Fatalf("a refused reopen must leave the day sealed: %+v %v", still, err)
	}

	reopened, err := Reopen(db, actor, day.ID, "1234")
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if reopened.Status != StatusReopened || reopened.ClosedAt != nil || reopened.ClosedBy != nil {
		t.Fatalf("reopened: %+v", reopened)
	}
	if reopened.CountedCash == nil || !near(*reopened.CountedCash, 1000) {
		t.Fatalf("the count survives a reopen: %+v", reopened.CountedCash)
	}
	// Idempotent on an already-open day.
	if again, err := Reopen(db, actor, day.ID, "1234"); err != nil || again.Status != StatusReopened {
		t.Fatalf("second reopen: %+v %v", again, err)
	}
	if got := auditActions(t, db); len(got) != 3 || got[2] != "reopen" {
		t.Fatalf("audit trail: %v", got)
	}
}

func TestReopen_BlockedWhileAnotherDayHoldsTheOpenSlot(t *testing.T) {
	db := testdb.Fresh(t)
	actor, _ := admin(t, db, "1234")
	closedYesterday := seedDay(t, db, daysAgoKey(1), StatusClosed, 500)
	if _, err := Open(db, actor, 1000, nil); err != nil {
		t.Fatal(err)
	}

	if _, err := Reopen(db, actor, closedYesterday, "1234"); !errors.Is(err, ErrPreviousDayOpen) {
		t.Fatalf("want ErrPreviousDayOpen, got %v", err)
	}
	if still, err := Get(db, closedYesterday); err != nil || still.Status != StatusClosed {
		t.Fatalf("yesterday must stay sealed: %+v %v", still, err)
	}
}

func TestForceClose_CountsEqualExpectedAndSaysSo(t *testing.T) {
	db := testdb.Fresh(t)
	actor, _ := admin(t, db, "1234")
	dayID := seedDay(t, db, daysAgoKey(1), StatusOpen, 800)
	seedInvoice(t, db, dayID, daysAgoKey(1), "20260919-001", "cash", "completed", 900, 0, 100, 1000, nil)

	if _, err := ForceClose(db, actor, dayID, "1234", "x"); !errors.Is(err, ErrReasonRequired) {
		t.Fatalf("short reason: want ErrReasonRequired, got %v", err)
	}
	if _, err := ForceClose(db, actor, dayID, "0000", "nobody counted the drawer"); !errors.Is(err, ErrInvalidPin) {
		t.Fatalf("wrong PIN: want ErrInvalidPin, got %v", err)
	}

	closed, err := ForceClose(db, actor, dayID, "1234", "nobody counted the drawer before locking up")
	if err != nil {
		t.Fatalf("force close: %v", err)
	}
	if closed.Status != StatusClosed {
		t.Fatalf("status: %s", closed.Status)
	}
	if closed.ExpectedCash == nil || !near(*closed.ExpectedCash, 1800) ||
		closed.CountedCash == nil || !near(*closed.CountedCash, 1800) ||
		closed.CashVariance == nil || !near(*closed.CashVariance, 0) {
		t.Fatalf("counted must equal expected on a force close: %+v", closed)
	}
	if closed.ClosingNotes == nil || !strings.Contains(*closed.ClosingNotes, "no count") {
		t.Fatalf("the note must say no count was taken: %+v", closed.ClosingNotes)
	}
	if got := auditActions(t, db); len(got) != 1 || got[0] != "force_close" {
		t.Fatalf("audit trail: %v", got)
	}
	if _, err := ForceClose(db, actor, dayID, "1234", "already sealed, trying again"); !errors.Is(err, ErrDayClosed) {
		t.Fatalf("second force close: want ErrDayClosed, got %v", err)
	}
}

// day_close_audit_log carries the only record of who unsealed a day and why,
// so the append-only trigger is part of the contract, not a nicety.
func TestAuditLog_IsAppendOnly(t *testing.T) {
	db := testdb.Fresh(t)
	actor, _ := admin(t, db, "1234")
	if _, err := Open(db, actor, 1000, nil); err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, db, "day_close_audit_log"); n != 1 {
		t.Fatalf("one audit row expected, got %d", n)
	}
	// metadata is JSONB, not a blob: the day-history screen reads fields out
	// of it, so it has to be queryable.
	var openingCash string
	if err := db.QueryRow(`SELECT metadata->>'opening_cash' FROM day_close_audit_log`).Scan(&openingCash); err != nil {
		t.Fatalf("audit metadata must be readable JSONB: %v", err)
	}
	if openingCash != "1000" {
		t.Fatalf("audit metadata opening_cash: %q", openingCash)
	}
	if _, err := db.Exec(`UPDATE day_close_audit_log SET summary = 'nothing happened'`); err == nil {
		t.Fatal("day_close_audit_log must refuse UPDATE")
	}
	if _, err := db.Exec(`DELETE FROM day_close_audit_log`); err == nil {
		t.Fatal("day_close_audit_log must refuse DELETE")
	}
	if n := countRows(t, db, "day_close_audit_log"); n != 1 {
		t.Fatalf("the row must survive: %d", n)
	}
}

func TestGetAndExpected_UnknownDay(t *testing.T) {
	db := testdb.Fresh(t)
	if _, err := Get(db, uuid.New()); !errors.Is(err, ErrDayNotFound) {
		t.Fatalf("get: %v", err)
	}
	if _, err := ComputeExpected(db, uuid.New()); !errors.Is(err, ErrDayNotFound) {
		t.Fatalf("expected: %v", err)
	}
	if _, err := ZData(db, uuid.New()); !errors.Is(err, ErrDayNotFound) {
		t.Fatalf("z: %v", err)
	}
}
