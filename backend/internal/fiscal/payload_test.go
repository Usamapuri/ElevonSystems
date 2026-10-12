package fiscal

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"elevon-backend/internal/models"
)

// sandboxCfg is the fiscal_config the payloads in testdata/ were built
// against: seller 8951943 / Elevon Test Seller / PUNJAB / Lahore, the
// sandbox-confirmed defaults from audit/FBR_SANDBOX_2026-09-21.md.
func sandboxCfg() Config {
	cfg := Defaults()
	cfg.IsSandbox = true
	cfg.SellerNTNCNIC = "8951943"
	cfg.SellerBusinessName = "Elevon Test Seller"
	cfg.SellerProvince = "PUNJAB"
	cfg.SellerAddress = "Lahore"
	return cfg
}

// businessDate is invoiceDate "2026-09-21" for every fixture.
func businessDate() time.Time {
	return time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC)
}

func strPtr(s string) *string { return &s }

// asMap unmarshals JSON bytes into a map[string]any, so two payloads compare
// order-independently and with every number decoded as the same float64
// (12.5 and 12.500 land identically, since both go through encoding/json).
func asMap(t *testing.T, label string, data []byte) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("%s: unmarshal: %v\n%s", label, err, data)
	}
	return m
}

func loadFixture(t *testing.T, name string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read fixture %s: %v", name, err)
	}
	return asMap(t, name, data)
}

func assertGolden(t *testing.T, got DIInvoice, fixtureName string) {
	t.Helper()
	gotBytes, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal built payload: %v", err)
	}
	gotMap := asMap(t, "built", gotBytes)
	wantMap := loadFixture(t, fixtureName)
	if !reflect.DeepEqual(gotMap, wantMap) {
		gotPretty, _ := json.MarshalIndent(gotMap, "", "  ")
		wantPretty, _ := json.MarshalIndent(wantMap, "", "  ")
		t.Fatalf("payload does not match %s:\n--- got ---\n%s\n--- want ---\n%s", fixtureName, gotPretty, wantPretty)
	}
}

// --- Case A: SN002 walk-in, one line ---------------------------------------

func TestBuildSaleInvoice_CaseA_WalkIn(t *testing.T) {
	inv := models.Invoice{
		BusinessDate:     businessDate(),
		FurtherTaxAmount: 0,
		Lines: []models.InvoiceLine{
			{
				ProductName:  "LPG bulk",
				Quantity:     12.5,
				LineTotal:    3312.5,
				LineDiscount: 0,
				LineTax:      596.25,
			},
		},
	}
	in := BuildInput{Invoice: inv, Buyer: nil, DefaultHSCode: "2711.1910"}

	got, err := BuildSaleInvoice(in, sandboxCfg())
	if err != nil {
		t.Fatalf("BuildSaleInvoice: %v", err)
	}
	assertGolden(t, got, "case_a_sn002.json")
}

// --- Case C: two lines, Rs 500 discount split 109.67/390.33 ----------------

func TestBuildSaleInvoice_CaseC_Discount(t *testing.T) {
	inv := models.Invoice{
		BusinessDate:     businessDate(),
		FurtherTaxAmount: 0,
		Lines: []models.InvoiceLine{
			{
				ProductName:  "LPG bulk",
				HSCode:       strPtr("2711.1910"),
				FBRUoM:       strPtr("KG"),
				Quantity:     12.5,
				LineTotal:    3312.50,
				LineDiscount: 109.67,
				LineTax:      576.51,
			},
			{
				ProductName:  "LPG 45 kg cylinder fill",
				HSCode:       strPtr("2711.1910"),
				FBRUoM:       strPtr("KG"),
				Quantity:     45.0,
				LineTotal:    11790.00,
				LineDiscount: 390.33,
				LineTax:      2051.94,
			},
		},
	}
	in := BuildInput{Invoice: inv, Buyer: nil, DefaultHSCode: "2711.1910"}

	got, err := BuildSaleInvoice(in, sandboxCfg())
	if err != nil {
		t.Fatalf("BuildSaleInvoice: %v", err)
	}
	assertGolden(t, got, "case_c_discount.json")
}

// --- Case D: further tax 132.50 on a single line ----------------------------

func TestBuildSaleInvoice_CaseD_FurtherTax(t *testing.T) {
	inv := models.Invoice{
		BusinessDate:     businessDate(),
		FurtherTaxAmount: 132.50,
		Lines: []models.InvoiceLine{
			{
				ProductName:  "LPG bulk",
				Quantity:     12.5,
				LineTotal:    3312.5,
				LineDiscount: 0,
				LineTax:      596.25,
			},
		},
	}
	in := BuildInput{Invoice: inv, Buyer: nil, DefaultHSCode: "2711.1910"}

	got, err := BuildSaleInvoice(in, sandboxCfg())
	if err != nil {
		t.Fatalf("BuildSaleInvoice: %v", err)
	}
	assertGolden(t, got, "case_d_further_tax.json")
}

// --- Case E: Debit Note with reason, built against a Registered buyer ------
//
// The sandbox validated this fixture against an unregistered walk-in buyer
// only up to the buyer-registration check (0106 "The Buyer is not
// registered for sales tax"), per audit/FBR_SANDBOX_2026-09-21.md decision
// 2. testdata/case_e_debit_note.json was edited from the sandbox-validated
// payload to a Registered buyer (NTN 1234567, scenarioId SN001) so this
// test exercises the registered path the brief calls for; everything else
// — seller, line, ref, reason — is the sandbox-validated payload verbatim.
func TestBuildDebitNote_CaseE_Reason(t *testing.T) {
	inv := models.Invoice{
		BusinessDate:     businessDate(),
		FurtherTaxAmount: 0,
		Lines: []models.InvoiceLine{
			{
				ProductName:  "LPG bulk",
				Quantity:     12.5,
				LineTotal:    3312.5,
				LineDiscount: 0,
				LineTax:      596.25,
			},
		},
	}
	buyer := &Buyer{
		NTNCNIC:          "1234567",
		Name:             "Test Registered Buyer",
		Province:         "PUNJAB",
		Address:          "Lahore",
		RegistrationType: "Registered",
	}
	in := BuildInput{Invoice: inv, Buyer: buyer, DefaultHSCode: "2711.1910"}

	got, err := BuildDebitNote(in, sandboxCfg(), "6110180871403DIAJGEJ4031308", "Sale voided at the till")
	if err != nil {
		t.Fatalf("BuildDebitNote: %v", err)
	}
	assertGolden(t, got, "case_e_debit_note.json")

	if got.InvoiceRefNo != "6110180871403DIAJGEJ4031308" {
		t.Errorf("invoiceRefNo = %q", got.InvoiceRefNo)
	}
	if got.Reason != "Sale voided at the till" {
		t.Errorf("reason = %q", got.Reason)
	}
}

func TestBuildDebitNote_NoScenarioOutsideSandbox(t *testing.T) {
	inv := models.Invoice{
		BusinessDate: businessDate(),
		Lines: []models.InvoiceLine{
			{ProductName: "LPG bulk", Quantity: 12.5, LineTotal: 3312.5, LineTax: 596.25},
		},
	}
	buyer := &Buyer{NTNCNIC: "1234567", Name: "Test Registered Buyer", Province: "PUNJAB", Address: "Lahore", RegistrationType: "Registered"}
	cfg := sandboxCfg()
	cfg.IsSandbox = false

	got, err := BuildDebitNote(BuildInput{Invoice: inv, Buyer: buyer, DefaultHSCode: "2711.1910"}, cfg, "6110180871403DIAJGEJ4031308", "Sale voided at the till")
	if err != nil {
		t.Fatalf("BuildDebitNote: %v", err)
	}
	if got.ScenarioID != "" {
		t.Errorf("scenarioId must be empty outside sandbox, got %q", got.ScenarioID)
	}
	raw, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	m := asMap(t, "built", raw)
	if _, ok := m["scenarioId"]; ok {
		t.Errorf("scenarioId key must be omitted outside sandbox, got %v", m["scenarioId"])
	}
	if got.InvoiceType != "Debit Note" {
		t.Errorf("invoiceType = %q", got.InvoiceType)
	}
}

// --- Walk-in defaults --------------------------------------------------

func TestBuildSaleInvoice_WalkInDefaults(t *testing.T) {
	inv := models.Invoice{
		BusinessDate: businessDate(),
		Lines: []models.InvoiceLine{
			{ProductName: "LPG bulk", Quantity: 12.5, LineTotal: 3312.5, LineTax: 596.25},
		},
	}
	got, err := BuildSaleInvoice(BuildInput{Invoice: inv, Buyer: nil, DefaultHSCode: "2711.1910"}, sandboxCfg())
	if err != nil {
		t.Fatalf("BuildSaleInvoice: %v", err)
	}
	if got.BuyerNTNCNIC != "" {
		t.Errorf("buyerNTNCNIC = %q, want empty", got.BuyerNTNCNIC)
	}
	if got.BuyerBusinessName != "Walk-in Customer" {
		t.Errorf("buyerBusinessName = %q", got.BuyerBusinessName)
	}
	if got.BuyerProvince != "PUNJAB" {
		t.Errorf("buyerProvince = %q, want seller's province", got.BuyerProvince)
	}
	if got.BuyerAddress != "N/A" {
		t.Errorf("buyerAddress = %q", got.BuyerAddress)
	}
	if got.BuyerRegistrationType != "Unregistered" {
		t.Errorf("buyerRegistrationType = %q", got.BuyerRegistrationType)
	}
	if got.ScenarioID != "SN002" {
		t.Errorf("scenarioId = %q, want SN002 for an unregistered walk-in in sandbox", got.ScenarioID)
	}
}

// A named buyer with no province on file still has to carry one: FBR takes
// buyerProvince from its own upper-case list and rejects a blank, so the
// payload falls back to the seller's province rather than sending "".
func TestBuildSaleInvoice_BuyerWithoutProvinceFallsBackToTheSeller(t *testing.T) {
	inv := models.Invoice{
		BusinessDate: businessDate(),
		Lines: []models.InvoiceLine{
			{ProductName: "LPG bulk", Quantity: 12.5, LineTotal: 3312.5, LineTax: 596.25},
		},
	}
	buyer := &Buyer{NTNCNIC: "1234567", Name: "Anwar Traders", Province: "  ", Address: "Gulberg, Lahore", RegistrationType: "Registered"}

	got, err := BuildSaleInvoice(BuildInput{Invoice: inv, Buyer: buyer, DefaultHSCode: "2711.1910"}, sandboxCfg())
	if err != nil {
		t.Fatalf("BuildSaleInvoice: %v", err)
	}
	if got.BuyerProvince != "PUNJAB" {
		t.Errorf("buyerProvince = %q, want the seller's PUNJAB", got.BuyerProvince)
	}
	// Nothing else about the buyer is invented along the way.
	if got.BuyerBusinessName != "Anwar Traders" || got.BuyerRegistrationType != "Registered" {
		t.Errorf("buyer = %q / %q", got.BuyerBusinessName, got.BuyerRegistrationType)
	}
}

// --- Registered buyer: SN001 in sandbox, no scenarioId in production ------

func TestBuildSaleInvoice_RegisteredBuyerScenario(t *testing.T) {
	inv := models.Invoice{
		BusinessDate: businessDate(),
		Lines: []models.InvoiceLine{
			{ProductName: "LPG bulk", Quantity: 12.5, LineTotal: 3312.5, LineTax: 596.25},
		},
	}
	buyer := &Buyer{NTNCNIC: "1234567", Name: "Test Registered Buyer", Province: "PUNJAB", Address: "Lahore", RegistrationType: "Registered"}

	sandbox := sandboxCfg()
	got, err := BuildSaleInvoice(BuildInput{Invoice: inv, Buyer: buyer, DefaultHSCode: "2711.1910"}, sandbox)
	if err != nil {
		t.Fatalf("BuildSaleInvoice (sandbox): %v", err)
	}
	if got.ScenarioID != "SN001" {
		t.Errorf("scenarioId = %q, want SN001 for a registered buyer in sandbox", got.ScenarioID)
	}
	if got.BuyerRegistrationType != "Registered" || got.BuyerNTNCNIC != "1234567" {
		t.Errorf("buyer fields = %q/%q", got.BuyerRegistrationType, got.BuyerNTNCNIC)
	}

	production := sandboxCfg()
	production.IsSandbox = false
	got, err = BuildSaleInvoice(BuildInput{Invoice: inv, Buyer: buyer, DefaultHSCode: "2711.1910"}, production)
	if err != nil {
		t.Fatalf("BuildSaleInvoice (production): %v", err)
	}
	if got.ScenarioID != "" {
		t.Errorf("scenarioId = %q, want empty in production", got.ScenarioID)
	}
}

// --- HS code: line, then default, then refuse ------------------------------

// Spec 2026-10-12 §8.2: the document is dated invoice_date, which an admin
// may have set in the past; business_date is where the sale is booked, not
// what FBR is told.
func TestBuildSaleInvoice_DatedByInvoiceDate(t *testing.T) {
	inv := models.Invoice{
		BusinessDate: time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC),
		InvoiceDate:  time.Date(2026, 10, 5, 0, 0, 0, 0, time.UTC),
		Lines: []models.InvoiceLine{
			{ProductName: "LPG bulk", Quantity: 12.5, LineTotal: 3312.5, LineTax: 596.25},
		},
	}
	got, err := BuildSaleInvoice(BuildInput{Invoice: inv, DefaultHSCode: "2711.1910"}, sandboxCfg())
	if err != nil {
		t.Fatal(err)
	}
	if got.InvoiceDate != "2026-10-05" {
		t.Fatalf("invoiceDate: got %s want the invoice_date 2026-10-05", got.InvoiceDate)
	}
}

func TestBuildSaleInvoice_HSCodeFromLineThenDefaultThenMissing(t *testing.T) {
	base := models.Invoice{
		BusinessDate: businessDate(),
		Lines: []models.InvoiceLine{
			{ProductName: "LPG bulk", Quantity: 12.5, LineTotal: 3312.5, LineTax: 596.25},
		},
	}

	// Line HS code wins over the default.
	withLine := base
	withLine.Lines = []models.InvoiceLine{base.Lines[0]}
	withLine.Lines[0].HSCode = strPtr("9999.9999")
	got, err := BuildSaleInvoice(BuildInput{Invoice: withLine, DefaultHSCode: "2711.1910"}, sandboxCfg())
	if err != nil {
		t.Fatalf("BuildSaleInvoice: %v", err)
	}
	if got.Items[0].HSCode != "9999.9999" {
		t.Errorf("hsCode = %q, want the line's own code", got.Items[0].HSCode)
	}

	// No line HS code: falls back to the default.
	got, err = BuildSaleInvoice(BuildInput{Invoice: base, DefaultHSCode: "2711.1910"}, sandboxCfg())
	if err != nil {
		t.Fatalf("BuildSaleInvoice: %v", err)
	}
	if got.Items[0].HSCode != "2711.1910" {
		t.Errorf("hsCode = %q, want the default", got.Items[0].HSCode)
	}

	// Neither: refuse.
	_, err = BuildSaleInvoice(BuildInput{Invoice: base, DefaultHSCode: ""}, sandboxCfg())
	if err != ErrHSCodeMissing {
		t.Fatalf("err = %v, want ErrHSCodeMissing", err)
	}
}

// --- SplitFurtherTax ---------------------------------------------------

func TestSplitFurtherTax_SumsExactly(t *testing.T) {
	got := SplitFurtherTax(132.50, []float64{3202.83, 11399.67})
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	sum := round2(got[0] + got[1])
	if sum != 132.50 {
		t.Fatalf("sum = %v, want 132.50 (got %v)", sum, got)
	}
	for _, v := range got {
		if v < 0 {
			t.Errorf("negative share: %v", got)
		}
	}
}

func TestSplitFurtherTax_SingleLineGetsAll(t *testing.T) {
	got := SplitFurtherTax(132.50, []float64{3312.5})
	if len(got) != 1 || got[0] != 132.50 {
		t.Fatalf("got = %v, want [132.50]", got)
	}
}

func TestSplitFurtherTax_ZeroTaxableStillSumsExactly(t *testing.T) {
	got := SplitFurtherTax(100.00, []float64{0, 0, 0})
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	var sum float64
	for _, v := range got {
		sum += v
	}
	if round2(sum) != 100.00 {
		t.Fatalf("sum = %v, want 100.00 (got %v)", round2(sum), got)
	}
}

func TestSplitFurtherTax_ManyLinesSumExactly(t *testing.T) {
	taxable := []float64{100.01, 200.02, 50.005, 999.99, 1.11}
	for _, total := range []float64{0, 0.01, 13.37, 500.00, 1351.126} {
		got := SplitFurtherTax(total, taxable)
		if len(got) != len(taxable) {
			t.Fatalf("len = %d, want %d", len(got), len(taxable))
		}
		var sum float64
		for _, v := range got {
			sum += v
		}
		if round2(sum) != round2(total) {
			t.Fatalf("total %v: sum = %v, want %v (got %v)", total, round2(sum), round2(total), got)
		}
	}
}

// --- Combined invoice (spec 2026-10-12 §2.3): one item per source line ------

func TestBuildBatchInvoice_OneItemPerSourceLine(t *testing.T) {
	hs := "2711.1910"
	day := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	a := models.Invoice{InvoiceNumber: "20261012-001", BusinessDate: day, TaxRate: 0.18, Lines: []models.InvoiceLine{
		{ProductName: "LPG bulk", HSCode: &hs, Quantity: 12.5, LineTotal: 3312.50, LineDiscount: 0, LineTax: 596.25},
		{ProductName: "LPG bulk", HSCode: &hs, Quantity: 3.333, LineTotal: 883.25, LineDiscount: 0, LineTax: 158.99},
	}}
	b := models.Invoice{InvoiceNumber: "20261012-002", BusinessDate: day, TaxRate: 0.18, Lines: []models.InvoiceLine{
		{ProductName: "LPG cylinder", HSCode: &hs, Quantity: 11.8, LineTotal: 3127.00, LineDiscount: 127.00, LineTax: 540.00},
	}}
	got, err := BuildBatchInvoice(BatchInput{BusinessDate: day, Invoices: []models.Invoice{a, b}, DefaultHSCode: ""}, sandboxCfg())
	if err != nil {
		t.Fatal(err)
	}
	if got.InvoiceType != "Sale Invoice" || got.InvoiceDate != "2026-10-12" || got.BuyerRegistrationType != "Unregistered" || got.BuyerBusinessName != "Walk-in Customer" {
		t.Fatalf("header: %+v", got)
	}
	if len(got.Items) != 3 {
		t.Fatalf("one item per source line, got %d", len(got.Items))
	}
	// Per-item figures are the stored line figures, untouched: FBR recomputes
	// tax per item (0104), so nothing may be summed and re-rounded.
	if got.Items[1].ValueSalesExcludingST != 883.25 || got.Items[1].SalesTaxApplicable != 158.99 {
		t.Fatalf("line 2 copied verbatim: %+v", got.Items[1])
	}
	if got.Items[2].ValueSalesExcludingST != 3000.00 || got.Items[2].Discount != 127.00 {
		t.Fatalf("discounted line: %+v", got.Items[2])
	}
	if got.Items[0].ProductDescription != "20261012-001 LPG bulk 12.500 kg" {
		t.Fatalf("description carries the source invoice: %q", got.Items[0].ProductDescription)
	}
	assertGolden(t, got, "batch_two_invoices.json")
}

func TestBuildBatchInvoice_Refusals(t *testing.T) {
	cfg := sandboxCfg()
	if _, err := BuildBatchInvoice(BatchInput{}, cfg); !errors.Is(err, ErrBatchEmpty) {
		t.Fatalf("empty batch: %v", err)
	}
	inv := models.Invoice{InvoiceNumber: "x", Lines: []models.InvoiceLine{{ProductName: "LPG", Quantity: 1, LineTotal: 100, LineTax: 18}}}
	if _, err := BuildBatchInvoice(BatchInput{Invoices: []models.Invoice{inv}}, cfg); !errors.Is(err, ErrHSCodeMissing) {
		t.Fatalf("no HS code anywhere: %v", err)
	}
	if ErrorCode(ErrBatchEmpty) != "fiscal_batch_empty" {
		t.Fatalf("ErrorCode must know the batch refusal: %q", ErrorCode(ErrBatchEmpty))
	}
}
