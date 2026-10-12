package handlers

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"elevon-backend/internal/fiscal"
	"elevon-backend/internal/models"
	"elevon-backend/internal/testdb"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// Filing is the cashier's choice (spec 2026-10-12 §2.2) and the void rules
// follow the invoice's fiscal state (§2.5). These drive POST /invoices and
// the void route against a fake FBR.

type fakeFBR struct {
	server       *httptest.Server
	validateBody []byte
	postBody     []byte
	status       int
	calls        int
}

func fiscalFixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "fiscal", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func newFakeFBR(t *testing.T, validateBody, postBody []byte, status int) *fakeFBR {
	t.Helper()
	f := &fakeFBR{validateBody: validateBody, postBody: postBody, status: status}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.calls++
		w.WriteHeader(f.status)
		if r.URL.Path == "/post" {
			_, _ = w.Write(f.postBody)
			return
		}
		_, _ = w.Write(f.validateBody)
	}))
	t.Cleanup(f.server.Close)
	return f
}

func acceptingFBR(t *testing.T) *fakeFBR {
	return newFakeFBR(t, fiscalFixture(t, "resp_caseC.json"), fiscalFixture(t, "resp_postA.json"), http.StatusOK)
}

func rejectingFBR(t *testing.T) *fakeFBR {
	return newFakeFBR(t, fiscalFixture(t, "resp_negTax.json"), fiscalFixture(t, "resp_postA.json"), http.StatusOK)
}

func unreachableFBR(t *testing.T) *fakeFBR {
	return newFakeFBR(t, []byte(`<html>bad gateway</html>`), nil, http.StatusBadGateway)
}

// enableFiscalAt writes an enabled sandbox config pointing at the fake and
// stores an encrypted sandbox token, as Settings → FBR would.
func enableFiscalAt(t *testing.T, db *sql.DB, f *fakeFBR) {
	t.Helper()
	t.Setenv("GIN_MODE", "debug")
	t.Setenv(fiscal.EnvSecretsKey, base64.StdEncoding.EncodeToString([]byte("0123456789abcdef0123456789abcdef")))
	cfg := fiscal.Defaults()
	cfg.Enabled = true
	cfg.IsSandbox = true
	cfg.SellerNTNCNIC = "8951943"
	cfg.SellerBusinessName = "Elevon Test Seller"
	cfg.SellerProvince = "PUNJAB"
	cfg.SellerAddress = "Lahore"
	if f != nil {
		cfg.ValidateURLOverride = f.server.URL + "/validate"
		cfg.PostURLOverride = f.server.URL + "/post"
	}
	raw, _ := json.Marshal(cfg)
	setSetting(t, db, fiscal.KeyConfig, string(raw))
	key, err := fiscal.KeyFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := fiscal.Encrypt(key, "sandbox-token-value-9f3c")
	if err != nil {
		t.Fatal(err)
	}
	enc, _ := json.Marshal(sealed)
	setSetting(t, db, fiscal.KeyAPIKeySandboxEnc, string(enc))
	setSetting(t, db, "default_hs_code", `"2711.1910"`)
	setSetting(t, db, "tax_rate_cash", "0.18")
}

func fiscalRouter(db *sql.DB, a actor) *gin.Engine {
	r := gin.New()
	inv := NewInvoicesHandler(db).WithFiscal(fiscal.Deps{DB: db, Timeout: 5 * time.Second})
	staff := r.Group("", asActor(a))
	staff.POST("/invoices", inv.Create)
	staff.POST("/invoices/:id/void", inv.Void)
	return r
}

type fiscalSale struct {
	fileFBR    bool
	customerID *uuid.UUID
}

func ringSale(t *testing.T, r http.Handler, productID uuid.UUID, o fiscalSale) models.Invoice {
	t.Helper()
	req := models.CreateInvoiceRequest{
		ClientOpID:    uuid.NewString(),
		PaymentMethod: "cash",
		FileFBR:       o.fileFBR,
		Lines:         []models.InvoiceLineRequest{{ProductID: productID.String(), Quantity: 12.5, EnteredAs: "kg"}},
	}
	if o.customerID != nil {
		req.CustomerID = o.customerID.String()
	}
	w := doJSON(r, http.MethodPost, "/invoices", req)
	if w.Code != http.StatusCreated {
		t.Fatalf("sale: %d %s", w.Code, w.Body.String())
	}
	var inv models.Invoice
	dataAs(t, decodeEnvelope(t, w), &inv)
	return inv
}

func fiscalSetup(t *testing.T, db *sql.DB) (adminID, productID uuid.UUID) {
	t.Helper()
	adminID = seedUser(t, db, "owner", "admin", "owner-pass-1", nil)
	setPin(t, db, adminID, "4321")
	openBusinessDay(t, db, adminID, businessToday())
	productID = seedProductRow(t, db, "LPG bulk", 265)
	return
}

func TestCreate_FiscalOffStaysOffEvenWhenAsked(t *testing.T) {
	db := testdb.Fresh(t)
	adminID, productID := fiscalSetup(t, db)
	f := acceptingFBR(t)
	r := fiscalRouter(db, actor{id: adminID, username: "owner", role: "admin"})

	if inv := ringSale(t, r, productID, fiscalSale{}); inv.FiscalStatus != "off" {
		t.Fatalf("plain: %s", inv.FiscalStatus)
	}
	if inv := ringSale(t, r, productID, fiscalSale{fileFBR: true}); inv.FiscalStatus != "off" || f.calls != 0 {
		t.Fatalf("asked with fiscal off: %s %d", inv.FiscalStatus, f.calls)
	}
}

func TestCreate_FiscalOnPlainSaleIsUnfiled(t *testing.T) {
	db := testdb.Fresh(t)
	adminID, productID := fiscalSetup(t, db)
	f := acceptingFBR(t)
	enableFiscalAt(t, db, f)
	r := fiscalRouter(db, actor{id: adminID, username: "owner", role: "admin"})

	inv := ringSale(t, r, productID, fiscalSale{})
	if inv.FiscalStatus != "unfiled" || f.calls != 0 {
		t.Fatalf("nothing files on its own: %s %d", inv.FiscalStatus, f.calls)
	}
	var jobs int
	if err := db.QueryRow(`SELECT count(*) FROM fiscal_outbound_jobs`).Scan(&jobs); err != nil || jobs != 0 {
		t.Fatalf("no job either: %d %v", jobs, err)
	}
}

func TestCreate_ChargeWithFBR(t *testing.T) {
	db := testdb.Fresh(t)
	adminID, productID := fiscalSetup(t, db)
	f := acceptingFBR(t)
	enableFiscalAt(t, db, f)
	r := fiscalRouter(db, actor{id: adminID, username: "owner", role: "admin"})

	inv := ringSale(t, r, productID, fiscalSale{fileFBR: true})
	if inv.FiscalStatus != "synced" || inv.FiscalInvoiceNumber == nil || *inv.FiscalInvoiceNumber == "" {
		t.Fatalf("filed inline: %+v", inv)
	}
	// Idempotent replay returns the same sale and never files twice.
	calls := f.calls
	w := doJSON(r, http.MethodPost, "/invoices", models.CreateInvoiceRequest{
		ClientOpID: inv.ClientOpID.String(), PaymentMethod: "cash", FileFBR: true,
		Lines: []models.InvoiceLineRequest{{ProductID: productID.String(), Quantity: 12.5, EnteredAs: "kg"}},
	})
	if w.Code != http.StatusOK || f.calls != calls {
		t.Fatalf("replay: %d, FBR calls %d → %d", w.Code, calls, f.calls)
	}
}

func TestCreate_ChargeWithFBRRejectedKeepsTheSale(t *testing.T) {
	db := testdb.Fresh(t)
	adminID, productID := fiscalSetup(t, db)
	enableFiscalAt(t, db, rejectingFBR(t))
	r := fiscalRouter(db, actor{id: adminID, username: "owner", role: "admin"})

	inv := ringSale(t, r, productID, fiscalSale{fileFBR: true})
	if inv.FiscalStatus != "failed" || inv.FiscalErrorCode != "fiscal_rejected" {
		t.Fatalf("sale stands, filing failed: %+v", inv)
	}
	var status string
	if err := db.QueryRow(`SELECT status FROM invoices WHERE id = $1`, inv.ID).Scan(&status); err != nil || status != "completed" {
		t.Fatalf("the sale is never rolled back: %s %v", status, err)
	}
}

func TestCreate_ChargeWithFBRUnreachableQueues(t *testing.T) {
	db := testdb.Fresh(t)
	adminID, productID := fiscalSetup(t, db)
	enableFiscalAt(t, db, unreachableFBR(t))
	r := fiscalRouter(db, actor{id: adminID, username: "owner", role: "admin"})

	inv := ringSale(t, r, productID, fiscalSale{fileFBR: true})
	if inv.FiscalStatus != "pending" {
		t.Fatalf("pending for the worker: %+v", inv)
	}
	var jobs int
	if err := db.QueryRow(`SELECT count(*) FROM fiscal_outbound_jobs WHERE invoice_id = $1 AND kind = 'sale' AND status = 'pending'`, inv.ID).Scan(&jobs); err != nil || jobs != 1 {
		t.Fatalf("exactly one job: %d %v", jobs, err)
	}
}

func voidPath(inv models.Invoice) string { return "/invoices/" + inv.ID.String() + "/void" }

func TestVoid_InFlightFilingIsRefused(t *testing.T) {
	db := testdb.Fresh(t)
	adminID, productID := fiscalSetup(t, db)
	enableFiscalAt(t, db, unreachableFBR(t))
	r := fiscalRouter(db, actor{id: adminID, username: "owner", role: "admin"})
	inv := ringSale(t, r, productID, fiscalSale{fileFBR: true}) // pending

	w := doJSON(r, http.MethodPost, voidPath(inv), models.VoidInvoiceRequest{Reason: "wrong weight", Pin: "4321"})
	if w.Code != http.StatusConflict || errCode(decodeEnvelope(t, w)) != "fiscal_filing_in_flight" {
		t.Fatalf("void while filing: %d %s", w.Code, w.Body.String())
	}
	var status string
	if err := db.QueryRow(`SELECT status FROM invoices WHERE id = $1`, inv.ID).Scan(&status); err != nil || status != "completed" {
		t.Fatalf("nothing voided: %s %v", status, err)
	}
}

func TestVoid_CoveredInvoiceVoidsLocallyAndLeavesTheBatch(t *testing.T) {
	db := testdb.Fresh(t)
	adminID, productID := fiscalSetup(t, db)
	f := acceptingFBR(t)
	enableFiscalAt(t, db, f)
	r := fiscalRouter(db, actor{id: adminID, username: "owner", role: "admin"})
	inv := ringSale(t, r, productID, fiscalSale{})
	cfg, _ := fiscal.LoadConfig(db)
	b, err := fiscal.CreateBatch(db, businessToday(), []uuid.UUID{inv.ID}, cfg, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fiscal.SubmitBatch(t.Context(), fiscal.Deps{DB: db}, b.ID, nil); err != nil {
		t.Fatal(err)
	}
	calls := f.calls

	w := doJSON(r, http.MethodPost, voidPath(inv), models.VoidInvoiceRequest{Reason: "wrong weight", Pin: "4321"})
	if w.Code != http.StatusOK {
		t.Fatalf("void covered: %d %s", w.Code, w.Body.String())
	}
	var out models.Invoice
	dataAs(t, decodeEnvelope(t, w), &out)
	if out.Status != "voided" || out.FiscalVoidStatus != "unfiled" || out.FiscalStatus != "covered" || f.calls != calls {
		t.Fatalf("local void only: %+v calls %d→%d", out, calls, f.calls)
	}
	after, _, _ := fiscal.LoadBatch(db, b.ID)
	if after.Status != "synced" || after.InvoiceCount != b.InvoiceCount || after.TotalAmount != b.TotalAmount {
		t.Fatalf("batch figures untouched: %+v", after)
	}
}

func TestVoid_SyncedRegisteredBuyerFilesDebitNote(t *testing.T) {
	db := testdb.Fresh(t)
	adminID, productID := fiscalSetup(t, db)
	f := acceptingFBR(t)
	enableFiscalAt(t, db, f)
	registered := seedCustomerRow(t, db, "Ali Traders", false, nil)
	if _, err := db.Exec(`UPDATE customers SET buyer_registration_type = 'Registered', address = 'Gulberg', province = 'PUNJAB' WHERE id = $1`, registered); err != nil {
		t.Fatal(err)
	}
	r := fiscalRouter(db, actor{id: adminID, username: "owner", role: "admin"})
	inv := ringSale(t, r, productID, fiscalSale{fileFBR: true, customerID: &registered})
	if inv.FiscalStatus != "synced" {
		t.Fatalf("precondition: %+v", inv)
	}

	w := doJSON(r, http.MethodPost, voidPath(inv), models.VoidInvoiceRequest{Reason: "wrong weight", Pin: "4321"})
	if w.Code != http.StatusOK {
		t.Fatalf("void: %d %s", w.Code, w.Body.String())
	}
	var out models.Invoice
	dataAs(t, decodeEnvelope(t, w), &out)
	if out.FiscalVoidStatus != "synced" || out.FiscalDebitNoteNumber == nil {
		t.Fatalf("debit note filed: %+v", out)
	}
}

func TestVoid_SyncedWalkInStaysUnfiled(t *testing.T) {
	db := testdb.Fresh(t)
	adminID, productID := fiscalSetup(t, db)
	f := acceptingFBR(t)
	enableFiscalAt(t, db, f)
	r := fiscalRouter(db, actor{id: adminID, username: "owner", role: "admin"})
	inv := ringSale(t, r, productID, fiscalSale{fileFBR: true})
	calls := f.calls

	w := doJSON(r, http.MethodPost, voidPath(inv), models.VoidInvoiceRequest{Reason: "wrong weight", Pin: "4321"})
	if w.Code != http.StatusOK {
		t.Fatalf("void: %d %s", w.Code, w.Body.String())
	}
	var out models.Invoice
	dataAs(t, decodeEnvelope(t, w), &out)
	if out.FiscalVoidStatus != "unfiled" || f.calls != calls {
		t.Fatalf("walk-in void cannot be a debit note (0106): %+v %d→%d", out, calls, f.calls)
	}
}

func TestVoid_UnfiledDropsOutOfPending(t *testing.T) {
	db := testdb.Fresh(t)
	adminID, productID := fiscalSetup(t, db)
	enableFiscalAt(t, db, acceptingFBR(t))
	r := fiscalRouter(db, actor{id: adminID, username: "owner", role: "admin"})
	inv := ringSale(t, r, productID, fiscalSale{})

	w := doJSON(r, http.MethodPost, voidPath(inv), models.VoidInvoiceRequest{Reason: "wrong weight", Pin: "4321"})
	if w.Code != http.StatusOK {
		t.Fatalf("void: %d %s", w.Code, w.Body.String())
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM invoices WHERE status = 'completed' AND fiscal_status IN ('unfiled','failed')`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("pending to file: %d %v", n, err)
	}
}
