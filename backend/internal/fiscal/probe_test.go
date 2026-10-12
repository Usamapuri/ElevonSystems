package fiscal_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"elevon-backend/internal/fiscal"
	"elevon-backend/internal/testdb"
)

// The Settings → FBR "Run test" (spec 2026-10-12 §2.7): a one-line sample
// sale, validated and — sandbox only — posted, with the exact outbound body
// and the raw gateway responses handed back for the dialog.

func sealToken(t *testing.T, token string) string {
	t.Helper()
	key, err := fiscal.KeyFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := fiscal.Encrypt(key, token)
	if err != nil {
		t.Fatal(err)
	}
	enc, _ := json.Marshal(sealed)
	return string(enc)
}

func sample() fiscal.TestSample { return fiscal.TestSample{TaxRate: 0.18, DefaultHSCode: "2711.1910"} }

func TestRunTest_SandboxValidatesThenPosts(t *testing.T) {
	sandboxEnv(t)
	db := testdb.Fresh(t)
	s := acceptingStub(t)
	enableFiscal(t, db, s, true)

	r := fiscal.RunTest(context.Background(), deps(db), fiscal.EnvSandbox, sample(), nil)
	if !r.OK || r.Environment != fiscal.EnvSandbox || r.InvoiceNumber == "" || r.ErrorCode != "" {
		t.Fatalf("passed: %+v", r)
	}
	if s.validateHits != 1 || s.postHits != 1 {
		t.Fatalf("validate then post: %d/%d", s.validateHits, s.postHits)
	}
	if len(r.Steps) != 2 || r.Steps[0].Phase != "validate" || r.Steps[1].Phase != "post" || r.Steps[1].StatusCode != "00" {
		t.Fatalf("steps: %+v", r.Steps)
	}
	body := string(r.RequestBody)
	if !strings.Contains(body, `"Sale Invoice"`) || !strings.Contains(body, `"Connection test`) || !strings.Contains(body, `"valueSalesExcludingST":100`) {
		t.Fatalf("outbound body: %s", body)
	}
	if strings.Contains(body, sandboxToken) || strings.Contains(string(r.Steps[0].RawResponse), sandboxToken) {
		t.Fatal("the token must never appear in the result")
	}
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM fiscal_invoices`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("a test never writes the ledger: %d %v", n, err)
	}
	if err := db.QueryRow(`SELECT count(*) FROM fiscal_audit_events WHERE kind = 'test'`).Scan(&n); err != nil || n != 2 {
		t.Fatalf("one audit row per call: %d %v", n, err)
	}
}

func TestRunTest_ProductionValidatesOnly(t *testing.T) {
	sandboxEnv(t)
	db := testdb.Fresh(t)
	s := acceptingStub(t)
	enableFiscal(t, db, s, true)
	putSetting(t, db, fiscal.KeyAPIKeyProductionEnc, sealToken(t, "live-token-0001"))

	r := fiscal.RunTest(context.Background(), deps(db), fiscal.EnvProduction, sample(), nil)
	if !r.OK || r.Environment != fiscal.EnvProduction || r.InvoiceNumber != "" {
		t.Fatalf("validated, nothing filed: %+v", r)
	}
	if s.validateHits != 1 || s.postHits != 0 {
		t.Fatalf("production never posts: %d/%d", s.validateHits, s.postHits)
	}
	if s.lastAuth != "Bearer live-token-0001" {
		t.Fatalf("production uses the production slot: %q", s.lastAuth)
	}
	if len(r.Steps) != 1 || r.Steps[0].Phase != "validate" {
		t.Fatalf("steps: %+v", r.Steps)
	}
}

func TestRunTest_RejectedStopsBeforePost(t *testing.T) {
	sandboxEnv(t)
	db := testdb.Fresh(t)
	s := newStub(t, fixtureBody(t, "resp_negTax.json"), fixtureBody(t, "resp_postA.json"))
	enableFiscal(t, db, s, true)

	r := fiscal.RunTest(context.Background(), deps(db), fiscal.EnvSandbox, sample(), nil)
	if r.OK || s.postHits != 0 || len(r.Steps) != 1 {
		t.Fatalf("rejected at validate: %+v", r)
	}
	if r.Steps[0].ErrorCode != "0104" || r.Steps[0].Line != 1 || r.Steps[0].Error == "" {
		t.Fatalf("the step carries FBR's code, message and line: %+v", r.Steps[0])
	}
	if r.ErrorCode != "fiscal_rejected" {
		t.Fatalf("top-level code: %q", r.ErrorCode)
	}
}

// Review Focus 5: no token, or sandbox on a release server — a Failed
// result with the reason, no HTTP, never a 500.
func TestRunTest_RefusalsMakeNoHTTP(t *testing.T) {
	sandboxEnv(t)
	db := testdb.Fresh(t)
	s := acceptingStub(t)
	enableFiscal(t, db, s, true)

	r := fiscal.RunTest(context.Background(), deps(db), fiscal.EnvProduction, sample(), nil)
	if r.OK || r.ErrorCode != "fiscal_token_missing" || len(r.Steps) != 0 || s.hits() != 0 {
		t.Fatalf("no production token: %+v hits %d", r, s.hits())
	}

	t.Setenv("GIN_MODE", "release")
	t.Setenv(fiscal.EnvAllowSandbox, "")
	r = fiscal.RunTest(context.Background(), deps(db), fiscal.EnvSandbox, sample(), nil)
	if r.OK || r.ErrorCode != "sandbox_in_release" || len(r.Steps) != 0 || s.hits() != 0 {
		t.Fatalf("sandbox in release: %+v hits %d", r, s.hits())
	}

	t.Setenv("GIN_MODE", "debug")
	r = fiscal.RunTest(context.Background(), deps(db), fiscal.EnvSandbox, fiscal.TestSample{TaxRate: 0.18}, nil)
	if r.OK || r.ErrorCode != "fiscal_hs_code_missing" || s.hits() != 0 {
		t.Fatalf("no HS code: %+v hits %d", r, s.hits())
	}
}

func TestRunTest_DoesNotNeedFilingEnabled(t *testing.T) {
	sandboxEnv(t)
	db := testdb.Fresh(t)
	s := acceptingStub(t)
	enableFiscal(t, db, s, true)
	if _, err := db.Exec(`UPDATE settings SET value = jsonb_set(value, '{enabled}', 'false') WHERE key = 'fiscal_config'`); err != nil {
		t.Fatal(err)
	}
	r := fiscal.RunTest(context.Background(), deps(db), fiscal.EnvSandbox, sample(), nil)
	if !r.OK {
		t.Fatalf("the test is how setup is checked before enabling: %+v", r)
	}
}
