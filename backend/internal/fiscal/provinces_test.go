package fiscal

import "testing"

// FBR's /pdi/v1/provinces list has seven rows in upper case (audit
// FBR_SANDBOX_2026-09-21.md, re-fetched 2026-10-13). A buyer province that
// is not one of them is refused by the gateway with 0074, so the customers
// handler canonicalises what it stores through CanonicalProvince.
func TestCanonicalProvince(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"PUNJAB", "PUNJAB", true},
		{"punjab", "PUNJAB", true},
		{"  Capital Territory ", "CAPITAL TERRITORY", true},
		{"Khyber Pakhtunkhwa", "KHYBER PAKHTUNKHWA", true},
		{"Azad Jammu and Kashmir", "AZAD JAMMU AND KASHMIR", true},
		{"Gilgit Baltistan", "GILGIT BALTISTAN", true},
		{"Sindh", "SINDH", true},
		{"Balochistan", "BALOCHISTAN", true},
		{"ICT", "", false},
		{"Islamabad", "", false},
		{"", "", false},
	}
	for _, c := range cases {
		got, ok := CanonicalProvince(c.in)
		if got != c.want || ok != c.ok {
			t.Errorf("CanonicalProvince(%q) = %q,%v want %q,%v", c.in, got, ok, c.want, c.ok)
		}
	}
	if len(Provinces) != 7 {
		t.Fatalf("Provinces has %d entries, FBR lists 7", len(Provinces))
	}
}
