package handlers

import (
	"errors"
	"net/http"
	"sync"
	"testing"
	"time"

	"elevon-backend/internal/middleware"
	"elevon-backend/internal/models"
	"elevon-backend/internal/staffpin"
	"elevon-backend/internal/testdb"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func usersRouter(h *UsersHandler, a actor) *gin.Engine {
	r := gin.New()
	g := r.Group("/admin", asActor(a))
	g.GET("/users", h.List)
	g.POST("/users", h.Create)
	g.PUT("/users/:id", h.Update)
	g.PUT("/users/:id/pin", h.SetPin)
	return r
}

func TestUsers_CreateListAndConflicts(t *testing.T) {
	db := testdb.Fresh(t)
	ownerID := seedUser(t, db, "owner", "admin", "owner-pass-1", nil)
	r := usersRouter(NewUsersHandler(db), actor{id: ownerID, username: "owner", role: "admin"})

	w := doJSON(r, http.MethodPost, "/admin/users", models.CreateUserRequest{Username: " Ali.K ", Email: "Ali@Example.pk", Password: "till-pass-1", FirstName: "Ali", LastName: "Khan", Role: "counter"})
	if w.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", w.Code, w.Body.String())
	}
	var created models.User
	dataAs(t, decodeEnvelope(t, w), &created)
	if created.Username != "ali.k" || created.Email == nil || *created.Email != "ali@example.pk" || created.HasPin {
		t.Fatalf("normalised user: %+v", created)
	}

	for _, tc := range []struct {
		req  models.CreateUserRequest
		code string
	}{
		{models.CreateUserRequest{Username: "ALI.K", Password: "till-pass-1", FirstName: "X", Role: "counter"}, "username_taken"},
		{models.CreateUserRequest{Username: "other", Email: "ali@example.pk", Password: "till-pass-1", FirstName: "X", Role: "counter"}, "email_taken"},
		{models.CreateUserRequest{Username: "ab", Password: "till-pass-1", FirstName: "X", Role: "counter"}, "invalid_username"},
		{models.CreateUserRequest{Username: "okname", Password: "short", FirstName: "X", Role: "counter"}, "weak_password"},
		{models.CreateUserRequest{Username: "okname", Password: "till-pass-1", FirstName: "X", Role: "manager"}, "invalid_role"},
		{models.CreateUserRequest{Username: "okname", Password: "till-pass-1", FirstName: "", Role: "counter"}, "invalid_name"},
	} {
		w := doJSON(r, http.MethodPost, "/admin/users", tc.req)
		if got := errCode(decodeEnvelope(t, w)); got != tc.code {
			t.Errorf("%+v: want %s got %s (%d)", tc.req, tc.code, got, w.Code)
		}
	}

	w = doJSON(r, http.MethodGet, "/admin/users?search=khan", nil)
	var page models.PaginatedResponse
	if err := jsonUnmarshal(w.Body.Bytes(), &page); err != nil || w.Code != http.StatusOK || page.Meta.Total != 1 {
		t.Fatalf("search: %d %s (%v)", w.Code, w.Body.String(), err)
	}
	w = doJSON(r, http.MethodGet, "/admin/users?role=admin", nil)
	_ = jsonUnmarshal(w.Body.Bytes(), &page)
	if page.Meta.Total != 1 {
		t.Fatalf("role filter: %s", w.Body.String())
	}
}

func TestUsers_UpdateRevokesAndGuards(t *testing.T) {
	db := testdb.Fresh(t)
	ownerID := seedUser(t, db, "owner", "admin", "owner-pass-1", nil)
	tillID := seedUser(t, db, "till", "counter", "till-pass-1", nil)
	r := usersRouter(NewUsersHandler(db), actor{id: ownerID, username: "owner", role: "admin"})
	f, tr := false, true

	// Deactivate → inactive + sessions rejected.
	if w := doJSON(r, http.MethodPut, "/admin/users/"+tillID.String(), models.UpdateUserRequest{IsActive: &f}); w.Code != http.StatusOK {
		t.Fatalf("deactivate: %d %s", w.Code, w.Body.String())
	}
	if err := middleware.CheckTokenNotRevoked(db, tillID, time.Now().Add(-2*time.Second)); !errors.Is(err, middleware.ErrUserInactive) {
		t.Fatalf("inactive user sessions: %v", err)
	}
	// Reactivate + promote → token revoked, still fine to log in later.
	role := "admin"
	if w := doJSON(r, http.MethodPut, "/admin/users/"+tillID.String(), models.UpdateUserRequest{IsActive: &tr, Role: &role}); w.Code != http.StatusOK {
		t.Fatalf("promote: %d %s", w.Code, w.Body.String())
	}
	if err := middleware.CheckTokenNotRevoked(db, tillID, time.Now().Add(-2*time.Second)); !errors.Is(err, middleware.ErrTokenRevoked) {
		t.Fatalf("role change must revoke: %v", err)
	}
	// Self-protection.
	if w := doJSON(r, http.MethodPut, "/admin/users/"+ownerID.String(), models.UpdateUserRequest{IsActive: &f}); errCode(decodeEnvelope(t, w)) != "cannot_modify_self" {
		t.Fatalf("self deactivate: %s", w.Body.String())
	}
	// Demote the other admin → allowed (owner remains), and its PIN is cleared.
	if _, err := db.Exec(`UPDATE users SET pin_hash = 'x' WHERE id = $1`, tillID); err != nil {
		t.Fatal(err)
	}
	counter := "counter"
	w := doJSON(r, http.MethodPut, "/admin/users/"+tillID.String(), models.UpdateUserRequest{Role: &counter})
	var u models.User
	dataAs(t, decodeEnvelope(t, w), &u)
	if w.Code != http.StatusOK || u.Role != "counter" || u.HasPin {
		t.Fatalf("demote clears pin: %d %+v", w.Code, u)
	}
	// Last-admin guard, exercised with a counter actor (defence in depth
	// behind RequireRoles).
	rc := usersRouter(NewUsersHandler(db), actor{id: tillID, username: "till", role: "counter"})
	if w := doJSON(rc, http.MethodPut, "/admin/users/"+ownerID.String(), models.UpdateUserRequest{IsActive: &f}); errCode(decodeEnvelope(t, w)) != "last_admin" {
		t.Fatalf("last admin: %s", w.Body.String())
	}
	// Admin password reset.
	pw := "reset-pass-1"
	if w := doJSON(r, http.MethodPut, "/admin/users/"+tillID.String(), models.UpdateUserRequest{Password: &pw}); w.Code != http.StatusOK {
		t.Fatalf("password: %d %s", w.Code, w.Body.String())
	}
	if w := doJSON(r, http.MethodPut, "/admin/users/"+tillID.String(), models.UpdateUserRequest{}); errCode(decodeEnvelope(t, w)) != "no_changes" {
		t.Fatalf("empty update: %s", w.Body.String())
	}
	if w := doJSON(r, http.MethodPut, "/admin/users/not-a-uuid", models.UpdateUserRequest{Password: &pw}); w.Code != http.StatusNotFound {
		t.Fatalf("bad id: %d", w.Code)
	}
}

// TestUsers_ProfileEditDoesNotRevoke guards against the bug where the
// frontend always sends role and is_active on an edit form, which made
// Update revoke sessions on every save — including an admin editing their
// own row, which then bounced them to /login. Only a real change to role
// or is_active (or a password reset) should revoke.
func TestUsers_ProfileEditDoesNotRevoke(t *testing.T) {
	db := testdb.Fresh(t)
	ownerID := seedUser(t, db, "owner", "admin", "owner-pass-1", nil)
	tillID := seedUser(t, db, "till", "counter", "till-pass-1", nil)
	r := usersRouter(NewUsersHandler(db), actor{id: ownerID, username: "owner", role: "admin"})

	counter, tr := "counter", true
	if w := doJSON(r, http.MethodPut, "/admin/users/"+tillID.String(), models.UpdateUserRequest{FirstName: strp("New"), Role: &counter, IsActive: &tr}); w.Code != http.StatusOK {
		t.Fatalf("profile edit: %d %s", w.Code, w.Body.String())
	}
	var revokedAt *time.Time
	if err := db.QueryRow(`SELECT token_revoked_at FROM users WHERE id = $1`, tillID).Scan(&revokedAt); err != nil {
		t.Fatal(err)
	}
	if revokedAt != nil {
		t.Fatalf("unchanged role/active must not revoke, got token_revoked_at=%v", *revokedAt)
	}

	admin := "admin"
	if w := doJSON(r, http.MethodPut, "/admin/users/"+tillID.String(), models.UpdateUserRequest{Role: &admin}); w.Code != http.StatusOK {
		t.Fatalf("role change: %d %s", w.Code, w.Body.String())
	}
	if err := db.QueryRow(`SELECT token_revoked_at FROM users WHERE id = $1`, tillID).Scan(&revokedAt); err != nil {
		t.Fatal(err)
	}
	if revokedAt == nil {
		t.Fatal("a real role change must revoke")
	}
}

func TestUsers_SetPin(t *testing.T) {
	db := testdb.Fresh(t)
	ownerID := seedUser(t, db, "owner", "admin", "owner-pass-1", nil)
	secondID := seedUser(t, db, "second", "admin", "owner-pass-2", nil)
	tillID := seedUser(t, db, "till", "counter", "till-pass-1", nil)
	r := usersRouter(NewUsersHandler(db), actor{id: ownerID, username: "owner", role: "admin"})

	if w := doJSON(r, http.MethodPut, "/admin/users/"+ownerID.String()+"/pin", models.SetPinRequest{Pin: "1234"}); w.Code != http.StatusOK {
		t.Fatalf("set pin: %d %s", w.Code, w.Body.String())
	}
	got, err := staffpin.Identify(db, "1234", staffpin.AdminOnly)
	if err != nil || got.UserID != ownerID {
		t.Fatalf("identify: %+v %v", got, err)
	}
	// Re-setting your own PIN to the same value is fine.
	if w := doJSON(r, http.MethodPut, "/admin/users/"+ownerID.String()+"/pin", models.SetPinRequest{Pin: "1234"}); w.Code != http.StatusOK {
		t.Fatalf("re-set: %d", w.Code)
	}
	if w := doJSON(r, http.MethodPut, "/admin/users/"+secondID.String()+"/pin", models.SetPinRequest{Pin: "1234"}); errCode(decodeEnvelope(t, w)) != "pin_in_use" {
		t.Fatalf("duplicate pin: %s", w.Body.String())
	}
	if w := doJSON(r, http.MethodPut, "/admin/users/"+tillID.String()+"/pin", models.SetPinRequest{Pin: "5678"}); errCode(decodeEnvelope(t, w)) != "pin_not_allowed_for_role" {
		t.Fatalf("counter pin: %s", w.Body.String())
	}
	if w := doJSON(r, http.MethodPut, "/admin/users/"+secondID.String()+"/pin", models.SetPinRequest{Pin: "12a4"}); errCode(decodeEnvelope(t, w)) != "invalid_pin_format" {
		t.Fatalf("format: %s", w.Body.String())
	}
}

// TestUsers_SetPinIsSerialised guards against two concurrent SetPin
// requests for two different admins both observing "no match" for the same
// PIN and both committing — which would leave two admins sharing a PIN.
// The advisory lock in SetPin forces every request through one at a time,
// so the DB never ends up with more than one holder of a given PIN.
func TestUsers_SetPinIsSerialised(t *testing.T) {
	db := testdb.Fresh(t)
	aID := seedUser(t, db, "admin-a", "admin", "owner-pass-1", nil)
	bID := seedUser(t, db, "admin-b", "admin", "owner-pass-2", nil)
	r := usersRouter(NewUsersHandler(db), actor{id: aID, username: "admin-a", role: "admin"})

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		target := aID
		if i%2 == 1 {
			target = bID
		}
		wg.Add(1)
		go func(id uuid.UUID) {
			defer wg.Done()
			doJSON(r, http.MethodPut, "/admin/users/"+id.String()+"/pin", models.SetPinRequest{Pin: "2468"})
		}(target)
	}
	wg.Wait()

	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM users WHERE pin_hash IS NOT NULL`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected exactly one admin to hold the PIN, got %d", count)
	}
	if _, err := staffpin.Identify(db, "2468", staffpin.AdminOnly); err != nil {
		t.Fatalf("identify: %v", err)
	}
}

// TestUsers_LastAdminGuardIsSerialised guards against two concurrent
// deactivate requests, each targeting the other of exactly two active
// admins, both observing "1 other active admin" and both committing — which
// would leave zero active admins. The advisory lock in Update forces one
// request to see the other's committed change before its own last-admin
// count runs.
func TestUsers_LastAdminGuardIsSerialised(t *testing.T) {
	db := testdb.Fresh(t)
	aID := seedUser(t, db, "admin-a", "admin", "owner-pass-1", nil)
	bID := seedUser(t, db, "admin-b", "admin", "owner-pass-2", nil)
	rAsB := usersRouter(NewUsersHandler(db), actor{id: bID, username: "admin-b", role: "admin"})
	rAsA := usersRouter(NewUsersHandler(db), actor{id: aID, username: "admin-a", role: "admin"})
	f := false

	codes := make([]int, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		w := doJSON(rAsB, http.MethodPut, "/admin/users/"+aID.String(), models.UpdateUserRequest{IsActive: &f})
		codes[0] = w.Code
	}()
	go func() {
		defer wg.Done()
		w := doJSON(rAsA, http.MethodPut, "/admin/users/"+bID.String(), models.UpdateUserRequest{IsActive: &f})
		codes[1] = w.Code
	}()
	wg.Wait()

	var active int
	if err := db.QueryRow(`SELECT COUNT(*) FROM users WHERE role = 'admin' AND is_active`).Scan(&active); err != nil {
		t.Fatal(err)
	}
	if active != 1 {
		t.Fatalf("expected exactly one active admin left, got %d", active)
	}
	successes, conflicts := 0, 0
	for _, code := range codes {
		switch code {
		case http.StatusOK:
			successes++
		case http.StatusConflict:
			conflicts++
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("expected one 200 and one 409, got codes=%v", codes)
	}
}
