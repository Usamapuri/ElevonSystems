package fiscal_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"elevon-backend/internal/fiscal"
)

// Refresh pulls FBR's reference lists: the bodies below are the shapes the
// sandbox returned on 2026-09-21 (audit/FBR_SANDBOX_2026-09-21.md).
func TestRefresh_ReadsTheFourLists(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path+"?"+r.URL.RawQuery)
		if r.Header.Get("Authorization") != "Bearer tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/provinces"):
			_, _ = w.Write([]byte(`[{"stateProvinceCode":7,"stateProvinceDesc":"PUNJAB"},{"stateProvinceCode":8,"stateProvinceDesc":"SINDH"}]`))
		case strings.HasSuffix(r.URL.Path, "/uom"):
			_, _ = w.Write([]byte(`[{"uoM_ID":13,"description":"KG"},{"uoM_ID":63,"description":"Kilogram"}]`))
		case strings.HasSuffix(r.URL.Path, "/HS_UOM"):
			if r.URL.Query().Get("hs_code") == "2711.1400" {
				_, _ = w.Write([]byte(`[{"uoM_ID":13,"description":"KG"},{"uoM_ID":3,"description":"MT"}]`))
				return
			}
			_, _ = w.Write([]byte(`[{"uoM_ID":13,"description":"KG"}]`))
		case strings.HasSuffix(r.URL.Path, "/SaleTypeToRate"):
			_, _ = w.Write([]byte(`[{"ratE_ID":728,"ratE_DESC":"18%","ratE_VALUE":18}]`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	cfg := fiscal.Defaults()
	cfg.ReferenceBaseOverride = srv.URL
	today := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	ref, err := fiscal.Refresh(context.Background(), fiscal.Deps{}, cfg, "tok", []string{"2711.1910", "2711.1400", "2711.1910"}, today)
	if err != nil {
		t.Fatal(err)
	}
	if len(ref.Provinces) != 2 || ref.Provinces[0] != "PUNJAB" {
		t.Fatalf("provinces: %+v", ref.Provinces)
	}
	if len(ref.UoMs) != 2 || ref.UoMs[0].ID != 13 || ref.UoMs[0].Description != "KG" {
		t.Fatalf("uoms: %+v", ref.UoMs)
	}
	if len(ref.HSUoM) != 2 || len(ref.HSUoM["2711.1400"]) != 2 || ref.HSUoM["2711.1910"][0] != "KG" {
		t.Fatalf("hs_uom covers every distinct code once: %+v", ref.HSUoM)
	}
	if len(ref.Rates) != 1 || ref.Rates[0].ID != 728 || ref.Rates[0].Desc != "18%" || ref.Rates[0].Value != 18 {
		t.Fatalf("rates: %+v", ref.Rates)
	}
	if ref.RefreshedAt.IsZero() {
		t.Fatal("refreshed_at is stamped")
	}
	joined := strings.Join(paths, " ")
	if !strings.Contains(joined, "date=12-Oct-2026") || !strings.Contains(joined, "transTypeId=75") || !strings.Contains(joined, "annexure_id=3") {
		t.Fatalf("query strings: %s", joined)
	}
	if strings.Count(joined, "/HS_UOM") != 2 {
		t.Fatalf("one HS_UOM call per distinct code: %s", joined)
	}
}

func TestRefresh_UnreachableIsAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(502) }))
	t.Cleanup(srv.Close)
	cfg := fiscal.Defaults()
	cfg.ReferenceBaseOverride = srv.URL
	if _, err := fiscal.Refresh(context.Background(), fiscal.Deps{}, cfg, "tok", nil, time.Now()); err == nil {
		t.Fatal("a 502 is not a reference list")
	}
}
