package fiscal

import "strings"

// Provinces is FBR's /pdi/v1/provinces list as the gateway spells it (seven
// rows, upper case; audit FBR_SANDBOX_2026-09-21.md, re-fetched 2026-10-13).
// A buyer province outside this list is refused with 0074, so the customers
// handler stores only these spellings and the till offers only these names.
// The seller province is still picked from the live list cached by Refresh
// from FBR; this copy exists so a customer can be saved before that refresh
// has ever run.
var Provinces = []string{
	"PUNJAB",
	"SINDH",
	"KHYBER PAKHTUNKHWA",
	"BALOCHISTAN",
	"CAPITAL TERRITORY",
	"AZAD JAMMU AND KASHMIR",
	"GILGIT BALTISTAN",
}

// CanonicalProvince maps a typed province onto FBR's spelling, ignoring case
// and surrounding space. Anything FBR would not recognise, including blank
// and abbreviations such as "ICT", returns ("", false).
func CanonicalProvince(s string) (string, bool) {
	want := strings.ToUpper(strings.Join(strings.Fields(s), " "))
	for _, p := range Provinces {
		if p == want {
			return p, true
		}
	}
	return "", false
}
