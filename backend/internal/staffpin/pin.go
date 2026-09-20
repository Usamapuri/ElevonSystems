// Package staffpin identifies an admin by their bcrypt-hashed PIN (voids,
// day reopen, force close, credit-limit override). PINs cannot be queried by
// hash, so every active candidate is compared in turn — and every row is
// compared even after a hit, so wall-clock time never reveals which row
// matched (retail audit F-CTRL-09).
package staffpin

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

// ErrNoMatch: no active user in the role set holds this PIN.
var ErrNoMatch = errors.New("no_matching_pin")

// AdminOnly is the role set for every elevation in this app (spec D6).
var AdminOnly = []string{"admin"}

// Identity is the matched user.
type Identity struct {
	UserID   uuid.UUID
	Username string
	Name     string
	Role     string
}

// Querier is the subset of *sql.DB / *sql.Tx Identify needs, so a void can
// authorise inside the transaction that performs it.
type Querier interface {
	Query(query string, args ...any) (*sql.Rows, error)
}

// Identify returns the active user in allowedRoles whose pin_hash matches
// pin, or ErrNoMatch. Bounded by the number of admins (a handful), so the
// bcrypt cost is tens of milliseconds — fine for human-paced flows.
func Identify(q Querier, pin string, allowedRoles []string) (Identity, error) {
	if len(allowedRoles) == 0 || pin == "" {
		return Identity{}, ErrNoMatch
	}
	placeholders := make([]string, len(allowedRoles))
	args := make([]any, len(allowedRoles))
	for i, r := range allowedRoles {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
		args[i] = r
	}
	rows, err := q.Query(`SELECT id, username, COALESCE(NULLIF(TRIM(first_name || ' ' || last_name), ''), username), role, pin_hash
		FROM users WHERE is_active = true AND pin_hash IS NOT NULL AND role IN (`+strings.Join(placeholders, ",")+`)`, args...)
	if err != nil {
		return Identity{}, err
	}
	defer rows.Close()
	pinBytes := []byte(pin)
	var match Identity
	found := false
	for rows.Next() {
		var id uuid.UUID
		var username, name, role, hash string
		if err := rows.Scan(&id, &username, &name, &role, &hash); err != nil {
			return Identity{}, err
		}
		// Compare every row; never short-circuit.
		if bcrypt.CompareHashAndPassword([]byte(hash), pinBytes) == nil && !found {
			match = Identity{UserID: id, Username: username, Name: name, Role: role}
			found = true
		}
	}
	if err := rows.Err(); err != nil {
		return Identity{}, err
	}
	if !found {
		return Identity{}, ErrNoMatch
	}
	return match, nil
}
