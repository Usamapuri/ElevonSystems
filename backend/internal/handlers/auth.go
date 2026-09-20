// Package handlers holds HTTP handlers. Each handler returns models.APIResponse.
package handlers

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"log"
	"net/http"
	"os"
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
	// /auth/forgot-password throttle: per email only. The backend sits
	// behind nginx with SetTrustedProxies(nil), so c.ClientIP() resolves to
	// the proxy for every request — an IP-keyed bucket would be one
	// store-wide budget, not a per-client one. An unknown email is a no-op
	// (no mail, no DB write beyond the lookup), so the bound that matters is
	// how many reset attempts a given address can absorb, checked before the
	// DB lookup so timing cannot reveal which emails exist.
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

// dummyHash is compared against on an unknown username/email so a lookup
// miss costs the same bcrypt work as a real password check — otherwise the
// response-time gap between "no such user" and "wrong password" is a
// timing oracle for username enumeration.
var dummyHash []byte

func init() {
	h, err := bcrypt.GenerateFromPassword([]byte("elevon-dummy"), bcrypt.DefaultCost)
	if err != nil {
		panic("auth: failed to generate dummy bcrypt hash: " + err.Error())
	}
	dummyHash = h
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
	notFound := errors.Is(err, sql.ErrNoRows)
	if err != nil && !notFound {
		log.Printf("login: query: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not sign in right now", "internal_error"))
		return
	}
	// Always compare against a bcrypt hash — the real one when the user
	// exists, a fixed dummy one otherwise — so both paths cost the same.
	compareHash := []byte(hash)
	if notFound {
		compareHash = dummyHash
	}
	passwordOK := bcrypt.CompareHashAndPassword(compareHash, []byte(req.Password)) == nil
	if notFound || !passwordOK {
		h.loginRL.Record(rlKey)
		c.JSON(http.StatusUnauthorized, models.Fail("Invalid username or password", "invalid_credentials"))
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

// resetTokenTTL is short enough that a leaked inbox item expires before most
// attackers notice, long enough for someone checking mail on a phone.
const resetTokenTTL = time.Hour

// ForgotPassword starts the reset flow. Always 200 with the same message,
// whether or not the email exists, so the login page cannot enumerate
// staff. Throttled per email before the lookup (see forgotMaxRequests).
func (h *AuthHandler) ForgotPassword(c *gin.Context) {
	var req models.ForgotPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, models.Fail("Email is required", "missing_email"))
		return
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if email == "" {
		c.JSON(http.StatusBadRequest, models.Fail("Email is required", "missing_email"))
		return
	}
	if !h.forgotRL.Allow("email:" + email) {
		c.JSON(http.StatusTooManyRequests, models.Fail("Too many reset requests. Try again in a few minutes.", "rate_limited"))
		return
	}
	generic := models.OK("If that email is registered, a reset link has been sent.", nil)

	var id uuid.UUID
	var firstName string
	err := h.db.QueryRow(`SELECT id, first_name FROM users WHERE lower(email) = $1 AND is_active = true`, email).Scan(&id, &firstName)
	if errors.Is(err, sql.ErrNoRows) {
		c.JSON(http.StatusOK, generic)
		return
	}
	if err != nil {
		log.Printf("forgot-password: lookup: %v", err)
		c.JSON(http.StatusOK, generic)
		return
	}
	token, err := generateResetToken()
	if err != nil {
		log.Printf("forgot-password: token: %v", err)
		c.JSON(http.StatusOK, generic)
		return
	}
	if _, err := h.db.Exec(`UPDATE users SET password_reset_token_hash = $1, password_reset_expires_at = $2, updated_at = now() WHERE id = $3`,
		hashResetToken(token), time.Now().Add(resetTokenTTL), id); err != nil {
		log.Printf("forgot-password: store token for %s: %v", id, err)
		c.JSON(http.StatusOK, generic)
		return
	}
	link := buildResetURL(token)
	if h.mailer == nil {
		log.Printf("forgot-password: no mailer configured; reset URL for %s: %s", email, link)
	} else {
		business := h.businessName()
		// Detached from the request so a slow mail API cannot reveal, by
		// response time, that the address exists.
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			if err := h.mailer.SendPasswordReset(ctx, email, firstName, link, business); err != nil {
				log.Printf("forgot-password: send to %s: %v", email, err)
			}
		}()
	}
	c.JSON(http.StatusOK, generic)
}

// ResetPassword completes the flow. The token hash is compared in constant
// time against every unexpired hash; success clears the token (single use)
// and revokes existing sessions.
func (h *AuthHandler) ResetPassword(c *gin.Context) {
	var req models.ResetPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Token) == "" {
		c.JSON(http.StatusBadRequest, models.Fail("Reset token is required", "missing_token"))
		return
	}
	if code := checkPassword(req.NewPassword); code != "" {
		c.JSON(http.StatusBadRequest, models.Fail(passwordMessage(code), code))
		return
	}
	want := []byte(hashResetToken(strings.TrimSpace(req.Token)))
	rows, err := h.db.Query(`SELECT id, password_reset_token_hash FROM users
		WHERE password_reset_token_hash IS NOT NULL AND password_reset_expires_at > now() AND is_active = true`)
	if err != nil {
		log.Printf("reset-password: query: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not reset the password right now", "internal_error"))
		return
	}
	defer rows.Close()
	var matched uuid.UUID
	found := false
	for rows.Next() {
		var id uuid.UUID
		var stored string
		if err := rows.Scan(&id, &stored); err != nil {
			continue
		}
		if subtle.ConstantTimeCompare([]byte(stored), want) == 1 && !found {
			matched, found = id, true
		}
	}
	if !found {
		c.JSON(http.StatusBadRequest, models.Fail("This reset link is invalid or has expired. Request a new one.", "invalid_or_expired_token"))
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		log.Printf("reset-password: hash: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not reset the password right now", "internal_error"))
		return
	}
	if _, err := h.db.Exec(`UPDATE users SET password_hash = $1, password_reset_token_hash = NULL, password_reset_expires_at = NULL,
		token_revoked_at = now(), updated_at = now() WHERE id = $2`, string(hash), matched); err != nil {
		log.Printf("reset-password: update %s: %v", matched, err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not reset the password right now", "internal_error"))
		return
	}
	c.JSON(http.StatusOK, models.OK("Password updated. Sign in with your new password.", nil))
}

// ChangePassword rotates the caller's own password. Needs the current
// password as well as a valid session, and revokes every other session.
func (h *AuthHandler) ChangePassword(c *gin.Context) {
	id, _, _, ok := middleware.UserFromContext(c)
	if !ok || id == uuid.Nil {
		c.JSON(http.StatusUnauthorized, models.Fail("Not signed in", "auth_required"))
		return
	}
	var req models.ChangePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.CurrentPassword == "" {
		c.JSON(http.StatusBadRequest, models.Fail("Current password is required", "missing_current_password"))
		return
	}
	if code := checkPassword(req.NewPassword); code != "" {
		c.JSON(http.StatusBadRequest, models.Fail(passwordMessage(code), code))
		return
	}
	if req.CurrentPassword == req.NewPassword {
		c.JSON(http.StatusBadRequest, models.Fail("New password must differ from the current one", "password_unchanged"))
		return
	}
	var current string
	err := h.db.QueryRow(`SELECT password_hash FROM users WHERE id = $1 AND is_active = true`, id).Scan(&current)
	if errors.Is(err, sql.ErrNoRows) {
		c.JSON(http.StatusUnauthorized, models.Fail("Account is not active.", "user_inactive"))
		return
	}
	if err != nil {
		log.Printf("change-password: query: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not change the password right now", "internal_error"))
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(current), []byte(req.CurrentPassword)) != nil {
		// 400, not 401: the session is valid — only the submitted current
		// password is wrong. A 401 here would hit the client's response
		// interceptor, which treats every 401 except missing_auth_header as
		// an expired session and force-logs the user out.
		c.JSON(http.StatusBadRequest, models.Fail("Current password is incorrect", "invalid_current_password"))
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		log.Printf("change-password: hash: %v", err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not change the password right now", "internal_error"))
		return
	}
	if _, err := h.db.Exec(`UPDATE users SET password_hash = $1, token_revoked_at = now(), updated_at = now() WHERE id = $2`, string(hash), id); err != nil {
		log.Printf("change-password: update %s: %v", id, err)
		c.JSON(http.StatusInternalServerError, models.Fail("Could not change the password right now", "internal_error"))
		return
	}
	c.JSON(http.StatusOK, models.OK("Password updated. Sign in again with the new password.", nil))
}

// businessName reads settings.business_name for the email subject.
func (h *AuthHandler) businessName() string {
	var name string
	if err := h.db.QueryRow(`SELECT value #>> '{}' FROM settings WHERE key = 'business_name'`).Scan(&name); err == nil && strings.TrimSpace(name) != "" {
		return name
	}
	return "Elevon POS"
}

// generateResetToken: 32 random bytes → 43 URL-safe chars (~256 bits).
func generateResetToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// hashResetToken: only sha256(token) is ever stored.
func hashResetToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// buildResetURL points at the frontend's reset page. APP_URL is the public
// frontend origin; read per call so tests can override it.
func buildResetURL(token string) string {
	base := strings.TrimRight(strings.TrimSpace(os.Getenv("APP_URL")), "/")
	if base == "" {
		base = "http://localhost:3000"
	}
	return base + "/reset-password?token=" + token
}
