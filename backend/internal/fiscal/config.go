// Package fiscal owns everything that talks to FBR Digital Invoicing: the
// store's configuration, the encrypted API token, the PRAL endpoints and the
// runtime guards around them.
//
// Import direction is one way. settings imports fiscal (for ParseConfig and
// Validate); fiscal never imports settings or handlers. Anything fiscal needs
// out of the settings table it reads itself through the tiny Querier below,
// which both *sql.DB and *sql.Tx satisfy.
package fiscal

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
)

// Querier is the slice of database/sql that fiscal's loaders need. *sql.DB
// and *sql.Tx both satisfy it, so a caller can read inside its transaction.
type Querier interface {
	QueryRow(query string, args ...any) *sql.Row
}

// Setting keys this package reads. They are declared here, not imported from
// settings, to keep the dependency one way.
const (
	KeyConfig    = "fiscal_config"
	KeyAPIKeyEnc = "fiscal_api_key_enc"
	KeyReference = "fiscal_reference"
)

// Config is the fiscal_config setting. The JSON names are the wire contract
// with the admin screen and with migration 003's seed; do not rename them.
//
// The two URL overrides are fields with an "Override" suffix because Config
// also carries ValidateURL()/PostURL() methods (urls.go) that resolve an
// override against the sandbox/live constants; a field and a method cannot
// share a name.
type Config struct {
	Enabled   bool `json:"enabled"`
	IsSandbox bool `json:"is_sandbox"`

	SellerNTNCNIC      string `json:"seller_ntn_cnic"`
	SellerBusinessName string `json:"seller_business_name"`
	SellerProvince     string `json:"seller_province"`
	SellerAddress      string `json:"seller_address"`

	ScenarioRegistered   string `json:"scenario_registered"`
	ScenarioUnregistered string `json:"scenario_unregistered"`

	RateDesc                 string `json:"rate_desc"`
	SaleType                 string `json:"sale_type"`
	TransTypeID              int    `json:"trans_type_id"`
	DefaultUoM               string `json:"default_uom"`
	BuyerRegistrationDefault string `json:"buyer_registration_default"`

	ValidateURLOverride string `json:"validate_url"`
	PostURLOverride     string `json:"post_url"`
}

// PublicConfig is what the admin API hands the browser: the config plus the
// three facts about the environment that the screen needs. The token itself
// never appears here.
type PublicConfig struct {
	Config
	APIKeySet      bool   `json:"api_key_set"`
	APIKeyMasked   string `json:"api_key_masked"`
	SandboxAllowed bool   `json:"sandbox_allowed"`
}

// Defaults are the values migration 003 seeds and the values ParseConfig
// fills in for any field a caller leaves out. Every one of them is confirmed
// against the sandbox (audit/FBR_SANDBOX_2026-09-21.md): trans type 75 is
// "Goods at standard rate (default)" (18 is Services), "18%" is the only
// rate the API offers for it, and KG is the one UoM 2711.1910 allows.
//
// There is deliberately no HS code here: CLAUDE.md invariant 7 keeps it a
// settings value the tax advisor confirms, never a literal in Go.
func Defaults() Config {
	return Config{
		Enabled:                  false,
		IsSandbox:                true,
		ScenarioRegistered:       "SN001",
		ScenarioUnregistered:     "SN002",
		RateDesc:                 "18%",
		SaleType:                 "Goods at Standard Rate (default)",
		TransTypeID:              75,
		DefaultUoM:               "KG",
		BuyerRegistrationDefault: "Unregistered",
	}
}

var (
	ntnRe      = regexp.MustCompile(`^[0-9]{7}$|^[0-9]{13}$`)
	rateDescRe = regexp.MustCompile(`^[0-9]+(\.[0-9]+)?%$`)
	scenarioRe = regexp.MustCompile(`^SN[0-9]{3}$`)
	provinceRe = regexp.MustCompile(`^[A-Z][A-Z ]*[A-Z]$`)
)

// ParseConfig reads a fiscal_config value. It is strict about shape —
// anything but a JSON object, an unknown field or a field of the wrong type
// is an error — and lenient about completeness: a missing field keeps its
// Defaults() value, so a partial save from the admin screen cannot silently
// blank out trans_type_id or the rate string.
//
// The error text is curated, not the decoder's: it reaches the owner through
// settings.ValueError.
func ParseConfig(raw json.RawMessage) (Config, error) {
	cfg := Defaults()
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return cfg, errors.New("must be a fiscal settings object")
	}
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		return Defaults(), errors.New("must be a fiscal settings object with known fields of the right types")
	}
	return cfg, nil
}

// Validate is the gate on switching FBR filing on. While Enabled is false the
// owner may save a half-filled form; the moment it is true every field the
// payload depends on must already be right, because a rejected invoice at the
// till is worse than a refused save in Settings.
//
// The cross-key rules — rate_desc against tax_rate_cash, the token, the
// reference lists — live in settings.CheckConsistency, which sees every key.
func (c Config) Validate() error {
	if err := checkOverrideURL(c.ValidateURLOverride); err != nil {
		return err
	}
	if err := checkOverrideURL(c.PostURLOverride); err != nil {
		return err
	}
	if !c.Enabled {
		return nil
	}
	if strings.TrimSpace(c.SellerBusinessName) == "" {
		return errors.New("needs the seller business name before it can be enabled")
	}
	if strings.TrimSpace(c.SellerAddress) == "" {
		return errors.New("needs the seller address before it can be enabled")
	}
	if !ntnRe.MatchString(strings.TrimSpace(c.SellerNTNCNIC)) {
		return errors.New("needs a seller NTN of 7 digits or a CNIC of 13 digits")
	}
	province := strings.TrimSpace(c.SellerProvince)
	if province == "" || province != strings.ToUpper(province) || !provinceRe.MatchString(province) {
		return errors.New("needs the seller province spelled in upper case exactly as FBR lists it, for example PUNJAB")
	}
	if !rateDescRe.MatchString(c.RateDesc) {
		return errors.New("needs a rate description like 18%")
	}
	if !scenarioRe.MatchString(c.ScenarioRegistered) || !scenarioRe.MatchString(c.ScenarioUnregistered) {
		return errors.New("needs both sandbox scenarios in the form SN001")
	}
	if c.TransTypeID <= 0 {
		return errors.New("needs an FBR transaction type id")
	}
	if strings.TrimSpace(c.SaleType) == "" {
		return errors.New("needs the FBR sale type")
	}
	if strings.TrimSpace(c.DefaultUoM) == "" {
		return errors.New("needs a default unit of measure")
	}
	switch c.BuyerRegistrationDefault {
	case "Registered", "Unregistered":
	default:
		return errors.New("needs a buyer registration default of Registered or Unregistered")
	}
	return nil
}

func checkOverrideURL(u string) error {
	if u == "" || strings.HasPrefix(u, "https://") {
		return nil
	}
	return errors.New("URL overrides must be blank or https://")
}

// Public wraps the config for the admin screen. The caller supplies whether a
// token is stored and its masked tail; Public never touches the token itself.
func (c Config) Public(apiKeySet bool, masked string) PublicConfig {
	return PublicConfig{Config: c, APIKeySet: apiKeySet, APIKeyMasked: masked, SandboxAllowed: SandboxAllowed()}
}

// MaskAPIKey renders a stored token for display: "****" plus its last four
// characters, or "" when there is no token.
func MaskAPIKey(token string) string {
	if token == "" {
		return ""
	}
	if len(token) <= 4 {
		return "****"
	}
	return "****" + token[len(token)-4:]
}

// LoadConfig reads fiscal_config. A missing row yields Defaults() with
// Enabled false — filing stays off rather than running on guesses.
func LoadConfig(q Querier) (Config, error) {
	var raw []byte
	err := q.QueryRow(`SELECT value FROM settings WHERE key = $1`, KeyConfig).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return Defaults(), nil
	}
	if err != nil {
		return Defaults(), err
	}
	return ParseConfig(raw)
}

// LoadAPIKey reads fiscal_api_key_enc and decrypts it with the environment's
// secrets key. An unset token is "" with no error; a set token with no usable
// key is an error, because filing with the wrong token is not recoverable.
func LoadAPIKey(q Querier) (string, error) {
	var raw []byte
	err := q.QueryRow(`SELECT value FROM settings WHERE key = $1`, KeyAPIKeyEnc).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	var enc string
	if err := json.Unmarshal(raw, &enc); err != nil {
		return "", errors.New("fiscal_api_key_enc is not a string")
	}
	if strings.TrimSpace(enc) == "" {
		return "", nil
	}
	key, err := KeyFromEnv()
	if err != nil {
		return "", err
	}
	return Decrypt(key, enc)
}
