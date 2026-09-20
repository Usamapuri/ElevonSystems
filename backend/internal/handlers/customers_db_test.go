package handlers

import (
	"net/http"
	"strings"
	"testing"

	"elevon-backend/internal/ledger"
	"elevon-backend/internal/models"
	"elevon-backend/internal/testdb"

	"github.com/gin-gonic/gin"
)

func customersRouter(h *CustomersHandler, a actor) *gin.Engine {
	r := gin.New()
	staff := r.Group("", asActor(a))
	staff.GET("/customers", h.List)
	staff.GET("/customers/:id", h.Get)
	staff.GET("/customers/:id/statement", h.Statement)
	admin := r.Group("/admin", asActor(a))
	admin.POST("/customers", h.Create)
	admin.PUT("/customers/:id", h.Update)
	return r
}

func TestCustomers_CreateListSearchAndConflicts(t *testing.T) {
	db := testdb.Fresh(t)
	ownerID := seedUser(t, db, "owner", "admin", "owner-pass-1", nil)
	r := customersRouter(NewCustomersHandler(db), actor{id: ownerID, username: "owner", role: "admin"})

	limit := 5000.0
	w := doJSON(r, http.MethodPost, "/admin/customers", models.CreateCustomerRequest{
		Name: " Ali Traders ", Phone: " 0300 1234567 ", NTN: " 1234567-8 ", CNIC: " 3520112345678 ",
		BuyerRegistrationType: "Registered", Address: " Shop 4, Main Market ", Province: " Punjab ",
		CreditAllowed: true, CreditLimit: &limit, Notes: " VIP customer ",
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var created models.Customer
	dataAs(t, decodeEnvelope(t, w), &created)
	if created.Name != "Ali Traders" || created.Phone == nil || *created.Phone != "0300 1234567" ||
		created.NTN == nil || *created.NTN != "1234567-8" || created.CNIC == nil || *created.CNIC != "3520112345678" ||
		created.BuyerRegistrationType != "Registered" || created.Address == nil || *created.Address != "Shop 4, Main Market" ||
		created.Province == nil || *created.Province != "Punjab" || !created.CreditAllowed ||
		created.CreditLimit == nil || *created.CreditLimit != 5000 || created.Balance != 0 || !created.IsActive {
		t.Fatalf("normalised customer: %+v", created)
	}

	// A blank buyer_registration_type defaults to Unregistered.
	w = doJSON(r, http.MethodPost, "/admin/customers", models.CreateCustomerRequest{Name: "Beta Gas", Phone: "0311 1111111"})
	var second models.Customer
	dataAs(t, decodeEnvelope(t, w), &second)
	if w.Code != http.StatusCreated || second.BuyerRegistrationType != "Unregistered" {
		t.Fatalf("default buyer type: %d %+v", w.Code, second)
	}

	negative := -1.0
	longProvince := strings.Repeat("x", 41)
	for _, tc := range []struct {
		req  models.CreateCustomerRequest
		code string
	}{
		{models.CreateCustomerRequest{Name: ""}, "invalid_name"},
		{models.CreateCustomerRequest{Name: "Gamma", Phone: "abc-123"}, "invalid_phone"},
		{models.CreateCustomerRequest{Name: "Gamma", NTN: "abc"}, "invalid_ntn"},
		{models.CreateCustomerRequest{Name: "Gamma", CNIC: "12-345"}, "invalid_cnic"},
		{models.CreateCustomerRequest{Name: "Gamma", BuyerRegistrationType: "Foreign"}, "invalid_buyer_registration_type"},
		{models.CreateCustomerRequest{Name: "Gamma", Province: longProvince}, "invalid_province"},
		{models.CreateCustomerRequest{Name: "Gamma", CreditLimit: &negative}, "invalid_credit_limit"},
	} {
		w := doJSON(r, http.MethodPost, "/admin/customers", tc.req)
		if got := errCode(decodeEnvelope(t, w)); got != tc.code {
			t.Errorf("%+v: want %s got %s (%d %s)", tc.req, tc.code, got, w.Code, w.Body.String())
		}
	}

	// Same phone (after trim/normalise) as the first customer -> conflict.
	w = doJSON(r, http.MethodPost, "/admin/customers", models.CreateCustomerRequest{Name: "Duplicate Phone", Phone: "0300 1234567"})
	if got := errCode(decodeEnvelope(t, w)); got != "phone_taken" {
		t.Fatalf("phone conflict: %d %s", w.Code, w.Body.String())
	}

	// List: unfiltered returns both, search narrows to one.
	w = doJSON(r, http.MethodGet, "/customers", nil)
	var all []models.Customer
	dataAs(t, decodeEnvelope(t, w), &all)
	if len(all) != 2 {
		t.Fatalf("unfiltered list: %+v", all)
	}

	w = doJSON(r, http.MethodGet, "/customers?search=ali", nil)
	var page models.PaginatedResponse
	if err := jsonUnmarshal(w.Body.Bytes(), &page); err != nil || w.Code != http.StatusOK || page.Meta.Total != 1 {
		t.Fatalf("search: %d %s (%v)", w.Code, w.Body.String(), err)
	}

	w = doJSON(r, http.MethodGet, "/customers?search=0311", nil)
	_ = jsonUnmarshal(w.Body.Bytes(), &page)
	if page.Meta.Total != 1 {
		t.Fatalf("search by phone: %s", w.Body.String())
	}
}

func TestCustomers_GetNotFound(t *testing.T) {
	db := testdb.Fresh(t)
	ownerID := seedUser(t, db, "owner", "admin", "owner-pass-1", nil)
	r := customersRouter(NewCustomersHandler(db), actor{id: ownerID, username: "owner", role: "admin"})

	if w := doJSON(r, http.MethodGet, "/customers/00000000-0000-0000-0000-000000000000", nil); w.Code != http.StatusNotFound {
		t.Fatalf("unknown customer: %d %s", w.Code, w.Body.String())
	}
	if w := doJSON(r, http.MethodGet, "/customers/not-a-uuid", nil); w.Code != http.StatusNotFound {
		t.Fatalf("bad id: %d", w.Code)
	}
}

func TestCustomers_BalanceReflectsLedgerEntries(t *testing.T) {
	db := testdb.Fresh(t)
	ownerID := seedUser(t, db, "owner", "admin", "owner-pass-1", nil)
	r := customersRouter(NewCustomersHandler(db), actor{id: ownerID, username: "owner", role: "admin"})

	w := doJSON(r, http.MethodPost, "/admin/customers", models.CreateCustomerRequest{Name: "Ledger Co"})
	var cust models.Customer
	dataAs(t, decodeEnvelope(t, w), &cust)
	if cust.Balance != 0 {
		t.Fatalf("new customer balance: got %v want 0", cust.Balance)
	}

	w = doJSON(r, http.MethodGet, "/customers/"+cust.ID.String(), nil)
	var got models.Customer
	dataAs(t, decodeEnvelope(t, w), &got)
	if w.Code != http.StatusOK || got.Balance != 0 {
		t.Fatalf("get balance with no ledger rows: %d %+v", w.Code, got)
	}

	// Post two entries directly via ledger.Post in a transaction — the same
	// way Task D6 will post invoice/receipt entries.
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ledger.Post(tx, ledger.Entry{CustomerID: cust.ID, EntryType: "invoice", Debit: 800}); err != nil {
		t.Fatalf("post invoice entry: %v", err)
	}
	if _, err := ledger.Post(tx, ledger.Entry{CustomerID: cust.ID, EntryType: "receipt", Credit: 300}); err != nil {
		t.Fatalf("post receipt entry: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	w = doJSON(r, http.MethodGet, "/customers/"+cust.ID.String(), nil)
	dataAs(t, decodeEnvelope(t, w), &got)
	if got.Balance != 500 {
		t.Fatalf("balance after entries: got %v want 500", got.Balance)
	}

	// List also carries the updated balance.
	w = doJSON(r, http.MethodGet, "/customers", nil)
	var list []models.Customer
	dataAs(t, decodeEnvelope(t, w), &list)
	found := false
	for _, cc := range list {
		if cc.ID == cust.ID {
			found = true
			if cc.Balance != 500 {
				t.Fatalf("list balance: got %v want 500", cc.Balance)
			}
		}
	}
	if !found {
		t.Fatal("customer missing from list")
	}
}

func TestCustomers_StatementReturnsRunningBalance(t *testing.T) {
	db := testdb.Fresh(t)
	ownerID := seedUser(t, db, "owner", "admin", "owner-pass-1", nil)
	r := customersRouter(NewCustomersHandler(db), actor{id: ownerID, username: "owner", role: "admin"})

	w := doJSON(r, http.MethodPost, "/admin/customers", models.CreateCustomerRequest{Name: "Statement Co"})
	var cust models.Customer
	dataAs(t, decodeEnvelope(t, w), &cust)

	if _, err := ledger.Post(db, ledger.Entry{CustomerID: cust.ID, EntryType: "invoice", Debit: 1200}); err != nil {
		t.Fatal(err)
	}
	if _, err := ledger.Post(db, ledger.Entry{CustomerID: cust.ID, EntryType: "receipt", Credit: 500}); err != nil {
		t.Fatal(err)
	}

	w = doJSON(r, http.MethodGet, "/customers/"+cust.ID.String()+"/statement", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("statement: %d %s", w.Code, w.Body.String())
	}
	var rows []ledger.StatementRow
	dataAs(t, decodeEnvelope(t, w), &rows)
	if len(rows) != 2 || rows[0].RunningBalance != 1200 || rows[1].RunningBalance != 700 {
		t.Fatalf("statement running balance order: %+v", rows)
	}
	if rows[0].EntryType != "invoice" || rows[1].EntryType != "receipt" {
		t.Fatalf("statement entry types: %+v", rows)
	}

	if w := doJSON(r, http.MethodGet, "/customers/00000000-0000-0000-0000-000000000000/statement", nil); w.Code != http.StatusNotFound {
		t.Fatalf("unknown customer statement: %d", w.Code)
	}
	if w := doJSON(r, http.MethodGet, "/customers/"+cust.ID.String()+"/statement?from=not-a-date", nil); errCode(decodeEnvelope(t, w)) != "invalid_request" {
		t.Fatalf("bad from: %s", w.Body.String())
	}
}

func TestCustomers_Update(t *testing.T) {
	db := testdb.Fresh(t)
	ownerID := seedUser(t, db, "owner", "admin", "owner-pass-1", nil)
	r := customersRouter(NewCustomersHandler(db), actor{id: ownerID, username: "owner", role: "admin"})

	w := doJSON(r, http.MethodPost, "/admin/customers", models.CreateCustomerRequest{Name: "Edit Me", Phone: "0300 9999999"})
	var cust models.Customer
	dataAs(t, decodeEnvelope(t, w), &cust)

	newName := "Edited Name"
	newLimit := 10000.0
	creditAllowed := true
	w = doJSON(r, http.MethodPut, "/admin/customers/"+cust.ID.String(), models.UpdateCustomerRequest{
		Name: &newName, CreditAllowed: &creditAllowed, CreditLimit: &newLimit,
	})
	var edited models.Customer
	dataAs(t, decodeEnvelope(t, w), &edited)
	if w.Code != http.StatusOK || edited.Name != newName || !edited.CreditAllowed ||
		edited.CreditLimit == nil || *edited.CreditLimit != 10000 {
		t.Fatalf("update: %d %+v", w.Code, edited)
	}

	if w := doJSON(r, http.MethodPut, "/admin/customers/not-a-uuid", models.UpdateCustomerRequest{Name: &newName}); w.Code != http.StatusNotFound {
		t.Fatalf("bad id: %d", w.Code)
	}
	if w := doJSON(r, http.MethodPut, "/admin/customers/"+cust.ID.String(), models.UpdateCustomerRequest{}); errCode(decodeEnvelope(t, w)) != "no_changes" {
		t.Fatalf("empty update: %s", w.Body.String())
	}

	w2 := doJSON(r, http.MethodPost, "/admin/customers", models.CreateCustomerRequest{Name: "Other", Phone: "0300 1111111"})
	var other models.Customer
	dataAs(t, decodeEnvelope(t, w2), &other)
	dupPhone := "0300 1111111"
	if w := doJSON(r, http.MethodPut, "/admin/customers/"+cust.ID.String(), models.UpdateCustomerRequest{Phone: &dupPhone}); errCode(decodeEnvelope(t, w)) != "phone_taken" {
		t.Fatalf("phone conflict on update: %s", w.Body.String())
	}

	inactive := false
	if w := doJSON(r, http.MethodPut, "/admin/customers/"+cust.ID.String(), models.UpdateCustomerRequest{IsActive: &inactive}); w.Code != http.StatusOK {
		t.Fatalf("deactivate: %d %s", w.Code, w.Body.String())
	}
}
