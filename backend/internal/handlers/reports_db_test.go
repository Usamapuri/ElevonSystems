package handlers

import (
	"bytes"
	"database/sql"
	"encoding/csv"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"elevon-backend/internal/dayops"
	"elevon-backend/internal/reports"
	"elevon-backend/internal/testdb"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/xuri/excelize/v2"
)

// reportsRouter mounts the read side on exactly the paths routes.go uses, so
// a gin conflict between /admin/reports/:name and the rest of the admin
// group, or between /customers/:id/ageing and the receipts routes, fails
// here rather than at boot.
func reportsRouter(db *sql.DB, a actor) *gin.Engine {
	r := gin.New()
	h := NewReportsHandler(db)
	admin := r.Group("/admin", asActor(a))
	admin.GET("/dashboard", h.Dashboard)
	admin.GET("/reports/:name", h.Report)
	staff := r.Group("", asActor(a))
	staff.GET("/customers/:id/ageing", h.Ageing)
	return r
}

// ── the fixture ──────────────────────────────────────────────────────────

// reportsFixture is one business day, dated today so the dashboard (which
// always asks about util.BusinessDate(now)) and the report endpoints (which
// take an explicit window) can be checked against the same hand-computed
// figures:
//
//	INV-R001 cash   1000.00 + 18% = 1180   (LPG Domestic,   4.000 kg)
//	INV-R002 credit 2000.00 + 18% = 2360   (LPG Commercial, 8.000 kg) — on account
//	INV-R003 VOIDED  300.00 + 18% =  354   (LPG Domestic,   1.200 kg) — excluded everywhere
//	REC-R001 cash 500 against the credit customer's account
//
//	→ invoices 2, voids 1, kg 12.000, gross 3000, discount 0, taxable 3000,
//	  tax 540, further 0, rounding 0, net 3540
//	  tenders cash 1180 / on-account 2360 · receipts cash 500, total 500
//	  ledger: debit 2360, credit 500 → balance 1860, all of it 0–30 days old
type reportsFixture struct {
	today            time.Time
	todayKey         string
	owner, cashier   uuid.UUID
	dayID            uuid.UUID
	domestic, commer uuid.UUID
	credit, settled  uuid.UUID
}

const (
	fxInvoices  = 2
	fxVoids     = 1
	fxKg        = 12.000
	fxGross     = 3000.00
	fxTax       = 540.00
	fxNet       = 3540.00
	fxCash      = 1180.00
	fxOnAccount = 2360.00
	fxReceipts  = 500.00
	fxBalance   = 1860.00
)

func seedReportsFixture(t *testing.T, db *sql.DB) reportsFixture {
	t.Helper()
	f := reportsFixture{today: businessToday()}
	f.todayKey = f.today.Format(dayops.DateLayout)

	f.owner = seedUser(t, db, "rpt-owner", "admin", "owner-pass-1", nil)
	f.cashier = seedUser(t, db, "rpt-ali", "counter", "counter-pass", nil)
	f.dayID = openBusinessDay(t, db, f.owner, f.today)
	f.domestic = seedProductRow(t, db, "LPG Domestic", 250)
	f.commer = seedProductRow(t, db, "LPG Commercial", 250)
	f.credit = seedCustomerRow(t, db, "Gas Traders", true, nil)
	f.settled = seedCustomerRow(t, db, "Settled Traders", true, nil)

	inv1 := rptInvoice(t, db, f, "INV-R001", "completed", "cash", nil, 1000, 180, 1180)
	rptLine(t, db, inv1, f.domestic, "LPG Domestic", 4.000, 250, 1000)

	inv2 := rptInvoice(t, db, f, "INV-R002", "completed", "credit", &f.credit, 2000, 360, 2360)
	rptLine(t, db, inv2, f.commer, "LPG Commercial", 8.000, 250, 2000)

	inv3 := rptInvoice(t, db, f, "INV-R003", "voided", "cash", nil, 300, 54, 354)
	rptLine(t, db, inv3, f.domestic, "LPG Domestic", 1.200, 250, 300)

	receipt := rptReceipt(t, db, f, "REC-R001", f.credit, fxReceipts, "cash")
	rptLedger(t, db, f, "invoice", &inv2, nil, fxOnAccount, 0)
	rptLedger(t, db, f, "receipt", nil, &receipt, 0, fxReceipts)

	return f
}

// rptInvoice writes an invoice directly: the create API would not let a test
// choose an invoice number, a void or a tender split, which is exactly what
// a reporting fixture needs to control.
func rptInvoice(t *testing.T, db *sql.DB, f reportsFixture, number, status, method string,
	customer *uuid.UUID, subtotal, tax, payable float64) uuid.UUID {
	t.Helper()
	var voidedAt any
	if status == "voided" {
		voidedAt = time.Now()
	}
	var id uuid.UUID
	if err := db.QueryRow(`
		INSERT INTO invoices (invoice_number, business_day_id, business_date, status,
			cashier_id, cashier_name, customer_id,
			subtotal, discount_amount, tax_rate, tax_amount, further_tax_amount,
			total_amount, rounding_adjustment, total_payable, payment_method, voided_at)
		VALUES ($1, $2, $3::date, $4, $5, 'ali', $6, $7, 0, 0.18, $8, 0, $9, 0, $9, $10, $11)
		RETURNING id`,
		number, f.dayID, f.todayKey, status, f.cashier, customer,
		subtotal, tax, payable, method, voidedAt).Scan(&id); err != nil {
		t.Fatalf("seed invoice %s: %v", number, err)
	}
	return id
}

func rptLine(t *testing.T, db *sql.DB, invoiceID, productID uuid.UUID, name string, qty, unitPrice, lineTotal float64) {
	t.Helper()
	if _, err := db.Exec(`
		INSERT INTO invoice_lines (invoice_id, product_id, product_name, quantity, unit_price, line_total)
		VALUES ($1, $2, $3, $4, $5, $6)`, invoiceID, productID, name, qty, unitPrice, lineTotal); err != nil {
		t.Fatalf("seed line %s: %v", name, err)
	}
}

func rptReceipt(t *testing.T, db *sql.DB, f reportsFixture, number string, customer uuid.UUID,
	amount float64, method string) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := db.QueryRow(`
		INSERT INTO customer_receipts (receipt_number, customer_id, amount, method, business_day_id, business_date)
		VALUES ($1, $2, $3, $4, $5, $6::date) RETURNING id`,
		number, customer, amount, method, f.dayID, f.todayKey).Scan(&id); err != nil {
		t.Fatalf("seed receipt %s: %v", number, err)
	}
	return id
}

func rptLedger(t *testing.T, db *sql.DB, f reportsFixture, entryType string,
	invoiceID, receiptID *uuid.UUID, debit, credit float64) {
	t.Helper()
	if _, err := db.Exec(`
		INSERT INTO customer_ledger_entries (customer_id, entry_type, invoice_id, receipt_id, debit, credit, business_date)
		VALUES ($1, $2, $3, $4, $5, $6, $7::date)`,
		f.credit, entryType, invoiceID, receiptID, debit, credit, f.todayKey); err != nil {
		t.Fatalf("seed ledger %s: %v", entryType, err)
	}
}

// todayWindow is the query string every report test uses.
func (f reportsFixture) todayWindow() string {
	return "?from=" + f.todayKey + "&to=" + f.todayKey
}

// ── JSON ─────────────────────────────────────────────────────────────────

// reportEnvelope is the JSON body of every report: the rows stay raw so each
// test decodes the shape it cares about, and totals is nil for the two
// reports that are a position rather than a period.
type reportEnvelope struct {
	Rows   []map[string]any       `json:"rows"`
	Totals *reports.PeriodSummary `json:"totals"`
}

func getReport(t *testing.T, r http.Handler, name, query string) (*httptest.ResponseRecorder, reportEnvelope) {
	t.Helper()
	w := doJSON(r, http.MethodGet, "/admin/reports/"+name+query, nil)
	var out reportEnvelope
	if w.Code == http.StatusOK {
		dataAs(t, decodeEnvelope(t, w), &out)
	}
	return w, out
}

// Every report name answers 200 with the seeded figures. The totals block is
// the same reports.LoadPeriodSummary the dashboard and the Z-report use, so
// checking it here pins that all three agree about this day.
func TestReports_EachNameReturnsSeededTotals(t *testing.T) {
	db := testdb.Fresh(t)
	f := seedReportsFixture(t, db)
	r := reportsRouter(db, actor{id: f.owner, username: "rpt-owner", role: "admin"})

	// The four period reports carry the same totals block.
	for _, name := range []string{"daily", "products", "tax", "cashiers", "hourly"} {
		w, body := getReport(t, r, name, f.todayWindow())
		if w.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", name, w.Code, w.Body.String())
		}
		if body.Totals == nil {
			t.Fatalf("%s: totals missing", name)
		}
		if body.Totals.Invoices != fxInvoices || body.Totals.Voids != fxVoids {
			t.Errorf("%s: invoices %d voids %d, want %d/%d",
				name, body.Totals.Invoices, body.Totals.Voids, fxInvoices, fxVoids)
		}
		if !sameMoney(body.Totals.Net, fxNet) || !sameMoney(body.Totals.Gross, fxGross) ||
			!sameMoney(body.Totals.Tax, fxTax) || !sameMoney(body.Totals.KgSold, fxKg) {
			t.Errorf("%s: gross %.2f tax %.2f net %.2f kg %.3f, want %.2f/%.2f/%.2f/%.3f",
				name, body.Totals.Gross, body.Totals.Tax, body.Totals.Net, body.Totals.KgSold,
				fxGross, fxTax, fxNet, fxKg)
		}
		if !sameMoney(body.Totals.Tenders.Cash, fxCash) || !sameMoney(body.Totals.Tenders.OnAccount, fxOnAccount) {
			t.Errorf("%s: tenders cash %.2f on-account %.2f, want %.2f/%.2f",
				name, body.Totals.Tenders.Cash, body.Totals.Tenders.OnAccount, fxCash, fxOnAccount)
		}
		if !sameMoney(body.Totals.ReceiptsTotal, fxReceipts) {
			t.Errorf("%s: receipts %.2f, want %.2f", name, body.Totals.ReceiptsTotal, fxReceipts)
		}
	}

	// Row counts and a value from each shape.
	daily := decodeRows[reports.DailyRow](t, r, "daily", f.todayWindow())
	if len(daily) != 1 || daily[0].BusinessDate != f.todayKey || !sameMoney(daily[0].Net, fxNet) {
		t.Errorf("daily rows = %+v", daily)
	}

	products := decodeRows[reports.ProductRow](t, r, "products", f.todayWindow())
	if len(products) != 2 {
		t.Fatalf("products rows = %d, want 2 (the voided invoice's product must not add a row of its own)", len(products))
	}
	// Biggest gross first; the voided 300 never reaches either row.
	if products[0].Name != "LPG Commercial" || !sameMoney(products[0].Gross, 2000) || !sameMoney(products[0].Kg, 8) {
		t.Errorf("products[0] = %+v", products[0])
	}
	if products[1].Name != "LPG Domestic" || !sameMoney(products[1].Gross, 1000) || !sameMoney(products[1].Kg, 4) {
		t.Errorf("products[1] = %+v", products[1])
	}

	// One rate band, reconciling to the window's tax by construction.
	bands := decodeRows[reports.TaxBand](t, r, "tax", f.todayWindow())
	if len(bands) != 1 || !sameMoney(bands[0].Rate, 0.18) || !sameMoney(bands[0].Tax, fxTax) {
		t.Errorf("tax bands = %+v", bands)
	}

	cashiers := decodeRows[reports.CashierRow](t, r, "cashiers", f.todayWindow())
	if len(cashiers) != 1 || cashiers[0].Invoices != fxInvoices || cashiers[0].Voids != fxVoids ||
		!sameMoney(cashiers[0].Gross, fxGross) || !sameMoney(cashiers[0].Average, 1500) {
		t.Errorf("cashiers = %+v", cashiers)
	}

	// All 24 buckets, always, so the chart has a fixed x-axis.
	hours := decodeRows[reports.HourRow](t, r, "hourly", f.todayWindow())
	if len(hours) != 24 {
		t.Fatalf("hourly rows = %d, want 24", len(hours))
	}
	var hourlyNet float64
	for _, h := range hours {
		hourlyNet += h.Net
	}
	if !sameMoney(hourlyNet, fxNet) {
		t.Errorf("hourly net = %.2f, want %.2f", hourlyNet, fxNet)
	}

	// The heatmap grid rides alongside the flat rows: all 168 cells, zero-
	// filled, summing to the same net the flat rows and the totals block
	// already agree on.
	cells := decodeCells(t, r, f.todayWindow())
	if len(cells) != 168 {
		t.Fatalf("hourly cells = %d, want 168", len(cells))
	}
	var cellInvoices int
	var cellNet float64
	for _, c := range cells {
		cellInvoices += c.Invoices
		cellNet += c.Net
	}
	if cellInvoices != fxInvoices || !sameMoney(cellNet, fxNet) {
		t.Errorf("Σ cells invoices=%d net=%.2f, want %d/%.2f", cellInvoices, cellNet, fxInvoices, fxNet)
	}

	// Every other report's JSON stays exactly {rows, totals} — no stray
	// cells field on a report that has no heatmap.
	for _, name := range []string{"daily", "products", "tax", "cashiers"} {
		w := doJSON(r, http.MethodGet, "/admin/reports/"+name+f.todayWindow(), nil)
		env := decodeEnvelope(t, w)
		data, ok := env.Data.(map[string]any)
		if !ok {
			t.Fatalf("%s: data is %T, want an object", name, env.Data)
		}
		if _, exists := data["cells"]; exists {
			t.Errorf("%s: must not carry a cells field", name)
		}
	}

	// The two position reports carry no totals block.
	for _, name := range []string{"receivables", "day-closes"} {
		w, body := getReport(t, r, name, f.todayWindow())
		if w.Code != http.StatusOK {
			t.Fatalf("%s: %d %s", name, w.Code, w.Body.String())
		}
		if body.Totals != nil {
			t.Errorf("%s: totals must be null for a position report, got %+v", name, body.Totals)
		}
		if len(body.Rows) != 1 {
			t.Errorf("%s: rows = %d, want 1", name, len(body.Rows))
		}
	}

	receivables := decodeRows[reports.ReceivableRow](t, r, "receivables", f.todayWindow())
	if len(receivables) != 1 || receivables[0].CustomerID != f.credit ||
		!sameMoney(receivables[0].Balance, fxBalance) || !sameMoney(receivables[0].B0_30, fxBalance) {
		t.Errorf("receivables = %+v", receivables)
	}

	closes := decodeRows[reports.DayCloseRow](t, r, "day-closes", f.todayWindow())
	if len(closes) != 1 || closes[0].BusinessDate != f.todayKey || closes[0].Status != dayops.StatusOpen {
		t.Errorf("day closes = %+v", closes)
	}
}

// decodeRows re-decodes a report's rows into their own type.
func decodeRows[T any](t *testing.T, r http.Handler, name, query string) []T {
	t.Helper()
	w := doJSON(r, http.MethodGet, "/admin/reports/"+name+query, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("%s: %d %s", name, w.Code, w.Body.String())
	}
	var body struct {
		Rows []T `json:"rows"`
	}
	dataAs(t, decodeEnvelope(t, w), &body)
	return body.Rows
}

// decodeCells re-decodes the hourly report's heatmap grid.
func decodeCells(t *testing.T, r http.Handler, query string) []reports.HeatCell {
	t.Helper()
	w := doJSON(r, http.MethodGet, "/admin/reports/hourly"+query, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("hourly: %d %s", w.Code, w.Body.String())
	}
	var body struct {
		Cells []reports.HeatCell `json:"cells"`
	}
	dataAs(t, decodeEnvelope(t, w), &body)
	return body.Cells
}

// ── validation ───────────────────────────────────────────────────────────

func TestReports_RangeAndNameValidation(t *testing.T) {
	db := testdb.Fresh(t)
	f := seedReportsFixture(t, db)
	r := reportsRouter(db, actor{id: f.owner, username: "rpt-owner", role: "admin"})

	cases := []struct {
		name, path, code string
		status           int
	}{
		{"unknown report", "/admin/reports/nonsense" + f.todayWindow(), "report_not_found", http.StatusNotFound},
		{"reversed window", "/admin/reports/daily?from=2026-03-11&to=2026-03-10", "invalid_range", http.StatusBadRequest},
		{"over 366 days", "/admin/reports/daily?from=2024-01-01&to=2026-01-01", "invalid_range", http.StatusBadRequest},
		{"unparseable date", "/admin/reports/daily?from=11-03-2026&to=2026-03-12", "invalid_range", http.StatusBadRequest},
		{"unknown format", "/admin/reports/daily" + f.todayWindow() + "&format=pdf", "invalid_format", http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := doJSON(r, http.MethodGet, tc.path, nil)
			if w.Code != tc.status {
				t.Fatalf("status %d, want %d: %s", w.Code, tc.status, w.Body.String())
			}
			if got := errCode(decodeEnvelope(t, w)); got != tc.code {
				t.Errorf("error = %q, want %q", got, tc.code)
			}
		})
	}

	// Exactly 366 days inclusive is allowed — the cap is a typo guard, not a
	// business rule, and a leap year must fit.
	w := doJSON(r, http.MethodGet, "/admin/reports/daily?from=2024-01-01&to=2024-12-31", nil)
	if w.Code != http.StatusOK {
		t.Errorf("a 366-day window: %d %s", w.Code, w.Body.String())
	}
}

// ── exports ──────────────────────────────────────────────────────────────

// Every report exports as a CSV that parses back, with the download headers
// the browser needs to name the file.
func TestReports_CSVExport(t *testing.T) {
	db := testdb.Fresh(t)
	f := seedReportsFixture(t, db)
	r := reportsRouter(db, actor{id: f.owner, username: "rpt-owner", role: "admin"})

	wantRows := map[string]int{
		"daily": 1, "products": 2, "tax": 1, "cashiers": 1,
		"hourly": 24, "receivables": 1, "day-closes": 1,
	}
	for name, dataRows := range wantRows {
		t.Run(name, func(t *testing.T) {
			w := doJSON(r, http.MethodGet, "/admin/reports/"+name+f.todayWindow()+"&format=csv", nil)
			if w.Code != http.StatusOK {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
			if ct := w.Header().Get("Content-Type"); ct != "text/csv; charset=utf-8" {
				t.Errorf("Content-Type = %q", ct)
			}
			want := `attachment; filename="` + name + "_" + f.todayKey + "_" + f.todayKey + `.csv"`
			if cd := w.Header().Get("Content-Disposition"); cd != want {
				t.Errorf("Content-Disposition = %q, want %q", cd, want)
			}
			records, err := csv.NewReader(bytes.NewReader(w.Body.Bytes())).ReadAll()
			if err != nil {
				t.Fatalf("parse csv: %v", err)
			}
			if len(records) != dataRows+1 {
				t.Fatalf("rows = %d, want %d incl. the header", len(records), dataRows+1)
			}
			if len(records[0]) < 3 {
				t.Errorf("header = %v", records[0])
			}
		})
	}

	// The daily CSV carries the day's money in the columns the header names.
	w := doJSON(r, http.MethodGet, "/admin/reports/daily"+f.todayWindow()+"&format=csv", nil)
	records, err := csv.NewReader(bytes.NewReader(w.Body.Bytes())).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	col := map[string]int{}
	for i, h := range records[0] {
		col[h] = i
	}
	if got := records[1][col["Net"]]; got != "3540.00" {
		t.Errorf("Net = %q, want 3540.00", got)
	}
	if got := records[1][col["Kg"]]; got != "12.000" {
		t.Errorf("Kg = %q, want 12.000", got)
	}
	if got := records[1][col["Date"]]; got != f.today.Format("02-01-2006") {
		t.Errorf("Date = %q, want the DD-MM-YYYY label", got)
	}
}

// A CSV column that carries a customer's own text is neutralised against
// spreadsheet formula injection; the number columns beside it are not, so a
// negative variance still reads as a number.
func TestReports_CSVNeutralisesFreeTextOnly(t *testing.T) {
	db := testdb.Fresh(t)
	f := seedReportsFixture(t, db)
	if _, err := db.Exec(`UPDATE customers SET name = $1 WHERE id = $2`,
		`=cmd|' /c calc'!A1`, f.credit); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE business_days SET cash_variance = -150.00 WHERE id = $1`, f.dayID); err != nil {
		t.Fatal(err)
	}
	r := reportsRouter(db, actor{id: f.owner, username: "rpt-owner", role: "admin"})

	w := doJSON(r, http.MethodGet, "/admin/reports/receivables"+f.todayWindow()+"&format=csv", nil)
	records, err := csv.NewReader(bytes.NewReader(w.Body.Bytes())).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if got := records[1][0]; got != `'=cmd|' /c calc'!A1` {
		t.Errorf("customer name = %q, want it prefixed with an apostrophe", got)
	}

	w = doJSON(r, http.MethodGet, "/admin/reports/day-closes"+f.todayWindow()+"&format=csv", nil)
	records, err = csv.NewReader(bytes.NewReader(w.Body.Bytes())).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	col := map[string]int{}
	for i, h := range records[0] {
		col[h] = i
	}
	if got := records[1][col["Cash Variance"]]; got != "-150.00" {
		t.Errorf("Cash Variance = %q, want the bare number -150.00", got)
	}
}

// Every report exports as a one-sheet .xlsx that excelize can read back.
func TestReports_XLSXExport(t *testing.T) {
	db := testdb.Fresh(t)
	f := seedReportsFixture(t, db)
	r := reportsRouter(db, actor{id: f.owner, username: "rpt-owner", role: "admin"})

	wantSheet := map[string]string{
		"daily": "Daily", "products": "Products", "tax": "Tax", "cashiers": "Cashiers",
		"hourly": "Hourly", "receivables": "Receivables", "day-closes": "Day Closes",
	}
	for name, sheet := range wantSheet {
		t.Run(name, func(t *testing.T) {
			w := doJSON(r, http.MethodGet, "/admin/reports/"+name+f.todayWindow()+"&format=xlsx", nil)
			if w.Code != http.StatusOK {
				t.Fatalf("%d %s", w.Code, w.Body.String())
			}
			if ct := w.Header().Get("Content-Type"); ct != xlsxContentType {
				t.Errorf("Content-Type = %q", ct)
			}
			want := `attachment; filename="` + name + "_" + f.todayKey + "_" + f.todayKey + `.xlsx"`
			if cd := w.Header().Get("Content-Disposition"); cd != want {
				t.Errorf("Content-Disposition = %q, want %q", cd, want)
			}
			book := openWorkbook(t, w.Body.Bytes())
			if names := book.GetSheetList(); len(names) != 1 || names[0] != sheet {
				t.Fatalf("sheets = %v, want [%s]", names, sheet)
			}
			rows, err := book.GetRows(sheet)
			if err != nil {
				t.Fatal(err)
			}
			if len(rows) < 2 {
				t.Fatalf("%s has %d rows, want a header and at least one row", sheet, len(rows))
			}
		})
	}

	// Money lands in the workbook as a number, not pre-formatted text, so
	// the accountant can sum the column.
	w := doJSON(r, http.MethodGet, "/admin/reports/daily"+f.todayWindow()+"&format=xlsx", nil)
	book := openWorkbook(t, w.Body.Bytes())
	rows, err := book.GetRows("Daily")
	if err != nil {
		t.Fatal(err)
	}
	net := -1
	for i, h := range rows[0] {
		if h == "Net" {
			net = i
		}
	}
	if net < 0 {
		t.Fatalf("no Net column in %v", rows[0])
	}
	if rows[1][net] != "3540" {
		t.Errorf("Net cell = %q, want the number 3540", rows[1][net])
	}
}

// daily + format=xlsx + pack=1 is the period pack: the whole window in one
// workbook, sheets in a fixed order because someone will write a formula
// against them.
func TestReports_PeriodPack(t *testing.T) {
	db := testdb.Fresh(t)
	f := seedReportsFixture(t, db)
	r := reportsRouter(db, actor{id: f.owner, username: "rpt-owner", role: "admin"})

	w := doJSON(r, http.MethodGet, "/admin/reports/daily"+f.todayWindow()+"&format=xlsx&pack=1", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != xlsxContentType {
		t.Errorf("Content-Type = %q", ct)
	}
	book := openWorkbook(t, w.Body.Bytes())
	want := []string{"Summary", "Daily", "Products", "Tax", "Cashiers", "Receivables"}
	got := book.GetSheetList()
	if len(got) != len(want) {
		t.Fatalf("sheets = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("sheets = %v, want %v", got, want)
		}
		if len(got[i]) > 31 {
			t.Errorf("sheet name %q is over Excel's 31-character limit", got[i])
		}
	}
	summary, err := book.GetRows("Summary")
	if err != nil {
		t.Fatal(err)
	}
	figures := map[string]string{}
	for _, row := range summary[1:] {
		if len(row) >= 2 {
			figures[row[0]] = row[1]
		}
	}
	if figures["Net sales"] != "3540" || figures["Invoices"] != "2" || figures["Voids"] != "1" {
		t.Errorf("summary sheet = %v", figures)
	}
}

func openWorkbook(t *testing.T, body []byte) *excelize.File {
	t.Helper()
	book, err := excelize.OpenReader(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("open xlsx: %v", err)
	}
	t.Cleanup(func() { book.Close() })
	return book
}

// ── dashboard ────────────────────────────────────────────────────────────

func TestReports_Dashboard(t *testing.T) {
	db := testdb.Fresh(t)
	f := seedReportsFixture(t, db)
	r := reportsRouter(db, actor{id: f.owner, username: "rpt-owner", role: "admin"})

	w := doJSON(r, http.MethodGet, "/admin/dashboard", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	var body dashboardResponse
	dataAs(t, decodeEnvelope(t, w), &body)

	// Today's KPIs come from the same LoadPeriodSummary the daily report uses.
	if body.Today.Invoices != fxInvoices || body.Today.Voids != fxVoids ||
		!sameMoney(body.Today.Net, fxNet) || !sameMoney(body.Today.KgSold, fxKg) {
		t.Errorf("today = %+v", body.Today)
	}
	if !sameMoney(body.Today.Tenders.OnAccount, fxOnAccount) || !sameMoney(body.Today.ReceiptsTotal, fxReceipts) {
		t.Errorf("today tenders/receipts = %+v / %.2f", body.Today.Tenders, body.Today.ReceiptsTotal)
	}
	if !sameMoney(body.ReceivablesOutstanding, fxBalance) {
		t.Errorf("receivables outstanding = %.2f, want %.2f", body.ReceivablesOutstanding, fxBalance)
	}

	// Both series are padded to a fixed length, so a quiet day is a zero
	// rather than a missing point, and the short one is the long one's tail.
	if len(body.Series30d) != 30 || len(body.Series7d) != 7 {
		t.Fatalf("series lengths = %d / %d, want 30 / 7", len(body.Series30d), len(body.Series7d))
	}
	last30 := body.Series30d[len(body.Series30d)-1]
	last7 := body.Series7d[len(body.Series7d)-1]
	if last30.BusinessDate != f.todayKey || last7.BusinessDate != f.todayKey {
		t.Errorf("series must end on today: %q / %q, want %q", last30.BusinessDate, last7.BusinessDate, f.todayKey)
	}
	if !sameMoney(last30.Net, fxNet) || !sameMoney(last7.Net, fxNet) {
		t.Errorf("series net = %.2f / %.2f, want %.2f", last30.Net, last7.Net, fxNet)
	}
	if first := body.Series30d[0]; first.Invoices != 0 || !sameMoney(first.Net, 0) || first.Label == "" {
		t.Errorf("a padded day must be a labelled row of zeros, got %+v", first)
	}

	if len(body.TopProducts) != 2 || len(body.TopProducts) > 5 {
		t.Errorf("top products = %+v", body.TopProducts)
	}

	// Voids are listed: a void the cashier just did is what the owner opened
	// this screen to see.
	if len(body.RecentInvoices) != 3 {
		t.Fatalf("recent invoices = %d, want 3", len(body.RecentInvoices))
	}
	statuses := map[string]int{}
	for _, inv := range body.RecentInvoices {
		statuses[inv.Status]++
		if inv.InvoiceNumber == "" || inv.BusinessDate != f.todayKey {
			t.Errorf("recent invoice = %+v", inv)
		}
	}
	if statuses["voided"] != 1 || statuses["completed"] != 2 {
		t.Errorf("recent invoice statuses = %v", statuses)
	}

	if body.Day == nil || body.Day.Status != dayops.StatusOpen || body.Day.OpenedAt.IsZero() {
		t.Errorf("day = %+v", body.Day)
	}
}

// With no day and nothing sold the dashboard is a page of zeros, not an
// error: that is what the screen looks like before the till is opened.
func TestReports_DashboardOnAnEmptyStore(t *testing.T) {
	db := testdb.Fresh(t)
	owner := seedUser(t, db, "rpt-owner", "admin", "owner-pass-1", nil)
	r := reportsRouter(db, actor{id: owner, username: "rpt-owner", role: "admin"})

	w := doJSON(r, http.MethodGet, "/admin/dashboard", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	var body dashboardResponse
	dataAs(t, decodeEnvelope(t, w), &body)
	if body.Today.Invoices != 0 || !sameMoney(body.Today.Net, 0) || !sameMoney(body.ReceivablesOutstanding, 0) {
		t.Errorf("today = %+v, outstanding %.2f", body.Today, body.ReceivablesOutstanding)
	}
	if len(body.Series30d) != 30 || len(body.Series7d) != 7 {
		t.Errorf("series lengths = %d / %d", len(body.Series30d), len(body.Series7d))
	}
	if body.Day != nil {
		t.Errorf("day = %+v, want null before the till is opened", body.Day)
	}
	if body.TopProducts == nil || body.RecentInvoices == nil {
		t.Error("empty lists must serialise as [], not null")
	}
}

// ── ageing ───────────────────────────────────────────────────────────────

func TestReports_CustomerAgeing(t *testing.T) {
	db := testdb.Fresh(t)
	f := seedReportsFixture(t, db)
	r := reportsRouter(db, actor{id: f.owner, username: "rpt-owner", role: "admin"})

	// A customer with a ledger: the same row the receivables report shows.
	w := doJSON(r, http.MethodGet, "/customers/"+f.credit.String()+"/ageing", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	var row reports.ReceivableRow
	dataAs(t, decodeEnvelope(t, w), &row)
	if row.CustomerID != f.credit || !sameMoney(row.Balance, fxBalance) || !sameMoney(row.B0_30, fxBalance) {
		t.Errorf("ageing = %+v", row)
	}
	if !sameMoney(row.B31_60, 0) || !sameMoney(row.B61_90, 0) || !sameMoney(row.B90, 0) {
		t.Errorf("a debt raised today belongs in 0–30 only: %+v", row)
	}
	if row.LastReceipt == nil || *row.LastReceipt != f.todayKey {
		t.Errorf("last receipt = %v, want %q", row.LastReceipt, f.todayKey)
	}

	// A customer with no ledger at all: zeros carrying their name, not a 404.
	w = doJSON(r, http.MethodGet, "/customers/"+f.settled.String()+"/ageing", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
	dataAs(t, decodeEnvelope(t, w), &row)
	if row.CustomerID != f.settled || row.Name != "Settled Traders" ||
		!sameMoney(row.Balance, 0) || !sameMoney(row.B0_30, 0) || row.LastReceipt != nil {
		t.Errorf("settled customer ageing = %+v", row)
	}

	// An unknown id is the customers' own code, not a reports one.
	w = doJSON(r, http.MethodGet, "/customers/"+uuid.NewString()+"/ageing", nil)
	if w.Code != http.StatusNotFound || errCode(decodeEnvelope(t, w)) != "customer_not_found" {
		t.Errorf("unknown customer: %d %s", w.Code, w.Body.String())
	}
}
