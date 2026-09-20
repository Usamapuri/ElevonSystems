package handlers

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"elevon-backend/internal/middleware"
	"elevon-backend/internal/models"
	"elevon-backend/internal/staffpin"
	"elevon-backend/internal/util"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

// UsersHandler administers staff accounts (admin only).
type UsersHandler struct{ db *sql.DB }

// NewUsersHandler builds a UsersHandler.
func NewUsersHandler(db *sql.DB) *UsersHandler { return &UsersHandler{db: db} }

var (
	usernameRe = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{2,49}$`)
	pinRe      = regexp.MustCompile(`^[0-9]{4}$`)
)

// adminMutationLockKey is the Postgres advisory-lock key that serialises
// admin-account mutations (PIN writes in SetPin, role/active changes in
// Update) across concurrent requests. Held for the duration of the
// transaction (pg_advisory_xact_lock), so a second request checking
// "does anyone else hold this PIN" or "is there another active admin"
// always sees the first request's write — a unique index can't do this for
// the PIN because bcrypt salts differ per hash. The value has no meaning
// beyond being a fixed, collision-free key for this one lock.
const adminMutationLockKey = 74391

// normaliseUsername trims, lowercases and validates (same rule as the
// initial admin: 3–50 of a-z 0-9 . _ -, starting alphanumeric).
func normaliseUsername(s string) (string, bool) {
	u := strings.ToLower(strings.TrimSpace(s))
	return u, usernameRe.MatchString(u)
}

// normaliseEmail returns (nil, true) for blank, the lowercased address when
// plausible, or ok=false. Deliverability is proven by the reset flow, not here.
func normaliseEmail(s string) (*string, bool) {
	e := strings.ToLower(strings.TrimSpace(s))
	if e == "" {
		return nil, true
	}
	at := strings.Index(e, "@")
	if at < 1 || at == len(e)-1 || strings.Count(e, "@") != 1 || strings.ContainsAny(e, " \t\r\n") || len(e) > 100 {
		return nil, false
	}
	return &e, true
}

func validName(s string) bool { return utf8.RuneCountInString(s) <= 50 }

func validPin(s string) bool { return pinRe.MatchString(s) }

// List returns a page of users. Filters: search (name/username/email),
// role, active=true|false. Sorted active first, then role, then username.
func (h *UsersHandler) List(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	if page < 1 {
		page = 1
	}
	perPage, _ := strconv.Atoi(c.DefaultQuery("per_page", "50"))
	if perPage < 1 || perPage > 100 {
		perPage = 50
	}
	where := []string{"true"}
	args := []any{}
	if role := c.Query("role"); role != "" {
		args = append(args, role)
		where = append(where, fmt.Sprintf("role = $%d", len(args)))
	}
	if active := c.Query("active"); active != "" {
		args = append(args, active == "true")
		where = append(where, fmt.Sprintf("is_active = $%d", len(args)))
	}
	if s := strings.TrimSpace(c.Query("search")); s != "" {
		args = append(args, "%"+s+"%")
		n := len(args)
		where = append(where, fmt.Sprintf("(username ILIKE $%d OR email ILIKE $%d OR first_name ILIKE $%d OR last_name ILIKE $%d)", n, n, n, n))
	}
	cond := strings.Join(where, " AND ")
	var total int
	if err := h.db.QueryRow(`SELECT COUNT(*) FROM users WHERE `+cond, args...).Scan(&total); err != nil {
		log.Printf("users list: count: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not load users", "internal_error"))
		return
	}
	args = append(args, perPage, (page-1)*perPage)
	rows, err := h.db.Query(`SELECT `+userColumns+` FROM users WHERE `+cond+
		fmt.Sprintf(` ORDER BY is_active DESC, role, username LIMIT $%d OFFSET $%d`, len(args)-1, len(args)), args...)
	if err != nil {
		log.Printf("users list: query: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not load users", "internal_error"))
		return
	}
	defer rows.Close()
	users := []models.User{}
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			log.Printf("users list: scan: %v", err)
			c.JSON(http.StatusInternalServerError, models.Fail("Could not load users", "internal_error"))
			return
		}
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		log.Printf("users list: rows: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not load users", "internal_error"))
		return
	}
	c.JSON(http.StatusOK, models.PaginatedResponse{
		Success: true, Message: "OK", Data: users,
		Meta: models.MetaData{CurrentPage: page, PerPage: perPage, Total: total, TotalPages: (total + perPage - 1) / perPage},
	})
}

// Create adds a staff account.
func (h *UsersHandler) Create(c *gin.Context) {
	var req models.CreateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.Fail("Invalid request body", "invalid_request"))
		return
	}
	username, ok := normaliseUsername(req.Username)
	if !ok {
		c.JSON(http.StatusBadRequest, models.Fail("Username must be 3–50 characters of a-z, 0-9, dot, dash or underscore", "invalid_username"))
		return
	}
	email, ok := normaliseEmail(req.Email)
	if !ok {
		c.JSON(http.StatusBadRequest, models.Fail("Enter a valid email address or leave it blank", "invalid_email"))
		return
	}
	if code := checkPassword(req.Password); code != "" {
		c.JSON(http.StatusBadRequest, models.Fail(passwordMessage(code), code))
		return
	}
	if !util.ValidRole(req.Role) {
		c.JSON(http.StatusBadRequest, models.Fail("Role must be admin or counter", "invalid_role"))
		return
	}
	first, last := strings.TrimSpace(req.FirstName), strings.TrimSpace(req.LastName)
	if first == "" || !validName(first) || !validName(last) {
		c.JSON(http.StatusBadRequest, models.Fail("First name is required; names are at most 50 characters", "invalid_name"))
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		log.Printf("users create: hash: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not create the user", "internal_error"))
		return
	}
	u, err := scanUser(h.db.QueryRow(`INSERT INTO users (username, email, password_hash, first_name, last_name, role)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING `+userColumns, username, email, string(hash), first, last, req.Role))
	switch {
	case util.IsUniqueViolation(err, "uniq_users_username_lower"):
		c.JSON(http.StatusConflict, models.Fail("That username is already taken", "username_taken"))
		return
	case util.IsUniqueViolation(err, "uniq_users_email_lower"):
		c.JSON(http.StatusConflict, models.Fail("That email is already used by another account", "email_taken"))
		return
	case err != nil:
		log.Printf("users create: insert: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not create the user", "internal_error"))
		return
	}
	c.JSON(http.StatusCreated, models.OK("User created", u))
}

// Update changes profile, role, active flag or password. Security-relevant
// changes (password, role, is_active) revoke the user's sessions. Demoting
// to counter also clears the PIN. An admin cannot demote or deactivate
// themself, and the last active admin can never be demoted or deactivated.
func (h *UsersHandler) Update(c *gin.Context) {
	actorID, _, _, ok := middleware.UserFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, models.Fail("Not signed in", "auth_required"))
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, models.Fail("User not found", "user_not_found"))
		return
	}
	var req models.UpdateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.Fail("Invalid request body", "invalid_request"))
		return
	}
	sets := []string{}
	args := []any{}
	add := func(col string, v any) {
		args = append(args, v)
		sets = append(sets, fmt.Sprintf("%s = $%d", col, len(args)))
	}
	if req.Email != nil {
		e, ok := normaliseEmail(*req.Email)
		if !ok {
			c.JSON(http.StatusBadRequest, models.Fail("Enter a valid email address or leave it blank", "invalid_email"))
			return
		}
		if e == nil {
			sets = append(sets, "email = NULL")
		} else {
			add("email", *e)
		}
	}
	if req.FirstName != nil {
		v := strings.TrimSpace(*req.FirstName)
		if v == "" || !validName(v) {
			c.JSON(http.StatusBadRequest, models.Fail("First name is required; names are at most 50 characters", "invalid_name"))
			return
		}
		add("first_name", v)
	}
	if req.LastName != nil {
		v := strings.TrimSpace(*req.LastName)
		if !validName(v) {
			c.JSON(http.StatusBadRequest, models.Fail("Names are at most 50 characters", "invalid_name"))
			return
		}
		add("last_name", v)
	}
	if req.Password != nil {
		if code := checkPassword(*req.Password); code != "" {
			c.JSON(http.StatusBadRequest, models.Fail(passwordMessage(code), code))
			return
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(*req.Password), bcrypt.DefaultCost)
		if err != nil {
			log.Printf("users update: hash: %v", err)
			c.JSON(http.StatusInternalServerError, models.Fail("Could not update the user", "internal_error"))
			return
		}
		add("password_hash", string(hash))
	}
	demoting := req.Role != nil && *req.Role != util.RoleAdmin
	if req.Role != nil {
		if !util.ValidRole(*req.Role) {
			c.JSON(http.StatusBadRequest, models.Fail("Role must be admin or counter", "invalid_role"))
			return
		}
		add("role", *req.Role)
		if demoting {
			sets = append(sets, "pin_hash = NULL")
		}
	}
	deactivating := req.IsActive != nil && !*req.IsActive
	if req.IsActive != nil {
		add("is_active", *req.IsActive)
	}
	if len(sets) == 0 {
		c.JSON(http.StatusBadRequest, models.Fail("Nothing to update", "no_changes"))
		return
	}
	if id == actorID && (demoting || deactivating) {
		c.JSON(http.StatusBadRequest, models.Fail("You cannot demote or deactivate your own account", "cannot_modify_self"))
		return
	}

	tx, err := h.db.Begin()
	if err != nil {
		log.Printf("users update: begin: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not update the user", "internal_error"))
		return
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`SELECT pg_advisory_xact_lock($1)`, adminMutationLockKey); err != nil {
		log.Printf("users update: advisory lock: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not update the user", "internal_error"))
		return
	}
	var curRole string
	var curActive bool
	err = tx.QueryRow(`SELECT role, is_active FROM users WHERE id = $1 FOR UPDATE`, id).Scan(&curRole, &curActive)
	if errors.Is(err, sql.ErrNoRows) {
		c.JSON(http.StatusNotFound, models.Fail("User not found", "user_not_found"))
		return
	}
	if err != nil {
		log.Printf("users update: lock: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not update the user", "internal_error"))
		return
	}
	// Revoke only on a change that actually matters for a live session: a
	// new password, or a role/active flip from what's currently stored. The
	// frontend always sends role and is_active on an edit, so comparing
	// against the request alone would revoke on every profile tweak
	// (including an admin editing their own row, which would then bounce
	// them to /login).
	revoke := req.Password != nil ||
		(req.Role != nil && *req.Role != curRole) ||
		(req.IsActive != nil && *req.IsActive != curActive)
	if curRole == util.RoleAdmin && curActive && (demoting || deactivating) {
		var others int
		if err := tx.QueryRow(`SELECT COUNT(*) FROM users WHERE role = 'admin' AND is_active = true AND id <> $1`, id).Scan(&others); err != nil {
			log.Printf("users update: count admins: %v", err)
			c.JSON(http.StatusInternalServerError, models.Fail("Could not update the user", "internal_error"))
			return
		}
		if others == 0 {
			c.JSON(http.StatusConflict, models.Fail("This is the only active admin. Add another admin first.", "last_admin"))
			return
		}
	}
	sets = append(sets, "updated_at = now()")
	if revoke {
		sets = append(sets, "token_revoked_at = now()")
	}
	args = append(args, id)
	u, err := scanUser(tx.QueryRow(fmt.Sprintf(`UPDATE users SET %s WHERE id = $%d RETURNING `+userColumns, strings.Join(sets, ", "), len(args)), args...))
	if util.IsUniqueViolation(err, "uniq_users_email_lower") {
		c.JSON(http.StatusConflict, models.Fail("That email is already used by another account", "email_taken"))
		return
	}
	if err != nil {
		log.Printf("users update: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not update the user", "internal_error"))
		return
	}
	if err := tx.Commit(); err != nil {
		log.Printf("users update: commit: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not update the user", "internal_error"))
		return
	}
	c.JSON(http.StatusOK, models.OK("User updated", u))
}

// SetPin sets an active admin's 4-digit PIN. PINs must be unique across
// admins because Identify resolves a PIN to one person.
func (h *UsersHandler) SetPin(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusNotFound, models.Fail("User not found", "user_not_found"))
		return
	}
	var req models.SetPinRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.Fail("Invalid request body", "invalid_request"))
		return
	}
	if !validPin(req.Pin) {
		c.JSON(http.StatusBadRequest, models.Fail("PIN must be exactly 4 digits", "invalid_pin_format"))
		return
	}
	tx, err := h.db.Begin()
	if err != nil {
		log.Printf("users pin: begin: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not set the PIN", "internal_error"))
		return
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`SELECT pg_advisory_xact_lock($1)`, adminMutationLockKey); err != nil {
		log.Printf("users pin: advisory lock: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not set the PIN", "internal_error"))
		return
	}
	var role string
	var active bool
	err = tx.QueryRow(`SELECT role, is_active FROM users WHERE id = $1`, id).Scan(&role, &active)
	if errors.Is(err, sql.ErrNoRows) {
		c.JSON(http.StatusNotFound, models.Fail("User not found", "user_not_found"))
		return
	}
	if err != nil {
		log.Printf("users pin: lookup: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not set the PIN", "internal_error"))
		return
	}
	if role != util.RoleAdmin || !active {
		c.JSON(http.StatusBadRequest, models.Fail("Only active admins can hold a PIN", "pin_not_allowed_for_role"))
		return
	}
	existing, err := staffpin.Identify(tx, req.Pin, staffpin.AdminOnly)
	if err == nil && existing.UserID != id {
		c.JSON(http.StatusConflict, models.Fail("Another admin already uses this PIN", "pin_in_use"))
		return
	}
	if err != nil && !errors.Is(err, staffpin.ErrNoMatch) {
		log.Printf("users pin: identify: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not set the PIN", "internal_error"))
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.Pin), bcrypt.DefaultCost)
	if err != nil {
		log.Printf("users pin: hash: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not set the PIN", "internal_error"))
		return
	}
	if _, err := tx.Exec(`UPDATE users SET pin_hash = $1, updated_at = now() WHERE id = $2`, string(hash), id); err != nil {
		log.Printf("users pin: update: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not set the PIN", "internal_error"))
		return
	}
	if err := tx.Commit(); err != nil {
		log.Printf("users pin: commit: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not set the PIN", "internal_error"))
		return
	}
	c.JSON(http.StatusOK, models.OK("PIN set", nil))
}
