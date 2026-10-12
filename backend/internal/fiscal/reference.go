package fiscal

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// FBR's reference lists (spec §7.1), cached in settings.fiscal_reference so
// the admin screen can offer provinces and units from the real lists and
// settings.CheckConsistency can refuse a UoM FBR would reject (0099).

// UoM is one /pdi/v1/uom row.
type UoM struct {
	ID          int    `json:"id"`
	Description string `json:"description"`
}

// Rate is one /pdi/v2/SaleTypeToRate row.
type Rate struct {
	ID    int     `json:"id"`
	Desc  string  `json:"desc"`
	Value float64 `json:"value"`
}

// Reference is the cached set. The JSON names are the settings contract.
type Reference struct {
	Provinces   []string            `json:"provinces"`
	UoMs        []UoM               `json:"uoms"`
	HSUoM       map[string][]string `json:"hs_uom"`
	Rates       []Rate              `json:"rates"`
	RefreshedAt time.Time           `json:"refreshed_at"`
}

// ErrReferenceUnreadable: a list came back in a shape this app cannot read.
var ErrReferenceUnreadable = errors.New("fiscal_reference_unreadable")

// Refresh fetches the lists with token. hsCodes is every HS code in use
// (settings.default_hs_code plus each product's); duplicates are collapsed
// and each code gets one HS_UOM call. today is formatted DD-Mon-YYYY for
// SaleTypeToRate, which answers for the rates in force on that day.
func Refresh(ctx context.Context, d Deps, cfg Config, token string, hsCodes []string, today time.Time) (Reference, error) {
	cl := d.client(cfg, token)
	base := cfg.referenceBase()
	ref := Reference{HSUoM: map[string][]string{}}

	body, err := cl.Get(ctx, base+"/pdi/v1/provinces")
	if err != nil {
		return Reference{}, err
	}
	for _, row := range decodeRows(body) {
		if desc := findString(row, "desc", "description", "name"); desc != "" {
			ref.Provinces = append(ref.Provinces, desc)
		}
	}
	if len(ref.Provinces) == 0 {
		return Reference{}, ErrReferenceUnreadable
	}

	body, err = cl.Get(ctx, base+"/pdi/v1/uom")
	if err != nil {
		return Reference{}, err
	}
	ref.UoMs = decodeUoMs(body)
	if len(ref.UoMs) == 0 {
		return Reference{}, ErrReferenceUnreadable
	}

	seen := map[string]bool{}
	for _, code := range hsCodes {
		code = strings.TrimSpace(code)
		if code == "" || seen[code] {
			continue
		}
		seen[code] = true
		q := url.Values{"hs_code": {code}, "annexure_id": {"3"}}
		body, err = cl.Get(ctx, base+"/pdi/v2/HS_UOM?"+q.Encode())
		if err != nil {
			return Reference{}, err
		}
		names := []string{}
		for _, u := range decodeUoMs(body) {
			names = append(names, u.Description)
		}
		ref.HSUoM[code] = names
	}

	q := url.Values{"date": {today.Format("02-Jan-2006")}, "transTypeId": {strconv.Itoa(cfg.TransTypeID)}, "originationSupplier": {"1"}}
	body, err = cl.Get(ctx, base+"/pdi/v2/SaleTypeToRate?"+q.Encode())
	if err != nil {
		return Reference{}, err
	}
	for _, row := range decodeRows(body) {
		r := Rate{Desc: findString(row, "ratE_DESC", "desc")}
		r.ID = int(findNumber(row, "ratE_ID", "id"))
		r.Value = findNumber(row, "ratE_VALUE", "value")
		if r.Desc != "" {
			ref.Rates = append(ref.Rates, r)
		}
	}
	ref.RefreshedAt = d.now()
	return ref, nil
}

// referenceBase is the host the reference lists live on, overridable for
// tests on the same terms as the validate/post URLs.
func (c Config) referenceBase() string {
	if c.ReferenceBaseOverride != "" {
		return strings.TrimRight(c.ReferenceBaseOverride, "/")
	}
	return "https://gw.fbr.gov.pk"
}

func decodeRows(body []byte) []map[string]any {
	var rows []map[string]any
	if err := json.Unmarshal(body, &rows); err != nil {
		return nil
	}
	return rows
}

func decodeUoMs(body []byte) []UoM {
	out := []UoM{}
	for _, row := range decodeRows(body) {
		desc := findString(row, "description", "desc")
		if desc == "" {
			continue
		}
		out = append(out, UoM{ID: int(findNumber(row, "uoM_ID", "id")), Description: desc})
	}
	return out
}

// findString returns the first string field whose key matches one of the
// names, case-insensitively, then any key containing the first name. PRAL's
// field names are inconsistent across lists (stateProvinceDesc, description,
// ratE_DESC), so the match is deliberately loose.
func findString(row map[string]any, names ...string) string {
	for _, want := range names {
		for k, v := range row {
			if s, ok := v.(string); ok && strings.EqualFold(k, want) {
				return s
			}
		}
	}
	for k, v := range row {
		if s, ok := v.(string); ok && strings.Contains(strings.ToLower(k), strings.ToLower(names[0])) {
			return s
		}
	}
	return ""
}

func findNumber(row map[string]any, names ...string) float64 {
	for _, want := range names {
		for k, v := range row {
			if f, ok := v.(float64); ok && strings.EqualFold(k, want) {
				return f
			}
		}
	}
	return 0
}
