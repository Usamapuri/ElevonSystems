package ledger

import (
	"database/sql"
	"testing"
	"time"

	"elevon-backend/internal/testdb"
	"elevon-backend/internal/util"

	"github.com/google/uuid"
)

// seedCustomer inserts a minimal active customer and returns its id. Ledger
// tests never touch the customers handlers (a different package), so this
// is a tiny local seed rather than a shared helper.
func seedCustomer(t *testing.T, db *sql.DB, name string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := db.QueryRow(`INSERT INTO customers (name) VALUES ($1) RETURNING id`, name).Scan(&id); err != nil {
		t.Fatalf("seed customer %s: %v", name, err)
	}
	return id
}

// dateOnly compares the calendar date a DATE column round-trips to. A value
// scanned back from Postgres carries a fixed +00:00 location (see lib/pq's
// ParseTimestamp), while util.BusinessDate returns midnight in Asia/Karachi
// — different instants for the "same" business day, so comparing via
// time.Time.Equal would be wrong. Comparing the formatted wall-clock date
// (each in its own location) is what actually matters here.
func dateOnly(t time.Time) string { return t.Format("2006-01-02") }

func TestBalance_ZeroWithNoLedgerRows(t *testing.T) {
	db := testdb.Fresh(t)
	custID := seedCustomer(t, db, "Ali Traders")

	bal, err := Balance(db, custID)
	if err != nil {
		t.Fatal(err)
	}
	if bal != 0 {
		t.Fatalf("balance with no ledger rows: got %v want 0", bal)
	}
}

func TestPost_And_Balance_InsideATransaction(t *testing.T) {
	db := testdb.Fresh(t)
	custID := seedCustomer(t, db, "Ali Traders")

	// D6 posts ledger.Post inside the invoice transaction; prove that works
	// by posting both entries through a *sql.Tx (Querier is satisfied by
	// both *sql.DB and *sql.Tx).
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Post(tx, Entry{CustomerID: custID, EntryType: "invoice", Debit: 500}); err != nil {
		t.Fatalf("post invoice entry: %v", err)
	}
	if _, err := Post(tx, Entry{CustomerID: custID, EntryType: "receipt", Credit: 200}); err != nil {
		t.Fatalf("post receipt entry: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	bal, err := Balance(db, custID)
	if err != nil {
		t.Fatal(err)
	}
	if bal != 300 {
		t.Fatalf("balance after entries: got %v want 300", bal)
	}
}

func TestPost_DefaultsBusinessDateWhenUnset(t *testing.T) {
	db := testdb.Fresh(t)
	custID := seedCustomer(t, db, "Beta Gas")

	id, err := Post(db, Entry{CustomerID: custID, EntryType: "adjustment", Debit: 10})
	if err != nil {
		t.Fatal(err)
	}
	var got time.Time
	if err := db.QueryRow(`SELECT business_date FROM customer_ledger_entries WHERE id = $1`, id).Scan(&got); err != nil {
		t.Fatal(err)
	}
	want := util.BusinessDate(time.Now())
	if dateOnly(got) != dateOnly(want) {
		t.Fatalf("default business_date: got %s want %s", dateOnly(got), dateOnly(want))
	}
}

func TestPost_HonoursAnExplicitBusinessDate(t *testing.T) {
	db := testdb.Fresh(t)
	custID := seedCustomer(t, db, "Beta Gas")
	explicit := util.BusinessDate(time.Now().AddDate(0, 0, -3))

	id, err := Post(db, Entry{CustomerID: custID, EntryType: "adjustment", Debit: 10, BusinessDate: explicit})
	if err != nil {
		t.Fatal(err)
	}
	var got time.Time
	if err := db.QueryRow(`SELECT business_date FROM customer_ledger_entries WHERE id = $1`, id).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if dateOnly(got) != dateOnly(explicit) {
		t.Fatalf("explicit business_date: got %s want %s", dateOnly(got), dateOnly(explicit))
	}
}

func TestStatement_RunningBalanceOrder(t *testing.T) {
	db := testdb.Fresh(t)
	custID := seedCustomer(t, db, "Gamma Cylinders")

	// Explicit, increasing created_at so the order the window function
	// (and the test) relies on is deterministic.
	base := time.Now().Add(-time.Hour)
	entries := []struct {
		entryType     string
		debit, credit float64
	}{
		{"invoice", 1000, 0},
		{"receipt", 0, 400},
		{"invoice", 250, 0},
	}
	businessDate := util.BusinessDate(base)
	for i, e := range entries {
		if _, err := db.Exec(`
			INSERT INTO customer_ledger_entries (customer_id, entry_type, debit, credit, business_date, created_at)
			VALUES ($1, $2, $3, $4, $5, $6)`,
			custID, e.entryType, e.debit, e.credit, businessDate, base.Add(time.Duration(i)*time.Minute),
		); err != nil {
			t.Fatalf("seed entry %d: %v", i, err)
		}
	}

	rows, err := Statement(db, custID, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatalf("statement rows: got %d want 3", len(rows))
	}
	wantRunning := []float64{1000, 600, 850}
	for i, r := range rows {
		if r.RunningBalance != wantRunning[i] {
			t.Fatalf("row %d running balance: got %v want %v", i, r.RunningBalance, wantRunning[i])
		}
		if r.Debit != entries[i].debit || r.Credit != entries[i].credit || r.EntryType != entries[i].entryType {
			t.Fatalf("row %d fields: got %+v want %+v", i, r, entries[i])
		}
	}

	bal, err := Balance(db, custID)
	if err != nil {
		t.Fatal(err)
	}
	if bal != 850 {
		t.Fatalf("final balance: got %v want 850", bal)
	}
}

// TestStatement_FiltersByBusinessDateButKeepsTrueRunningBalance proves the
// running balance always reflects the whole account, even when from/to
// narrow which rows come back — a filtered statement must not look like the
// balance reset to zero at the window start.
func TestStatement_FiltersByBusinessDateButKeepsTrueRunningBalance(t *testing.T) {
	db := testdb.Fresh(t)
	custID := seedCustomer(t, db, "Delta LPG")

	day1 := util.BusinessDate(time.Now().AddDate(0, 0, -2))
	day2 := util.BusinessDate(time.Now())

	if _, err := db.Exec(`INSERT INTO customer_ledger_entries (customer_id, entry_type, debit, credit, business_date, created_at)
		VALUES ($1, 'invoice', 1000, 0, $2, $3)`, custID, day1, day1); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO customer_ledger_entries (customer_id, entry_type, debit, credit, business_date, created_at)
		VALUES ($1, 'receipt', 0, 300, $2, $3)`, custID, day2, day2); err != nil {
		t.Fatal(err)
	}

	rows, err := Statement(db, custID, &day2, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("filtered rows: got %d want 1", len(rows))
	}
	if rows[0].RunningBalance != 700 {
		t.Fatalf("filtered row must keep the true running balance: got %v want 700", rows[0].RunningBalance)
	}

	// The unfiltered statement still returns both rows in order.
	all, err := Statement(db, custID, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 2 || all[0].RunningBalance != 1000 || all[1].RunningBalance != 700 {
		t.Fatalf("unfiltered statement: %+v", all)
	}
}

func TestLedgerAppendOnly_BlocksUpdateAndDelete(t *testing.T) {
	db := testdb.Fresh(t)
	custID := seedCustomer(t, db, "Epsilon Traders")
	id, err := Post(db, Entry{CustomerID: custID, EntryType: "adjustment", Debit: 50})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE customer_ledger_entries SET debit = 999 WHERE id = $1`, id); err == nil {
		t.Fatal("expected the append-only trigger to block UPDATE")
	}
	if _, err := db.Exec(`DELETE FROM customer_ledger_entries WHERE id = $1`, id); err == nil {
		t.Fatal("expected the append-only trigger to block DELETE")
	}
	// The row is exactly as posted — the trigger really did stop the write.
	var debit float64
	if err := db.QueryRow(`SELECT debit::float8 FROM customer_ledger_entries WHERE id = $1`, id).Scan(&debit); err != nil {
		t.Fatal(err)
	}
	if debit != 50 {
		t.Fatalf("row must be unchanged: got debit %v want 50", debit)
	}
}
