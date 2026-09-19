package util

// Canonical staff roles. Mirrors frontend/src/lib/roles.ts and the
// users_role_check constraint in migrations/001_init.sql.
const (
	RoleAdmin   = "admin"
	RoleCounter = "counter"
)

// AllRoles lists every role, for route groups every signed-in user may reach.
var AllRoles = []string{RoleAdmin, RoleCounter}

// ValidRole reports whether s is a known role.
func ValidRole(s string) bool {
	for _, r := range AllRoles {
		if r == s {
			return true
		}
	}
	return false
}
