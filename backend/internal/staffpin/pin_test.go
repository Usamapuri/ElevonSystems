package staffpin

import (
	"database/sql"
	"errors"
	"testing"

	"elevon-backend/internal/testdb"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

func seed(t *testing.T, db *sql.DB, username, role, pin string, active bool) uuid.UUID {
	t.Helper()
	var pinHash *string
	if pin != "" {
		h, _ := bcrypt.GenerateFromPassword([]byte(pin), bcrypt.MinCost)
		s := string(h)
		pinHash = &s
	}
	var id uuid.UUID
	if err := db.QueryRow(`INSERT INTO users (username, password_hash, first_name, last_name, role, pin_hash, is_active)
		VALUES ($1, 'x', 'A', $1, $2, $3, $4) RETURNING id`, username, role, pinHash, active).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestIdentify_MatchesOnlyActiveAdminsWithThatPin(t *testing.T) {
	db := testdb.Fresh(t)
	owner := seed(t, db, "owner", "admin", "1234", true)
	seed(t, db, "second", "admin", "5678", true)
	seed(t, db, "gone", "admin", "9999", false)
	seed(t, db, "till", "counter", "4321", true)

	got, err := Identify(db, "1234", AdminOnly)
	if err != nil || got.UserID != owner || got.Username != "owner" || got.Role != "admin" {
		t.Fatalf("got %+v, %v", got, err)
	}
	if _, err := Identify(db, "0000", AdminOnly); !errors.Is(err, ErrNoMatch) {
		t.Fatalf("unknown PIN: %v", err)
	}
	if _, err := Identify(db, "9999", AdminOnly); !errors.Is(err, ErrNoMatch) {
		t.Fatalf("inactive admin must not match: %v", err)
	}
	if _, err := Identify(db, "4321", AdminOnly); !errors.Is(err, ErrNoMatch) {
		t.Fatalf("counter PIN must not match the admin set: %v", err)
	}
	if _, err := Identify(db, "1234", nil); !errors.Is(err, ErrNoMatch) {
		t.Fatal("empty role set matches nothing")
	}
}
