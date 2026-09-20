package handlers

import (
	"database/sql"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"elevon-backend/internal/dayops"
	"elevon-backend/internal/invoice"
	"elevon-backend/internal/ledger"
	"elevon-backend/internal/models"
	"elevon-backend/internal/testdb"
	"elevon-backend/internal/util"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// moneyRouter mounts the money-core routes on exactly the paths routes.go
// uses, so a gin routing conflict (/invoices/recent against /invoices/:id, or
// the nested receipt void) fails here rather than at boot.
func moneyRouter(db *sql.DB, a actor) *gin.Engine {
	r := gin.New()
	inv := NewInvoicesHandler(db)
	cus := NewCustomersHandler(db)
	staff := r.Group("", asActor(a))
	staff.POST("/invoices", inv.Create)
	staff.GET("/invoices", inv.List)
	staff.GET("/invoices/recent", inv.Recent)
	staff.GET("/invoices/:id", inv.Get)
	staff.POST("/invoices/:id/void", inv.Void)
	staff.GET("/customers/:id", cus.Get)
	staff.GET("/customers/:id/statement", cus.Statement)
	staff.GET("/customers/:id/receipts", cus.ListReceipts)
	staff.POST("/customers/:id/receipts", cus.CreateReceipt)
	staff.POST("/customers/:id/receipts/:rid/void", cus.VoidReceipt)
	return r
}

// businessToday is the business date the handlers will key everything to.
func businessToday() time.Time { return util.BusinessDate(time.Now()) }

// openBusinessDay inserts an open day directly — the day-open path has its
// own tests, and these tests are about what happens once a day is open.
func openBusinessDay(t *testing.T, db *sql.DB, openedBy uuid.UUID, date time.Time) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := db.QueryRow(`INSERT INTO business_days (business_date, status, opened_by, opening_cash)
		VALUES ($1::date, 'open', $2, 0) RETURNING id`, date.Format(dayops.DateLayout), openedBy).Scan(&id); err != nil {
		t.Fatalf("open day %s: %v", date.Format(dayops.DateLayout), err)
	}
	return id
}

func seedProductRow(t *testing.T, db *sql.DB, name string, rate float64) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := db.QueryRow(`INSERT INTO products (name, rate, hs_code, fbr_uom)
		VALUES ($1, $2, '2711.1910', 'Kilogram') RETURNING id`, name, rate).Scan(&id); err != nil {
		t.Fatalf("seed product %s: %v", name, err)
	}
	return id
}

func seedCustomerRow(t *testing.T, db *sql.DB, name string, creditAllowed bool, creditLimit *float64) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	if err := db.QueryRow(`INSERT INTO customers (name, phone, ntn, cnic, credit_allowed, credit_limit)
		VALUES ($1, NULL, '1234567-8', '3520112345678', $2, $3) RETURNING id`, name, creditAllowed, creditLimit).Scan(&id); err != nil {
		t.Fatalf("seed customer %s: %v", name, err)
	}
	return id
}

func setSetting(t *testing.T, db *sql.DB, key, jsonValue string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO settings (key, value, updated_at) VALUES ($1, $2::jsonb, now())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`, key, jsonValue); err != nil {
		t.Fatalf("set %s: %v", key, err)
	}
}

// sameMoney compares two rupee figures at half-paisa tolerance.
func sameMoney(a, b float64) bool { return math.Abs(a-b) < 0.005 }

func mustBalance(t *testing.T, db *sql.DB, customerID uuid.UUID) float64 {
	t.Helper()
	b, err := ledger.Balance(db, customerID)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// The core claim of §6.2/§6.3: money is computed server-side from
// products.rate and the submitted quantity. The request below sends a full
// set of money fields — line totals, subtotal, tax, total_payable — all
// wrong, all cheaper. None of them can reach the invoice, because none of
// them is representable in the request type.
func TestInvoice_LinesRecomputedFromProductRate(t *testing.T) {
	db := testdb.Fresh(t)
	ownerID := seedUser(t, db, "owner", "admin", "owner-pass-1", nil)
	dayID := openBusinessDay(t, db, ownerID, businessToday())
	productID := seedProductRow(t, db, "LPG bulk", 250.00)
	setSetting(t, db, "tax_rate_cash", "0.18")
	r := moneyRouter(db, actor{id: ownerID, username: "owner", role: "admin"})

	w := doJSON(r, http.MethodPost, "/invoices", map[string]any{
		"client_op_id":   uuid.NewString(),
		"payment_method": "cash",
		"lines": []map[string]any{{
			"product_id": productID.String(),
			"quantity":   12.5,
			"entered_as": "kg",
			// Everything below is the client trying to price the sale.
			"unit_price": 1.0, "rate": 1.0, "line_total": 1.0, "line_tax": 0.0,
		}},
		// So is everything here.
		"subtotal": 1.0, "tax_amount": 0.0, "total_amount": 1.0, "total_payable": 1,
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var inv models.Invoice
	dataAs(t, decodeEnvelope(t, w), &inv)

	// 12.5 kg × Rs 250 = 3 125.00; 18% tax = 562.50; total 3 687.50;
	// half-up to the rupee = 3 688, so the rounding line is +0.50.
	if !sameMoney(inv.Subtotal, 3125) || !sameMoney(inv.TaxAmount, 562.50) ||
		!sameMoney(inv.TotalAmount, 3687.50) || inv.TotalPayable != 3688 || !sameMoney(inv.RoundingAdjustment, 0.50) {
		t.Fatalf("totals recomputed from the rate: %+v", inv)
	}
	if !sameMoney(inv.TaxRate, 0.18) || !sameMoney(inv.FurtherTaxAmount, 0) || !sameMoney(inv.DiscountAmount, 0) {
		t.Fatalf("tax snapshot: %+v", inv)
	}
	if len(inv.Lines) != 1 {
		t.Fatalf("lines: %+v", inv.Lines)
	}
	line := inv.Lines[0]
	if !sameMoney(line.UnitPrice, 250) || !sameMoney(line.LineTotal, 3125) || !sameMoney(line.LineTax, 562.50) ||
		!sameMoney(line.LineDiscount, 0) || !sameMoney(line.Quantity, 12.5) {
		t.Fatalf("line recomputed: %+v", line)
	}
	if line.ProductName != "LPG bulk" || line.HSCode == nil || *line.HSCode != "2711.1910" ||
		line.FBRUoM == nil || *line.FBRUoM != "Kilogram" {
		t.Fatalf("line snapshots: %+v", line)
	}
	if line.EnteredAs == nil || *line.EnteredAs != "kg" || line.GrossWeight != nil || line.TareWeight != nil {
		t.Fatalf("entry mode: %+v", line)
	}

	// Numbered from today's counter, tied to the open day, not fiscalised.
	if want := invoice.Format("", businessToday(), 1); inv.InvoiceNumber != want {
		t.Fatalf("invoice number = %q, want %q", inv.InvoiceNumber, want)
	}
	if inv.BusinessDayID != dayID {
		t.Fatalf("business_day_id = %s, want %s", inv.BusinessDayID, dayID)
	}
	if inv.BusinessDate.Format(dayops.DateLayout) != businessToday().Format(dayops.DateLayout) {
		t.Fatalf("business_date = %s", inv.BusinessDate)
	}
	if inv.Status != "completed" || inv.FiscalStatus != "off" || inv.CashierName != "Test owner" ||
		inv.CashierID == nil || *inv.CashierID != ownerID {
		t.Fatalf("invoice header: %+v", inv)
	}
	if inv.CustomerID != nil || inv.CustomerBalanceAfter != nil {
		t.Fatalf("walk-in sale should carry no customer: %+v", inv)
	}

	// The row on disk says the same thing as the response.
	var subtotal, payable float64
	if err := db.QueryRow(`SELECT subtotal::float8, total_payable::float8 FROM invoices WHERE id = $1`, inv.ID).
		Scan(&subtotal, &payable); err != nil {
		t.Fatal(err)
	}
	if !sameMoney(subtotal, 3125) || !sameMoney(payable, 3688) {
		t.Fatalf("persisted totals: %v %v", subtotal, payable)
	}
}

// Amount mode: the cashier types a rupee figure, the client converts it to a
// quantity, and the line total is recomputed from that quantity — so it can
// differ from the typed amount by a few paisa (§6.2). The invoice records the
// mode and the recomputed total, never the typed figure.
func TestInvoice_AmountEnteredLineStoresModeAndRecomputedTotal(t *testing.T) {
	db := testdb.Fresh(t)
	ownerID := seedUser(t, db, "owner", "admin", "owner-pass-1", nil)
	openBusinessDay(t, db, ownerID, businessToday())
	productID := seedProductRow(t, db, "LPG bulk", 137.50)
	r := moneyRouter(db, actor{id: ownerID, username: "owner", role: "admin"})

	// Rs 5 000 ÷ 137.50 = 36.3636…, rounded to 36.364 kg by the pad;
	// 36.364 × 137.50 = 5 000.05 — five paisa more than the figure typed.
	w := doJSON(r, http.MethodPost, "/invoices", models.CreateInvoiceRequest{
		ClientOpID:    uuid.NewString(),
		PaymentMethod: "cash",
		Lines:         []models.InvoiceLineRequest{{ProductID: productID.String(), Quantity: 36.364, EnteredAs: "amount"}},
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var inv models.Invoice
	dataAs(t, decodeEnvelope(t, w), &inv)
	if len(inv.Lines) != 1 {
		t.Fatalf("lines: %+v", inv.Lines)
	}
	line := inv.Lines[0]
	if line.EnteredAs == nil || *line.EnteredAs != "amount" {
		t.Fatalf("entered_as: %+v", line)
	}
	if !sameMoney(line.LineTotal, 5000.05) || !sameMoney(inv.Subtotal, 5000.05) || inv.TotalPayable != 5000 {
		t.Fatalf("recomputed from the quantity, not the typed amount: %+v / %+v", line, inv)
	}
	// 5 000.05 rounds down to 5 000, so the rounding line is −0.05.
	if !sameMoney(inv.RoundingAdjustment, -0.05) {
		t.Fatalf("rounding adjustment: %v", inv.RoundingAdjustment)
	}
}

// gross − tare is the cylinder mode: both weights are stored and they have to
// reconcile to the net quantity being charged for.
func TestInvoice_GrossTareLineStoresBothWeights(t *testing.T) {
	db := testdb.Fresh(t)
	ownerID := seedUser(t, db, "owner", "admin", "owner-pass-1", nil)
	openBusinessDay(t, db, ownerID, businessToday())
	productID := seedProductRow(t, db, "Cylinder refill", 300)
	r := moneyRouter(db, actor{id: ownerID, username: "owner", role: "admin"})

	gross, tare := 50.500, 12.250
	w := doJSON(r, http.MethodPost, "/invoices", models.CreateInvoiceRequest{
		ClientOpID:    uuid.NewString(),
		PaymentMethod: "cash",
		Lines: []models.InvoiceLineRequest{{
			ProductID: productID.String(), Quantity: 38.250, EnteredAs: "gross_tare",
			GrossWeight: &gross, TareWeight: &tare,
		}},
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var inv models.Invoice
	dataAs(t, decodeEnvelope(t, w), &inv)
	line := inv.Lines[0]
	if line.GrossWeight == nil || !sameMoney(*line.GrossWeight, 50.5) ||
		line.TareWeight == nil || !sameMoney(*line.TareWeight, 12.25) {
		t.Fatalf("weights stored: %+v", line)
	}
	if !sameMoney(line.LineTotal, 11475) || inv.TotalPayable != 11475 {
		t.Fatalf("38.25 kg × 300 = 11 475: %+v", inv)
	}

	// A net that does not match the two weights is refused.
	w = doJSON(r, http.MethodPost, "/invoices", models.CreateInvoiceRequest{
		ClientOpID:    uuid.NewString(),
		PaymentMethod: "cash",
		Lines: []models.InvoiceLineRequest{{
			ProductID: productID.String(), Quantity: 40, EnteredAs: "gross_tare",
			GrossWeight: &gross, TareWeight: &tare,
		}},
	})
	if w.Code != http.StatusBadRequest || errCode(decodeEnvelope(t, w)) != "invalid_quantity" {
		t.Fatalf("gross − tare mismatch: %d %s", w.Code, w.Body.String())
	}

	// So is a gross_tare line with no weights on it.
	w = doJSON(r, http.MethodPost, "/invoices", models.CreateInvoiceRequest{
		ClientOpID:    uuid.NewString(),
		PaymentMethod: "cash",
		Lines:         []models.InvoiceLineRequest{{ProductID: productID.String(), Quantity: 38.250, EnteredAs: "gross_tare"}},
	})
	if w.Code != http.StatusBadRequest || errCode(decodeEnvelope(t, w)) != "invalid_quantity" {
		t.Fatalf("gross_tare without weights: %d %s", w.Code, w.Body.String())
	}
}

// §6.7's day gate, from the till's side: no day at all, and a previous day
// still holding the open slot. Neither path may leave an invoice behind.
func TestInvoice_DayGateRefusals(t *testing.T) {
	db := testdb.Fresh(t)
	ownerID := seedUser(t, db, "owner", "admin", "owner-pass-1", nil)
	productID := seedProductRow(t, db, "LPG bulk", 250)
	r := moneyRouter(db, actor{id: ownerID, username: "owner", role: "admin"})

	body := models.CreateInvoiceRequest{
		ClientOpID:    uuid.NewString(),
		PaymentMethod: "cash",
		Lines:         []models.InvoiceLineRequest{{ProductID: productID.String(), Quantity: 1, EnteredAs: "kg"}},
	}
	w := doJSON(r, http.MethodPost, "/invoices", body)
	if w.Code != http.StatusConflict || errCode(decodeEnvelope(t, w)) != "day_not_open" {
		t.Fatalf("no day open: %d %s", w.Code, w.Body.String())
	}

	// A day for another date holds the single open slot.
	yesterday := businessToday().AddDate(0, 0, -1)
	openBusinessDay(t, db, ownerID, yesterday)
	body.ClientOpID = uuid.NewString()
	w = doJSON(r, http.MethodPost, "/invoices", body)
	if w.Code != http.StatusConflict || errCode(decodeEnvelope(t, w)) != "previous_day_open" {
		t.Fatalf("previous day open: %d %s", w.Code, w.Body.String())
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM invoices`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("a refused sale left %d invoices behind", count)
	}
}

// The till retries after a dropped response: the same client_op_id must
// return the sale already rung, not ring it again.
func TestInvoice_DuplicateClientOpIDReturnsTheSameInvoice(t *testing.T) {
	db := testdb.Fresh(t)
	ownerID := seedUser(t, db, "owner", "admin", "owner-pass-1", nil)
	openBusinessDay(t, db, ownerID, businessToday())
	productID := seedProductRow(t, db, "LPG bulk", 250)
	r := moneyRouter(db, actor{id: ownerID, username: "owner", role: "admin"})

	body := models.CreateInvoiceRequest{
		ClientOpID:    uuid.NewString(),
		PaymentMethod: "cash",
		Lines:         []models.InvoiceLineRequest{{ProductID: productID.String(), Quantity: 4, EnteredAs: "kg"}},
	}
	w := doJSON(r, http.MethodPost, "/invoices", body)
	if w.Code != http.StatusCreated {
		t.Fatalf("first: %d %s", w.Code, w.Body.String())
	}
	var first models.Invoice
	dataAs(t, decodeEnvelope(t, w), &first)

	w = doJSON(r, http.MethodPost, "/invoices", body)
	if w.Code != http.StatusOK {
		t.Fatalf("repeat should be 200: %d %s", w.Code, w.Body.String())
	}
	var second models.Invoice
	dataAs(t, decodeEnvelope(t, w), &second)
	if second.ID != first.ID || second.InvoiceNumber != first.InvoiceNumber || len(second.Lines) != 1 {
		t.Fatalf("repeat returned a different invoice: %+v vs %+v", second, first)
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM invoices`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("idempotency wrote %d invoices", count)
	}
}

// A credit sale is money owed: it needs an account, that account needs to be
// allowed credit, the limit is enforced when the setting says so, and an
// admin PIN is the only way past it. The ledger debit is written in the same
// transaction as the invoice.
func TestInvoice_CreditWritesLedgerAndRespectsTheLimit(t *testing.T) {
	db := testdb.Fresh(t)
	ownerID := seedUser(t, db, "owner", "admin", "owner-pass-1", nil)
	setPin(t, db, ownerID, "4321")
	openBusinessDay(t, db, ownerID, businessToday())
	productID := seedProductRow(t, db, "LPG bulk", 250)
	setSetting(t, db, "credit_limit_enforced", "true")
	limit := 1000.0
	customerID := seedCustomerRow(t, db, "Ali Traders", true, &limit)
	cashOnly := seedCustomerRow(t, db, "Walk-in Ltd", false, nil)
	r := moneyRouter(db, actor{id: ownerID, username: "owner", role: "admin"})

	// Credit with no account named at all.
	w := doJSON(r, http.MethodPost, "/invoices", models.CreateInvoiceRequest{
		ClientOpID:    uuid.NewString(),
		PaymentMethod: "credit",
		Lines:         []models.InvoiceLineRequest{{ProductID: productID.String(), Quantity: 1, EnteredAs: "kg"}},
	})
	if w.Code != http.StatusBadRequest || errCode(decodeEnvelope(t, w)) != "customer_required" {
		t.Fatalf("credit without a customer: %d %s", w.Code, w.Body.String())
	}

	// An account that is not set up for credit.
	w = doJSON(r, http.MethodPost, "/invoices", models.CreateInvoiceRequest{
		ClientOpID:    uuid.NewString(),
		CustomerID:    cashOnly.String(),
		PaymentMethod: "credit",
		Lines:         []models.InvoiceLineRequest{{ProductID: productID.String(), Quantity: 1, EnteredAs: "kg"}},
	})
	if w.Code != http.StatusConflict || errCode(decodeEnvelope(t, w)) != "credit_not_allowed" {
		t.Fatalf("credit not allowed: %d %s", w.Code, w.Body.String())
	}

	// 8 kg × 250 = Rs 2 000, well past the Rs 1 000 limit.
	opID := uuid.NewString()
	body := models.CreateInvoiceRequest{
		ClientOpID:    opID,
		CustomerID:    customerID.String(),
		PaymentMethod: "credit",
		Lines:         []models.InvoiceLineRequest{{ProductID: productID.String(), Quantity: 8, EnteredAs: "kg"}},
		Notes:         "delivery to yard",
	}
	w = doJSON(r, http.MethodPost, "/invoices", body)
	if w.Code != http.StatusConflict || errCode(decodeEnvelope(t, w)) != "credit_limit_exceeded" {
		t.Fatalf("over the limit: %d %s", w.Code, w.Body.String())
	}

	// A PIN that matches nobody says so, rather than blaming the limit.
	body.Pin = "9999"
	w = doJSON(r, http.MethodPost, "/invoices", body)
	if w.Code != http.StatusUnauthorized || errCode(decodeEnvelope(t, w)) != "invalid_pin" {
		t.Fatalf("wrong override pin: %d %s", w.Code, w.Body.String())
	}
	if b := mustBalance(t, db, customerID); b != 0 {
		t.Fatalf("a refused credit sale moved the balance to %v", b)
	}

	// The admin PIN lets it through, and says so in the notes.
	body.Pin = "4321"
	w = doJSON(r, http.MethodPost, "/invoices", body)
	if w.Code != http.StatusCreated {
		t.Fatalf("override: %d %s", w.Code, w.Body.String())
	}
	var inv models.Invoice
	dataAs(t, decodeEnvelope(t, w), &inv)
	if inv.TotalPayable != 2000 || inv.PaymentMethod != "credit" {
		t.Fatalf("credit invoice: %+v", inv)
	}
	if inv.Notes == nil || !strings.Contains(*inv.Notes, "delivery to yard") ||
		!strings.Contains(*inv.Notes, "credit limit overridden by Test owner") {
		t.Fatalf("override note: %+v", inv.Notes)
	}
	if inv.CustomerName == nil || *inv.CustomerName != "Ali Traders" ||
		inv.CustomerNTN == nil || *inv.CustomerNTN != "1234567-8" {
		t.Fatalf("customer snapshot: %+v", inv)
	}
	if inv.CustomerBalanceAfter == nil || !sameMoney(*inv.CustomerBalanceAfter, 2000) {
		t.Fatalf("customer_balance_after: %+v", inv.CustomerBalanceAfter)
	}

	// One ledger row, debit = total payable, tied to the invoice.
	var entryType string
	var debit, credit float64
	var ledgerInvoiceID uuid.UUID
	if err := db.QueryRow(`SELECT entry_type, debit::float8, credit::float8, invoice_id
		FROM customer_ledger_entries WHERE customer_id = $1`, customerID).Scan(&entryType, &debit, &credit, &ledgerInvoiceID); err != nil {
		t.Fatalf("ledger: %v", err)
	}
	if entryType != "invoice" || !sameMoney(debit, 2000) || !sameMoney(credit, 0) || ledgerInvoiceID != inv.ID {
		t.Fatalf("ledger entry: %s %v %v %s", entryType, debit, credit, ledgerInvoiceID)
	}
	if b := mustBalance(t, db, customerID); !sameMoney(b, 2000) {
		t.Fatalf("balance = %v", b)
	}

	// A credit sale is on account, not in any tender: it never lands in the
	// drawer expectation.
	dayID := inv.BusinessDayID
	expected, err := dayops.ComputeExpected(db, dayID)
	if err != nil {
		t.Fatal(err)
	}
	if !sameMoney(expected.OnAccountSales, 2000) || !sameMoney(expected.Cash, 0) {
		t.Fatalf("on-account sale reached a tender: %+v", expected)
	}
}

// Two credit sales racing on one account must not both pass the credit-limit
// check: lockCustomer takes FOR UPDATE for a credit tender, so the second
// sale's balance read has to wait behind the first one's commit and sees its
// debit. Without that lock both requests can read a zero balance, both price
// under the limit and both succeed — over-extending the account.
func TestInvoice_ConcurrentCreditSalesRespectLimit(t *testing.T) {
	db := testdb.Fresh(t)
	ownerID := seedUser(t, db, "owner", "admin", "owner-pass-1", nil)
	openBusinessDay(t, db, ownerID, businessToday())
	productID := seedProductRow(t, db, "LPG bulk", 1000) // 3 kg = Rs 3 000 a sale
	setSetting(t, db, "credit_limit_enforced", "true")
	limit := 5000.0
	customerID := seedCustomerRow(t, db, "Ali Traders", true, &limit)
	r := moneyRouter(db, actor{id: ownerID, username: "owner", role: "admin"})

	const n = 2
	results := make([]*httptest.ResponseRecorder, n)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			results[i] = doJSON(r, http.MethodPost, "/invoices", models.CreateInvoiceRequest{
				ClientOpID:    uuid.NewString(),
				CustomerID:    customerID.String(),
				PaymentMethod: "credit",
				Lines:         []models.InvoiceLineRequest{{ProductID: productID.String(), Quantity: 3, EnteredAs: "kg"}},
			})
		}(i)
	}
	close(start) // release both goroutines together
	wg.Wait()

	var created, exceeded int
	for _, w := range results {
		switch {
		case w.Code == http.StatusCreated:
			created++
		case w.Code == http.StatusConflict && errCode(decodeEnvelope(t, w)) == "credit_limit_exceeded":
			exceeded++
		default:
			t.Fatalf("unexpected response: %d %s", w.Code, w.Body.String())
		}
	}
	if created != 1 || exceeded != 1 {
		t.Fatalf("want exactly one 201 and one 409 credit_limit_exceeded, got %d created and %d exceeded", created, exceeded)
	}
	if b := mustBalance(t, db, customerID); !sameMoney(b, 3000) {
		t.Fatalf("final balance = %v, want 3000 — only one of the two sales should have landed", b)
	}
}

// A receipt is money arriving against the account: it takes the same day gate
// as a sale, gets its own R- number and lowers the balance.
func TestReceipt_LowersTheBalanceAndItsVoidRaisesItBack(t *testing.T) {
	db := testdb.Fresh(t)
	ownerID := seedUser(t, db, "owner", "admin", "owner-pass-1", nil)
	setPin(t, db, ownerID, "4321")
	productID := seedProductRow(t, db, "LPG bulk", 250)
	customerID := seedCustomerRow(t, db, "Ali Traders", true, nil)
	r := moneyRouter(db, actor{id: ownerID, username: "owner", role: "admin"})

	// Before the day is open, money cannot be taken either.
	w := doJSON(r, http.MethodPost, "/customers/"+customerID.String()+"/receipts",
		models.CreateReceiptRequest{Amount: 500, Method: "cash"})
	if w.Code != http.StatusConflict || errCode(decodeEnvelope(t, w)) != "day_not_open" {
		t.Fatalf("receipt before the day is open: %d %s", w.Code, w.Body.String())
	}

	dayID := openBusinessDay(t, db, ownerID, businessToday())

	// A credit sale puts Rs 2 000 on the account.
	w = doJSON(r, http.MethodPost, "/invoices", models.CreateInvoiceRequest{
		ClientOpID:    uuid.NewString(),
		CustomerID:    customerID.String(),
		PaymentMethod: "credit",
		Lines:         []models.InvoiceLineRequest{{ProductID: productID.String(), Quantity: 8, EnteredAs: "kg"}},
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("credit sale: %d %s", w.Code, w.Body.String())
	}

	w = doJSON(r, http.MethodPost, "/customers/"+customerID.String()+"/receipts",
		models.CreateReceiptRequest{Amount: 500.50, Method: "cash", Note: " part payment "})
	if w.Code != http.StatusCreated {
		t.Fatalf("receipt: %d %s", w.Code, w.Body.String())
	}
	var receipt models.Receipt
	dataAs(t, decodeEnvelope(t, w), &receipt)
	// 002, not 001: the refused attempt above had already been handed a
	// number before the day gate turned it away. That gap is the documented
	// cost of allocating outside the transaction (see package invoice), and
	// this assertion is here to keep it a deliberate choice rather than a
	// surprise the first time someone reconciles a day's receipts.
	if want := invoice.Format(invoice.ReceiptPrefix, businessToday(), 2); receipt.ReceiptNumber != want {
		t.Fatalf("receipt number = %q, want %q", receipt.ReceiptNumber, want)
	}
	if receipt.BusinessDayID != dayID || receipt.Method != "cash" || !sameMoney(receipt.Amount, 500.50) {
		t.Fatalf("receipt row: %+v", receipt)
	}
	if receipt.Note == nil || *receipt.Note != "part payment" || receipt.CustomerName != "Ali Traders" ||
		receipt.ReceivedByName != "Test owner" {
		t.Fatalf("receipt details: %+v", receipt)
	}
	if receipt.CustomerBalanceAfter == nil || !sameMoney(*receipt.CustomerBalanceAfter, 1499.50) {
		t.Fatalf("balance after receipt: %+v", receipt.CustomerBalanceAfter)
	}
	if b := mustBalance(t, db, customerID); !sameMoney(b, 1499.50) {
		t.Fatalf("balance = %v, want 1499.50", b)
	}

	// The account screen lists it with both names filled in.
	w = doJSON(r, http.MethodGet, "/customers/"+customerID.String()+"/receipts", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("list receipts: %d %s", w.Code, w.Body.String())
	}
	var listed []models.Receipt
	dataAs(t, decodeEnvelope(t, w), &listed)
	if len(listed) != 1 || listed[0].ID != receipt.ID ||
		listed[0].CustomerName != "Ali Traders" || listed[0].ReceivedByName != "Test owner" {
		t.Fatalf("listed receipts: %+v", listed)
	}

	// It counts as cash in the drawer for the day.
	expected, err := dayops.ComputeExpected(db, dayID)
	if err != nil {
		t.Fatal(err)
	}
	if !sameMoney(expected.CashReceipts, 500.50) || !sameMoney(expected.Cash, 500.50) {
		t.Fatalf("receipt in the tender: %+v", expected)
	}

	// Voiding it needs an admin PIN and a written reason, and mirrors the
	// ledger with a debit rather than editing the credit.
	path := "/customers/" + customerID.String() + "/receipts/" + receipt.ID.String() + "/void"
	w = doJSON(r, http.MethodPost, path, models.VoidReceiptRequest{Reason: "wrong account", Pin: "0000"})
	if w.Code != http.StatusUnauthorized || errCode(decodeEnvelope(t, w)) != "invalid_pin" {
		t.Fatalf("void with a bad pin: %d %s", w.Code, w.Body.String())
	}
	w = doJSON(r, http.MethodPost, path, models.VoidReceiptRequest{Reason: "x", Pin: "4321"})
	if w.Code != http.StatusBadRequest || errCode(decodeEnvelope(t, w)) != "reason_required" {
		t.Fatalf("void without a reason: %d %s", w.Code, w.Body.String())
	}
	w = doJSON(r, http.MethodPost, path, models.VoidReceiptRequest{Reason: "wrong account", Pin: "4321"})
	if w.Code != http.StatusOK {
		t.Fatalf("void: %d %s", w.Code, w.Body.String())
	}
	var voided models.Receipt
	dataAs(t, decodeEnvelope(t, w), &voided)
	if voided.VoidedAt == nil || voided.VoidReason == nil || *voided.VoidReason != "wrong account" {
		t.Fatalf("voided receipt: %+v", voided)
	}
	if voided.CustomerBalanceAfter == nil || !sameMoney(*voided.CustomerBalanceAfter, 2000) {
		t.Fatalf("balance after the void: %+v", voided.CustomerBalanceAfter)
	}
	if b := mustBalance(t, db, customerID); !sameMoney(b, 2000) {
		t.Fatalf("balance after void = %v", b)
	}
	var voidEntries int
	if err := db.QueryRow(`SELECT COUNT(*) FROM customer_ledger_entries
		WHERE customer_id = $1 AND entry_type = 'receipt_void' AND debit = 500.50`, customerID).Scan(&voidEntries); err != nil {
		t.Fatal(err)
	}
	if voidEntries != 1 {
		t.Fatalf("receipt_void entries: %d", voidEntries)
	}

	// A voided receipt is out of the tender count.
	expected, err = dayops.ComputeExpected(db, dayID)
	if err != nil {
		t.Fatal(err)
	}
	if !sameMoney(expected.CashReceipts, 0) {
		t.Fatalf("voided receipt still counted: %+v", expected)
	}

	// Voiding it twice is refused.
	w = doJSON(r, http.MethodPost, path, models.VoidReceiptRequest{Reason: "wrong account", Pin: "4321"})
	if w.Code != http.StatusConflict || errCode(decodeEnvelope(t, w)) != "receipt_already_voided" {
		t.Fatalf("second void: %d %s", w.Code, w.Body.String())
	}
}

// Voids: admin PIN, written reason, a void_log row that can never be edited,
// a mirroring ledger entry for a credit sale, and exclusion from every total
// while the invoice itself stays visible.
func TestInvoice_VoidMirrorsLedgerAndVoidLogIsAppendOnly(t *testing.T) {
	db := testdb.Fresh(t)
	ownerID := seedUser(t, db, "owner", "admin", "owner-pass-1", nil)
	counterID := seedUser(t, db, "counter", "counter", "counter-pass-1", nil)
	setPin(t, db, ownerID, "4321")
	dayID := openBusinessDay(t, db, ownerID, businessToday())
	productID := seedProductRow(t, db, "LPG bulk", 250)
	customerID := seedCustomerRow(t, db, "Ali Traders", true, nil)
	// The cashier at the till is not the admin: the void_log has to keep
	// "who did it" and "who allowed it" apart.
	r := moneyRouter(db, actor{id: counterID, username: "counter", role: "counter"})

	w := doJSON(r, http.MethodPost, "/invoices", models.CreateInvoiceRequest{
		ClientOpID:    uuid.NewString(),
		CustomerID:    customerID.String(),
		PaymentMethod: "credit",
		Lines:         []models.InvoiceLineRequest{{ProductID: productID.String(), Quantity: 8, EnteredAs: "kg"}},
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("credit sale: %d %s", w.Code, w.Body.String())
	}
	var inv models.Invoice
	dataAs(t, decodeEnvelope(t, w), &inv)

	// A cash sale on the same day, to prove the void only removes its own.
	w = doJSON(r, http.MethodPost, "/invoices", models.CreateInvoiceRequest{
		ClientOpID:    uuid.NewString(),
		PaymentMethod: "cash",
		Lines:         []models.InvoiceLineRequest{{ProductID: productID.String(), Quantity: 2, EnteredAs: "kg"}},
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("cash sale: %d %s", w.Code, w.Body.String())
	}

	path := "/invoices/" + inv.ID.String() + "/void"
	w = doJSON(r, http.MethodPost, path, models.VoidInvoiceRequest{Reason: "wrong weight", Pin: ""})
	if w.Code != http.StatusUnauthorized || errCode(decodeEnvelope(t, w)) != "invalid_pin" {
		t.Fatalf("void without a pin: %d %s", w.Code, w.Body.String())
	}
	w = doJSON(r, http.MethodPost, path, models.VoidInvoiceRequest{Reason: "wrong weight", Pin: "1111"})
	if w.Code != http.StatusUnauthorized || errCode(decodeEnvelope(t, w)) != "invalid_pin" {
		t.Fatalf("void with a wrong pin: %d %s", w.Code, w.Body.String())
	}
	w = doJSON(r, http.MethodPost, path, models.VoidInvoiceRequest{Reason: "", Pin: "4321"})
	if w.Code != http.StatusBadRequest || errCode(decodeEnvelope(t, w)) != "reason_required" {
		t.Fatalf("void without a reason: %d %s", w.Code, w.Body.String())
	}
	if b := mustBalance(t, db, customerID); !sameMoney(b, 2000) {
		t.Fatalf("a refused void moved the balance: %v", b)
	}

	w = doJSON(r, http.MethodPost, path, models.VoidInvoiceRequest{Reason: "wrong weight on the scale", Pin: "4321"})
	if w.Code != http.StatusOK {
		t.Fatalf("void: %d %s", w.Code, w.Body.String())
	}
	var voided models.Invoice
	dataAs(t, decodeEnvelope(t, w), &voided)
	if voided.Status != "voided" || voided.VoidedAt == nil || voided.VoidReason == nil ||
		*voided.VoidReason != "wrong weight on the scale" || len(voided.Lines) != 1 {
		t.Fatalf("voided invoice: %+v", voided)
	}
	if voided.VoidedBy == nil || *voided.VoidedBy != counterID {
		t.Fatalf("voided_by should be the cashier: %+v", voided.VoidedBy)
	}
	if voided.CustomerBalanceAfter == nil || !sameMoney(*voided.CustomerBalanceAfter, 0) {
		t.Fatalf("balance after the void: %+v", voided.CustomerBalanceAfter)
	}

	// who did it ≠ who allowed it.
	var voidedBy, authorizedBy uuid.UUID
	var logNumber, logReason string
	var logPayable float64
	if err := db.QueryRow(`SELECT voided_by, authorized_by, invoice_number, total_payable::float8, reason FROM void_log`).
		Scan(&voidedBy, &authorizedBy, &logNumber, &logPayable, &logReason); err != nil {
		t.Fatalf("void_log: %v", err)
	}
	if voidedBy != counterID || authorizedBy != ownerID || logNumber != inv.InvoiceNumber ||
		!sameMoney(logPayable, 2000) || logReason != "wrong weight on the scale" {
		t.Fatalf("void_log row: %s %s %s %v %s", voidedBy, authorizedBy, logNumber, logPayable, logReason)
	}

	// The credit is mirrored, never edited in place.
	if b := mustBalance(t, db, customerID); !sameMoney(b, 0) {
		t.Fatalf("balance after void = %v", b)
	}
	var entries int
	if err := db.QueryRow(`SELECT COUNT(*) FROM customer_ledger_entries WHERE customer_id = $1`, customerID).Scan(&entries); err != nil {
		t.Fatal(err)
	}
	if entries != 2 {
		t.Fatalf("ledger entries = %d, want the debit and its mirror", entries)
	}

	// The append-only trigger refuses to let anyone rewrite the audit trail.
	if _, err := db.Exec(`UPDATE void_log SET reason = 'nothing happened'`); err == nil {
		t.Fatal("void_log accepted an UPDATE — the append-only trigger is gone")
	}
	if _, err := db.Exec(`DELETE FROM void_log`); err == nil {
		t.Fatal("void_log accepted a DELETE — the append-only trigger is gone")
	}
	if _, err := db.Exec(`UPDATE customer_ledger_entries SET debit = 0`); err == nil {
		t.Fatal("customer_ledger_entries accepted an UPDATE — the append-only trigger is gone")
	}

	// Voiding it twice is refused, and the invoice stays visible.
	w = doJSON(r, http.MethodPost, path, models.VoidInvoiceRequest{Reason: "wrong weight on the scale", Pin: "4321"})
	if w.Code != http.StatusConflict || errCode(decodeEnvelope(t, w)) != "invoice_already_voided" {
		t.Fatalf("second void: %d %s", w.Code, w.Body.String())
	}
	w = doJSON(r, http.MethodGet, "/invoices/"+inv.ID.String(), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("voided invoice should still be readable: %d %s", w.Code, w.Body.String())
	}

	// And it is out of every total, while the cash sale beside it is not.
	expected, err := dayops.ComputeExpected(db, dayID)
	if err != nil {
		t.Fatal(err)
	}
	if !sameMoney(expected.OnAccountSales, 0) || expected.VoidCount != 1 || expected.InvoiceCount != 1 ||
		!sameMoney(expected.CashSales, 500) || !sameMoney(expected.NetSales, 500) {
		t.Fatalf("voided invoice still in the totals: %+v", expected)
	}
}

// The refusals that keep a malformed cart out of the ledger.
func TestInvoice_ValidationRefusals(t *testing.T) {
	db := testdb.Fresh(t)
	ownerID := seedUser(t, db, "owner", "admin", "owner-pass-1", nil)
	openBusinessDay(t, db, ownerID, businessToday())
	productID := seedProductRow(t, db, "LPG bulk", 250)
	var inactiveID uuid.UUID
	if err := db.QueryRow(`INSERT INTO products (name, rate, is_active) VALUES ('Retired', 100, false) RETURNING id`).
		Scan(&inactiveID); err != nil {
		t.Fatal(err)
	}
	r := moneyRouter(db, actor{id: ownerID, username: "owner", role: "admin"})

	line := func(id string, qty float64) []models.InvoiceLineRequest {
		return []models.InvoiceLineRequest{{ProductID: id, Quantity: qty, EnteredAs: "kg"}}
	}
	pct := 150.0
	pct3dp := 12.345
	cases := []struct {
		name string
		body models.CreateInvoiceRequest
		code string
		want int
	}{
		{"no lines", models.CreateInvoiceRequest{PaymentMethod: "cash"}, "invalid_request", http.StatusBadRequest},
		{"unknown tender", models.CreateInvoiceRequest{PaymentMethod: "cheque", Lines: line(productID.String(), 1)}, "invalid_request", http.StatusBadRequest},
		{"unknown entry mode", models.CreateInvoiceRequest{PaymentMethod: "cash",
			Lines: []models.InvoiceLineRequest{{ProductID: productID.String(), Quantity: 1, EnteredAs: "pounds"}}}, "invalid_request", http.StatusBadRequest},
		{"zero quantity", models.CreateInvoiceRequest{PaymentMethod: "cash", Lines: line(productID.String(), 0)}, "invalid_quantity", http.StatusBadRequest},
		{"negative quantity", models.CreateInvoiceRequest{PaymentMethod: "cash", Lines: line(productID.String(), -5)}, "invalid_quantity", http.StatusBadRequest},
		{"four decimal places", models.CreateInvoiceRequest{PaymentMethod: "cash", Lines: line(productID.String(), 1.2345)}, "invalid_quantity", http.StatusBadRequest},
		{"unparseable product", models.CreateInvoiceRequest{PaymentMethod: "cash", Lines: line("not-a-uuid", 1)}, "invalid_request", http.StatusBadRequest},
		{"unknown product", models.CreateInvoiceRequest{PaymentMethod: "cash", Lines: line(uuid.NewString(), 1)}, "product_not_found", http.StatusNotFound},
		{"inactive product", models.CreateInvoiceRequest{PaymentMethod: "cash", Lines: line(inactiveID.String(), 1)}, "product_not_found", http.StatusNotFound},
		{"negative discount", models.CreateInvoiceRequest{PaymentMethod: "cash", Lines: line(productID.String(), 1), DiscountAmount: -10}, "invalid_discount", http.StatusBadRequest},
		{"discount over 100%", models.CreateInvoiceRequest{PaymentMethod: "cash", Lines: line(productID.String(), 1), DiscountPercent: &pct}, "invalid_discount", http.StatusBadRequest},
		{"discount percent with 3 decimal places", models.CreateInvoiceRequest{PaymentMethod: "cash", Lines: line(productID.String(), 1), DiscountPercent: &pct3dp}, "invalid_discount", http.StatusBadRequest},
		{"unknown customer", models.CreateInvoiceRequest{PaymentMethod: "cash", CustomerID: uuid.NewString(), Lines: line(productID.String(), 1)}, "customer_not_found", http.StatusNotFound},
	}
	for _, tc := range cases {
		tc.body.ClientOpID = uuid.NewString()
		w := doJSON(r, http.MethodPost, "/invoices", tc.body)
		if w.Code != tc.want || errCode(decodeEnvelope(t, w)) != tc.code {
			t.Errorf("%s: got %d %s, want %d %s", tc.name, w.Code, w.Body.String(), tc.want, tc.code)
		}
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM invoices`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("refused requests wrote %d invoices", count)
	}
}

// client_op_id is mandatory: a blank one or one that is not a UUID is refused
// before the request is looked at any further, not silently accepted as "no
// idempotency key given".
func TestInvoice_ClientOpIDIsMandatory(t *testing.T) {
	db := testdb.Fresh(t)
	ownerID := seedUser(t, db, "owner", "admin", "owner-pass-1", nil)
	openBusinessDay(t, db, ownerID, businessToday())
	productID := seedProductRow(t, db, "LPG bulk", 250)
	r := moneyRouter(db, actor{id: ownerID, username: "owner", role: "admin"})

	body := models.CreateInvoiceRequest{
		PaymentMethod: "cash",
		Lines:         []models.InvoiceLineRequest{{ProductID: productID.String(), Quantity: 1, EnteredAs: "kg"}},
	}
	w := doJSON(r, http.MethodPost, "/invoices", body)
	if w.Code != http.StatusBadRequest || errCode(decodeEnvelope(t, w)) != "invalid_request" {
		t.Fatalf("blank client_op_id: %d %s", w.Code, w.Body.String())
	}

	body.ClientOpID = "not-a-uuid"
	w = doJSON(r, http.MethodPost, "/invoices", body)
	if w.Code != http.StatusBadRequest || errCode(decodeEnvelope(t, w)) != "invalid_request" {
		t.Fatalf("malformed client_op_id: %d %s", w.Code, w.Body.String())
	}

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM invoices`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("refused requests wrote %d invoices", count)
	}
}

// A discount is allocated across the lines pro rata and taxed per line, and
// the invoice-level figures are what the lines add up to.
func TestInvoice_DiscountIsAllocatedAcrossLines(t *testing.T) {
	db := testdb.Fresh(t)
	ownerID := seedUser(t, db, "owner", "admin", "owner-pass-1", nil)
	openBusinessDay(t, db, ownerID, businessToday())
	bulkID := seedProductRow(t, db, "LPG bulk", 250)
	cylinderID := seedProductRow(t, db, "Cylinder refill", 300)
	setSetting(t, db, "tax_rate_cash", "0.18")
	r := moneyRouter(db, actor{id: ownerID, username: "owner", role: "admin"})

	w := doJSON(r, http.MethodPost, "/invoices", models.CreateInvoiceRequest{
		ClientOpID:    uuid.NewString(),
		PaymentMethod: "cash",
		Lines: []models.InvoiceLineRequest{
			{ProductID: bulkID.String(), Quantity: 10, EnteredAs: "kg"},    // 2 500.00
			{ProductID: cylinderID.String(), Quantity: 5, EnteredAs: "kg"}, // 1 500.00
		},
		DiscountAmount: 100,
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var inv models.Invoice
	dataAs(t, decodeEnvelope(t, w), &inv)
	if !sameMoney(inv.Subtotal, 4000) || !sameMoney(inv.DiscountAmount, 100) {
		t.Fatalf("subtotal/discount: %+v", inv)
	}
	var lineDiscounts, lineTax, taxable float64
	for _, l := range inv.Lines {
		lineDiscounts += l.LineDiscount
		lineTax += l.LineTax
		taxable += l.LineTotal - l.LineDiscount
	}
	if !sameMoney(taxable, 3900) {
		t.Fatalf("Σ taxable = %v, want 3900", taxable)
	}
	if !sameMoney(lineDiscounts, inv.DiscountAmount) {
		t.Fatalf("Σ line_discount = %v, invoice discount = %v", lineDiscounts, inv.DiscountAmount)
	}
	if !sameMoney(lineTax, inv.TaxAmount) {
		t.Fatalf("Σ line_tax = %v, invoice tax = %v", lineTax, inv.TaxAmount)
	}
	// 2 500 : 1 500 split of a Rs 100 discount is 62.50 / 37.50.
	if !sameMoney(inv.Lines[0].LineDiscount, 62.50) || !sameMoney(inv.Lines[1].LineDiscount, 37.50) {
		t.Fatalf("pro-rata split: %v / %v", inv.Lines[0].LineDiscount, inv.Lines[1].LineDiscount)
	}
	// 3 900 taxable + 702 tax = 4 602 exactly, so nothing to round.
	if !sameMoney(inv.TotalAmount, 4602) || inv.TotalPayable != 4602 || !sameMoney(inv.RoundingAdjustment, 0) {
		t.Fatalf("totals: %+v", inv)
	}
}

// The browser: filters on business_date and the facets, free text over the
// number and the snapshotted customer name, plus the recent strip and the
// single-invoice read.
func TestInvoice_ListRecentAndGet(t *testing.T) {
	db := testdb.Fresh(t)
	ownerID := seedUser(t, db, "owner", "admin", "owner-pass-1", nil)
	setPin(t, db, ownerID, "4321")
	openBusinessDay(t, db, ownerID, businessToday())
	productID := seedProductRow(t, db, "LPG bulk", 250)
	customerID := seedCustomerRow(t, db, "Ali Traders", true, nil)
	r := moneyRouter(db, actor{id: ownerID, username: "owner", role: "admin"})

	create := func(method, customer string, qty float64) models.Invoice {
		t.Helper()
		w := doJSON(r, http.MethodPost, "/invoices", models.CreateInvoiceRequest{
			ClientOpID:    uuid.NewString(),
			CustomerID:    customer,
			PaymentMethod: method,
			Lines:         []models.InvoiceLineRequest{{ProductID: productID.String(), Quantity: qty, EnteredAs: "kg"}},
		})
		if w.Code != http.StatusCreated {
			t.Fatalf("create %s: %d %s", method, w.Code, w.Body.String())
		}
		var inv models.Invoice
		dataAs(t, decodeEnvelope(t, w), &inv)
		return inv
	}
	cash := create("cash", "", 1)
	card := create("card", "", 2)
	credit := create("credit", customerID.String(), 3)

	type page struct {
		Data []models.Invoice `json:"data"`
		Meta models.MetaData  `json:"meta"`
	}
	list := func(query string) page {
		t.Helper()
		w := doJSON(r, http.MethodGet, "/invoices"+query, nil)
		if w.Code != http.StatusOK {
			t.Fatalf("list %s: %d %s", query, w.Code, w.Body.String())
		}
		var p page
		if err := jsonUnmarshal(w.Body.Bytes(), &p); err != nil {
			t.Fatal(err)
		}
		return p
	}

	all := list("")
	if all.Meta.Total != 3 || len(all.Data) != 3 {
		t.Fatalf("list all: %+v", all.Meta)
	}
	// Newest first, and a listing carries no lines.
	if all.Data[0].ID != credit.ID || len(all.Data[0].Lines) != 0 {
		t.Fatalf("list order/shape: %+v", all.Data[0])
	}
	if got := list("?payment_method=cash"); got.Meta.Total != 1 || got.Data[0].ID != cash.ID {
		t.Fatalf("filter by tender: %+v", got.Meta)
	}
	if got := list("?customer_id=" + customerID.String()); got.Meta.Total != 1 || got.Data[0].ID != credit.ID {
		t.Fatalf("filter by customer: %+v", got.Meta)
	}
	if got := list("?cashier_id=" + ownerID.String()); got.Meta.Total != 3 {
		t.Fatalf("filter by cashier: %+v", got.Meta)
	}
	if got := list("?search=" + card.InvoiceNumber); got.Meta.Total != 1 || got.Data[0].ID != card.ID {
		t.Fatalf("search by number: %+v", got.Meta)
	}
	if got := list("?search=Ali"); got.Meta.Total != 1 || got.Data[0].ID != credit.ID {
		t.Fatalf("search by customer name: %+v", got.Meta)
	}
	today := businessToday().Format(dayops.DateLayout)
	if got := list("?from=" + today + "&to=" + today); got.Meta.Total != 3 {
		t.Fatalf("filter by date: %+v", got.Meta)
	}
	tomorrow := businessToday().AddDate(0, 0, 1).Format(dayops.DateLayout)
	if got := list("?from=" + tomorrow); got.Meta.Total != 0 {
		t.Fatalf("future window should be empty: %+v", got.Meta)
	}
	if got := list("?status=voided"); got.Meta.Total != 0 {
		t.Fatalf("nothing is voided yet: %+v", got.Meta)
	}
	if w := doJSON(r, http.MethodGet, "/invoices?from=nonsense", nil); w.Code != http.StatusBadRequest {
		t.Fatalf("bad date filter: %d %s", w.Code, w.Body.String())
	}

	// Voiding moves an invoice between the two status filters rather than
	// removing it.
	w := doJSON(r, http.MethodPost, "/invoices/"+cash.ID.String()+"/void",
		models.VoidInvoiceRequest{Reason: "rang up twice", Pin: "4321"})
	if w.Code != http.StatusOK {
		t.Fatalf("void: %d %s", w.Code, w.Body.String())
	}
	if got := list("?status=voided"); got.Meta.Total != 1 || got.Data[0].ID != cash.ID {
		t.Fatalf("voided filter: %+v", got.Meta)
	}
	if got := list("?status=completed"); got.Meta.Total != 2 {
		t.Fatalf("completed filter: %+v", got.Meta)
	}

	// The recent strip.
	w = doJSON(r, http.MethodGet, "/invoices/recent?limit=2", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("recent: %d %s", w.Code, w.Body.String())
	}
	var recent []models.Invoice
	dataAs(t, decodeEnvelope(t, w), &recent)
	if len(recent) != 2 || recent[0].ID != credit.ID {
		t.Fatalf("recent: %+v", recent)
	}

	// One invoice, with its lines.
	w = doJSON(r, http.MethodGet, "/invoices/"+credit.ID.String(), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("get: %d %s", w.Code, w.Body.String())
	}
	var one models.Invoice
	dataAs(t, decodeEnvelope(t, w), &one)
	if one.ID != credit.ID || len(one.Lines) != 1 || !sameMoney(one.Lines[0].LineTotal, 750) {
		t.Fatalf("get: %+v", one)
	}
	if w := doJSON(r, http.MethodGet, "/invoices/"+uuid.NewString(), nil); w.Code != http.StatusNotFound ||
		errCode(decodeEnvelope(t, w)) != "invoice_not_found" {
		t.Fatalf("get unknown: %d %s", w.Code, w.Body.String())
	}
	if w := doJSON(r, http.MethodGet, "/invoices/not-a-uuid", nil); w.Code != http.StatusNotFound {
		t.Fatalf("get malformed: %d %s", w.Code, w.Body.String())
	}
	if w := doJSON(r, http.MethodPost, "/invoices/"+uuid.NewString()+"/void",
		models.VoidInvoiceRequest{Reason: "nothing", Pin: "4321"}); w.Code != http.StatusNotFound ||
		errCode(decodeEnvelope(t, w)) != "invoice_not_found" {
		t.Fatalf("void unknown: %d %s", w.Code, w.Body.String())
	}
}
