// Package handlers holds HTTP handlers. Each handler returns models.APIResponse.
package handlers

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"elevon-backend/internal/middleware"
	"elevon-backend/internal/models"
	"elevon-backend/internal/ratelimit"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

// ResetMailer delivers the password-reset link. Nil is allowed: the URL is
// then only logged (dev, tests).
type ResetMailer interface {
	SendPasswordReset(ctx context.Context, toEmail, firstName, resetURL, businessName string) error
}

const (
	// /auth/login throttle keyed by (identifier, client IP). Only failures
	// consume the bucket (spec §6.1: 10 failures per 15 min).
	loginMaxFailures = 10
	loginWindow      = 15 * time.Minute
	// /auth/forgot-password throttle: every request counts, checked before
	// the DB lookup so timing cannot reveal which emails exist.
	forgotMaxRequests = 5
	forgotWindow      = 5 * time.Minute
)

// AuthHandler serves login, the current-user lookup and the password flows.
type AuthHandler struct {
	db       *sql.DB
	mailer   ResetMailer
	loginRL  *ratelimit.Window
	forgotRL *ratelimit.Window
}

// NewAuthHandler builds an AuthHandler. mailer may be nil.
func NewAuthHandler(db *sql.DB, mailer ResetMailer) *AuthHandler {
	return &AuthHandler{
		db:       db,
		mailer:   mailer,
		loginRL:  ratelimit.New(loginMaxFailures, loginWindow),
		forgotRL: ratelimit.New(forgotMaxRequests, forgotWindow),
	}
}

const userColumns = `id, username, email, first_name, last_name, role, is_active,
	(pin_hash IS NOT NULL) AS has_pin, last_login_at, created_at, updated_at`

func scanUser(row interface{ Scan(dest ...interface{}) error }) (models.User, error) {
	var u models.User
	err := row.Scan(&u.ID, &u.Username, &u.Email, &u.FirstName, &u.LastName, &u.Role, &u.IsActive,
		&u.HasPin, &u.LastLoginAt, &u.CreatedAt, &u.UpdatedAt)
	return u, err
}

// Login accepts a username or email plus password and returns a JWT. Failed
// attempts are throttled per identifier+IP; the same key is used for
// "no such user" and "wrong password" so the limiter is not an oracle.
func (h *AuthHandler) Login(c *gin.Context) {
	var req models.LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.Fail("Username and password are required", "missing_credentials"))
		return
	}
	ident := strings.ToLower(strings.TrimSpace(req.Username))
	if ident == "" || req.Password == "" {
		c.JSON(http.StatusBadRequest, models.Fail("Username and password are required", "missing_credentials"))
		return
	}
	rlKey := ident + "|" + c.ClientIP()
	if !h.loginRL.Check(rlKey) {
		c.JSON(http.StatusTooManyRequests, models.Fail("Too many failed sign-in attempts. Wait a few minutes and try again.", "rate_limited"))
		return
	}
	var hash string
	row := h.db.QueryRow(`SELECT `+userColumns+`, password_hash FROM users
		WHERE (lower(username) = $1 OR lower(email) = $1) AND is_active = true`, ident)
	var u models.User
	err := row.Scan(&u.ID, &u.Username, &u.Email, &u.FirstName, &u.LastName, &u.Role, &u.IsActive,
		&u.HasPin, &u.LastLoginAt, &u.CreatedAt, &u.UpdatedAt, &hash)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.Password)) != nil) {
		h.loginRL.Record(rlKey)
		c.JSON(http.StatusUnauthorized, models.Fail("Invalid username or password", "invalid_credentials"))
		return
	}
	if err != nil {
		log.Printf("login: query: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not sign in right now", "internal_error"))
		return
	}
	token, err := middleware.GenerateToken(u.ID, u.Username, u.Role)
	if err != nil {
		log.Printf("login: token: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not sign in right now", "internal_error"))
		return
	}
	if _, err := h.db.Exec(`UPDATE users SET last_login_at = now() WHERE id = $1`, u.ID); err != nil {
		log.Printf("login: last_login_at: %v", err) // best effort
	}
	c.JSON(http.StatusOK, models.OK("Signed in", models.LoginResponse{Token: token, User: u}))
}

// Me returns the authenticated user's record.
func (h *AuthHandler) Me(c *gin.Context) {
	id, _, _, ok := middleware.UserFromContext(c)
	if !ok || id == uuid.Nil {
		c.JSON(http.StatusUnauthorized, models.Fail("Not signed in", "auth_required"))
		return
	}
	u, err := scanUser(h.db.QueryRow(`SELECT `+userColumns+` FROM users WHERE id = $1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		c.JSON(http.StatusUnauthorized, models.Fail("Account not found", "user_inactive"))
		return
	}
	if err != nil {
		log.Printf("me: query: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not load your account", "internal_error"))
		return
	}
	c.JSON(http.StatusOK, models.OK("OK", u))
}
