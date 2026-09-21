package settings

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// The fiscal_config value migration 003_fiscal.sql seeds, verbatim.
const seededFiscalConfig = `{"enabled":false,"is_sandbox":true,"seller_ntn_cnic":"","seller_business_name":"","seller_province":"","seller_address":"","scenario_registered":"SN001","scenario_unregistered":"SN002","rate_desc":"18%","sale_type":"Goods at Standard Rate (default)","trans_type_id":75,"default_uom":"KG","buyer_registration_default":"Unregistered","validate_url":"","post_url":""}`

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
		{"fiscal_config", `{}`}, {"fiscal_config", seededFiscalConfig},
		{"fiscal_config", `{"enabled":true,"seller_ntn_cnic":"8951943","seller_business_name":"Elevon Gas","seller_province":"PUNJAB","seller_address":"Main Bazar"}`},
		{"fiscal_api_key_enc", `""`}, {"fiscal_api_key_enc", `"AAAA"`},
		{"fiscal_reference", `{}`}, {"fiscal_reference", `{"hs_uom":{"2711.1910":["KG"]}}`},
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
		{"fiscal_config", `{"nope":1}`}, {"fiscal_config", `{"trans_type_id":"75"}`}, {"fiscal_config", `[]`},
		{"fiscal_config", `{"enabled":true}`}, {"fiscal_config", `{"enabled":true,"seller_province":"Punjab","seller_ntn_cnic":"8951943","seller_business_name":"G","seller_address":"A"}`},
		{"fiscal_reference", `[]`}, {"fiscal_reference", `"x"`},
		{"fiscal_api_key_enc", `"` + strings.Repeat("x", 513) + `"`}, {"fiscal_api_key_enc", `42`},
		{"nope", `1`},
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
	if n := len(Keys()); n != 24 {
		t.Fatalf("001_init seeds 21 keys and 003_fiscal three more; rules cover %d", n)
	}
	for _, k := range []string{"fiscal_config", "fiscal_api_key_enc", "fiscal_reference"} {
		if !Known(k) {
			t.Errorf("%s is seeded by 003_fiscal and needs a rule", k)
		}
	}
}

func TestPrivate_OnlyTheToken(t *testing.T) {
	if !Private("fiscal_api_key_enc") {
		t.Fatal("the encrypted FBR token must be private")
	}
	for _, k := range Keys() {
		if k != "fiscal_api_key_enc" && Private(k) {
			t.Errorf("%s must not be private", k)
		}
	}
}

// Enabling FBR filing is a gate: every key the payload depends on must agree
// before the switch can go on. Each refusal carries its own stable code.
func TestCheckConsistency_FiscalGate(t *testing.T) {
	base := func() map[string]json.RawMessage {
		return map[string]json.RawMessage{
			"tax_rate_cash":      json.RawMessage(`0.18`),
			"default_hs_code":    json.RawMessage(`"2711.1910"`),
			"fiscal_config":      json.RawMessage(`{"enabled":true,"rate_desc":"18%","default_uom":"KG"}`),
			"fiscal_api_key_enc": json.RawMessage(`"c2VhbGVk"`),
			"fiscal_reference":   json.RawMessage(`{"hs_uom":{"2711.1910":["KG"]}}`),
		}
	}

	if err := CheckConsistency(base()); err != nil {
		t.Fatalf("a complete fiscal setup must pass: %v", err)
	}

	// Disabled: none of it applies, so setup can happen in any order.
	off := base()
	off["fiscal_config"] = json.RawMessage(`{"enabled":false,"rate_desc":"18%"}`)
	off["fiscal_api_key_enc"] = json.RawMessage(`""`)
	off["fiscal_reference"] = json.RawMessage(`{}`)
	if err := CheckConsistency(off); err != nil {
		t.Fatalf("a disabled config must not be gated: %v", err)
	}

	cases := []struct {
		name string
		code string
		edit func(m map[string]json.RawMessage)
	}{
		{"rate_desc against a different tax rate", "tax_rate_mismatch", func(m map[string]json.RawMessage) {
			m["tax_rate_cash"] = json.RawMessage(`0.17`)
		}},
		{"tax rate missing", "tax_rate_mismatch", func(m map[string]json.RawMessage) {
			delete(m, "tax_rate_cash")
		}},
		{"no token", "fiscal_token_missing", func(m map[string]json.RawMessage) {
			m["fiscal_api_key_enc"] = json.RawMessage(`"  "`)
		}},
		{"reference never refreshed", "fiscal_reference_stale", func(m map[string]json.RawMessage) {
			m["fiscal_reference"] = json.RawMessage(`{}`)
		}},
		{"reference does not cover the HS code", "fiscal_reference_stale", func(m map[string]json.RawMessage) {
			m["fiscal_reference"] = json.RawMessage(`{"hs_uom":{"2710.1210":["KG"]}}`)
		}},
		{"UoM FBR does not allow for the HS code", "fiscal_uom_not_allowed", func(m map[string]json.RawMessage) {
			m["fiscal_config"] = json.RawMessage(`{"enabled":true,"rate_desc":"18%","default_uom":"Liter"}`)
		}},
	}
	for _, c := range cases {
		m := base()
		c.edit(m)
		err := CheckConsistency(m)
		var ve *ValueError
		if !errors.As(err, &ve) {
			t.Errorf("%s: want a *ValueError, got %v", c.name, err)
			continue
		}
		if ve.Code != c.code {
			t.Errorf("%s: want code %s, got %s", c.name, c.code, ve.Code)
		}
		if ve.Key != "fiscal_config" {
			t.Errorf("%s: the refusal belongs to fiscal_config, got %s", c.name, ve.Key)
		}
	}

	// 0.18 × 100 must not trip float comparison.
	exact := base()
	exact["tax_rate_cash"] = json.RawMessage(`0.18`)
	exact["fiscal_config"] = json.RawMessage(`{"enabled":true,"rate_desc":"18.0%","default_uom":"KG"}`)
	if err := CheckConsistency(exact); err != nil {
		t.Fatalf("18.0%% is the same rate as 18%%: %v", err)
	}
}
