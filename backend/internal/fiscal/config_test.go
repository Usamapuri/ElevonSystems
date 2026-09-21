package fiscal

import (
	"encoding/json"
	"strings"
	"testing"
)

// The exact value migration 003 seeds. Parsing it must reproduce Defaults().
const seeded = `{"enabled":false,"is_sandbox":true,"seller_ntn_cnic":"","seller_business_name":"","seller_province":"","seller_address":"","scenario_registered":"SN001","scenario_unregistered":"SN002","rate_desc":"18%","sale_type":"Goods at Standard Rate (default)","trans_type_id":75,"default_uom":"KG","buyer_registration_default":"Unregistered","validate_url":"","post_url":""}`

func TestParseConfig_SeedMatchesDefaults(t *testing.T) {
	cfg, err := ParseConfig(json.RawMessage(seeded))
	if err != nil {
		t.Fatal(err)
	}
	if cfg != Defaults() {
		t.Fatalf("003's seed must equal Defaults():\n got %+v\nwant %+v", cfg, Defaults())
	}
	// Sandbox-confirmed values (audit/FBR_SANDBOX_2026-09-21.md).
	if cfg.TransTypeID != 75 || cfg.RateDesc != "18%" || cfg.DefaultUoM != "KG" {
		t.Fatalf("sandbox-confirmed defaults drifted: %+v", cfg)
	}
}

func TestParseConfig_DefaultsForMissingStrictOnShape(t *testing.T) {
	cfg, err := ParseConfig(json.RawMessage(`{"enabled":true,"seller_ntn_cnic":"8951943"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.Enabled || cfg.SellerNTNCNIC != "8951943" {
		t.Fatalf("explicit fields: %+v", cfg)
	}
	if cfg.TransTypeID != 75 || cfg.RateDesc != "18%" || cfg.ScenarioUnregistered != "SN002" || !cfg.IsSandbox {
		t.Fatalf("a partial save must not blank the defaults: %+v", cfg)
	}
	// An explicit false must survive, or production could never be selected.
	cfg, err = ParseConfig(json.RawMessage(`{"is_sandbox":false}`))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.IsSandbox {
		t.Fatal("explicit is_sandbox:false must stick")
	}

	for _, bad := range []string{`{"nope":1}`, `{"trans_type_id":"75"}`, `[]`, `"x"`, `7`, `null`, ``} {
		if _, err := ParseConfig(json.RawMessage(bad)); err == nil {
			t.Errorf("%s must be rejected", bad)
		}
	}
}

func TestConfig_Validate(t *testing.T) {
	good := Defaults()
	good.Enabled = true
	good.SellerNTNCNIC = "8951943"
	good.SellerBusinessName = "Elevon Gas"
	good.SellerProvince = "PUNJAB"
	good.SellerAddress = "Main Bazar, Lahore"
	if err := good.Validate(); err != nil {
		t.Fatalf("a complete config must validate: %v", err)
	}
	// 13-digit CNIC and a two-word province are both fine.
	cnic := good
	cnic.SellerNTNCNIC = "3520112345671"
	cnic.SellerProvince = "KHYBER PAKHTUNKHWA"
	if err := cnic.Validate(); err != nil {
		t.Fatalf("CNIC seller: %v", err)
	}

	// Disabled: a half-filled form saves, so setup can happen in any order.
	half := Defaults()
	if err := half.Validate(); err != nil {
		t.Fatalf("a disabled config must save: %v", err)
	}

	bad := map[string]func(c *Config){
		"blank business name": func(c *Config) { c.SellerBusinessName = " " },
		"blank address":       func(c *Config) { c.SellerAddress = "" },
		"blank NTN":           func(c *Config) { c.SellerNTNCNIC = "" },
		"8-digit NTN":         func(c *Config) { c.SellerNTNCNIC = "89519437" },
		"NTN with a dash":     func(c *Config) { c.SellerNTNCNIC = "1234567-8" },
		"blank province":      func(c *Config) { c.SellerProvince = "" },
		"lower-case province": func(c *Config) { c.SellerProvince = "Punjab" },
		"rate without %":      func(c *Config) { c.RateDesc = "18" },
		"rate as words":       func(c *Config) { c.RateDesc = "Standard" },
		"scenario shape":      func(c *Config) { c.ScenarioRegistered = "S1" },
		"scenario blank":      func(c *Config) { c.ScenarioUnregistered = "" },
		"trans type zero":     func(c *Config) { c.TransTypeID = 0 },
		"trans type negative": func(c *Config) { c.TransTypeID = -1 },
		"blank sale type":     func(c *Config) { c.SaleType = "" },
		"blank uom":           func(c *Config) { c.DefaultUoM = "" },
		"buyer default":       func(c *Config) { c.BuyerRegistrationDefault = "Walk-in" },
		"http override":       func(c *Config) { c.ValidateURLOverride = "http://gw.fbr.gov.pk/x" },
		"post override":       func(c *Config) { c.PostURLOverride = "gw.fbr.gov.pk" },
	}
	for name, mutate := range bad {
		c := good
		mutate(&c)
		if err := c.Validate(); err == nil {
			t.Errorf("%s must be rejected", name)
		}
	}

	// A plain-http override is refused even while filing is off: it would be
	// the first thing to bite the moment the switch is flipped.
	off := Defaults()
	off.PostURLOverride = "http://example.test"
	if err := off.Validate(); err == nil {
		t.Error("an http override must be rejected even when disabled")
	}
}

func TestConfig_PublicNeverCarriesTheToken(t *testing.T) {
	t.Setenv("GIN_MODE", "debug")
	cfg := Defaults()
	cfg.SellerNTNCNIC = "8951943"
	pub := cfg.Public(true, MaskAPIKey("supersecret-9911"))

	raw, err := json.Marshal(pub)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"enabled", "is_sandbox", "seller_ntn_cnic", "trans_type_id", "api_key_set", "api_key_masked", "sandbox_allowed"} {
		if _, ok := out[want]; !ok {
			t.Errorf("PublicConfig is missing %s: %s", want, raw)
		}
	}
	if out["api_key_masked"] != "****9911" || out["api_key_set"] != true {
		t.Fatalf("masking: %s", raw)
	}
	if out["sandbox_allowed"] != true {
		t.Fatalf("outside release the sandbox is allowed: %s", raw)
	}
	if strings.Contains(string(raw), "supersecret") {
		t.Fatalf("PublicConfig leaks the token: %s", raw)
	}
}
