package settings

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestValidate_PerKeyRules(t *testing.T) {
	ok := []struct{ key, raw string }{
		{"business_name", `"Gas House"`}, {"business_address", `""`}, {"business_province", `"Punjab"`},
		{"day_boundary_hour", `0`}, {"day_boundary_hour", `12`},
		{"tax_rate_cash", `0`}, {"tax_rate_cash", `0.18`}, {"tax_rate_credit", `null`}, {"tax_rate_credit", `0.2`}, {"further_tax_rate", `0.04`},
		{"default_hs_code", `""`}, {"default_hs_code", `"2711.1910"`},
		{"receipt_paper_width_mm", `58`}, {"receipt_paper_width_mm", `80`}, {"receipt_printable_area_mm", `72`},
		{"receipt_logo_url", `""`}, {"receipt_logo_url", `"https://x/logo.png"`}, {"receipt_logo_url", `"data:image/png;base64,iVBORw0KGgo="`},
		{"receipt_header_lines", `[]`}, {"receipt_header_lines", `["a","b"]`}, {"receipt_default_document", `"a4"`},
		{"day_close_variance_threshold", `100`}, {"credit_limit_enforced", `true`},
	}
	for _, c := range ok {
		if err := Validate(c.key, json.RawMessage(c.raw)); err != nil {
			t.Errorf("%s=%s should be valid: %v", c.key, c.raw, err)
		}
	}
	bad := []struct{ key, raw string }{
		{"business_name", `""`}, {"business_name", `42`}, {"business_phone", `"` + strings.Repeat("x", 31) + `"`},
		{"day_boundary_hour", `13`}, {"day_boundary_hour", `-1`}, {"day_boundary_hour", `1.5`},
		{"tax_rate_cash", `1.5`}, {"tax_rate_cash", `-0.1`}, {"tax_rate_cash", `"18"`}, {"tax_rate_credit", `"x"`},
		{"default_hs_code", `"27111910"`}, {"default_hs_code", `"2711.19"`},
		{"receipt_paper_width_mm", `76`}, {"receipt_printable_area_mm", `39`}, {"receipt_printable_area_mm", `81`},
		{"receipt_logo_url", `"ftp://x"`}, {"receipt_logo_url", `"data:text/plain;base64,QQ=="`},
		{"receipt_header_lines", `["a","b","c","d","e","f","g"]`}, {"receipt_header_lines", `"a"`}, {"receipt_footer_lines", `[1]`},
		{"receipt_default_document", `"pdf"`}, {"day_close_variance_threshold", `-1`}, {"credit_limit_enforced", `"yes"`},
		{"fiscal_config", `{}`}, {"nope", `1`},
	}
	for _, c := range bad {
		err := Validate(c.key, json.RawMessage(c.raw))
		if err == nil {
			t.Errorf("%s=%s should be rejected", c.key, c.raw)
			continue
		}
		if ve, ok := err.(*ValueError); !ok || ve.Key != c.key {
			t.Errorf("%s: want *ValueError naming the key, got %v", c.key, err)
		}
	}
}

func TestValidate_RejectsNullForNonNullableKeys(t *testing.T) {
	for _, k := range Keys() {
		err := Validate(k, json.RawMessage("null"))
		if k == "tax_rate_credit" {
			if err != nil {
				t.Errorf("%s: null must be allowed, got %v", k, err)
			}
			continue
		}
		if err == nil {
			t.Errorf("%s: null must be rejected", k)
			continue
		}
		if ve, ok := err.(*ValueError); !ok || ve.Key != k {
			t.Errorf("%s: want *ValueError naming the key, got %v", k, err)
		}
	}
}

func TestCheckConsistency_PrintableAreaFitsPaper(t *testing.T) {
	all := map[string]json.RawMessage{"receipt_paper_width_mm": json.RawMessage(`58`), "receipt_printable_area_mm": json.RawMessage(`72`)}
	if err := CheckConsistency(all); err == nil {
		t.Fatal("72 mm printable on 58 mm paper must fail")
	}
	all["receipt_printable_area_mm"] = json.RawMessage(`48`)
	if err := CheckConsistency(all); err != nil {
		t.Fatal(err)
	}
}

func TestKeys_MatchTheSeededList(t *testing.T) {
	if n := len(Keys()); n != 21 {
		t.Fatalf("001_init seeds 21 keys; rules cover %d", n)
	}
	if Known("fiscal_config") {
		t.Fatal("fiscal_config is Phase 7")
	}
}
