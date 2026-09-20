package handlers

import "testing"

func TestNormaliseUsername(t *testing.T) {
	for _, ok := range []string{"ali", "  Ali.Khan-1 ", "a_b", "abc"} {
		if _, valid := normaliseUsername(ok); !valid {
			t.Errorf("%q should be valid", ok)
		}
	}
	for _, bad := range []string{"", "ab", ".ali", "ali khan", "ali@x", "-ali"} {
		if _, valid := normaliseUsername(bad); valid {
			t.Errorf("%q should be invalid", bad)
		}
	}
	if u, _ := normaliseUsername("  Ali.Khan-1 "); u != "ali.khan-1" {
		t.Fatalf("must trim and lowercase, got %q", u)
	}
}

func TestNormaliseEmail(t *testing.T) {
	if e, ok := normaliseEmail("   "); !ok || e != nil {
		t.Fatal("blank email is allowed and becomes NULL")
	}
	if e, ok := normaliseEmail(" Owner@Example.PK "); !ok || e == nil || *e != "owner@example.pk" {
		t.Fatal("email is trimmed and lowercased")
	}
	for _, bad := range []string{"owner", "@x.pk", "owner@", "a b@x.pk", "a@@x.pk"} {
		if _, ok := normaliseEmail(bad); ok {
			t.Errorf("%q should be rejected", bad)
		}
	}
}

func TestPinFormat(t *testing.T) {
	if !validPin("0123") {
		t.Fatal("4 digits is valid")
	}
	for _, bad := range []string{"", "123", "12345", "12a4", " 1234"} {
		if validPin(bad) {
			t.Errorf("%q should be invalid", bad)
		}
	}
}
