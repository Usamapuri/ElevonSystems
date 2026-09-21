package fiscal

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// Spec §7.1 and CLAUDE.md invariant 7. The sandbox validate endpoint is the
// `_sb` one. This reads urls.go as text so that renaming the constant, or
// pasting the live path over the sandbox one, fails the build rather than a
// filing run.
func TestURLs_SandboxValidateCarriesTheSBSuffix(t *testing.T) {
	src, err := os.ReadFile("urls.go")
	if err != nil {
		t.Fatal(err)
	}
	assign := regexp.MustCompile(`(?m)^\s*ValidateURLSandbox\s*=\s*"([^"]+)"`)
	m := assign.FindSubmatch(src)
	if m == nil {
		t.Fatal("urls.go must assign ValidateURLSandbox a string literal")
	}
	if !strings.HasSuffix(string(m[1]), "validateinvoicedata_sb") {
		t.Fatalf("sandbox validate URL must end in validateinvoicedata_sb, got %q", m[1])
	}
	if string(m[1]) != ValidateURLSandbox {
		t.Fatalf("literal %q does not match the constant %q", m[1], ValidateURLSandbox)
	}

	if ValidateURLSandbox == PostURLSandbox {
		t.Fatal("validate and post must be different endpoints")
	}
	if ValidateURLSandbox == ValidateURLLive || PostURLSandbox == PostURLLive {
		t.Fatal("sandbox and live endpoints must differ")
	}
	if strings.HasSuffix(ValidateURLLive, "_sb") || strings.HasSuffix(PostURLLive, "_sb") {
		t.Fatalf("live endpoints must not carry _sb: %q %q", ValidateURLLive, PostURLLive)
	}
	if !strings.HasSuffix(PostURLSandbox, "postinvoicedata_sb") {
		t.Fatalf("sandbox post URL must end in postinvoicedata_sb, got %q", PostURLSandbox)
	}
}

func TestConfig_URLResolution(t *testing.T) {
	sandbox := Config{IsSandbox: true}
	if sandbox.ValidateURL() != ValidateURLSandbox || sandbox.PostURL() != PostURLSandbox {
		t.Fatalf("sandbox: %s %s", sandbox.ValidateURL(), sandbox.PostURL())
	}
	live := Config{IsSandbox: false}
	if live.ValidateURL() != ValidateURLLive || live.PostURL() != PostURLLive {
		t.Fatalf("live: %s %s", live.ValidateURL(), live.PostURL())
	}
	over := Config{IsSandbox: true, ValidateURLOverride: "https://example.test/v", PostURLOverride: "https://example.test/p"}
	if over.ValidateURL() != "https://example.test/v" || over.PostURL() != "https://example.test/p" {
		t.Fatalf("override: %s %s", over.ValidateURL(), over.PostURL())
	}
}

// Spec §7.1: a release deployment configured for the sandbox refuses instead
// of filing test invoices, unless the environment opts in explicitly.
func TestCheckRuntime_SandboxInRelease(t *testing.T) {
	sandbox := Config{Enabled: true, IsSandbox: true}
	live := Config{Enabled: true, IsSandbox: false}

	t.Setenv("GIN_MODE", "release")
	t.Setenv(EnvAllowSandbox, "")
	if err := sandbox.CheckRuntime(); err != ErrSandboxInRelease {
		t.Fatalf("release + sandbox must refuse, got %v", err)
	}
	if SandboxAllowed() {
		t.Fatal("release without the opt-in must not allow the sandbox")
	}
	if err := live.CheckRuntime(); err != nil {
		t.Fatalf("release + live must run: %v", err)
	}

	t.Setenv(EnvAllowSandbox, "true")
	if err := sandbox.CheckRuntime(); err != nil {
		t.Fatalf("release + sandbox + opt-in must run: %v", err)
	}
	if !SandboxAllowed() {
		t.Fatal("the opt-in must allow the sandbox")
	}

	// Anything other than the exact string "true" is not an opt-in.
	t.Setenv(EnvAllowSandbox, "1")
	if err := sandbox.CheckRuntime(); err != ErrSandboxInRelease {
		t.Fatalf("only \"true\" opts in, got %v", err)
	}

	t.Setenv("GIN_MODE", "debug")
	t.Setenv(EnvAllowSandbox, "")
	if err := sandbox.CheckRuntime(); err != nil {
		t.Fatalf("outside release the sandbox is the normal case: %v", err)
	}
}
