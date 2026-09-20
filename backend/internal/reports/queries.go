package reports

import (
	"context"
	"math"
	"sort"
	"time"

	"elevon-backend/internal/pricing"
	"elevon-backend/internal/util"

	"github.com/google/uuid"
)

// ── products ─────────────────────────────────────────────────────────────

// ProductRow is one product's slice of the window. ProductID is nil when the
// product has since been deleted (invoice_lines.product_id is ON DELETE SET
// NULL); the line's snapshot name is used in that case, which is why the
// invoice keeps one. Share is a percentage of the window's gross, so the
// column sums to 100 whenever anything was sold.
type ProductRow struct {
	ProductID *uuid.UUID `json:"product_id"`
	Name      string     `json:"name"`
	Kg        float64    `json:"kg"`
	Invoices  int        `json:"invoices"`
	Gross     float64    `json:"gross"`
	Share     float64    `json:"share"`
}

// Products returns kg and gross by product over the window, biggest first.
//
// Gross is Σ line_total — the line's own contribution to the invoice
// subtotal, before the invoice-level discount, which has no per-product
// allocation and must not be guessed at one. That keeps Σ ProductRow.Gross
// equal to PeriodSummary.Gross and the Share column honest.
//
// Rows are grouped by product_id, not by name, so renaming a product
// mid-window does not split its row; the current products.name wins over the
// line snapshot for the label.
//
// Deliberately, every deleted product collapses into ONE row: product_id is
// NULL for a line whose product has since been deleted, and Postgres GROUP BY
// treats every NULL as the same group, so `GROUP BY l.product_id, p.name`
// bundles all of them together rather than one row per (now-gone) product.
// That row's label falls back to MIN(l.product_name) — whichever deleted
// product's snapshot name sorts first — which reads oddly with more than one
// deleted product in the window, but the kg/gross/invoices it totals are
// still exactly right; a report cannot single out which deleted product a
// past line belonged to without the row that named it, and merging them is
// preferable to silently dropping their money from the total.
func Products(ctx context.Context, db Querier, r Range) ([]ProductRow, error) {
	from, to := r.keys()
	rows, err := db.QueryContext(ctx, `
		SELECT l.product_id,
		       COALESCE(p.name, MIN(l.product_name))          AS name,
		       COALESCE(SUM(l.quantity), 0)::float8           AS kg,
		       COUNT(DISTINCT l.invoice_id)                   AS invoices,
		       COALESCE(SUM(l.line_total), 0)::float8         AS gross
		FROM invoice_lines l
		JOIN invoices i ON i.id = l.invoice_id
		LEFT JOIN products p ON p.id = l.product_id
		WHERE i.status = 'completed'
		  AND i.business_date BETWEEN $1::date AND $2::date
		GROUP BY l.product_id, p.name
		ORDER BY gross DESC, name ASC`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []ProductRow{}
	var total float64
	for rows.Next() {
		var row ProductRow
		if err := rows.Scan(&row.ProductID, &row.Name, &row.Kg, &row.Invoices, &row.Gross); err != nil {
			return nil, err
		}
		total += row.Gross
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if total != 0 {
		for i := range out {
			out[i].Share = pricing.Round2(out[i].Gross / total * 100)
		}
	}
	return out, nil
}

// ── tax bands ────────────────────────────────────────────────────────────

// TaxBand is one effective rate band. Rate is the invoice's own tax_rate
// snapshot as a fraction (0.18, not 18) — the same convention as
// settings.tax_rate_* and pricing.Input.TaxRate — so a rate change
// mid-window splits into two bands with the rate each invoice was actually
// charged at, not today's rate.
type TaxBand struct {
	Rate       float64 `json:"rate"`
	Invoices   int     `json:"invoices"`
	Taxable    float64 `json:"taxable"`
	Tax        float64 `json:"tax"`
	FurtherTax float64 `json:"further_tax"`
}

// TaxBands groups the window's completed invoices by their snapshot tax
// rate. Every invoice carries exactly one tax_rate and lands in exactly one
// band, so Σ TaxBand.Tax == Σ invoices.tax_amount for the window by
// construction — the accountant's cross-check can never be off by a
// rounding step (queries_db_test.go pins it).
func TaxBands(ctx context.Context, db Querier, r Range) ([]TaxBand, error) {
	from, to := r.keys()
	rows, err := db.QueryContext(ctx, `
		SELECT tax_rate::float8                                       AS rate,
		       COUNT(*)                                               AS invoices,
		       COALESCE(SUM(subtotal - discount_amount), 0)::float8    AS taxable,
		       COALESCE(SUM(tax_amount), 0)::float8                    AS tax,
		       COALESCE(SUM(further_tax_amount), 0)::float8            AS further_tax
		FROM invoices
		WHERE status = 'completed'
		  AND business_date BETWEEN $1::date AND $2::date
		GROUP BY tax_rate
		ORDER BY tax_rate`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []TaxBand{}
	for rows.Next() {
		var b TaxBand
		if err := rows.Scan(&b.Rate, &b.Invoices, &b.Taxable, &b.Tax, &b.FurtherTax); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// ── cashiers ─────────────────────────────────────────────────────────────

// CashierRow is one cashier's window. Gross is Σ subtotal, the same
// definition the word carries everywhere else in this package, and Average
// is that gross over the completed invoice count. Voids are counted but
// contribute no money. CashierID is nil for invoices whose user row was
// deleted; the invoice's cashier_name snapshot names them.
type CashierRow struct {
	CashierID *uuid.UUID `json:"cashier_id"`
	Name      string     `json:"name"`
	Invoices  int        `json:"invoices"`
	Gross     float64    `json:"gross"`
	Average   float64    `json:"average"`
	Voids     int        `json:"voids"`
}

// Cashiers returns per-cashier totals over the window, biggest first.
//
// Deliberately, every invoice whose cashier's user row has since been deleted
// collapses into ONE row, the same way Products bundles deleted products:
// cashier_id is NULL for those invoices, GROUP BY treats every NULL as one
// group, and the label falls back to MIN(i.cashier_name) — whichever
// invoice's cashier_name snapshot sorts first — rather than the individual
// deleted user. The totals are still exact; only the single label is a
// stand-in for however many different deleted cashiers are in the window.
func Cashiers(ctx context.Context, db Querier, r Range) ([]CashierRow, error) {
	from, to := r.keys()
	rows, err := db.QueryContext(ctx, `
		SELECT i.cashier_id,
		       COALESCE(
		           NULLIF(TRIM(COALESCE(u.first_name, '') || ' ' || COALESCE(u.last_name, '')), ''),
		           u.username,
		           NULLIF(MIN(i.cashier_name), ''),
		           ''
		       )                                                                          AS name,
		       COUNT(*) FILTER (WHERE i.status = 'completed')                              AS invoices,
		       COALESCE(SUM(i.subtotal) FILTER (WHERE i.status = 'completed'), 0)::float8  AS gross,
		       COUNT(*) FILTER (WHERE i.status = 'voided')                                 AS voids
		FROM invoices i
		LEFT JOIN users u ON u.id = i.cashier_id
		WHERE i.business_date BETWEEN $1::date AND $2::date
		GROUP BY i.cashier_id, u.first_name, u.last_name, u.username
		ORDER BY gross DESC, name ASC`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []CashierRow{}
	for rows.Next() {
		var c CashierRow
		if err := rows.Scan(&c.CashierID, &c.Name, &c.Invoices, &c.Gross, &c.Voids); err != nil {
			return nil, err
		}
		if c.Invoices > 0 {
			c.Average = pricing.Round2(c.Gross / float64(c.Invoices))
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ── hourly ───────────────────────────────────────────────────────────────

// HourRow is one hour-of-day bucket, 0–23 in the business timezone.
type HourRow struct {
	Hour     int     `json:"hour"`
	Invoices int     `json:"invoices"`
	Net      float64 `json:"net"`
}

// Hourly buckets the window's completed invoices by hour of day. Rows are
// selected by business_date like every other report, but bucketed on
// created_at — the wall clock is the whole point of the chart.
//
// The conversion is explicit (AT TIME ZONE 'Asia/Karachi') rather than
// leaning on the session's TimeZone: the pool does set it
// (database.injectBusinessTimezoneOption), but a report that silently
// re-buckets itself if that option is ever dropped is a report nobody can
// trust. A sale at 23:30 UTC is a 04:30 sale here, and lands in hour 4.
//
// All 24 buckets are always returned, in order, including empty ones, so the
// dashboard chart has a fixed x-axis.
func Hourly(ctx context.Context, db Querier, r Range) ([]HourRow, error) {
	from, to := r.keys()
	rows, err := db.QueryContext(ctx, `
		SELECT h.hour::int,
		       COALESCE(a.invoices, 0),
		       COALESCE(a.net, 0)::float8
		FROM generate_series(0, 23) AS h(hour)
		LEFT JOIN (
			SELECT EXTRACT(HOUR FROM created_at AT TIME ZONE $3)::int AS hour,
			       COUNT(*)                                           AS invoices,
			       COALESCE(SUM(total_payable), 0)::float8            AS net
			FROM invoices
			WHERE status = 'completed'
			  AND business_date BETWEEN $1::date AND $2::date
			GROUP BY 1
		) a ON a.hour = h.hour
		ORDER BY h.hour`, from, to, util.BusinessTimezoneName())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]HourRow, 0, 24)
	for rows.Next() {
		var h HourRow
		if err := rows.Scan(&h.Hour, &h.Invoices, &h.Net); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// ── receivables and ageing ───────────────────────────────────────────────

// ReceivableRow is one customer's outstanding account as of a date. Balance
// is Σ debit − Σ credit over every ledger entry dated on or before asOf; a
// negative balance is an advance the customer has paid ahead. The four
// buckets age the *unpaid* part of the balance and therefore sum to Balance
// whenever Balance is positive, and are all zero when it is not.
// LastReceipt is an ISO date, nil when the customer has never paid.
type ReceivableRow struct {
	CustomerID  uuid.UUID `json:"customer_id"`
	Name        string    `json:"name"`
	Phone       string    `json:"phone"`
	Balance     float64   `json:"balance"`
	B0_30       float64   `json:"b0_30"`
	B31_60      float64   `json:"b31_60"`
	B61_90      float64   `json:"b61_90"`
	B90         float64   `json:"b90"`
	LastReceipt *string   `json:"last_receipt"`
}

// openItem is one unsettled debit while FIFO allocation runs.
type openItem struct {
	date   time.Time
	amount float64
}

// Receivables returns every customer with a non-zero balance as of asOf,
// largest debt first, with the debt aged into 0–30 / 31–60 / 61–90 / 90+ day
// buckets.
//
// Ageing is FIFO and computed in Go rather than SQL: payments settle the
// oldest invoice first, which is how the owner and the customer both read a
// statement, and which no single aggregate query expresses. Walking the
// ledger in (business_date, created_at, id) order also makes the result
// deterministic — two entries on the same day always allocate in the order
// they were posted.
//
// Worked example, the one the test pins: an invoice of 1,000 seventy days
// ago, an invoice of 2,000 twenty days ago, a receipt of 1,200 ten days ago.
// Balance is 1,800. The receipt clears the 1,000 invoice entirely and 200 of
// the newer one, leaving 1,800 outstanding against a twenty-day-old invoice
// — so the whole balance sits in 0–30 and the 90+ bucket is empty, even
// though there is a seventy-day-old invoice in the ledger. Ageing the
// invoice instead of the debt would have shown a 1,000 rupee "90+" that the
// customer already paid.
//
// Voided invoices and voided receipts never reach this report: the void
// posts its own reversing ledger entry (entry_type invoice_void /
// receipt_void), so the append-only ledger nets out on its own.
func Receivables(ctx context.Context, db Querier, asOf time.Time) ([]ReceivableRow, error) {
	asOfKey := asOf.Format(dateLayout)
	asOfDay, err := time.Parse(dateLayout, asOfKey)
	if err != nil {
		return nil, err
	}

	lastReceipts, err := lastReceiptByCustomer(ctx, db, asOfKey)
	if err != nil {
		return nil, err
	}

	rows, err := db.QueryContext(ctx, `
		SELECT e.customer_id, c.name, COALESCE(c.phone, ''),
		       e.business_date, e.debit::float8, e.credit::float8
		FROM customer_ledger_entries e
		JOIN customers c ON c.id = e.customer_id
		WHERE e.business_date <= $1::date
		ORDER BY e.customer_id, e.business_date, e.created_at, e.id`, asOfKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type acc struct {
		row    ReceivableRow
		debits []openItem
		credit float64
	}
	byCustomer := map[uuid.UUID]*acc{}
	order := []uuid.UUID{}

	for rows.Next() {
		var (
			id          uuid.UUID
			name, phone string
			date        time.Time
			debit, cred float64
		)
		if err := rows.Scan(&id, &name, &phone, &date, &debit, &cred); err != nil {
			return nil, err
		}
		a := byCustomer[id]
		if a == nil {
			a = &acc{row: ReceivableRow{CustomerID: id, Name: name, Phone: phone, LastReceipt: lastReceipts[id]}}
			byCustomer[id] = a
			order = append(order, id)
		}
		a.row.Balance += debit - cred
		if debit > 0 {
			a.debits = append(a.debits, openItem{date: dayIndex(date), amount: debit})
		}
		if cred > 0 {
			a.credit += cred
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	out := []ReceivableRow{}
	for _, id := range order {
		a := byCustomer[id]
		a.row.Balance = pricing.Round2(a.row.Balance)
		if a.row.Balance == 0 {
			continue
		}
		// Settle credits against the oldest debits first; whatever is left
		// unpaid is what gets aged.
		pool := a.credit
		for i := range a.debits {
			if pool <= 0 {
				break
			}
			paid := math.Min(pool, a.debits[i].amount)
			a.debits[i].amount -= paid
			pool -= paid
		}
		for _, item := range a.debits {
			if item.amount <= 0 {
				continue
			}
			switch days := int(asOfDay.Sub(item.date).Hours() / 24); {
			case days <= 30:
				a.row.B0_30 += item.amount
			case days <= 60:
				a.row.B31_60 += item.amount
			case days <= 90:
				a.row.B61_90 += item.amount
			default:
				a.row.B90 += item.amount
			}
		}
		a.row.B0_30 = pricing.Round2(a.row.B0_30)
		a.row.B31_60 = pricing.Round2(a.row.B31_60)
		a.row.B61_90 = pricing.Round2(a.row.B61_90)
		a.row.B90 = pricing.Round2(a.row.B90)
		out = append(out, a.row)
	}
	// Biggest debt first; name then id break ties so the list is stable
	// across runs and across pages.
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Balance != out[j].Balance {
			return out[i].Balance > out[j].Balance
		}
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].CustomerID.String() < out[j].CustomerID.String()
	})
	return out, nil
}

// lastReceiptByCustomer maps each customer to the ISO business date of their
// most recent non-voided receipt on or before asOf.
func lastReceiptByCustomer(ctx context.Context, db Querier, asOfKey string) (map[uuid.UUID]*string, error) {
	rows, err := db.QueryContext(ctx, `
		SELECT customer_id, MAX(business_date)
		FROM customer_receipts
		WHERE voided_at IS NULL AND business_date <= $1::date
		GROUP BY customer_id`, asOfKey)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uuid.UUID]*string{}
	for rows.Next() {
		var id uuid.UUID
		var date time.Time
		if err := rows.Scan(&id, &date); err != nil {
			return nil, err
		}
		key := date.Format(dateLayout)
		out[id] = &key
	}
	return out, rows.Err()
}

// dayIndex strips a scanned DATE back to a bare UTC midnight so day
// arithmetic is exact. lib/pq hands a DATE back as a time.Time whose
// location depends on the driver's view of the session, and subtracting two
// times in different zones is off by the offset — enough to push a 30-day
// debt into the 31–60 bucket.
func dayIndex(t time.Time) time.Time {
	d, err := time.Parse(dateLayout, t.Format(dateLayout))
	if err != nil {
		return t
	}
	return d
}

// ── day closes ───────────────────────────────────────────────────────────

// DayCloseRow is one business day's close. Status is the day's own status,
// so an open or reopened day in the window is listed with its variance
// columns at zero rather than hidden — an unclosed day inside a reporting
// window is exactly what the owner needs to see. Net is the sealed
// net_sales, which for a closed day is what the Z-report printed.
type DayCloseRow struct {
	ID             uuid.UUID `json:"id"`
	BusinessDate   string    `json:"business_date"`
	Label          string    `json:"label"`
	Status         string    `json:"status"`
	ClosedBy       string    `json:"closed_by"`
	ExpectedCash   float64   `json:"expected_cash"`
	CountedCash    float64   `json:"counted_cash"`
	CashVariance   float64   `json:"cash_variance"`
	CardVariance   float64   `json:"card_variance"`
	OnlineVariance float64   `json:"online_variance"`
	Net            float64   `json:"net"`
}

// DayCloses lists the window's business days, newest first.
func DayCloses(ctx context.Context, db Querier, r Range) ([]DayCloseRow, error) {
	from, to := r.keys()
	rows, err := db.QueryContext(ctx, `
		SELECT bd.id, bd.business_date, bd.status,
		       COALESCE(
		           NULLIF(TRIM(COALESCE(u.first_name, '') || ' ' || COALESCE(u.last_name, '')), ''),
		           u.username,
		           ''
		       )                                       AS closed_by,
		       COALESCE(bd.expected_cash, 0)::float8   AS expected_cash,
		       COALESCE(bd.counted_cash, 0)::float8    AS counted_cash,
		       COALESCE(bd.cash_variance, 0)::float8   AS cash_variance,
		       COALESCE(bd.card_variance, 0)::float8   AS card_variance,
		       COALESCE(bd.online_variance, 0)::float8 AS online_variance,
		       COALESCE(bd.net_sales, 0)::float8       AS net
		FROM business_days bd
		LEFT JOIN users u ON u.id = bd.closed_by
		WHERE bd.business_date BETWEEN $1::date AND $2::date
		ORDER BY bd.business_date DESC`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []DayCloseRow{}
	for rows.Next() {
		var d DayCloseRow
		var date time.Time
		if err := rows.Scan(&d.ID, &date, &d.Status, &d.ClosedBy,
			&d.ExpectedCash, &d.CountedCash, &d.CashVariance, &d.CardVariance, &d.OnlineVariance, &d.Net); err != nil {
			return nil, err
		}
		d.BusinessDate = date.Format(dateLayout)
		d.Label = date.Format(labelLayout)
		out = append(out, d)
	}
	return out, rows.Err()
}
