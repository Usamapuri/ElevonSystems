package handlers

import (
	"maps"
	"net/http"
	"strings"
	"testing"

	"elevon-backend/internal/models"
	"elevon-backend/internal/testdb"

	"github.com/google/uuid"
)

// Spec 2026-10-12 §3: a cashier may name a deal rate per line. The server
// still prices every rupee from it; inside rate_override_band it stands on
// its own, outside it an admin PIN approves it; products.rate never moves.

// dealSale is a one-line cash sale at a deal rate. extra overrides or adds
// top-level request fields (pin, customer_id, payment_method, client_op_id).
func dealSale(productID uuid.UUID, qty, rate float64, extra map[string]any) map[string]any {
	body := map[string]any{
		"client_op_id": uuid.NewString(), "payment_method": "cash",
		"lines": []map[string]any{{"product_id": productID.String(), "quantity": qty, "entered_as": "kg", "rate_override": rate}},
	}
	maps.Copy(body, extra)
	return body
}

// Spec §3.4, row two: 1 000 kg at 249.50 against a 250.00 list rate.
func TestDealRate_WithinBandNeedsNoPin(t *testing.T) {
	db := testdb.Fresh(t)
	cashierID := seedUser(t, db, "counter", "counter", "counter-pass-1", nil)
	openBusinessDay(t, db, cashierID, businessToday())
	productID := seedProductRow(t, db, "LPG bulk", 250.00)
	setSetting(t, db, "tax_rate_cash", "0.18")
	setSetting(t, db, "rate_override_band", "1")
	r := moneyRouter(db, actor{id: cashierID, username: "counter", role: "counter"})

	w := doJSON(r, http.MethodPost, "/invoices", dealSale(productID, 1000, 249.50, nil))
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var inv models.Invoice
	dataAs(t, decodeEnvelope(t, w), &inv)
	if !sameMoney(inv.Subtotal, 249500) || !sameMoney(inv.TaxAmount, 44910) || !sameMoney(inv.TotalAmount, 294410) {
		t.Fatalf("priced at the deal rate: %+v", inv)
	}
	l := inv.Lines[0]
	if !sameMoney(l.UnitPrice, 249.50) || !sameMoney(l.ListRate, 250) || l.RateApprovedBy != nil || l.RateApprovedByName != nil {
		t.Fatalf("line snapshot: %+v", l)
	}
	var rate float64
	if err := db.QueryRow(`SELECT rate::float8 FROM products WHERE id = $1`, productID).Scan(&rate); err != nil || !sameMoney(rate, 250) {
		t.Fatalf("products.rate must not move: %v %v", rate, err)
	}
	var history int
	if err := db.QueryRow(`SELECT count(*) FROM product_rate_history WHERE product_id = $1`, productID).Scan(&history); err != nil || history != 0 {
		t.Fatalf("a deal rate writes no rate history: %d %v", history, err)
	}
}

func TestDealRate_OutsideBandWithoutPinIsRefused(t *testing.T) {
	db := testdb.Fresh(t)
	cashierID := seedUser(t, db, "counter", "counter", "counter-pass-1", nil)
	openBusinessDay(t, db, cashierID, businessToday())
	productID := seedProductRow(t, db, "LPG bulk", 250.00)
	setSetting(t, db, "rate_override_band", "1")
	r := moneyRouter(db, actor{id: cashierID, username: "counter", role: "counter"})

	w := doJSON(r, http.MethodPost, "/invoices", dealSale(productID, 1000, 245.00, nil))
	if w.Code != http.StatusForbidden || errCode(decodeEnvelope(t, w)) != "rate_override_pin_required" {
		t.Fatalf("outside band without pin: %d %s", w.Code, w.Body.String())
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM invoices`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("no invoice may be written: %d %v", n, err)
	}
}

func TestDealRate_OutsideBandWrongPin(t *testing.T) {
	db := testdb.Fresh(t)
	adminID := seedUser(t, db, "owner", "admin", "owner-pass-1", nil)
	setPin(t, db, adminID, "4321")
	cashierID := seedUser(t, db, "counter", "counter", "counter-pass-1", nil)
	openBusinessDay(t, db, adminID, businessToday())
	productID := seedProductRow(t, db, "LPG bulk", 250.00)
	setSetting(t, db, "rate_override_band", "1")
	r := moneyRouter(db, actor{id: cashierID, username: "counter", role: "counter"})

	w := doJSON(r, http.MethodPost, "/invoices", dealSale(productID, 1000, 245.00, map[string]any{"pin": "0000"}))
	if w.Code != http.StatusUnauthorized || errCode(decodeEnvelope(t, w)) != "invalid_pin" {
		t.Fatalf("wrong pin: %d %s", w.Code, w.Body.String())
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM invoices`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("no invoice may be written: %d %v", n, err)
	}
}

func TestDealRate_OutsideBandWithPinStoresApprover(t *testing.T) {
	db := testdb.Fresh(t)
	adminID := seedUser(t, db, "owner", "admin", "owner-pass-1", nil)
	setPin(t, db, adminID, "4321")
	cashierID := seedUser(t, db, "counter", "counter", "counter-pass-1", nil)
	openBusinessDay(t, db, adminID, businessToday())
	productID := seedProductRow(t, db, "LPG bulk", 250.00)
	setSetting(t, db, "rate_override_band", "1")
	r := moneyRouter(db, actor{id: cashierID, username: "counter", role: "counter"})

	w := doJSON(r, http.MethodPost, "/invoices", dealSale(productID, 1000, 245.00, map[string]any{"pin": "4321"}))
	if w.Code != http.StatusCreated {
		t.Fatalf("create with pin: %d %s", w.Code, w.Body.String())
	}
	var inv models.Invoice
	dataAs(t, decodeEnvelope(t, w), &inv)
	l := inv.Lines[0]
	if l.RateApprovedBy == nil || *l.RateApprovedBy != adminID || l.RateApprovedByName == nil || *l.RateApprovedByName == "" {
		t.Fatalf("approver recorded: %+v", l)
	}
	if !sameMoney(l.UnitPrice, 245) || !sameMoney(l.ListRate, 250) || !sameMoney(inv.Subtotal, 245000) {
		t.Fatalf("charged at the approved rate: %+v %+v", l, inv)
	}
}

func TestDealRate_DefaultBandZeroAlwaysAsks(t *testing.T) {
	db := testdb.Fresh(t)
	cashierID := seedUser(t, db, "counter", "counter", "counter-pass-1", nil)
	openBusinessDay(t, db, cashierID, businessToday())
	productID := seedProductRow(t, db, "LPG bulk", 250.00)
	r := moneyRouter(db, actor{id: cashierID, username: "counter", role: "counter"})

	w := doJSON(r, http.MethodPost, "/invoices", dealSale(productID, 10, 249.99, nil))
	if w.Code != http.StatusForbidden || errCode(decodeEnvelope(t, w)) != "rate_override_pin_required" {
		t.Fatalf("seeded band is 0, one paisa needs a pin: %d %s", w.Code, w.Body.String())
	}
}

func TestDealRate_EqualToListIsNotAnOverride(t *testing.T) {
	db := testdb.Fresh(t)
	cashierID := seedUser(t, db, "counter", "counter", "counter-pass-1", nil)
	openBusinessDay(t, db, cashierID, businessToday())
	productID := seedProductRow(t, db, "LPG bulk", 250.00)
	r := moneyRouter(db, actor{id: cashierID, username: "counter", role: "counter"})

	w := doJSON(r, http.MethodPost, "/invoices", dealSale(productID, 10, 250.00, nil))
	if w.Code != http.StatusCreated {
		t.Fatalf("list rate typed in is not an override: %d %s", w.Code, w.Body.String())
	}
	var inv models.Invoice
	dataAs(t, decodeEnvelope(t, w), &inv)
	if inv.Lines[0].RateApprovedBy != nil || !sameMoney(inv.Lines[0].UnitPrice, 250) {
		t.Fatalf("line: %+v", inv.Lines[0])
	}
}

func TestDealRate_RetryWithPinMakesOneInvoice(t *testing.T) {
	db := testdb.Fresh(t)
	adminID := seedUser(t, db, "owner", "admin", "owner-pass-1", nil)
	setPin(t, db, adminID, "4321")
	openBusinessDay(t, db, adminID, businessToday())
	productID := seedProductRow(t, db, "LPG bulk", 250.00)
	r := moneyRouter(db, actor{id: adminID, username: "owner", role: "admin"})

	body := dealSale(productID, 100, 240.00, nil)
	w := doJSON(r, http.MethodPost, "/invoices", body)
	if w.Code != http.StatusForbidden {
		t.Fatalf("first attempt: %d %s", w.Code, w.Body.String())
	}
	body["pin"] = "4321"
	w = doJSON(r, http.MethodPost, "/invoices", body)
	if w.Code != http.StatusCreated {
		t.Fatalf("retry with pin: %d %s", w.Code, w.Body.String())
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM invoices`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("exactly one invoice: %d %v", n, err)
	}
}

func TestDealRate_OnePinCoversCreditLimitAndRate(t *testing.T) {
	db := testdb.Fresh(t)
	adminID := seedUser(t, db, "owner", "admin", "owner-pass-1", nil)
	setPin(t, db, adminID, "4321")
	openBusinessDay(t, db, adminID, businessToday())
	productID := seedProductRow(t, db, "LPG bulk", 250.00)
	setSetting(t, db, "credit_limit_enforced", "true")
	limit := 1000.0
	customerID := seedCustomerRow(t, db, "Ali Traders", true, &limit)
	r := moneyRouter(db, actor{id: adminID, username: "owner", role: "admin"})

	body := dealSale(productID, 100, 240.00, map[string]any{
		"payment_method": "credit", "customer_id": customerID.String(), "pin": "4321",
	})
	w := doJSON(r, http.MethodPost, "/invoices", body)
	if w.Code != http.StatusCreated {
		t.Fatalf("one pin for both approvals: %d %s", w.Code, w.Body.String())
	}
	var inv models.Invoice
	dataAs(t, decodeEnvelope(t, w), &inv)
	if inv.Notes == nil || !strings.Contains(*inv.Notes, "credit limit overridden by") {
		t.Fatalf("credit override noted: %+v", inv.Notes)
	}
	if inv.Lines[0].RateApprovedBy == nil || *inv.Lines[0].RateApprovedBy != adminID {
		t.Fatalf("rate approved by the same pin: %+v", inv.Lines[0])
	}
}

func TestDealRate_BandMeasuredFromLockedRate(t *testing.T) {
	db := testdb.Fresh(t)
	adminID := seedUser(t, db, "owner", "admin", "owner-pass-1", nil)
	setPin(t, db, adminID, "4321")
	openBusinessDay(t, db, adminID, businessToday())
	productID := seedProductRow(t, db, "LPG bulk", 250.00)
	setSetting(t, db, "rate_override_band", "1")
	r := moneyRouter(db, actor{id: adminID, username: "owner", role: "admin"})

	// The cart was built at 250; the list rate moved to 260 before Charge.
	if _, err := db.Exec(`UPDATE products SET rate = 260 WHERE id = $1`, productID); err != nil {
		t.Fatal(err)
	}
	w := doJSON(r, http.MethodPost, "/invoices", dealSale(productID, 10, 250.00, nil))
	if w.Code != http.StatusForbidden {
		t.Fatalf("250 is ten rupees from the live 260: %d %s", w.Code, w.Body.String())
	}
	w = doJSON(r, http.MethodPost, "/invoices", dealSale(productID, 10, 250.00, map[string]any{"pin": "4321"}))
	if w.Code != http.StatusCreated {
		t.Fatalf("with pin: %d %s", w.Code, w.Body.String())
	}
	var inv models.Invoice
	dataAs(t, decodeEnvelope(t, w), &inv)
	if !sameMoney(inv.Lines[0].ListRate, 260) || !sameMoney(inv.Lines[0].UnitPrice, 250) {
		t.Fatalf("list rate is the locked row's: %+v", inv.Lines[0])
	}
}

func TestDealRate_InvalidRates(t *testing.T) {
	db := testdb.Fresh(t)
	adminID := seedUser(t, db, "owner", "admin", "owner-pass-1", nil)
	openBusinessDay(t, db, adminID, businessToday())
	productID := seedProductRow(t, db, "LPG bulk", 250.00)
	r := moneyRouter(db, actor{id: adminID, username: "owner", role: "admin"})

	for _, rate := range []float64{0, -5, 249.505} {
		w := doJSON(r, http.MethodPost, "/invoices", dealSale(productID, 10, rate, nil))
		if w.Code != http.StatusBadRequest || errCode(decodeEnvelope(t, w)) != "invalid_rate" {
			t.Errorf("rate %v: %d %s", rate, w.Code, w.Body.String())
		}
	}
}
