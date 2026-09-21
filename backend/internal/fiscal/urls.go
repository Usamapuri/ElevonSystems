package fiscal

import (
	"errors"
	"os"
)

// PRAL Digital Invoicing endpoints, spec §7.1. The sandbox pair is the `_sb`
// suffix on the same paths; urls_contract_test.go pins that the sandbox
// validate constant really carries it, because a sandbox/live mix-up once
// reached production in the sibling retail POS.
const (
	ValidateURLSandbox = "https://gw.fbr.gov.pk/di_data/v1/di/validateinvoicedata_sb"
	ValidateURLLive    = "https://gw.fbr.gov.pk/di_data/v1/di/validateinvoicedata"
	PostURLSandbox     = "https://gw.fbr.gov.pk/di_data/v1/di/postinvoicedata_sb"
	PostURLLive        = "https://gw.fbr.gov.pk/di_data/v1/di/postinvoicedata"
)

// Reference lists, spec §7.1. These are the same in sandbox and production.
const (
	ProvincesURL      = "https://gw.fbr.gov.pk/pdi/v1/provinces"
	UoMURL            = "https://gw.fbr.gov.pk/pdi/v1/uom"
	DocTypeCodeURL    = "https://gw.fbr.gov.pk/pdi/v1/doctypecode"
	TransTypeCodeURL  = "https://gw.fbr.gov.pk/pdi/v1/transtypecode"
	SaleTypeToRateURL = "https://gw.fbr.gov.pk/pdi/v2/SaleTypeToRate"
	HSUoMURL          = "https://gw.fbr.gov.pk/pdi/v2/HS_UOM"
)

// EnvAllowSandbox lets a non-production deployment that runs in release mode
// (the staging box) still talk to the sandbox. A production environment never
// sets it.
const EnvAllowSandbox = "FISCAL_ALLOW_SANDBOX"

// ErrSandboxInRelease is the refusal a live store gives when it finds itself
// configured for the sandbox. It never files anything.
var ErrSandboxInRelease = errors.New("sandbox_in_release")

// ValidateURL is where this config validates: an explicit override, else the
// sandbox or live constant.
func (c Config) ValidateURL() string {
	if c.ValidateURLOverride != "" {
		return c.ValidateURLOverride
	}
	if c.IsSandbox {
		return ValidateURLSandbox
	}
	return ValidateURLLive
}

// PostURL is where this config posts, on the same rule as ValidateURL.
func (c Config) PostURL() string {
	if c.PostURLOverride != "" {
		return c.PostURLOverride
	}
	if c.IsSandbox {
		return PostURLSandbox
	}
	return PostURLLive
}

// SandboxAllowed reports whether sandbox mode may run here: always outside
// release, and in release only when the environment opts in explicitly.
func SandboxAllowed() bool {
	return os.Getenv("GIN_MODE") != "release" || os.Getenv(EnvAllowSandbox) == "true"
}

// CheckRuntime is the guard every submission calls before it builds a
// payload: a release binary pointed at the sandbox refuses rather than filing
// test invoices against the store's real NTN.
func (c Config) CheckRuntime() error {
	if c.IsSandbox && !SandboxAllowed() {
		return ErrSandboxInRelease
	}
	return nil
}
