package fiscal

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The five bodies in testdata/resp_*.json are verbatim sandbox responses
// (audit/FBR_SANDBOX_2026-09-21.md), tab padding and JSON nulls included.
// Replaying them is the only way to prove the client reads what PRAL
// actually sends rather than what the spec pretty-prints.
func readResponse(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return data
}

// replay stands up a fake FBR that serves one body on every request and
// returns a Config pointing both endpoints at it. Config's URL overrides
// exist for exactly this.
func replay(t *testing.T, body []byte, status int) Config {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write(body)
	}))
	t.Cleanup(srv.Close)

	cfg := Defaults()
	cfg.IsSandbox = true
	cfg.ValidateURLOverride = srv.URL + "/validate"
	cfg.PostURLOverride = srv.URL + "/post"
	return cfg
}

// sampleInvoice is a minimal payload; what it contains does not matter to
// the client, only that it is what goes on the wire.
func sampleInvoice() DIInvoice {
	return DIInvoice{
		InvoiceType:        "Sale Invoice",
		InvoiceDate:        "2026-09-21",
		SellerNTNCNIC:      "8951943",
		SellerBusinessName: "Elevon Test Seller",
		Items:              []DIItem{{HSCode: "2711.1910", Quantity: 12.5}},
	}
}

func TestClient_ValidResponseHasNoRejection(t *testing.T) {
	cfg := replay(t, readResponse(t, "resp_caseC.json"), http.StatusOK)
	c := NewHTTPClient(cfg, "token-1234", 5*time.Second)

	resp, err := c.Validate(context.Background(), sampleInvoice())
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if resp.StatusCode != "00" {
		t.Fatalf("statusCode = %q, want 00", resp.StatusCode)
	}
	if len(resp.Lines) != 2 {
		t.Fatalf("lines = %d, want 2", len(resp.Lines))
	}
	if rejected := resp.Rejection(); rejected != nil {
		t.Fatalf("valid response must not be a rejection, got %+v", rejected)
	}
	if resp.HTTPStatus != http.StatusOK {
		t.Fatalf("http status = %d", resp.HTTPStatus)
	}
	if len(resp.Raw) == 0 {
		t.Fatal("Raw must keep the body for the audit trail")
	}
}

func TestClient_LineLevelRejections(t *testing.T) {
	cases := []struct {
		name     string
		fixture  string
		wantCode string
		wantLine int
	}{
		{"self invoicing", "resp_caseB.json", "0058", 1},
		{"tax mismatch", "resp_negTax.json", "0104", 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := replay(t, readResponse(t, tc.fixture), http.StatusOK)
			c := NewHTTPClient(cfg, "token-1234", 5*time.Second)

			resp, err := c.Validate(context.Background(), sampleInvoice())
			if err != nil {
				t.Fatalf("a rejection is HTTP 200 and not a transport error: %v", err)
			}
			rejected := resp.Rejection()
			if rejected == nil {
				t.Fatal("expected a rejection")
			}
			if rejected.Code != tc.wantCode {
				t.Fatalf("code = %q, want %q", rejected.Code, tc.wantCode)
			}
			if rejected.Line != tc.wantLine {
				t.Fatalf("line = %d, want %d", rejected.Line, tc.wantLine)
			}
			if rejected.Message == "" {
				t.Fatal("FBR's sentence must reach the audit trail")
			}
			if got, want := rejected.Error(), "fiscal_rejected "+tc.wantCode; got != want {
				t.Fatalf("Error() = %q, want %q", got, want)
			}
		})
	}
}

// An invoice-level rejection carries invoiceStatuses: null, which must not
// panic a client that loops over the lines.
func TestClient_InvoiceLevelRejection(t *testing.T) {
	cfg := replay(t, readResponse(t, "resp_negNtn.json"), http.StatusOK)
	c := NewHTTPClient(cfg, "token-1234", 5*time.Second)

	resp, err := c.Validate(context.Background(), sampleInvoice())
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if resp.Lines != nil {
		t.Fatalf("invoiceStatuses null must decode to no lines, got %+v", resp.Lines)
	}
	rejected := resp.Rejection()
	if rejected == nil {
		t.Fatal("expected a rejection")
	}
	if rejected.Code != "0205" {
		t.Fatalf("code = %q, want 0205", rejected.Code)
	}
	if rejected.Line != 0 {
		t.Fatalf("line = %d, want 0 (invoice level)", rejected.Line)
	}
}

func TestClient_PostReturnsTheInvoiceNumber(t *testing.T) {
	cfg := replay(t, readResponse(t, "resp_postA.json"), http.StatusOK)
	c := NewHTTPClient(cfg, "token-1234", 5*time.Second)

	resp, err := c.Post(context.Background(), sampleInvoice())
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	if resp.Rejection() != nil {
		t.Fatalf("post A was accepted: %+v", resp.Rejection())
	}
	if resp.InvoiceNumber != "6110180871403DIAJGEJ4031308" {
		t.Fatalf("invoiceNumber = %q", resp.InvoiceNumber)
	}
	if len(resp.Lines) != 1 || resp.Lines[0].InvoiceNo != "6110180871403DIAJGEJ4031308-1" {
		t.Fatalf("line numbers = %+v", resp.Lines)
	}
}

// The request the client sends: bearer token, JSON content type, the payload
// verbatim, and the config's own URL for each of the two calls.
func TestClient_RequestShape(t *testing.T) {
	var got struct {
		auth   string
		ctype  string
		path   string
		method string
		body   []byte
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.auth = r.Header.Get("Authorization")
		got.ctype = r.Header.Get("Content-Type")
		got.path = r.URL.Path
		got.method = r.Method
		buf := make([]byte, r.ContentLength)
		if r.ContentLength > 0 {
			if _, err := io.ReadFull(r.Body, buf); err != nil {
				t.Errorf("read body: %v", err)
			}
		}
		got.body = buf
		_, _ = w.Write(readResponse(t, "resp_caseC.json"))
	}))
	defer srv.Close()

	cfg := Defaults()
	cfg.ValidateURLOverride = srv.URL + "/validate"
	cfg.PostURLOverride = srv.URL + "/post"
	c := NewHTTPClient(cfg, "token-1234", 5*time.Second)

	payload := sampleInvoice()
	if _, err := c.Validate(context.Background(), payload); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if got.auth != "Bearer token-1234" {
		t.Fatalf("Authorization = %q, want Bearer token-1234", got.auth)
	}
	if got.ctype != "application/json" {
		t.Fatalf("Content-Type = %q", got.ctype)
	}
	if got.method != http.MethodPost || got.path != "/validate" {
		t.Fatalf("validate went to %s %s", got.method, got.path)
	}
	want, _ := json.Marshal(payload)
	if string(got.body) != string(want) {
		t.Fatalf("body on the wire:\n got %s\nwant %s", got.body, want)
	}

	if _, err := c.Post(context.Background(), payload); err != nil {
		t.Fatalf("post: %v", err)
	}
	if got.path != "/post" {
		t.Fatalf("post went to %s", got.path)
	}

	if _, err := c.Get(context.Background(), srv.URL+"/pdi/v1/provinces"); err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.method != http.MethodGet || got.auth != "Bearer token-1234" {
		t.Fatalf("reference GET: %s auth %q", got.method, got.auth)
	}
}

// A 500 is the gateway, not a verdict: retryable, and never mistaken for a
// rejection.
func TestClient_NonOKIsUnreachable(t *testing.T) {
	cfg := replay(t, []byte(`<html>gateway</html>`), http.StatusInternalServerError)
	c := NewHTTPClient(cfg, "token-1234", 5*time.Second)

	_, err := c.Validate(context.Background(), sampleInvoice())
	if err == nil {
		t.Fatal("expected an error")
	}
	if !errors.Is(err, ErrUnreachable) {
		t.Fatalf("err = %v, want fiscal_unreachable", err)
	}
	var rejected *RejectedError
	if errors.As(err, &rejected) {
		t.Fatal("a 500 must never look like an FBR rejection")
	}
}

func TestClient_TimeoutIsUnreachable(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
		_, _ = w.Write([]byte(`{}`))
	}))
	// LIFO: the handler is released before the server is closed, so Close
	// does not block on the in-flight request.
	defer srv.Close()
	defer close(release)

	cfg := Defaults()
	cfg.ValidateURLOverride = srv.URL
	c := NewHTTPClient(cfg, "token-1234", 20*time.Millisecond)

	_, err := c.Validate(context.Background(), sampleInvoice())
	if err == nil {
		t.Fatal("expected a timeout")
	}
	if !errors.Is(err, ErrUnreachable) {
		t.Fatalf("err = %v, want fiscal_unreachable", err)
	}
}

// A 200 that is not the DI envelope (a captive portal, a proxy error page)
// is unreachable too — it is not FBR answering.
func TestClient_NonEnvelopeBodyIsUnreachable(t *testing.T) {
	cfg := replay(t, []byte(`<html>sign in to the wifi</html>`), http.StatusOK)
	c := NewHTTPClient(cfg, "token-1234", 5*time.Second)

	if _, err := c.Validate(context.Background(), sampleInvoice()); !errors.Is(err, ErrUnreachable) {
		t.Fatalf("err = %v, want fiscal_unreachable", err)
	}
}

// No error this package produces may carry the token, whatever went wrong.
func TestClient_ErrorsNeverCarryTheToken(t *testing.T) {
	const token = "super-secret-fbr-token"
	cfg := replay(t, []byte(`nope`), http.StatusBadGateway)
	c := NewHTTPClient(cfg, token, 5*time.Second)

	_, err := c.Validate(context.Background(), sampleInvoice())
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), token) {
		t.Fatalf("error text leaked the token: %v", err)
	}
}

// Rejection() reads both levels and prefers the invoice-level error, which
// is the one that explains a whole-document refusal.
func TestRejection_PrefersInvoiceLevel(t *testing.T) {
	r := Response{
		StatusCode: "01",
		ErrorCode:  "0205",
		Error:      "Provided scenario not valid for unregistered user",
		Lines:      []LineStatus{{ItemSNo: "1", StatusCode: "01", ErrorCode: "0104", Error: "tax"}},
	}
	rejected := r.Rejection()
	if rejected == nil || rejected.Code != "0205" || rejected.Line != 0 {
		t.Fatalf("got %+v, want the invoice-level 0205", rejected)
	}

	// statusCode "01" with no code at all still refuses, with a lookup-able
	// placeholder rather than an empty string.
	bare := Response{StatusCode: "01"}
	if got := bare.Rejection(); got == nil || got.Code != unknownRejectionCode {
		t.Fatalf("got %+v, want an unknown-code rejection", got)
	}

	// A line that fails under an envelope that says "00" is still a refusal.
	mixed := Response{StatusCode: "00", Lines: []LineStatus{
		{ItemSNo: "1", StatusCode: "00"},
		{ItemSNo: "2", StatusCode: "01", ErrorCode: "0044", Error: "HS Code cannot be empty"},
	}}
	rejected = mixed.Rejection()
	if rejected == nil || rejected.Code != "0044" || rejected.Line != 2 {
		t.Fatalf("got %+v, want 0044 on line 2", rejected)
	}
}
