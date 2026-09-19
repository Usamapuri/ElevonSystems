package database

import (
	"strings"
	"testing"
)

func TestInitialAdminCredentials_DefaultsUsername(t *testing.T) {
	u, err := initialAdminCredentials("", "correct-horse-battery")
	if err != nil || u != "admin" {
		t.Fatalf("got %q, %v", u, err)
	}
}

func TestInitialAdminCredentials_RejectsShortPassword(t *testing.T) {
	if _, err := initialAdminCredentials("admin", "short"); err == nil {
		t.Fatal("password under 10 bytes must be rejected")
	}
}

func TestInitialAdminCredentials_RejectsOver72Bytes(t *testing.T) {
	if _, err := initialAdminCredentials("admin", strings.Repeat("x", 73)); err == nil {
		t.Fatal("bcrypt truncates past 72 bytes; must refuse, not truncate")
	}
}

func TestInitialAdminCredentials_NormalisesAndValidatesUsername(t *testing.T) {
	u, err := initialAdminCredentials("  Owner.1 ", "correct-horse-battery")
	if err != nil || u != "owner.1" {
		t.Fatalf("got %q, %v", u, err)
	}
	if _, err := initialAdminCredentials("a", "correct-horse-battery"); err == nil {
		t.Fatal("username shorter than 3 chars must be rejected")
	}
}
