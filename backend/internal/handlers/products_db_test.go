package handlers

import (
	"net/http"
	"testing"

	"elevon-backend/internal/models"
	"elevon-backend/internal/testdb"

	"github.com/gin-gonic/gin"
)

func productsRouter(h *ProductsHandler, a actor) *gin.Engine {
	r := gin.New()
	r.GET("/products", asActor(a), h.List)
	g := r.Group("/admin", asActor(a))
	g.POST("/products", h.Create)
	g.PUT("/products/:id", h.Update)
	g.PUT("/rates", h.UpdateRates)
	g.GET("/rates/history", h.RateHistory)
	return r
}

func TestProducts_CreateListAndConflicts(t *testing.T) {
	db := testdb.Fresh(t)
	ownerID := seedUser(t, db, "owner", "admin", "owner-pass-1", nil)
	r := productsRouter(NewProductsHandler(db), actor{id: ownerID, username: "owner", role: "admin"})

	w := doJSON(r, http.MethodPost, "/admin/products", models.CreateProductRequest{
		Name: " 45kg Cylinder ", SKU: " CYL-45 ", Rate: 265.50, HSCode: "2711.1910", SortOrder: 1,
	})
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var created models.Product
	dataAs(t, decodeEnvelope(t, w), &created)
	if created.Name != "45kg Cylinder" || created.SKU == nil || *created.SKU != "CYL-45" ||
		created.SellBy != "weight" || created.UnitLabel != "kg" || created.Rate != 265.50 ||
		created.HSCode == nil || *created.HSCode != "2711.1910" || !created.IsActive {
		t.Fatalf("normalised product: %+v", created)
	}

	for _, tc := range []struct {
		req  models.CreateProductRequest
		code string
	}{
		{models.CreateProductRequest{Name: "45KG CYLINDER", Rate: 100}, "product_name_taken"},
		{models.CreateProductRequest{Name: "Other", SKU: "CYL-45", Rate: 100}, "sku_taken"},
		{models.CreateProductRequest{Name: "", Rate: 100}, "invalid_name"},
		{models.CreateProductRequest{Name: "Bad rate", Rate: -1}, "invalid_rate"},
		{models.CreateProductRequest{Name: "Bad rate 2", Rate: 100.005}, "invalid_rate"},
		{models.CreateProductRequest{Name: "Bad HS", Rate: 100, HSCode: "27111910"}, "invalid_hs_code"},
	} {
		w := doJSON(r, http.MethodPost, "/admin/products", tc.req)
		if got := errCode(decodeEnvelope(t, w)); got != tc.code {
			t.Errorf("%+v: want %s got %s (%d %s)", tc.req, tc.code, got, w.Code, w.Body.String())
		}
	}

	// A second, inactive product proves the active filter and that the
	// unfiltered list (the /rates screen) still returns everything.
	w = doJSON(r, http.MethodPost, "/admin/products", models.CreateProductRequest{Name: "11kg Cylinder", Rate: 120})
	var second models.Product
	dataAs(t, decodeEnvelope(t, w), &second)
	f := false
	if w := doJSON(r, http.MethodPut, "/admin/products/"+second.ID.String(), models.UpdateProductRequest{IsActive: &f}); w.Code != http.StatusOK {
		t.Fatalf("deactivate: %d %s", w.Code, w.Body.String())
	}

	w = doJSON(r, http.MethodGet, "/products?active=1", nil)
	var active []models.Product
	dataAs(t, decodeEnvelope(t, w), &active)
	if len(active) != 1 || active[0].Name != "45kg Cylinder" {
		t.Fatalf("active list: %+v", active)
	}

	w = doJSON(r, http.MethodGet, "/products", nil)
	var all []models.Product
	dataAs(t, decodeEnvelope(t, w), &all)
	if len(all) != 2 {
		t.Fatalf("unfiltered list: %+v", all)
	}

	// Edit updates fields but never the rate.
	newName := "45kg Cylinder (Domestic)"
	w = doJSON(r, http.MethodPut, "/admin/products/"+created.ID.String(), models.UpdateProductRequest{Name: &newName})
	var edited models.Product
	dataAs(t, decodeEnvelope(t, w), &edited)
	if w.Code != http.StatusOK || edited.Name != newName || edited.Rate != 265.50 {
		t.Fatalf("edit: %d %+v", w.Code, edited)
	}
	if w := doJSON(r, http.MethodPut, "/admin/products/not-a-uuid", models.UpdateProductRequest{Name: &newName}); w.Code != http.StatusNotFound {
		t.Fatalf("bad id: %d", w.Code)
	}
	if w := doJSON(r, http.MethodPut, "/admin/products/"+created.ID.String(), models.UpdateProductRequest{}); errCode(decodeEnvelope(t, w)) != "no_changes" {
		t.Fatalf("empty update: %s", w.Body.String())
	}
}

func TestProducts_RatesUpdateAtomicWithHistory(t *testing.T) {
	db := testdb.Fresh(t)
	ownerID := seedUser(t, db, "owner", "admin", "owner-pass-1", nil)
	r := productsRouter(NewProductsHandler(db), actor{id: ownerID, username: "owner", role: "admin"})

	var a, b models.Product
	dataAs(t, decodeEnvelope(t, doJSON(r, http.MethodPost, "/admin/products", models.CreateProductRequest{Name: "A", Rate: 100})), &a)
	dataAs(t, decodeEnvelope(t, doJSON(r, http.MethodPost, "/admin/products", models.CreateProductRequest{Name: "B", Rate: 200})), &b)

	// One invalid entry aborts the whole batch: nothing changes, no history added.
	w := doJSON(r, http.MethodPut, "/admin/rates", models.UpdateRatesRequest{
		Changes: []models.RateChange{{ProductID: a.ID, Rate: 150}, {ProductID: b.ID, Rate: -5}},
	})
	if errCode(decodeEnvelope(t, w)) != "invalid_rate" {
		t.Fatalf("abort: %s", w.Body.String())
	}
	var rate float64
	if err := db.QueryRow(`SELECT rate::float8 FROM products WHERE id = $1`, a.ID).Scan(&rate); err != nil {
		t.Fatal(err)
	}
	if rate != 100 {
		t.Fatalf("aborted update must not touch rows, got %v", rate)
	}
	var histCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM product_rate_history`).Scan(&histCount); err != nil {
		t.Fatal(err)
	}
	if histCount != 2 { // just the two creation rows
		t.Fatalf("aborted update must not write history, got %d", histCount)
	}

	// Valid batch: A changes, B is resubmitted at its current rate and must
	// be skipped (no UPDATE, no history row).
	w = doJSON(r, http.MethodPut, "/admin/rates", models.UpdateRatesRequest{
		Changes: []models.RateChange{{ProductID: a.ID, Rate: 150}, {ProductID: b.ID, Rate: 200}},
		Note:    "weekly rate change",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("update: %d %s", w.Code, w.Body.String())
	}
	var resp models.UpdateRatesResponse
	dataAs(t, decodeEnvelope(t, w), &resp)
	if resp.Updated != 1 || len(resp.Products) != 2 {
		t.Fatalf("response: %+v", resp)
	}
	for _, p := range resp.Products {
		if p.ID == a.ID && p.Rate != 150 {
			t.Fatalf("A must be 150 in the response, got %v", p.Rate)
		}
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM product_rate_history WHERE product_id = $1`, a.ID).Scan(&histCount); err != nil {
		t.Fatal(err)
	}
	if histCount != 2 { // creation + this change
		t.Fatalf("history rows for A: %d", histCount)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM product_rate_history WHERE product_id = $1`, b.ID).Scan(&histCount); err != nil {
		t.Fatal(err)
	}
	if histCount != 1 { // only creation; the unchanged rate was skipped
		t.Fatalf("history rows for B (must be skipped): %d", histCount)
	}

	// Empty batch is rejected outright.
	if w := doJSON(r, http.MethodPut, "/admin/rates", models.UpdateRatesRequest{}); errCode(decodeEnvelope(t, w)) != "invalid_request" {
		t.Fatalf("empty batch: %s", w.Body.String())
	}
}

func TestProducts_RateHistoryReturnsActorName(t *testing.T) {
	db := testdb.Fresh(t)
	ownerID := seedUser(t, db, "owner", "admin", "owner-pass-1", nil)
	r := productsRouter(NewProductsHandler(db), actor{id: ownerID, username: "owner", role: "admin"})

	var a models.Product
	dataAs(t, decodeEnvelope(t, doJSON(r, http.MethodPost, "/admin/products", models.CreateProductRequest{Name: "A", Rate: 100})), &a)
	if w := doJSON(r, http.MethodPut, "/admin/rates", models.UpdateRatesRequest{
		Changes: []models.RateChange{{ProductID: a.ID, Rate: 150}}, Note: "bump",
	}); w.Code != http.StatusOK {
		t.Fatalf("rate change: %d %s", w.Code, w.Body.String())
	}

	w := doJSON(r, http.MethodGet, "/admin/rates/history?product_id="+a.ID.String(), nil)
	var entries []models.RateHistoryEntry
	dataAs(t, decodeEnvelope(t, w), &entries)
	if len(entries) != 2 {
		t.Fatalf("history entries: %+v", entries)
	}
	latest := entries[0] // newest first
	if latest.ProductName != "A" || latest.OldRate != 100 || latest.NewRate != 150 ||
		latest.ChangedByName == nil || *latest.ChangedByName != "Test owner" ||
		latest.Note == nil || *latest.Note != "bump" {
		t.Fatalf("latest entry: %+v", latest)
	}
	creation := entries[1]
	if creation.OldRate != 0 || creation.NewRate != 100 || creation.Note != nil {
		t.Fatalf("creation entry: %+v", creation)
	}

	// limit is respected.
	w = doJSON(r, http.MethodGet, "/admin/rates/history?limit=1", nil)
	dataAs(t, decodeEnvelope(t, w), &entries)
	if len(entries) != 1 {
		t.Fatalf("limit=1: %+v", entries)
	}
}
