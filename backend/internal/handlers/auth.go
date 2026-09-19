// Package handlers holds HTTP handlers. Each handler returns models.APIResponse.
package handlers

import (
	"database/sql"
	"errors"
	"log"
	"net/http"
	"strings"

	"elevon-backend/internal/middleware"
	"elevon-backend/internal/models"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

// AuthHandler serves login and the current-user lookup.
type AuthHandler struct{ db *sql.DB }

// NewAuthHandler builds an AuthHandler.
func NewAuthHandler(db *sql.DB) *AuthHandler { return &AuthHandler{db: db} }

const userColumns = `id, username, email, first_name, last_name, role, is_active,
	(pin_hash IS NOT NULL) AS has_pin, last_login_at, created_at, updated_at`

func scanUser(row interface{ Scan(dest ...interface{}) error }) (models.User, error) {
	var u models.User
	err := row.Scan(&u.ID, &u.Username, &u.Email, &u.FirstName, &u.LastName, &u.Role, &u.IsActive,
		&u.HasPin, &u.LastLoginAt, &u.CreatedAt, &u.UpdatedAt)
	return u, err
}

// Login accepts a username or email plus password and returns a JWT.
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
	var hash string
	row := h.db.QueryRow(`SELECT `+userColumns+`, password_hash FROM users
		WHERE (lower(username) = $1 OR lower(email) = $1) AND is_active = true`, ident)
	var u models.User
	err := row.Scan(&u.ID, &u.Username, &u.Email, &u.FirstName, &u.LastName, &u.Role, &u.IsActive,
		&u.HasPin, &u.LastLoginAt, &u.CreatedAt, &u.UpdatedAt, &hash)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && bcrypt.CompareHashAndPassword([]byte(hash), []byte(req.Password)) != nil) {
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
