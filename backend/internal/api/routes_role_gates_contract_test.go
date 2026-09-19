package api

import (
	_ "embed"
	"regexp"
	"strings"
	"testing"
)

//go:embed routes.go
var routesSource string

// Every handler registration must hang off `staff.` or `admin.` (both groups
// carry RequireRoles). The single exception is POST /auth/login.
func TestRoutes_EveryRouteIsRoleGated(t *testing.T) {
	reg := regexp.MustCompile(`^\s*(\w+)\.(GET|POST|PUT|PATCH|DELETE)\(`)
	if !strings.Contains(routesSource, `staff := r.Group("", auth, middleware.RequireRoles(util.AllRoles))`) {
		t.Fatal("staff group must be defined with auth + RequireRoles(util.AllRoles)")
	}
	if !strings.Contains(routesSource, `admin := r.Group("/admin", auth, middleware.RequireRoles([]string{util.RoleAdmin}))`) {
		t.Fatal("admin group must be defined with auth + RequireRoles(admin)")
	}
	for i, line := range strings.Split(routesSource, "\n") {
		m := reg.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		group := m[1]
		if strings.Contains(line, `public.POST("/login"`) {
			continue
		}
		if group != "staff" && group != "admin" {
			t.Errorf("routes.go:%d registers a route on group %q, which carries no RequireRoles: %s", i+1, group, strings.TrimSpace(line))
		}
	}
}
