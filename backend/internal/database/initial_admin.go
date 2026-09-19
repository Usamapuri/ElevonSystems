package database

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

const (
	// InitialAdminPasswordEnv is read only while no active admin exists.
	InitialAdminPasswordEnv = "INITIAL_ADMIN_PASSWORD"
	// InitialAdminUsernameEnv optionally overrides the username (default "admin").
	InitialAdminUsernameEnv = "INITIAL_ADMIN_USERNAME"

	initialAdminMinPasswordLen = 10
	initialAdminMaxPasswordLen = 72 // bcrypt ignores bytes past 72: refuse rather than truncate
)

var initialAdminUsernameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{2,49}$`)

func initialAdminCredentials(rawUsername, password string) (string, error) {
	username := strings.ToLower(strings.TrimSpace(rawUsername))
	if username == "" {
		username = "admin"
	}
	if !initialAdminUsernameRe.MatchString(username) {
		return "", fmt.Errorf("%s %q must be 3-50 characters of a-z, 0-9, dot, dash or underscore", InitialAdminUsernameEnv, username)
	}
	switch {
	case password == "":
		return "", fmt.Errorf("%s is not set", InitialAdminPasswordEnv)
	case len(password) < initialAdminMinPasswordLen:
		return "", fmt.Errorf("%s must be at least %d characters", InitialAdminPasswordEnv, initialAdminMinPasswordLen)
	case len(password) > initialAdminMaxPasswordLen:
		return "", fmt.Errorf("%s must be at most %d bytes", InitialAdminPasswordEnv, initialAdminMaxPasswordLen)
	}
	return username, nil
}

// EnsureInitialAdmin creates the first admin from env while the store has no
// active admin. Once one exists it does nothing, so real admins are never
// touched and the env var is inert. Never fatal: a missing password must not
// crash-loop the deploy that would let the operator set it.
func EnsureInitialAdmin(db *sql.DB) {
	var admins int
	if err := db.QueryRow(`SELECT COUNT(*) FROM users WHERE role = 'admin' AND is_active = true`).Scan(&admins); err != nil {
		log.Printf("initial admin: count admins: %v", err)
		return
	}
	if admins > 0 {
		return
	}
	username, err := initialAdminCredentials(os.Getenv(InitialAdminUsernameEnv), os.Getenv(InitialAdminPasswordEnv))
	if err != nil {
		log.Printf("WARNING: no active admin exists and %v — nobody can sign in. Set %s (optionally %s) and redeploy.", err, InitialAdminPasswordEnv, InitialAdminUsernameEnv)
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(os.Getenv(InitialAdminPasswordEnv)), bcrypt.DefaultCost)
	if err != nil {
		log.Printf("initial admin: hash: %v", err)
		return
	}
	res, err := db.Exec(`
		INSERT INTO users (username, email, password_hash, first_name, last_name, role, is_active)
		VALUES ($1, NULL, $2, 'Store', 'Admin', 'admin', true)
		ON CONFLICT DO NOTHING`, username, string(hash))
	if err != nil {
		log.Printf("initial admin: create %q: %v", username, err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		log.Printf("WARNING: username %q already exists (inactive?) — set %s to another name and redeploy.", username, InitialAdminUsernameEnv)
		return
	}
	log.Printf("initial admin: created %q from %s — sign in, change the password, then remove the env var", username, InitialAdminPasswordEnv)
}
