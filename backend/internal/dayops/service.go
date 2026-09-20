// Package dayops owns the business day: opening the till against a declared
// cash float, the paid-in/paid-out movements taken from the drawer during the
// day, the one-step close that counts cash, card and online against what the
// day's invoices and receipts say should be there, and the two admin-PIN
// escape hatches (reopen, force close).
//
// Ported in shape from the retail POS's dayops package, minus staging, shifts,
// tab release, offline reconciliation and report email: one till and one owner
// do not need a two-person handoff, so close is count → confirm in one step
// (spec §5.5).
//
// The rule this package exists to protect is §6.7's no-auto-open:
// EnsureOpenDayForInvoice never creates a business_days row. A day begins when
// a human says what is physically in the drawer — software cannot make that
// declaration, and inventing an opening float makes every cash figure for the
// next 24 hours a fiction. no_auto_open_contract_test.go pins it.
package dayops

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"elevon-backend/internal/staffpin"
	"elevon-backend/internal/util"

	"github.com/google/uuid"
)

// Day statuses, matching the business_days_status_check constraint.
const (
	StatusOpen     = "open"
	StatusClosed   = "closed"
	StatusReopened = "reopened"
)

// DateLayout is how a business_date is formatted for comparison and for the
// ::date parameters below. lib/pq scans a DATE into a time.Time carrying a UTC
// location, so business dates are always compared as formatted strings — never
// with time.Equal, which would see 00:00 UTC and 00:00 PKT as different
// instants for the same calendar day.
const DateLayout = "2006-01-02"

// DefaultVarianceThreshold is the fallback when settings cannot be read. It
// matches the seeded day_close_variance_threshold in migrations/001_init.sql.
const DefaultVarianceThreshold = 100.0

// Force-close reason bounds. The lower bound stops one-character placeholders;
// the upper bound is the practical width of the day-history row.
const (
	ReasonMinLen = 4
	ReasonMaxLen = 500
)

var (
	// ErrDayNotOpen: no business day is open and today has no row at all. The
	// till blocks until a human opens the day.
	ErrDayNotOpen = errors.New("dayops: no business day is open for today")
	// ErrPreviousDayOpen: a day for another date still holds the single open
	// slot. Close it before anything else can happen.
	ErrPreviousDayOpen = errors.New("dayops: an earlier business day is still open")
	// ErrDayAlreadyOpen: today already has a business_days row.
	ErrDayAlreadyOpen = errors.New("dayops: a business day already exists for today")
	// ErrDayNotFound: no row with that id.
	ErrDayNotFound = errors.New("dayops: business day not found")
	// ErrDayClosed: the day is sealed; reopen it first.
	ErrDayClosed = errors.New("dayops: business day is already closed")
	// ErrVarianceNoteRequired: a tender is off by more than the threshold and
	// nothing explains it.
	ErrVarianceNoteRequired = errors.New("dayops: variance exceeds the threshold — a closing note is required")
	// ErrInvalidPin: no active admin holds that PIN.
	ErrInvalidPin = errors.New("dayops: invalid pin")
	// ErrReasonRequired: force close without a written reason.
	ErrReasonRequired = errors.New("dayops: a written reason is required")
	// ErrInvalidMovement: bad movement type, amount or reason.
	ErrInvalidMovement = errors.New("dayops: invalid cash movement")
)

// Querier is satisfied by *sql.DB and *sql.Tx, so every read here works
// identically inside or outside a caller's transaction. The invoice path
// (Task D6) calls EnsureOpenDayForInvoice with the invoice's own *sql.Tx.
type Querier interface {
	QueryRow(query string, args ...any) *sql.Row
	Query(query string, args ...any) (*sql.Rows, error)
	Exec(query string, args ...any) (sql.Result, error)
}

// Actor is who is performing the action: the signed-in user. For the PIN-gated
// paths (reopen, force close) the PIN holder who authorised it is recorded
// separately in the audit row's metadata, so "who did it" and "who allowed it"
// stay distinguishable.
type Actor struct {
	ID   uuid.UUID
	Name string
	Role string
}

// Day is one business_days row. Every money column is NUMERIC in Postgres and
// float64 here; the counted/expected/variance/summary columns are nil until
// the day is closed.
type Day struct {
	ID           uuid.UUID  `json:"id"`
	BusinessDate time.Time  `json:"business_date"`
	Status       string     `json:"status"`
	OpenedAt     time.Time  `json:"opened_at"`
	OpenedBy     *uuid.UUID `json:"opened_by"`
	OpenedByName *string    `json:"opened_by_name"`
	OpeningCash  float64    `json:"opening_cash"`
	OpeningNotes *string    `json:"opening_notes"`
	ClosedAt     *time.Time `json:"closed_at"`
	ClosedBy     *uuid.UUID `json:"closed_by"`
	ClosedByName *string    `json:"closed_by_name"`

	CountedCash   *float64 `json:"counted_cash"`
	CountedCard   *float64 `json:"counted_card"`
	CountedOnline *float64 `json:"counted_online"`

	ExpectedCash   *float64 `json:"expected_cash"`
	ExpectedCard   *float64 `json:"expected_card"`
	ExpectedOnline *float64 `json:"expected_online"`

	CashVariance   *float64 `json:"cash_variance"`
	CardVariance   *float64 `json:"card_variance"`
	OnlineVariance *float64 `json:"online_variance"`

	GrossSales        *float64 `json:"gross_sales"`
	Discounts         *float64 `json:"discounts"`
	TaxCollected      *float64 `json:"tax_collected"`
	NetSales          *float64 `json:"net_sales"`
	OnAccountSales    *float64 `json:"on_account_sales"`
	ReceiptsCollected *float64 `json:"receipts_collected"`
	InvoiceCount      *int     `json:"invoice_count"`
	VoidCount         *int     `json:"void_count"`

	ClosingNotes *string   `json:"closing_notes"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// DateKey is the day's business_date as YYYY-MM-DD — the only safe way to
// compare two business dates (see DateLayout).
func (d Day) DateKey() string { return d.BusinessDate.Format(DateLayout) }

// IsOpen reports whether the day can still take sales.
func (d Day) IsOpen() bool { return d.Status == StatusOpen || d.Status == StatusReopened }

// Counted is what the person actually counted at close. All three are
// required — "enter 0" is a real answer, "skip" is not (spec §6.7), so a NULL
// counted column can only ever mean "nobody counted", never "counted zero".
type Counted struct {
	Cash   float64 `json:"cash"`
	Card   float64 `json:"card"`
	Online float64 `json:"online"`
}

// Expected is the live computation of what the day should hold, plus the sales
// summary that Close snapshots onto the row and the Z-report prints.
//
//	cash   = opening_cash + cash invoices + cash receipts + paid_in − paid_out
//	card   = card invoices + card receipts
//	online = online invoices + online receipts
//
// Voided invoices (status <> 'completed') and voided receipts (voided_at NOT
// NULL) are excluded from every figure. Credit sales are on_account_sales:
// they are reported separately and never enter a tender expectation, because
// no money arrived in the till for them.
type Expected struct {
	OpeningCash float64 `json:"opening_cash"`

	CashSales      float64 `json:"cash_sales"`
	CardSales      float64 `json:"card_sales"`
	OnlineSales    float64 `json:"online_sales"`
	OnAccountSales float64 `json:"on_account_sales"`

	CashReceipts      float64 `json:"cash_receipts"`
	CardReceipts      float64 `json:"card_receipts"`
	OnlineReceipts    float64 `json:"online_receipts"`
	ReceiptsCollected float64 `json:"receipts_collected"`

	PaidIn  float64 `json:"paid_in"`
	PaidOut float64 `json:"paid_out"`

	Cash   float64 `json:"cash"`
	Card   float64 `json:"card"`
	Online float64 `json:"online"`

	// Sales summary. GrossSales is Σ subtotal (before discount and tax),
	// Discounts is Σ discount_amount, TaxCollected is Σ (tax + further tax)
	// and NetSales is Σ total_payable — the rounded rupee figure the customer
	// actually owed.
	GrossSales   float64 `json:"gross_sales"`
	Discounts    float64 `json:"discounts"`
	TaxCollected float64 `json:"tax_collected"`
	NetSales     float64 `json:"net_sales"`

	InvoiceCount int `json:"invoice_count"`
	VoidCount    int `json:"void_count"`
}

// Movement is one cash_drawer_movements row.
type Movement struct {
	ID            uuid.UUID  `json:"id"`
	BusinessDayID uuid.UUID  `json:"business_day_id"`
	MovementType  string     `json:"movement_type"`
	Amount        float64    `json:"amount"`
	Reason        string     `json:"reason"`
	Notes         *string    `json:"notes"`
	CreatedBy     *uuid.UUID `json:"created_by"`
	CreatedByName *string    `json:"created_by_name"`
	CreatedAt     time.Time  `json:"created_at"`
}

// ZReport is everything the printable Z slip needs in one round-trip. Counted
// amounts and variances live on Day (they are nil until the day is closed).
// For an open or reopened day, Expected is the live ComputeExpected, so it
// moves with every new sale, receipt or movement. For a closed day, Expected
// is sealed: the tender totals and sales summary are exactly what Close (or
// ForceClose) wrote onto the row at seal time, not a fresh recomputation — a
// void entered afterwards must not silently restate a document that already
// went out (see sealedExpected). PaidIn/PaidOut are the one figure that was
// never separately sealed onto the row, so they are read from
// cash_drawer_movements even for a closed day; that is safe because
// movements for a sealed day are never edited or deleted.
type ZReport struct {
	GeneratedAt time.Time  `json:"generated_at"`
	Day         Day        `json:"day"`
	Expected    Expected   `json:"expected"`
	Movements   []Movement `json:"movements"`
}

// ── reads ────────────────────────────────────────────────────────────────

const dayColumns = `bd.id, bd.business_date, bd.status, bd.opened_at, bd.opened_by,
	COALESCE(NULLIF(TRIM(ou.first_name || ' ' || ou.last_name), ''), ou.username),
	bd.opening_cash::float8, bd.opening_notes,
	bd.closed_at, bd.closed_by,
	COALESCE(NULLIF(TRIM(cu.first_name || ' ' || cu.last_name), ''), cu.username),
	bd.counted_cash::float8, bd.counted_card::float8, bd.counted_online::float8,
	bd.expected_cash::float8, bd.expected_card::float8, bd.expected_online::float8,
	bd.cash_variance::float8, bd.card_variance::float8, bd.online_variance::float8,
	bd.gross_sales::float8, bd.discounts::float8, bd.tax_collected::float8, bd.net_sales::float8,
	bd.on_account_sales::float8, bd.receipts_collected::float8,
	bd.invoice_count, bd.void_count, bd.closing_notes, bd.created_at, bd.updated_at`

const dayFrom = ` FROM business_days bd
	LEFT JOIN users ou ON ou.id = bd.opened_by
	LEFT JOIN users cu ON cu.id = bd.closed_by`

type scanner interface{ Scan(dest ...any) error }

func scanDay(row scanner) (Day, error) {
	var d Day
	err := row.Scan(&d.ID, &d.BusinessDate, &d.Status, &d.OpenedAt, &d.OpenedBy, &d.OpenedByName,
		&d.OpeningCash, &d.OpeningNotes, &d.ClosedAt, &d.ClosedBy, &d.ClosedByName,
		&d.CountedCash, &d.CountedCard, &d.CountedOnline,
		&d.ExpectedCash, &d.ExpectedCard, &d.ExpectedOnline,
		&d.CashVariance, &d.CardVariance, &d.OnlineVariance,
		&d.GrossSales, &d.Discounts, &d.TaxCollected, &d.NetSales,
		&d.OnAccountSales, &d.ReceiptsCollected,
		&d.InvoiceCount, &d.VoidCount, &d.ClosingNotes, &d.CreatedAt, &d.UpdatedAt)
	return d, err
}

// Current returns the day holding the single open slot ('open' or 'reopened'),
// or (nil, nil) when nothing is open. uniq_business_days_single_open makes at
// most one such row possible, so this is unambiguous.
func Current(q Querier) (*Day, error) {
	d, err := scanDay(q.QueryRow(`SELECT ` + dayColumns + dayFrom +
		` WHERE bd.status IN ('open', 'reopened') ORDER BY bd.business_date DESC LIMIT 1`))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// Today returns today's row in any status, or (nil, nil) when the day has
// never been opened. The day-close screen uses it to show "sealed" after a
// close instead of offering an Open button that would only 409.
func Today(q Querier) (*Day, error) {
	d, err := byDate(q, util.BusinessDate(time.Now()).Format(DateLayout))
	if errors.Is(err, ErrDayNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// byDate loads the row for one business date. ErrDayNotFound when there is none.
func byDate(q Querier, dateKey string) (Day, error) {
	d, err := scanDay(q.QueryRow(`SELECT `+dayColumns+dayFrom+` WHERE bd.business_date = $1::date`, dateKey))
	if errors.Is(err, sql.ErrNoRows) {
		return Day{}, ErrDayNotFound
	}
	return d, err
}

// Get loads one day by id. ErrDayNotFound when there is none.
func Get(q Querier, dayID uuid.UUID) (Day, error) {
	d, err := scanDay(q.QueryRow(`SELECT `+dayColumns+dayFrom+` WHERE bd.id = $1`, dayID))
	if errors.Is(err, sql.ErrNoRows) {
		return Day{}, ErrDayNotFound
	}
	return d, err
}

// History returns the most recently closed days, newest first.
func History(q Querier, limit int) ([]Day, error) {
	if limit < 1 || limit > 365 {
		limit = 30
	}
	rows, err := q.Query(`SELECT `+dayColumns+dayFrom+
		` WHERE bd.status = 'closed' ORDER BY bd.business_date DESC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	days := []Day{}
	for rows.Next() {
		d, err := scanDay(rows)
		if err != nil {
			return nil, err
		}
		days = append(days, d)
	}
	return days, rows.Err()
}

// ListMovements returns a day's drawer movements, oldest first.
func ListMovements(q Querier, dayID uuid.UUID) ([]Movement, error) {
	rows, err := q.Query(`
		SELECT m.id, m.business_day_id, m.movement_type, m.amount::float8, m.reason, m.notes, m.created_by,
		       COALESCE(NULLIF(TRIM(u.first_name || ' ' || u.last_name), ''), u.username), m.created_at
		FROM cash_drawer_movements m
		LEFT JOIN users u ON u.id = m.created_by
		WHERE m.business_day_id = $1
		ORDER BY m.created_at`, dayID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Movement{}
	for rows.Next() {
		var m Movement
		if err := rows.Scan(&m.ID, &m.BusinessDayID, &m.MovementType, &m.Amount, &m.Reason, &m.Notes,
			&m.CreatedBy, &m.CreatedByName, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// Expected computes what the day should hold, from the day's own invoices,
// receipts and drawer movements. Scoped by business_day_id: a late sale that
// reopened a sealed day carries that day's id, so it lands in the same count.
func ComputeExpected(q Querier, dayID uuid.UUID) (Expected, error) {
	var e Expected
	err := q.QueryRow(`
		WITH inv AS (
			SELECT
				COALESCE(SUM(total_payable) FILTER (WHERE payment_method = 'cash'), 0)::float8   AS cash,
				COALESCE(SUM(total_payable) FILTER (WHERE payment_method = 'card'), 0)::float8   AS card,
				COALESCE(SUM(total_payable) FILTER (WHERE payment_method = 'online'), 0)::float8 AS online,
				COALESCE(SUM(total_payable) FILTER (WHERE payment_method = 'credit'), 0)::float8 AS on_account,
				COALESCE(SUM(subtotal), 0)::float8                                               AS gross,
				COALESCE(SUM(discount_amount), 0)::float8                                        AS discounts,
				COALESCE(SUM(tax_amount + further_tax_amount), 0)::float8                        AS tax,
				COALESCE(SUM(total_payable), 0)::float8                                          AS net,
				COUNT(*)                                                                         AS invoice_count
			FROM invoices WHERE business_day_id = $1 AND status = 'completed'
		), voided AS (
			SELECT COUNT(*) AS void_count FROM invoices WHERE business_day_id = $1 AND status = 'voided'
		), rec AS (
			SELECT
				COALESCE(SUM(amount) FILTER (WHERE method = 'cash'), 0)::float8   AS cash,
				COALESCE(SUM(amount) FILTER (WHERE method = 'card'), 0)::float8   AS card,
				COALESCE(SUM(amount) FILTER (WHERE method = 'online'), 0)::float8 AS online,
				COALESCE(SUM(amount), 0)::float8                                  AS total
			FROM customer_receipts WHERE business_day_id = $1 AND voided_at IS NULL
		), mov AS (
			SELECT
				COALESCE(SUM(amount) FILTER (WHERE movement_type = 'paid_in'), 0)::float8  AS paid_in,
				COALESCE(SUM(amount) FILTER (WHERE movement_type = 'paid_out'), 0)::float8 AS paid_out
			FROM cash_drawer_movements WHERE business_day_id = $1
		)
		SELECT bd.opening_cash::float8,
		       inv.cash, inv.card, inv.online, inv.on_account,
		       inv.gross, inv.discounts, inv.tax, inv.net, inv.invoice_count, voided.void_count,
		       rec.cash, rec.card, rec.online, rec.total,
		       mov.paid_in, mov.paid_out
		FROM business_days bd, inv, voided, rec, mov
		WHERE bd.id = $1`, dayID).Scan(
		&e.OpeningCash,
		&e.CashSales, &e.CardSales, &e.OnlineSales, &e.OnAccountSales,
		&e.GrossSales, &e.Discounts, &e.TaxCollected, &e.NetSales, &e.InvoiceCount, &e.VoidCount,
		&e.CashReceipts, &e.CardReceipts, &e.OnlineReceipts, &e.ReceiptsCollected,
		&e.PaidIn, &e.PaidOut)
	if errors.Is(err, sql.ErrNoRows) {
		return Expected{}, ErrDayNotFound
	}
	if err != nil {
		return Expected{}, err
	}
	e.Cash = round2(e.OpeningCash + e.CashSales + e.CashReceipts + e.PaidIn - e.PaidOut)
	e.Card = round2(e.CardSales + e.CardReceipts)
	e.Online = round2(e.OnlineSales + e.OnlineReceipts)
	return e, nil
}

// ZData assembles the Z-report payload for one day. A closed day returns the
// figures Close (or ForceClose) sealed onto its business_days row rather than
// a live recomputation: the Z slip is printed and handed to the owner at
// close, and a void entered afterwards must not silently restate a document
// that already went out. An open or reopened day has no seal yet, so its
// figures are still the live ComputeExpected.
func ZData(q Querier, dayID uuid.UUID) (ZReport, error) {
	day, err := Get(q, dayID)
	if err != nil {
		return ZReport{}, err
	}
	var expected Expected
	if day.Status == StatusClosed {
		expected, err = sealedExpected(q, day)
		if err != nil {
			return ZReport{}, err
		}
	} else {
		expected, err = ComputeExpected(q, dayID)
		if err != nil {
			return ZReport{}, err
		}
	}
	movements, err := ListMovements(q, dayID)
	if err != nil {
		return ZReport{}, err
	}
	return ZReport{GeneratedAt: time.Now(), Day: day, Expected: expected, Movements: movements}, nil
}

// sealedExpected rebuilds Expected from the figures a closed day sealed onto
// its own row at close time: expected_cash/card/online, gross_sales,
// discounts, tax_collected, net_sales, on_account_sales, receipts_collected,
// invoice_count and void_count. Close and ForceClose always write these
// together (never one without the others), so a closed row has them all —
// but the pointers are read defensively rather than dereferenced blind, since
// a nil here would otherwise panic a printed report instead of degrading to a
// visibly wrong zero.
//
// The rest of the per-tender breakdown that ComputeExpected also returns
// (CashSales vs. CashReceipts) was never separately sealed — only the
// combined expected_cash/card/online were — so those fields are left at zero
// on a closed day's Expected rather than recomputed live, which would mix a
// sealed total with an unsealed breakdown of it.
//
// PaidIn/PaidOut are the exception: they are read live from
// cash_drawer_movements for this day even though the day is sealed. That is
// safe specifically because movements are never edited or deleted once
// written — AddMovement only ever inserts, and there is no update/delete
// path for a movement row — so summing them for a closed day returns the
// same figure at any later read, unlike invoices, which a void can still
// change after the seal.
func sealedExpected(q Querier, day Day) (Expected, error) {
	var paidIn, paidOut float64
	err := q.QueryRow(`
		SELECT COALESCE(SUM(amount) FILTER (WHERE movement_type = 'paid_in'), 0)::float8,
		       COALESCE(SUM(amount) FILTER (WHERE movement_type = 'paid_out'), 0)::float8
		FROM cash_drawer_movements WHERE business_day_id = $1`, day.ID).Scan(&paidIn, &paidOut)
	if err != nil {
		return Expected{}, err
	}
	return Expected{
		OpeningCash:       day.OpeningCash,
		Cash:              floatOrZero(day.ExpectedCash),
		Card:              floatOrZero(day.ExpectedCard),
		Online:            floatOrZero(day.ExpectedOnline),
		OnAccountSales:    floatOrZero(day.OnAccountSales),
		ReceiptsCollected: floatOrZero(day.ReceiptsCollected),
		PaidIn:            round2(paidIn),
		PaidOut:           round2(paidOut),
		GrossSales:        floatOrZero(day.GrossSales),
		Discounts:         floatOrZero(day.Discounts),
		TaxCollected:      floatOrZero(day.TaxCollected),
		NetSales:          floatOrZero(day.NetSales),
		InvoiceCount:      intOrZero(day.InvoiceCount),
		VoidCount:         intOrZero(day.VoidCount),
	}, nil
}

func floatOrZero(v *float64) float64 {
	if v == nil {
		return 0
	}
	return *v
}

func intOrZero(v *int) int {
	if v == nil {
		return 0
	}
	return *v
}

// ── writes ───────────────────────────────────────────────────────────────

// Open starts today's business day with a declared cash float. Today only:
// there is no way to open a past date, and a day that was closed earlier today
// is reopened (admin PIN), never opened a second time.
//
// ErrDayAlreadyOpen when today already has a row in any status;
// ErrPreviousDayOpen when another date still holds the open slot.
func Open(db *sql.DB, actor Actor, openingCash float64, notes *string) (Day, error) {
	if openingCash < 0 || math.IsNaN(openingCash) || math.IsInf(openingCash, 0) {
		return Day{}, fmt.Errorf("dayops: opening cash must be zero or more")
	}
	todayKey := util.BusinessDate(time.Now()).Format(DateLayout)

	current, err := Current(db)
	if err != nil {
		return Day{}, err
	}
	if current != nil {
		if current.DateKey() == todayKey {
			return Day{}, ErrDayAlreadyOpen
		}
		return Day{}, ErrPreviousDayOpen
	}
	if _, err := byDate(db, todayKey); err == nil {
		return Day{}, ErrDayAlreadyOpen
	} else if !errors.Is(err, ErrDayNotFound) {
		return Day{}, err
	}

	tx, err := db.Begin()
	if err != nil {
		return Day{}, err
	}
	defer tx.Rollback() //nolint:errcheck // rollback after commit is a no-op

	var id uuid.UUID
	err = tx.QueryRow(`
		INSERT INTO business_days (business_date, status, opened_by, opening_cash, opening_notes)
		VALUES ($1::date, 'open', $2, $3, $4)
		RETURNING id`, todayKey, actorRef(actor), round2(openingCash), notes).Scan(&id)
	switch {
	// A second row in the open slot: another date was opened between the
	// check above and this insert.
	case util.IsUniqueViolation(err, "uniq_business_days_single_open"):
		return Day{}, ErrPreviousDayOpen
	case util.IsUniqueViolation(err, "business_days_business_date_key"):
		return Day{}, ErrDayAlreadyOpen
	case err != nil:
		return Day{}, err
	}

	if err := writeAuditTx(tx, &id, todayKey, "open", actor,
		fmt.Sprintf("Opened business day %s with a cash float of %.2f", todayKey, openingCash),
		map[string]any{"opening_cash": round2(openingCash), "notes": notes},
	); err != nil {
		return Day{}, err
	}
	if err := tx.Commit(); err != nil {
		return Day{}, err
	}
	return Get(db, id)
}

// EnsureOpenDayForInvoice resolves the business day an invoice created at
// `now` must carry (spec §6.7). It runs inside the invoice's own transaction
// and has exactly four branches, none of which creates a business_days row:
//
//  1. today's day holds the open slot → use it;
//  2. another date holds it → ErrPreviousDayOpen, blocking until it is closed;
//  3. today's row exists and is closed → a late sale has arrived: flip the SAME
//     row to 'reopened' (an UPDATE, so the counts already taken survive) and
//     write an audit row;
//  4. no row for today → ErrDayNotOpen.
//
// There is deliberately no fifth branch that opens the day. Auto-opening would
// have to invent an opening float, and every cash figure for the next 24 hours
// is measured against that number. Blocking the sale for the seconds it takes
// a person to tap "Open day" is the honest answer.
// no_auto_open_contract_test.go greps this function body for
// "INSERT INTO business_days" and fails the build if one appears, so the whole
// path is kept inline here rather than delegated to a helper.
func EnsureOpenDayForInvoice(tx *sql.Tx, actor Actor, now time.Time) (Day, error) {
	todayKey := util.BusinessDate(now).Format(DateLayout)

	current, err := Current(tx)
	if err != nil {
		return Day{}, err
	}
	if current != nil {
		if current.DateKey() == todayKey {
			return *current, nil // 1
		}
		return Day{}, fmt.Errorf("%w (open day %s, today %s)", ErrPreviousDayOpen, current.DateKey(), todayKey) // 2
	}

	today, err := byDate(tx, todayKey)
	if errors.Is(err, ErrDayNotFound) {
		return Day{}, ErrDayNotOpen // 4
	}
	if err != nil {
		return Day{}, err
	}
	if today.Status != StatusClosed {
		// Reachable under READ COMMITTED: another invoice's transaction
		// committed a reopen (or an operator opened the day) between the
		// Current read above and this one, so today's row already holds the
		// open slot. Harmless — hand back the row that is now open rather
		// than inventing a second one for the same date.
		return today, nil
	}

	// 3 — reopen for the late sale. counted_*/expected_*/variance columns are
	// left exactly as the close left them so the person re-sealing sees the
	// count they already took; closed_at/closed_by are cleared because the day
	// is no longer closed.
	if _, err := tx.Exec(`
		UPDATE business_days
		SET status = 'reopened', closed_at = NULL, closed_by = NULL, updated_at = now()
		WHERE id = $1 AND status = 'closed'`, today.ID); err != nil {
		if util.IsUniqueViolation(err, "uniq_business_days_single_open") {
			return Day{}, ErrPreviousDayOpen
		}
		return Day{}, err
	}
	if err := writeAuditTx(tx, &today.ID, todayKey, "reopen_for_sale", actor,
		fmt.Sprintf("Late sale after close — %s reopened automatically", todayKey),
		map[string]any{"auth": "late_sale"},
	); err != nil {
		return Day{}, err
	}
	return Get(tx, today.ID)
}

// AddMovement records a paid-in or paid-out against an open day.
func AddMovement(db *sql.DB, actor Actor, dayID uuid.UUID, kind string, amount float64, reason string, notes *string) (Movement, error) {
	if kind != "paid_in" && kind != "paid_out" {
		return Movement{}, fmt.Errorf("%w: type must be paid_in or paid_out", ErrInvalidMovement)
	}
	if amount <= 0 || math.IsNaN(amount) || math.IsInf(amount, 0) {
		return Movement{}, fmt.Errorf("%w: amount must be more than zero", ErrInvalidMovement)
	}
	reason = strings.TrimSpace(reason)
	if reason == "" || len(reason) > 200 {
		return Movement{}, fmt.Errorf("%w: a reason of 1–200 characters is required", ErrInvalidMovement)
	}
	day, err := Get(db, dayID)
	if err != nil {
		return Movement{}, err
	}
	if !day.IsOpen() {
		return Movement{}, ErrDayNotOpen
	}

	tx, err := db.Begin()
	if err != nil {
		return Movement{}, err
	}
	defer tx.Rollback() //nolint:errcheck

	var id uuid.UUID
	if err := tx.QueryRow(`
		INSERT INTO cash_drawer_movements (business_day_id, movement_type, amount, reason, notes, created_by)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id`, dayID, kind, round2(amount), reason, notes, actorRef(actor)).Scan(&id); err != nil {
		return Movement{}, err
	}
	if err := writeAuditTx(tx, &dayID, day.DateKey(), "movement", actor,
		fmt.Sprintf("%s %.2f — %s", kind, amount, reason),
		map[string]any{"type": kind, "amount": round2(amount), "reason": reason},
	); err != nil {
		return Movement{}, err
	}
	if err := tx.Commit(); err != nil {
		return Movement{}, err
	}

	movements, err := ListMovements(db, dayID)
	if err != nil {
		return Movement{}, err
	}
	for _, m := range movements {
		if m.ID == id {
			return m, nil
		}
	}
	return Movement{}, ErrDayNotFound
}

// lockDay takes a row-level write lock on one business day inside tx and
// returns it. Everything a close decides — what the day currently is, what it
// expects, whether the variance needs a note — has to be read after this lock
// and written before the commit that releases it. Without it two tills closing
// at the same moment both read "open", both compute a count, and the second
// UPDATE silently overwrites the first one's counted/expected figures while
// both write a 'close' audit row.
//
// The lock is taken on business_days alone rather than with Get's LEFT JOINs:
// Postgres refuses FOR UPDATE on the nullable side of an outer join, and the
// user rows are not what needs locking.
func lockDay(tx *sql.Tx, dayID uuid.UUID) (Day, error) {
	var status string
	err := tx.QueryRow(`SELECT status FROM business_days WHERE id = $1 FOR UPDATE`, dayID).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return Day{}, ErrDayNotFound
	}
	if err != nil {
		return Day{}, err
	}
	return Get(tx, dayID)
}

// Close seals a day in one step: count cash, card and online, compare against
// Expected, snapshot the summary and lock. |variance| above threshold on ANY
// tender needs a closing note; without one the close is refused with
// ErrVarianceNoteRequired and nothing is written.
//
// The whole sequence runs inside one transaction that holds a row lock on the
// day from the first read to the commit, so a second close arriving while this
// one is in flight waits and then finds the day sealed (ErrDayClosed) instead
// of overwriting it.
func Close(db *sql.DB, actor Actor, dayID uuid.UUID, counted Counted, closingNotes *string, threshold float64) (Day, error) {
	tx, err := db.Begin()
	if err != nil {
		return Day{}, err
	}
	defer tx.Rollback() //nolint:errcheck

	day, err := lockDay(tx, dayID)
	if err != nil {
		return Day{}, err
	}
	if day.Status == StatusClosed {
		return Day{}, ErrDayClosed
	}
	expected, err := ComputeExpected(tx, dayID)
	if err != nil {
		return Day{}, err
	}
	if threshold < 0 || math.IsNaN(threshold) {
		threshold = DefaultVarianceThreshold
	}

	countedCash, countedCard, countedOnline := round2(counted.Cash), round2(counted.Card), round2(counted.Online)
	cashVariance := round2(countedCash - expected.Cash)
	cardVariance := round2(countedCard - expected.Card)
	onlineVariance := round2(countedOnline - expected.Online)

	note := ""
	if closingNotes != nil {
		note = strings.TrimSpace(*closingNotes)
	}
	if overThreshold(cashVariance, threshold) || overThreshold(cardVariance, threshold) || overThreshold(onlineVariance, threshold) {
		if note == "" {
			return Day{}, ErrVarianceNoteRequired
		}
	}

	res, err := tx.Exec(`
		UPDATE business_days SET
			status = 'closed', closed_at = now(), closed_by = $1,
			counted_cash = $2, counted_card = $3, counted_online = $4,
			expected_cash = $5, expected_card = $6, expected_online = $7,
			cash_variance = $8, card_variance = $9, online_variance = $10,
			gross_sales = $11, discounts = $12, tax_collected = $13, net_sales = $14,
			on_account_sales = $15, receipts_collected = $16,
			invoice_count = $17, void_count = $18,
			closing_notes = $19, updated_at = now()
		WHERE id = $20 AND status IN ('open', 'reopened')`,
		actorRef(actor),
		countedCash, countedCard, countedOnline,
		expected.Cash, expected.Card, expected.Online,
		cashVariance, cardVariance, onlineVariance,
		expected.GrossSales, expected.Discounts, expected.TaxCollected, expected.NetSales,
		expected.OnAccountSales, expected.ReceiptsCollected,
		expected.InvoiceCount, expected.VoidCount,
		nullIfEmpty(note), dayID)
	if err != nil {
		return Day{}, err
	}
	// Belt and braces behind the row lock: the status predicate above means a
	// day sealed by anyone else matches no row, and that must never commit as
	// a silent no-op with an audit row claiming a close happened.
	sealed, err := res.RowsAffected()
	if err != nil {
		return Day{}, err
	}
	if sealed == 0 {
		return Day{}, ErrDayClosed
	}
	if err := writeAuditTx(tx, &dayID, day.DateKey(), "close", actor,
		fmt.Sprintf("Closed %s — cash variance %.2f, card %.2f, online %.2f", day.DateKey(), cashVariance, cardVariance, onlineVariance),
		map[string]any{
			"expected_cash": expected.Cash, "counted_cash": countedCash, "cash_variance": cashVariance,
			"expected_card": expected.Card, "counted_card": countedCard, "card_variance": cardVariance,
			"expected_online": expected.Online, "counted_online": countedOnline, "online_variance": onlineVariance,
			"net_sales": expected.NetSales, "on_account_sales": expected.OnAccountSales,
			"invoice_count": expected.InvoiceCount, "void_count": expected.VoidCount,
			"threshold": threshold,
		},
	); err != nil {
		return Day{}, err
	}
	if err := tx.Commit(); err != nil {
		return Day{}, err
	}
	return Get(db, dayID)
}

// Reopen unseals a closed day against an active admin's PIN. The counts and
// summary stay on the row; a re-close recomputes them. Idempotent on a day
// that is already open or reopened.
//
// actor is the signed-in user doing it; the PIN holder who authorised it is
// recorded in the audit metadata.
func Reopen(db *sql.DB, actor Actor, dayID uuid.UUID, pin string) (Day, error) {
	identity, err := staffpin.Identify(db, pin, staffpin.AdminOnly)
	if errors.Is(err, staffpin.ErrNoMatch) {
		return Day{}, ErrInvalidPin
	}
	if err != nil {
		return Day{}, err
	}
	day, err := Get(db, dayID)
	if err != nil {
		return Day{}, err
	}
	if day.IsOpen() {
		return day, nil
	}

	// uniq_business_days_single_open allows exactly one open row, so name the
	// blocker before the UPDATE rather than letting a 23505 surface as a 500.
	current, err := Current(db)
	if err != nil {
		return Day{}, err
	}
	if current != nil && current.ID != day.ID {
		return Day{}, fmt.Errorf("%w (open day %s)", ErrPreviousDayOpen, current.DateKey())
	}

	tx, err := db.Begin()
	if err != nil {
		return Day{}, err
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.Exec(`
		UPDATE business_days SET status = 'reopened', closed_at = NULL, closed_by = NULL, updated_at = now()
		WHERE id = $1 AND status = 'closed'`, dayID); err != nil {
		if util.IsUniqueViolation(err, "uniq_business_days_single_open") {
			return Day{}, ErrPreviousDayOpen
		}
		return Day{}, err
	}
	if err := writeAuditTx(tx, &dayID, day.DateKey(), "reopen", actor,
		fmt.Sprintf("Reopened %s with an admin PIN", day.DateKey()),
		map[string]any{
			"auth":                  "pin",
			"authorized_by":         identity.UserID.String(),
			"authorized_by_name":    identity.Name,
			"authorized_by_role":    identity.Role,
			"previous_closing_note": day.ClosingNotes,
		},
	); err != nil {
		return Day{}, err
	}
	if err := tx.Commit(); err != nil {
		return Day{}, err
	}
	return Get(db, dayID)
}

// ForceClose seals a day nobody counted, against an active admin's PIN and a
// written reason. counted_* is set to expected_* so every variance is exactly
// zero and the closing note says plainly that no count was taken — the row
// must never read as though someone counted and happened to agree.
func ForceClose(db *sql.DB, actor Actor, dayID uuid.UUID, pin, reason string) (Day, error) {
	reason = strings.TrimSpace(reason)
	if len(reason) < ReasonMinLen || len(reason) > ReasonMaxLen {
		return Day{}, ErrReasonRequired
	}
	identity, err := staffpin.Identify(db, pin, staffpin.AdminOnly)
	if errors.Is(err, staffpin.ErrNoMatch) {
		return Day{}, ErrInvalidPin
	}
	if err != nil {
		return Day{}, err
	}
	note := "[Force close — no count] " + reason

	// The PIN check is deliberately outside the transaction below: bcrypt
	// against every active admin takes tens of milliseconds and has nothing to
	// do with the day's state, so it must not be done holding the row lock.
	tx, err := db.Begin()
	if err != nil {
		return Day{}, err
	}
	defer tx.Rollback() //nolint:errcheck

	day, err := lockDay(tx, dayID)
	if err != nil {
		return Day{}, err
	}
	if day.Status == StatusClosed {
		return Day{}, ErrDayClosed
	}
	expected, err := ComputeExpected(tx, dayID)
	if err != nil {
		return Day{}, err
	}

	res, err := tx.Exec(`
		UPDATE business_days SET
			status = 'closed', closed_at = now(), closed_by = $1,
			counted_cash = $2, counted_card = $3, counted_online = $4,
			expected_cash = $2, expected_card = $3, expected_online = $4,
			cash_variance = 0, card_variance = 0, online_variance = 0,
			gross_sales = $5, discounts = $6, tax_collected = $7, net_sales = $8,
			on_account_sales = $9, receipts_collected = $10,
			invoice_count = $11, void_count = $12,
			closing_notes = $13, updated_at = now()
		WHERE id = $14 AND status IN ('open', 'reopened')`,
		actorRef(actor),
		expected.Cash, expected.Card, expected.Online,
		expected.GrossSales, expected.Discounts, expected.TaxCollected, expected.NetSales,
		expected.OnAccountSales, expected.ReceiptsCollected,
		expected.InvoiceCount, expected.VoidCount,
		note, dayID)
	if err != nil {
		return Day{}, err
	}
	sealed, err := res.RowsAffected()
	if err != nil {
		return Day{}, err
	}
	if sealed == 0 {
		return Day{}, ErrDayClosed
	}
	if err := writeAuditTx(tx, &dayID, day.DateKey(), "force_close", actor,
		fmt.Sprintf("Force-closed %s without a count — reason: %s", day.DateKey(), reason),
		map[string]any{
			"auth":               "pin",
			"authorized_by":      identity.UserID.String(),
			"authorized_by_name": identity.Name,
			"authorized_by_role": identity.Role,
			"reason":             reason,
			"expected_cash":      expected.Cash,
			"expected_card":      expected.Card,
			"expected_online":    expected.Online,
			"net_sales":          expected.NetSales,
		},
	); err != nil {
		return Day{}, err
	}
	if err := tx.Commit(); err != nil {
		return Day{}, err
	}
	return Get(db, dayID)
}

// writeAuditTx appends one day_close_audit_log row. The table is append-only
// (BEFORE UPDATE OR DELETE trigger in migrations/002_trading.sql), so every
// state change above records its own row inside the same transaction that made
// the change: either both land or neither does.
func writeAuditTx(q Querier, dayID *uuid.UUID, businessDate, action string, actor Actor, summary string, metadata map[string]any) error {
	var meta any
	if metadata != nil {
		b, err := json.Marshal(metadata)
		if err != nil {
			return err
		}
		meta = b
	}
	_, err := q.Exec(`
		INSERT INTO day_close_audit_log (business_day_id, business_date, action, actor_id, actor_name, actor_role, summary, metadata)
		VALUES ($1, $2::date, $3, $4, $5, $6, $7, $8::jsonb)`,
		dayID, businessDate, action, actorRef(actor), nullIfEmpty(actor.Name), nullIfEmpty(actor.Role), summary, meta)
	return err
}

// ── small helpers ────────────────────────────────────────────────────────

// round2 is paisa precision. Every figure that reaches a NUMERIC(12,2) column
// or a variance comparison passes through it, so float noise from summing
// float8 aggregates never becomes a spurious over-threshold variance.
func round2(v float64) float64 { return math.Round(v*100) / 100 }

// overThreshold compares |variance| against the threshold at paisa precision.
func overThreshold(variance, threshold float64) bool {
	return math.Abs(variance) > threshold+1e-9
}

// actorRef is the actor's id, or nil for an unauthenticated/system actor so
// the FK column takes NULL rather than the zero UUID.
func actorRef(a Actor) *uuid.UUID {
	if a.ID == uuid.Nil {
		return nil
	}
	id := a.ID
	return &id
}

func nullIfEmpty(s string) *string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return &s
}
