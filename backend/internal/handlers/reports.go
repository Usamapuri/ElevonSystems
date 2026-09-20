package handlers

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	"elevon-backend/internal/dayops"
	"elevon-backend/internal/models"
	"elevon-backend/internal/pricing"
	"elevon-backend/internal/reports"
	"elevon-backend/internal/util"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// The reports wire layer. Every figure the owner sees — the dashboard KPIs,
// a report tab, a CSV, an Excel period pack — comes from one call into
// internal/reports, so the four surfaces can never disagree about what a day
// sold (spec §6.8). No SQL and no money arithmetic lives in this file; it
// parses the window, dispatches, and renders.
//
// Three shapes are built once per request and reused across formats: the
// JSON rows (the query's own structs, so the frontend decodes the same types
// the package exports), the CSV rows (strings), and the workbook rows
// (numbers as numbers, so Excel can sum a column). Free-text columns —
// customer, product and cashier names, phone numbers, the closer's name —
// go through reports.NeutralizeXLSXCell on the CSV side too: WriteCSV
// deliberately does not neutralise (it would turn every negative variance
// into text), so the columns that carry user input are neutralised here,
// one by one, and the number columns are left alone.

// ReportsHandler serves /admin/dashboard, /admin/reports/:name and the
// single-customer ageing drill-down.
type ReportsHandler struct{ db *sql.DB }

// NewReportsHandler builds a ReportsHandler.
func NewReportsHandler(db *sql.DB) *ReportsHandler { return &ReportsHandler{db: db} }

const (
	// isoDate is the wire form of a business date; labelDate is the human
	// form every row carries alongside it (spec §6.8).
	isoDate   = "2006-01-02"
	labelDate = "02-01-2006"

	// maxRangeDaysInclusive caps a report window at a leap year. Anything
	// wider is a mis-typed year, not a question about the business, and a
	// full-table scan is not the way to find that out.
	maxRangeDaysInclusive = 366

	// dashboardSeriesDays is the long chart; the short one is its tail.
	dashboardSeriesDays = 30
	dashboardShortDays  = 7
	dashboardTopN       = 5
	dashboardRecentN    = 10
)

// errUnknownReport is the sentinel behind a 404 report_not_found.
var errUnknownReport = errors.New("handlers: unknown report")

// reportSheets names the worksheet each single-report export lands on. It
// doubles as the set of valid :name values, so adding a report here is the
// only edit needed to expose it. Every name is well inside Excel's 31-char
// sheet-name limit.
var reportSheets = map[string]string{
	"daily":       "Daily",
	"products":    "Products",
	"tax":         "Tax",
	"cashiers":    "Cashiers",
	"hourly":      "Hourly",
	"receivables": "Receivables",
	"day-closes":  "Day Closes",
}

// reportView is one report rendered into every shape the three formats need.
// Totals is nil for the two reports that are a position rather than a period
// — receivables is a balance as of a date and day closes are sealed rows, so
// a "period summary" of either would be a number nobody should add up.
type reportView struct {
	rows    any
	totals  *reports.PeriodSummary
	headers []string
	csvRows [][]string
	xlsRows [][]any
	sheet   string
}

// ── GET /admin/reports/:name ─────────────────────────────────────────────

// Report serves one report over a business-date window in JSON (default),
// CSV or .xlsx. `daily` additionally accepts `pack=1` with `format=xlsx` and
// returns the period pack: one workbook with Summary, Daily, Products, Tax,
// Cashiers and Receivables sheets.
func (h *ReportsHandler) Report(c *gin.Context) {
	name := c.Param("name")
	if _, ok := reportSheets[name]; !ok {
		c.JSON(http.StatusNotFound, models.Fail("No such report", "report_not_found"))
		return
	}
	window, ok := parseReportRange(c)
	if !ok {
		return
	}

	format := c.DefaultQuery("format", "json")
	if format != "json" && format != "csv" && format != "xlsx" {
		c.JSON(http.StatusBadRequest, models.Fail("format must be json, csv or xlsx", "invalid_format"))
		return
	}

	ctx := c.Request.Context()

	// The period pack is its own build: six reports in one workbook.
	if name == "daily" && format == "xlsx" && isTruthy(c.Query("pack")) {
		sheets, err := h.periodPack(ctx, window)
		if err != nil {
			log.Printf("reports pack: %v", err)
			c.JSON(http.StatusInternalServerError, models.Fail("Could not build the report", "internal_error"))
			return
		}
		writeWorkbookResponse(c, name, window, sheets)
		return
	}

	view, err := h.buildReport(ctx, name, window)
	if errors.Is(err, errUnknownReport) {
		c.JSON(http.StatusNotFound, models.Fail("No such report", "report_not_found"))
		return
	}
	if err != nil {
		log.Printf("reports %s: %v", name, err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not build the report", "internal_error"))
		return
	}

	switch format {
	case "csv":
		var buf bytes.Buffer
		if err := reports.WriteCSV(&buf, view.headers, view.csvRows); err != nil {
			log.Printf("reports %s: csv: %v", name, err)
			c.JSON(http.StatusInternalServerError, models.Fail("Could not build the report", "internal_error"))
			return
		}
		c.Header("Content-Disposition", contentDisposition(exportFilename(name, window, "csv")))
		c.Data(http.StatusOK, "text/csv; charset=utf-8", buf.Bytes())
	case "xlsx":
		writeWorkbookResponse(c, name, window, []reports.Sheet{{
			Name: view.sheet, Headers: view.headers, Rows: view.xlsRows,
		}})
	default:
		c.JSON(http.StatusOK, models.OK("OK", gin.H{"rows": view.rows, "totals": view.totals}))
	}
}

// parseReportRange reads ?from and ?to as ISO dates in the business
// timezone. Both default to today, so a report opened with no parameters
// answers about today rather than about all of history. A reversed window,
// an unparseable date or a span over a leap year is one error to the client:
// invalid_range. (The underlying queries return no rows for a reversed
// window rather than failing, which would look like "no sales" — so the
// check has to happen here.)
func parseReportRange(c *gin.Context) (reports.Range, bool) {
	today := util.BusinessDate(time.Now())

	from, ok := parseDateQuery(c.Query("from"))
	if !ok {
		failRange(c)
		return reports.Range{}, false
	}
	to, ok := parseDateQuery(c.Query("to"))
	if !ok {
		failRange(c)
		return reports.Range{}, false
	}
	window := reports.Range{From: today, To: today}
	if from != nil {
		window.From = *from
	}
	if to != nil {
		window.To = *to
	}
	if window.From.After(window.To) {
		failRange(c)
		return reports.Range{}, false
	}
	if days := int(window.To.Sub(window.From).Hours()/24) + 1; days > maxRangeDaysInclusive {
		failRange(c)
		return reports.Range{}, false
	}
	return window, true
}

func failRange(c *gin.Context) {
	c.JSON(http.StatusBadRequest,
		models.Fail("Give a from and to date in YYYY-MM-DD, from on or before to, at most 366 days apart", "invalid_range"))
}

// isTruthy reads a flag query parameter.
func isTruthy(s string) bool { return s == "1" || s == "true" || s == "yes" }

// exportFilename is "<report>_<from>_<to>.<ext>" with ISO dates, so a folder
// of downloads sorts itself.
func exportFilename(name string, r reports.Range, ext string) string {
	return fmt.Sprintf("%s_%s_%s.%s", name, r.From.Format(isoDate), r.To.Format(isoDate), ext)
}

func contentDisposition(filename string) string {
	return `attachment; filename="` + filename + `"`
}

// xlsxContentType is the OOXML worksheet media type; anything shorter makes
// Excel ask what the file is.
const xlsxContentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"

// writeWorkbookResponse renders sheets into a buffer first: once a status
// code is on the wire an encoding failure can no longer be reported, and a
// half-written .xlsx is a corrupt download rather than an error the owner
// can act on.
func writeWorkbookResponse(c *gin.Context, name string, r reports.Range, sheets []reports.Sheet) {
	var buf bytes.Buffer
	if err := reports.WriteWorkbook(&buf, sheets); err != nil {
		log.Printf("reports %s: xlsx: %v", name, err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not build the report", "internal_error"))
		return
	}
	c.Header("Content-Disposition", contentDisposition(exportFilename(name, r, "xlsx")))
	c.Data(http.StatusOK, xlsxContentType, buf.Bytes())
}

// ── report builders ──────────────────────────────────────────────────────

// buildReport runs one report's query and lays it out for all three formats.
func (h *ReportsHandler) buildReport(ctx context.Context, name string, r reports.Range) (*reportView, error) {
	switch name {
	case "daily":
		return h.dailyView(ctx, r)
	case "products":
		return h.productsView(ctx, r)
	case "tax":
		return h.taxView(ctx, r)
	case "cashiers":
		return h.cashiersView(ctx, r)
	case "hourly":
		return h.hourlyView(ctx, r)
	case "receivables":
		return h.receivablesView(ctx, r)
	case "day-closes":
		return h.dayClosesView(ctx, r)
	}
	return nil, errUnknownReport
}

func (h *ReportsHandler) dailyView(ctx context.Context, r reports.Range) (*reportView, error) {
	rows, err := reports.Daily(ctx, h.db, r)
	if err != nil {
		return nil, err
	}
	totals, err := reports.LoadPeriodSummary(ctx, h.db, r)
	if err != nil {
		return nil, err
	}
	v := &reportView{
		rows:   rows,
		totals: &totals,
		sheet:  reportSheets["daily"],
		headers: []string{"Date", "Invoices", "Voids", "Kg", "Gross", "Discount", "Taxable", "Tax",
			"Further Tax", "Rounding", "Net", "Cash", "Card", "Online", "On Account", "Receipts"},
	}
	for _, row := range rows {
		v.csvRows = append(v.csvRows, []string{
			row.Label, strconv.Itoa(row.Invoices), strconv.Itoa(row.Voids), kg3(row.KgSold),
			money2(row.Gross), money2(row.Discount), money2(row.Taxable), money2(row.Tax),
			money2(row.FurtherTax), money2(row.Rounding), money2(row.Net),
			money2(row.Tenders.Cash), money2(row.Tenders.Card), money2(row.Tenders.Online),
			money2(row.Tenders.OnAccount), money2(row.ReceiptsTotal),
		})
		v.xlsRows = append(v.xlsRows, []any{
			row.Label, row.Invoices, row.Voids, row.KgSold,
			row.Gross, row.Discount, row.Taxable, row.Tax,
			row.FurtherTax, row.Rounding, row.Net,
			row.Tenders.Cash, row.Tenders.Card, row.Tenders.Online,
			row.Tenders.OnAccount, row.ReceiptsTotal,
		})
	}
	return v, nil
}

func (h *ReportsHandler) productsView(ctx context.Context, r reports.Range) (*reportView, error) {
	rows, err := reports.Products(ctx, h.db, r)
	if err != nil {
		return nil, err
	}
	totals, err := reports.LoadPeriodSummary(ctx, h.db, r)
	if err != nil {
		return nil, err
	}
	v := &reportView{
		rows:    rows,
		totals:  &totals,
		sheet:   reportSheets["products"],
		headers: []string{"Product", "Kg", "Invoices", "Gross", "Share %"},
	}
	for _, row := range rows {
		v.csvRows = append(v.csvRows, []string{
			csvText(row.Name), kg3(row.Kg), strconv.Itoa(row.Invoices), money2(row.Gross), money2(row.Share),
		})
		v.xlsRows = append(v.xlsRows, []any{row.Name, row.Kg, row.Invoices, row.Gross, row.Share})
	}
	return v, nil
}

func (h *ReportsHandler) taxView(ctx context.Context, r reports.Range) (*reportView, error) {
	rows, err := reports.TaxBands(ctx, h.db, r)
	if err != nil {
		return nil, err
	}
	totals, err := reports.LoadPeriodSummary(ctx, h.db, r)
	if err != nil {
		return nil, err
	}
	v := &reportView{
		rows:    rows,
		totals:  &totals,
		sheet:   reportSheets["tax"],
		headers: []string{"Rate %", "Invoices", "Taxable", "Tax", "Further Tax"},
	}
	for _, row := range rows {
		// TaxBand.Rate is a fraction (0.18) everywhere in the API, matching
		// settings.tax_rate_*; only the export column is a percentage, and
		// it is rounded so 0.18 does not land in a cell as 18.000000000000004.
		percent := pricing.Round2(row.Rate * 100)
		v.csvRows = append(v.csvRows, []string{
			money2(percent), strconv.Itoa(row.Invoices), money2(row.Taxable), money2(row.Tax), money2(row.FurtherTax),
		})
		v.xlsRows = append(v.xlsRows, []any{percent, row.Invoices, row.Taxable, row.Tax, row.FurtherTax})
	}
	return v, nil
}

func (h *ReportsHandler) cashiersView(ctx context.Context, r reports.Range) (*reportView, error) {
	rows, err := reports.Cashiers(ctx, h.db, r)
	if err != nil {
		return nil, err
	}
	totals, err := reports.LoadPeriodSummary(ctx, h.db, r)
	if err != nil {
		return nil, err
	}
	v := &reportView{
		rows:    rows,
		totals:  &totals,
		sheet:   reportSheets["cashiers"],
		headers: []string{"Cashier", "Invoices", "Gross", "Average", "Voids"},
	}
	for _, row := range rows {
		v.csvRows = append(v.csvRows, []string{
			csvText(row.Name), strconv.Itoa(row.Invoices), money2(row.Gross), money2(row.Average), strconv.Itoa(row.Voids),
		})
		v.xlsRows = append(v.xlsRows, []any{row.Name, row.Invoices, row.Gross, row.Average, row.Voids})
	}
	return v, nil
}

func (h *ReportsHandler) hourlyView(ctx context.Context, r reports.Range) (*reportView, error) {
	rows, err := reports.Hourly(ctx, h.db, r)
	if err != nil {
		return nil, err
	}
	totals, err := reports.LoadPeriodSummary(ctx, h.db, r)
	if err != nil {
		return nil, err
	}
	v := &reportView{
		rows:    rows,
		totals:  &totals,
		sheet:   reportSheets["hourly"],
		headers: []string{"Hour", "Invoices", "Net"},
	}
	for _, row := range rows {
		label := fmt.Sprintf("%02d:00", row.Hour)
		v.csvRows = append(v.csvRows, []string{label, strconv.Itoa(row.Invoices), money2(row.Net)})
		v.xlsRows = append(v.xlsRows, []any{label, row.Invoices, row.Net})
	}
	return v, nil
}

// receivablesView is a position, not a period: the balance is as of the
// window's To date and the From date is ignored. Totals is nil for the same
// reason — a period summary next to a balance invites adding the two.
func (h *ReportsHandler) receivablesView(ctx context.Context, r reports.Range) (*reportView, error) {
	rows, err := reports.Receivables(ctx, h.db, r.To)
	if err != nil {
		return nil, err
	}
	v := &reportView{
		rows:    rows,
		sheet:   reportSheets["receivables"],
		headers: []string{"Customer", "Phone", "Balance", "0-30", "31-60", "61-90", "90+", "Last Receipt"},
	}
	for _, row := range rows {
		last := ""
		if row.LastReceipt != nil {
			last = *row.LastReceipt
		}
		v.csvRows = append(v.csvRows, []string{
			csvText(row.Name), csvText(row.Phone), money2(row.Balance),
			money2(row.B0_30), money2(row.B31_60), money2(row.B61_90), money2(row.B90), last,
		})
		v.xlsRows = append(v.xlsRows, []any{
			row.Name, row.Phone, row.Balance, row.B0_30, row.B31_60, row.B61_90, row.B90, last,
		})
	}
	return v, nil
}

func (h *ReportsHandler) dayClosesView(ctx context.Context, r reports.Range) (*reportView, error) {
	rows, err := reports.DayCloses(ctx, h.db, r)
	if err != nil {
		return nil, err
	}
	v := &reportView{
		rows:  rows,
		sheet: reportSheets["day-closes"],
		headers: []string{"Date", "Status", "Closed By", "Expected Cash", "Counted Cash",
			"Cash Variance", "Card Variance", "Online Variance", "Net"},
	}
	for _, row := range rows {
		v.csvRows = append(v.csvRows, []string{
			row.Label, row.Status, csvText(row.ClosedBy),
			money2(row.ExpectedCash), money2(row.CountedCash),
			money2(row.CashVariance), money2(row.CardVariance), money2(row.OnlineVariance), money2(row.Net),
		})
		v.xlsRows = append(v.xlsRows, []any{
			row.Label, row.Status, row.ClosedBy,
			row.ExpectedCash, row.CountedCash,
			row.CashVariance, row.CardVariance, row.OnlineVariance, row.Net,
		})
	}
	return v, nil
}

// ── the period pack ──────────────────────────────────────────────────────

// periodPack is the one workbook the owner sends the accountant: the window
// summarised, then the four period reports and the receivables position
// behind it. Sheet order is fixed and the names are stable, because someone
// will build a formula against them.
func (h *ReportsHandler) periodPack(ctx context.Context, r reports.Range) ([]reports.Sheet, error) {
	totals, err := reports.LoadPeriodSummary(ctx, h.db, r)
	if err != nil {
		return nil, err
	}
	sheets := []reports.Sheet{{
		Name:    "Summary",
		Headers: []string{"Figure", "Value"},
		Rows: [][]any{
			{"From", totals.From},
			{"To", totals.To},
			{"Invoices", totals.Invoices},
			{"Voids", totals.Voids},
			{"Kg sold", totals.KgSold},
			{"Gross", totals.Gross},
			{"Discount", totals.Discount},
			{"Taxable", totals.Taxable},
			{"Tax", totals.Tax},
			{"Further tax", totals.FurtherTax},
			{"Rounding", totals.Rounding},
			{"Net sales", totals.Net},
			{"Cash", totals.Tenders.Cash},
			{"Card", totals.Tenders.Card},
			{"Online", totals.Tenders.Online},
			{"On account", totals.Tenders.OnAccount},
			{"Receipts cash", totals.Receipts.Cash},
			{"Receipts card", totals.Receipts.Card},
			{"Receipts online", totals.Receipts.Online},
			{"Receipts total", totals.ReceiptsTotal},
		},
	}}
	for _, name := range []string{"daily", "products", "tax", "cashiers", "receivables"} {
		view, err := h.buildReport(ctx, name, r)
		if err != nil {
			return nil, err
		}
		sheets = append(sheets, reports.Sheet{Name: view.sheet, Headers: view.headers, Rows: view.xlsRows})
	}
	return sheets, nil
}

// ── GET /admin/dashboard ─────────────────────────────────────────────────

// dashboardDay is the till's state, thinned to what the dashboard banner
// needs. The full day (expected tenders, movements) is GET /day/current.
type dashboardDay struct {
	Status   string    `json:"status"`
	OpenedAt time.Time `json:"opened_at"`
}

// dashboardInvoice is a recent-sales row. Deliberately not models.Invoice:
// the dashboard lists ten sales, and shipping thirty fiscal and snapshot
// columns per row to draw six of them is noise on a screen that polls every
// 30 seconds.
type dashboardInvoice struct {
	ID            uuid.UUID `json:"id"`
	InvoiceNumber string    `json:"invoice_number"`
	BusinessDate  string    `json:"business_date"`
	Status        string    `json:"status"`
	CashierName   string    `json:"cashier_name"`
	CustomerName  *string   `json:"customer_name"`
	PaymentMethod string    `json:"payment_method"`
	TotalPayable  int64     `json:"total_payable"`
	CreatedAt     time.Time `json:"created_at"`
}

// dashboardResponse is the whole screen in one round trip (spec §3, the
// /dashboard row): today's figures, what the book is owed, the two revenue
// series, the leaders and the last few sales.
//
// TopProducts is the 30-day window, not today: at nine in the morning
// today's list is empty, and a card that is blank for the first hours of
// every day tells the owner nothing. The frontend must label it as such.
type dashboardResponse struct {
	Today                  reports.PeriodSummary `json:"today"`
	ReceivablesOutstanding float64               `json:"receivables_outstanding"`
	Series7d               []reports.DailyRow    `json:"series_7d"`
	Series30d              []reports.DailyRow    `json:"series_30d"`
	TopProducts            []reports.ProductRow  `json:"top_products"`
	RecentInvoices         []dashboardInvoice    `json:"recent_invoices"`
	Day                    *dashboardDay         `json:"day"`
}

// Dashboard answers the admin home screen.
func (h *ReportsHandler) Dashboard(c *gin.Context) {
	ctx := c.Request.Context()
	today := util.BusinessDate(time.Now())
	month := reports.Range{From: today.AddDate(0, 0, -(dashboardSeriesDays - 1)), To: today}

	fail := func(what string, err error) {
		log.Printf("dashboard %s: %v", what, err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not load the dashboard", "internal_error"))
	}

	todaySummary, err := reports.LoadPeriodSummary(ctx, h.db, reports.Range{From: today, To: today})
	if err != nil {
		fail("today", err)
		return
	}

	// One Daily query feeds both charts: the 7-day series is the tail of the
	// 30-day one, so the two can never show different figures for the same
	// date.
	daily, err := reports.Daily(ctx, h.db, month)
	if err != nil {
		fail("series", err)
		return
	}
	series30 := fillDailySeries(daily, month.From, month.To)
	series7 := series30
	if len(series30) > dashboardShortDays {
		series7 = series30[len(series30)-dashboardShortDays:]
	}

	products, err := reports.Products(ctx, h.db, month)
	if err != nil {
		fail("products", err)
		return
	}
	if len(products) > dashboardTopN {
		products = products[:dashboardTopN]
	}

	outstanding, err := h.outstandingReceivables(ctx, today)
	if err != nil {
		fail("receivables", err)
		return
	}

	recent, err := h.recentInvoices(ctx)
	if err != nil {
		fail("recent", err)
		return
	}

	day, err := currentOrTodayDay(h.db)
	if err != nil {
		fail("day", err)
		return
	}

	c.JSON(http.StatusOK, models.OK("OK", dashboardResponse{
		Today:                  todaySummary,
		ReceivablesOutstanding: outstanding,
		Series7d:               series7,
		Series30d:              series30,
		TopProducts:            products,
		RecentInvoices:         recent,
		Day:                    day,
	}))
}

// fillDailySeries pads the window so every date has a row. reports.Daily
// omits a date with no activity on purpose — a table should not carry rows
// of zeros — but a chart with a missing point draws a shorter week rather
// than a quiet Sunday, so the dashboard fills the gaps and the report does
// not.
func fillDailySeries(rows []reports.DailyRow, from, to time.Time) []reports.DailyRow {
	byDate := make(map[string]reports.DailyRow, len(rows))
	for _, row := range rows {
		byDate[row.BusinessDate] = row
	}
	out := []reports.DailyRow{}
	for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
		key := d.Format(isoDate)
		if row, ok := byDate[key]; ok {
			out = append(out, row)
			continue
		}
		out = append(out, reports.DailyRow{
			BusinessDate:  key,
			Label:         d.Format(labelDate),
			PeriodSummary: reports.PeriodSummary{From: key, To: key},
		})
	}
	return out
}

// outstandingReceivables is what customers owe the store as of asOf: the sum
// of the positive balances only. A negative balance is an advance the
// customer has paid ahead — a liability, not a receivable — and netting it
// off would understate the debt the owner is actually chasing.
func (h *ReportsHandler) outstandingReceivables(ctx context.Context, asOf time.Time) (float64, error) {
	rows, err := reports.Receivables(ctx, h.db, asOf)
	if err != nil {
		return 0, err
	}
	var total float64
	for _, row := range rows {
		if row.Balance > 0 {
			total += row.Balance
		}
	}
	return pricing.Round2(total), nil
}

// recentInvoices is the last few sales, voids included: a void the cashier
// just did is exactly what the owner opened this screen to see.
func (h *ReportsHandler) recentInvoices(ctx context.Context) ([]dashboardInvoice, error) {
	rows, err := h.db.QueryContext(ctx, `
		SELECT id, invoice_number, business_date, status, cashier_name, customer_name,
		       payment_method, total_payable::bigint, created_at
		FROM invoices
		ORDER BY created_at DESC, invoice_number DESC
		LIMIT $1`, dashboardRecentN)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []dashboardInvoice{}
	for rows.Next() {
		var inv dashboardInvoice
		var date time.Time
		if err := rows.Scan(&inv.ID, &inv.InvoiceNumber, &date, &inv.Status, &inv.CashierName,
			&inv.CustomerName, &inv.PaymentMethod, &inv.TotalPayable, &inv.CreatedAt); err != nil {
			return nil, err
		}
		inv.BusinessDate = date.Format(isoDate)
		out = append(out, inv)
	}
	return out, rows.Err()
}

// currentOrTodayDay is the day banner: whatever is open, else today's sealed
// row, else nothing at all (the till has not been started today).
func currentOrTodayDay(db *sql.DB) (*dashboardDay, error) {
	day, err := dayops.Current(db)
	if err != nil {
		return nil, err
	}
	if day == nil {
		if day, err = dayops.Today(db); err != nil {
			return nil, err
		}
	}
	if day == nil {
		return nil, nil
	}
	return &dashboardDay{Status: day.Status, OpenedAt: day.OpenedAt}, nil
}

// ── GET /customers/:id/ageing ────────────────────────────────────────────

// Ageing is the single-customer drill-down behind the receivables report and
// the Receive Payment screen. It runs the same reports.Receivables the
// report does and picks this customer's row out of it, rather than a second
// ageing implementation that could disagree with the first — one store's
// ledger is small enough that the extra rows cost nothing, and the two
// numbers matching matters more.
//
// A customer with a settled account is absent from that report (it lists
// non-zero balances only) and gets a zeroed row carrying their name, phone
// and the date they last paid, so the screen renders "nothing outstanding"
// instead of an error.
func (h *ReportsHandler) Ageing(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, models.Fail("Customer not found", "customer_not_found"))
		return
	}
	ctx := c.Request.Context()

	var name, phone string
	err = h.db.QueryRowContext(ctx,
		`SELECT name, COALESCE(phone, '') FROM customers WHERE id = $1`, id).Scan(&name, &phone)
	if errors.Is(err, sql.ErrNoRows) {
		c.JSON(http.StatusNotFound, models.Fail("Customer not found", "customer_not_found"))
		return
	}
	if err != nil {
		log.Printf("ageing: customer: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not load the account", "internal_error"))
		return
	}

	asOf := util.BusinessDate(time.Now())
	rows, err := reports.Receivables(ctx, h.db, asOf)
	if err != nil {
		log.Printf("ageing: receivables: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not load the account", "internal_error"))
		return
	}
	for _, row := range rows {
		if row.CustomerID == id {
			c.JSON(http.StatusOK, models.OK("OK", row))
			return
		}
	}

	zero := reports.ReceivableRow{CustomerID: id, Name: name, Phone: phone}
	var last sql.NullTime
	if err := h.db.QueryRowContext(ctx, `
		SELECT MAX(business_date) FROM customer_receipts
		WHERE customer_id = $1 AND voided_at IS NULL AND business_date <= $2::date`,
		id, asOf.Format(isoDate)).Scan(&last); err != nil {
		log.Printf("ageing: last receipt: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not load the account", "internal_error"))
		return
	}
	if last.Valid {
		key := last.Time.Format(isoDate)
		zero.LastReceipt = &key
	}
	c.JSON(http.StatusOK, models.OK("OK", zero))
}

// ── formatting ───────────────────────────────────────────────────────────

// money2 renders a rupee figure with two decimals and no thousands
// separator, so a spreadsheet parses it as a number.
func money2(v float64) string { return strconv.FormatFloat(v, 'f', 2, 64) }

// kg3 renders a weight at the three decimals the till works in.
func kg3(v float64) string { return strconv.FormatFloat(v, 'f', 3, 64) }

// csvText hardens one free-text CSV column against spreadsheet formula
// injection. WriteCSV does not neutralise on its own (it would mangle every
// negative number), so the columns carrying user input are named one by one
// here; number columns never pass through it.
func csvText(s string) string {
	if out, ok := reports.NeutralizeXLSXCell(s).(string); ok {
		return out
	}
	return s
}
