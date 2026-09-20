// Package reports is the read side of the business: one set of SQL
// aggregates behind the dashboard KPIs, the /reports tabs, the Z-report's
// cross-check and the Excel "period pack", so those four surfaces can never
// disagree about what a day sold (spec §6.8).
//
// Two rules hold everywhere in this package and are pinned by tests:
//
//   - Every window is on business_date, never created_at. A sale rung at
//     00:20 belongs to the day the till was open for, not to the calendar
//     day the row was written on. The one exception is Hourly, which by
//     definition buckets the wall clock — and it still selects its rows by
//     business_date, converting created_at to the business timezone
//     explicitly rather than trusting the session's TimeZone setting.
//   - Voided invoices (status = 'voided') and voided receipts (voided_at IS
//     NOT NULL) are excluded from every money total. Voids are counted, and
//     reported, separately.
//
// The money vocabulary is fixed, and matches dayops.Expected so a closed
// day's sealed business_days columns equal this package's row for that date
// (summary_db_test.go pins it):
//
//	gross      = Σ subtotal                      (before discount and tax)
//	discount   = Σ discount_amount
//	taxable    = Σ (subtotal − discount_amount)
//	tax        = Σ tax_amount                    (sales tax only)
//	furtherTax = Σ further_tax_amount            (shown separately, never folded into tax)
//	rounding   = Σ rounding_adjustment
//	net        = Σ total_payable                 (the whole-rupee figure the customer owed)
//
// so net == taxable + tax + furtherTax + rounding by construction. dayops
// seals tax_collected as tax + further tax combined; the test adds the two
// fields back together rather than this package blurring them.
//
// The package is handler-free on purpose: it takes a Querier (satisfied by
// *sql.DB and *sql.Tx), returns plain structs, and knows nothing about gin,
// HTTP status codes or the APIResponse envelope. Routes and wire shapes are
// the handlers' business.
package reports

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// dateLayout is the ISO form used on the wire and for every ::date
// parameter. Business dates are always passed to Postgres as formatted
// strings, never as time.Time: lib/pq would send a timestamptz and let the
// session zone decide which calendar day that is.
const dateLayout = "2006-01-02"

// labelLayout is the human form every response carries alongside the ISO
// date (spec §6.8: "DD-MM-YYYY labels in the response").
const labelLayout = "02-01-2006"

// Querier is satisfied by *sql.DB and *sql.Tx, so every report runs
// identically inside or outside a caller's transaction. Context-carrying
// methods only: a report is the kind of query a cancelled request should
// stop paying for.
type Querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Range is an inclusive window of business dates. Only the calendar date
// part of From/To is used; any clock time is discarded.
type Range struct {
	From time.Time
	To   time.Time
}

// keys renders the window as the two ISO strings every query binds.
func (r Range) keys() (string, string) {
	return r.From.Format(dateLayout), r.To.Format(dateLayout)
}

// Label renders the window as "DD-MM-YYYY — DD-MM-YYYY" for report headers
// and export filenames.
func (r Range) Label() string {
	return r.From.Format(labelLayout) + " — " + r.To.Format(labelLayout)
}

// TenderSplit is money broken down by how it arrived. OnAccount is credit
// sales — billed, not collected — so it is deliberately not part of any
// drawer count. It is unused for receipts, which can only be cash, card or
// online.
type TenderSplit struct {
	Cash      float64 `json:"cash"`
	Card      float64 `json:"card"`
	Online    float64 `json:"online"`
	OnAccount float64 `json:"on_account"`
}

// PeriodSummary is the one set of figures behind the dashboard KPIs, the
// daily report's totals row and the Excel period pack. From/To are ISO
// dates.
type PeriodSummary struct {
	From     string  `json:"from"`
	To       string  `json:"to"`
	Invoices int     `json:"invoices"`
	Voids    int     `json:"voids"`
	KgSold   float64 `json:"kg_sold"`

	Gross      float64 `json:"gross"`
	Discount   float64 `json:"discount"`
	Taxable    float64 `json:"taxable"`
	Tax        float64 `json:"tax"`
	FurtherTax float64 `json:"further_tax"`
	Rounding   float64 `json:"rounding"`
	Net        float64 `json:"net"`

	Tenders       TenderSplit `json:"tenders"`
	Receipts      TenderSplit `json:"receipts"`
	ReceiptsTotal float64     `json:"receipts_total"`
}

// invoiceAggSelect is the invoice half of both period queries. Every money
// sum is FILTERed to completed invoices so a void contributes to `voids`
// and to nothing else; ::float8 keeps NUMERIC out of Go's scan path, where
// lib/pq would otherwise hand back a []byte.
const invoiceAggSelect = `
		COUNT(*) FILTER (WHERE status = 'completed')                                                        AS invoices,
		COUNT(*) FILTER (WHERE status = 'voided')                                                           AS voids,
		COALESCE(SUM(subtotal)                   FILTER (WHERE status = 'completed'), 0)::float8            AS gross,
		COALESCE(SUM(discount_amount)            FILTER (WHERE status = 'completed'), 0)::float8            AS discount,
		COALESCE(SUM(subtotal - discount_amount) FILTER (WHERE status = 'completed'), 0)::float8            AS taxable,
		COALESCE(SUM(tax_amount)                 FILTER (WHERE status = 'completed'), 0)::float8            AS tax,
		COALESCE(SUM(further_tax_amount)         FILTER (WHERE status = 'completed'), 0)::float8            AS further_tax,
		COALESCE(SUM(rounding_adjustment)        FILTER (WHERE status = 'completed'), 0)::float8            AS rounding,
		COALESCE(SUM(total_payable)              FILTER (WHERE status = 'completed'), 0)::float8            AS net,
		COALESCE(SUM(total_payable) FILTER (WHERE status = 'completed' AND payment_method = 'cash'), 0)::float8   AS t_cash,
		COALESCE(SUM(total_payable) FILTER (WHERE status = 'completed' AND payment_method = 'card'), 0)::float8   AS t_card,
		COALESCE(SUM(total_payable) FILTER (WHERE status = 'completed' AND payment_method = 'online'), 0)::float8 AS t_online,
		COALESCE(SUM(total_payable) FILTER (WHERE status = 'completed' AND payment_method = 'credit'), 0)::float8 AS t_credit`

// receiptAggSelect is the receipts half. Receipts against account are money
// that arrived in a tender, so they count toward the drawer; a voided
// receipt never happened.
const receiptAggSelect = `
		COALESCE(SUM(amount) FILTER (WHERE method = 'cash'), 0)::float8   AS r_cash,
		COALESCE(SUM(amount) FILTER (WHERE method = 'card'), 0)::float8   AS r_card,
		COALESCE(SUM(amount) FILTER (WHERE method = 'online'), 0)::float8 AS r_online,
		COALESCE(SUM(amount), 0)::float8                                  AS r_total`

// loadPeriodSummarySQL is the whole-window rollup. Each CTE is an aggregate
// with no GROUP BY, so each returns exactly one row and the cross join is
// always 1×1×1.
var loadPeriodSummarySQL = fmt.Sprintf(`
	WITH inv AS (
		SELECT %s
		FROM invoices
		WHERE business_date BETWEEN $1::date AND $2::date
	), kg AS (
		SELECT COALESCE(SUM(l.quantity), 0)::float8 AS kg
		FROM invoice_lines l
		JOIN invoices i ON i.id = l.invoice_id
		WHERE i.status = 'completed' AND i.business_date BETWEEN $1::date AND $2::date
	), rec AS (
		SELECT %s
		FROM customer_receipts
		WHERE voided_at IS NULL AND business_date BETWEEN $1::date AND $2::date
	)
	SELECT inv.invoices, inv.voids, kg.kg,
	       inv.gross, inv.discount, inv.taxable, inv.tax, inv.further_tax, inv.rounding, inv.net,
	       inv.t_cash, inv.t_card, inv.t_online, inv.t_credit,
	       rec.r_cash, rec.r_card, rec.r_online, rec.r_total
	FROM inv, kg, rec`, invoiceAggSelect, receiptAggSelect)

// scanSummary reads the fixed 18-column projection both period queries end
// with, so Daily and LoadPeriodSummary can never drift apart on column
// order.
type summaryScanner interface{ Scan(dest ...any) error }

func scanSummary(row summaryScanner, s *PeriodSummary, lead ...any) error {
	dest := append(lead,
		&s.Invoices, &s.Voids, &s.KgSold,
		&s.Gross, &s.Discount, &s.Taxable, &s.Tax, &s.FurtherTax, &s.Rounding, &s.Net,
		&s.Tenders.Cash, &s.Tenders.Card, &s.Tenders.Online, &s.Tenders.OnAccount,
		&s.Receipts.Cash, &s.Receipts.Card, &s.Receipts.Online, &s.ReceiptsTotal,
	)
	return row.Scan(dest...)
}

// LoadPeriodSummary rolls the whole window into one set of figures. A window
// with no activity returns a zeroed summary carrying its own From/To, not an
// error: "nothing sold this week" is an answer, not a failure.
//
// KgSold is Σ invoice_lines.quantity over completed invoices. Every product
// in this store is sold by weight (spec §6.2), so quantity is kilogrammes;
// a future unit-sold product would add its piece count into the same total
// and this needs revisiting with the owner before that ships.
func LoadPeriodSummary(ctx context.Context, db Querier, r Range) (PeriodSummary, error) {
	from, to := r.keys()
	s := PeriodSummary{From: from, To: to}
	if err := scanSummary(db.QueryRowContext(ctx, loadPeriodSummarySQL, from, to), &s); err != nil {
		return PeriodSummary{}, err
	}
	return s, nil
}

// DailyRow is one business date's summary. BusinessDate is ISO, Label is
// DD-MM-YYYY; the embedded PeriodSummary carries From == To == BusinessDate
// so a single row can be handed to anything that renders a summary.
type DailyRow struct {
	BusinessDate string `json:"business_date"`
	Label        string `json:"label"`
	PeriodSummary
}

// dailySQL groups the same aggregates by business_date. The `days` CTE is a
// UNION of the dates that have invoices and the dates that have receipts, so
// a date whose only activity was a payment against account still gets a row,
// and a date with nothing at all is absent rather than a row of zeros.
var dailySQL = fmt.Sprintf(`
	WITH inv AS (
		SELECT business_date, %s
		FROM invoices
		WHERE business_date BETWEEN $1::date AND $2::date
		GROUP BY business_date
	), kg AS (
		SELECT i.business_date, COALESCE(SUM(l.quantity), 0)::float8 AS kg
		FROM invoice_lines l
		JOIN invoices i ON i.id = l.invoice_id
		WHERE i.status = 'completed' AND i.business_date BETWEEN $1::date AND $2::date
		GROUP BY i.business_date
	), rec AS (
		SELECT business_date, %s
		FROM customer_receipts
		WHERE voided_at IS NULL AND business_date BETWEEN $1::date AND $2::date
		GROUP BY business_date
	), days AS (
		SELECT business_date FROM inv
		UNION
		SELECT business_date FROM rec
	)
	SELECT d.business_date,
	       COALESCE(inv.invoices, 0), COALESCE(inv.voids, 0), COALESCE(kg.kg, 0),
	       COALESCE(inv.gross, 0), COALESCE(inv.discount, 0), COALESCE(inv.taxable, 0),
	       COALESCE(inv.tax, 0), COALESCE(inv.further_tax, 0), COALESCE(inv.rounding, 0), COALESCE(inv.net, 0),
	       COALESCE(inv.t_cash, 0), COALESCE(inv.t_card, 0), COALESCE(inv.t_online, 0), COALESCE(inv.t_credit, 0),
	       COALESCE(rec.r_cash, 0), COALESCE(rec.r_card, 0), COALESCE(rec.r_online, 0), COALESCE(rec.r_total, 0)
	FROM days d
	LEFT JOIN inv ON inv.business_date = d.business_date
	LEFT JOIN kg  ON kg.business_date  = d.business_date
	LEFT JOIN rec ON rec.business_date = d.business_date
	ORDER BY d.business_date`, invoiceAggSelect, receiptAggSelect)

// Daily returns one row per business date with activity, oldest first.
func Daily(ctx context.Context, db Querier, r Range) ([]DailyRow, error) {
	from, to := r.keys()
	rows, err := db.QueryContext(ctx, dailySQL, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []DailyRow{}
	for rows.Next() {
		var date time.Time
		var row DailyRow
		if err := scanSummary(rows, &row.PeriodSummary, &date); err != nil {
			return nil, err
		}
		row.BusinessDate = date.Format(dateLayout)
		row.Label = date.Format(labelLayout)
		row.From, row.To = row.BusinessDate, row.BusinessDate
		out = append(out, row)
	}
	return out, rows.Err()
}
