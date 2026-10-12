package handlers

import (
	"net/http"
	"testing"
	"time"

	"elevon-backend/internal/dayops"
	"elevon-backend/internal/models"
	"elevon-backend/internal/testdb"

	"github.com/google/uuid"
)

// Back-dated invoices (spec 2026-10-12 §8): an admin may give a sale an
// invoice_date up to 30 days before the current business date. The sale is
// still numbered, drawered and reported on today's business day; only the
// document date and what FBR is told change.

func backdateBody(productID uuid.UUID, invoiceDate string) map[string]any {
	body := map[string]any{
		"client_op_id":   uuid.NewString(),
		"payment_method": "cash",
		"lines":          []map[string]any{{"product_id": productID.String(), "quantity": 10, "entered_as": "kg"}},
	}
	if invoiceDate != "" {
		body["invoice_date"] = invoiceDate
	}
	return body
}

func TestInvoice_BackDated_AdminWithinThirtyDays(t *testing.T) {
	db := testdb.Fresh(t)
	ownerID := seedUser(t, db, "owner", "admin", "owner-pass-1", nil)
	today := businessToday()
	openBusinessDay(t, db, ownerID, today)
	productID := seedProductRow(t, db, "LPG bulk", 250.00)
	setSetting(t, db, "tax_rate_cash", "0.18")
	r := moneyRouter(db, actor{id: ownerID, username: "owner", role: "admin"})

	past := today.AddDate(0, 0, -3)
	w := doJSON(r, http.MethodPost, "/invoices", backdateBody(productID, past.Format(dayops.DateLayout)))
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var inv models.Invoice
	dataAs(t, decodeEnvelope(t, w), &inv)

	if inv.InvoiceDate.Format(dayops.DateLayout) != past.Format(dayops.DateLayout) {
		t.Fatalf("invoice_date: got %s want %s", inv.InvoiceDate.Format(dayops.DateLayout), past.Format(dayops.DateLayout))
	}
	// Still today's sale in every other respect.
	if inv.BusinessDate.Format(dayops.DateLayout) != today.Format(dayops.DateLayout) {
		t.Fatalf("business_date moved: %s", inv.BusinessDate)
	}
	if want := today.Format("20060102") + "-001"; inv.InvoiceNumber != want {
		t.Fatalf("number: got %s want %s", inv.InvoiceNumber, want)
	}

	// GET returns the same date, so the drawer and the reprint agree with
	// the slip that was printed.
	g := doJSON(r, http.MethodGet, "/invoices/"+inv.ID.String(), nil)
	var again models.Invoice
	dataAs(t, decodeEnvelope(t, g), &again)
	if again.InvoiceDate.Format(dayops.DateLayout) != past.Format(dayops.DateLayout) {
		t.Fatalf("get invoice_date: %s", again.InvoiceDate)
	}
}

func TestInvoice_BackDated_OmittedEqualsBusinessDate(t *testing.T) {
	db := testdb.Fresh(t)
	ownerID := seedUser(t, db, "owner", "admin", "owner-pass-1", nil)
	today := businessToday()
	openBusinessDay(t, db, ownerID, today)
	productID := seedProductRow(t, db, "LPG bulk", 250.00)
	r := moneyRouter(db, actor{id: ownerID, username: "owner", role: "admin"})

	w := doJSON(r, http.MethodPost, "/invoices", backdateBody(productID, ""))
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var inv models.Invoice
	dataAs(t, decodeEnvelope(t, w), &inv)
	if inv.InvoiceDate.Format(dayops.DateLayout) != today.Format(dayops.DateLayout) {
		t.Fatalf("invoice_date should default to the business date: %s vs %s", inv.InvoiceDate, inv.BusinessDate)
	}
}

func TestInvoice_BackDated_Refusals(t *testing.T) {
	db := testdb.Fresh(t)
	ownerID := seedUser(t, db, "owner", "admin", "owner-pass-1", nil)
	counterID := seedUser(t, db, "till1", "counter", "till-pass-1", nil)
	today := businessToday()
	openBusinessDay(t, db, ownerID, today)
	productID := seedProductRow(t, db, "LPG bulk", 250.00)
	admin := moneyRouter(db, actor{id: ownerID, username: "owner", role: "admin"})
	counter := moneyRouter(db, actor{id: counterID, username: "till1", role: "counter"})

	cases := []struct {
		name   string
		router http.Handler
		date   string
		status int
		code   string
	}{
		{"counter cannot back-date", counter, today.AddDate(0, 0, -1).Format(dayops.DateLayout), http.StatusForbidden, "invoice_date_requires_admin"},
		{"tomorrow", admin, today.AddDate(0, 0, 1).Format(dayops.DateLayout), http.StatusBadRequest, "invoice_date_future"},
		{"31 days back", admin, today.AddDate(0, 0, -31).Format(dayops.DateLayout), http.StatusBadRequest, "invoice_date_too_old"},
		{"garbage", admin, "12/10/2026", http.StatusBadRequest, "invalid_request"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := doJSON(c.router, http.MethodPost, "/invoices", backdateBody(productID, c.date))
			env := decodeEnvelope(t, w)
			code := ""
			if env.Error != nil {
				code = *env.Error
			}
			if w.Code != c.status || code != c.code {
				t.Fatalf("got %d %q (%s), want %d %q", w.Code, code, env.Message, c.status, c.code)
			}
		})
	}

	// The edges are allowed: today itself (for either role — it changes
	// nothing) and exactly 30 days back for an admin.
	for _, c := range []struct {
		name   string
		router http.Handler
		date   time.Time
	}{
		{"counter sends today", counter, today},
		{"admin sends 30 days back", admin, today.AddDate(0, 0, -30)},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := doJSON(c.router, http.MethodPost, "/invoices", backdateBody(productID, c.date.Format(dayops.DateLayout)))
			if w.Code != http.StatusCreated {
				t.Fatalf("create: %d %s", w.Code, w.Body.String())
			}
		})
	}
}
