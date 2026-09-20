package handlers

const (
	// MinPasswordLen applies to every self-service and admin-set password.
	MinPasswordLen = 8
	// MaxPasswordBytes: bcrypt ignores bytes past 72 — refuse, never truncate.
	MaxPasswordBytes = 72
)

// checkPassword returns "" when pw is acceptable, else a stable error code.
func checkPassword(pw string) string {
	switch {
	case len(pw) < MinPasswordLen:
		return "weak_password"
	case len(pw) > MaxPasswordBytes:
		return "password_too_long"
	}
	return ""
}

// passwordMessage maps a checkPassword code to the sentence a person reads.
func passwordMessage(code string) string {
	switch code {
	case "weak_password":
		return "Password must be at least 8 characters"
	case "password_too_long":
		return "Password must be at most 72 bytes"
	}
	return "Invalid password"
}
