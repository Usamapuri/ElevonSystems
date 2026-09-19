// Package middleware holds Gin middleware: JWT auth and role gates.
package middleware

import (
	"database/sql"
	"errors"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"elevon-backend/internal/models"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

// jwtSecret is per store and mandatory in release mode: a shared default would
// let a token minted on one store validate on another.
var jwtSecret = func() []byte {
	if s := os.Getenv("JWT_SECRET"); s != "" {
		return []byte(s)
	}
	if os.Getenv("GIN_MODE") == "release" {
		log.Fatal("JWT_SECRET must be set when GIN_MODE=release (generate one with: openssl rand -base64 48)")
	}
	log.Println("WARNING: JWT_SECRET unset — using dev fallback. Never run like this in production.")
	return []byte("dev-only-insecure-secret")
}()

// FallbackJWTHeader duplicates the JWT for proxies that strip Authorization.
const FallbackJWTHeader = "X-POS-JWT"

// Claims are the JWT claims for a staff session.
type Claims struct {
	UserID   uuid.UUID `json:"user_id"`
	Username string    `json:"username"`
	Role     string    `json:"role"`
	jwt.RegisteredClaims
}

// GenerateToken mints a 24-hour HS256 token. IssuedAt is always set because
// revocation compares against it.
func GenerateToken(userID uuid.UUID, username, role string) (string, error) {
	now := time.Now()
	claims := &Claims{
		UserID:   userID,
		Username: username,
		Role:     role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(now),
			Issuer:    "elevon-pos",
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(jwtSecret)
}

// ValidateToken parses and verifies a token.
func ValidateToken(tokenString string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, jwt.ErrSignatureInvalid
		}
		return jwtSecret, nil
	})
	if err != nil {
		return nil, err
	}
	if claims, ok := token.Claims.(*Claims); ok && token.Valid {
		return claims, nil
	}
	return nil, jwt.ErrInvalidKey
}

var (
	// ErrTokenRevoked: users.token_revoked_at is at or after the token's iat.
	ErrTokenRevoked = errors.New("token_revoked")
	// ErrUserInactive: user missing or deactivated (same error so a deleted
	// user cannot be told apart from a deactivated one).
	ErrUserInactive = errors.New("user_inactive")
)

// CheckTokenNotRevoked verifies a validated token against the live user row.
// One PK query per request. Fails closed on DB error. db may be nil in tests.
func CheckTokenNotRevoked(db *sql.DB, userID uuid.UUID, issuedAt time.Time) error {
	if db == nil {
		return nil
	}
	var revokedAt sql.NullTime
	var isActive bool
	err := db.QueryRow(`SELECT token_revoked_at, is_active FROM users WHERE id = $1`, userID).Scan(&revokedAt, &isActive)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrUserInactive
	}
	if err != nil {
		return err
	}
	if !isActive {
		return ErrUserInactive
	}
	// iat is whole-second; reject unless strictly after the revoke instant.
	if revokedAt.Valid && !issuedAt.After(revokedAt.Time) {
		return ErrTokenRevoked
	}
	return nil
}

// AuthMiddleware authenticates every request except CORS preflight.
func AuthMiddleware(db *sql.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method == http.MethodOptions {
			c.Next()
			return
		}
		header := strings.TrimSpace(c.GetHeader("Authorization"))
		if header == "" {
			if alt := strings.TrimSpace(c.GetHeader(FallbackJWTHeader)); alt != "" {
				header = "Bearer " + alt
			}
		}
		if header == "" {
			abort(c, http.StatusUnauthorized, "Authorization header is required", "missing_auth_header")
			return
		}
		if !strings.HasPrefix(header, "Bearer ") {
			abort(c, http.StatusUnauthorized, "Invalid authorization header format", "invalid_auth_format")
			return
		}
		claims, err := ValidateToken(strings.TrimPrefix(header, "Bearer "))
		if err != nil || claims.IssuedAt == nil {
			abort(c, http.StatusUnauthorized, "Invalid or expired token", "invalid_token")
			return
		}
		if err := CheckTokenNotRevoked(db, claims.UserID, claims.IssuedAt.Time); err != nil {
			switch {
			case errors.Is(err, ErrTokenRevoked):
				abort(c, http.StatusUnauthorized, "Session ended. Please sign in again.", "token_revoked")
			case errors.Is(err, ErrUserInactive):
				abort(c, http.StatusUnauthorized, "Account is not active.", "user_inactive")
			default:
				log.Printf("auth: revocation check failed for %s: %v", claims.UserID, err)
				abort(c, http.StatusUnauthorized, "Authentication check failed", "auth_check_failed")
			}
			return
		}
		c.Set("user_id", claims.UserID)
		c.Set("username", claims.Username)
		c.Set("role", claims.Role)
		c.Next()
	}
}

// RequireRoles allows the request only when the caller's role is listed.
func RequireRoles(roles []string) gin.HandlerFunc {
	return func(c *gin.Context) {
		role, ok := c.Get("role")
		if !ok {
			abort(c, http.StatusForbidden, "Role information not found", "missing_role")
			return
		}
		r, _ := role.(string)
		for _, allowed := range roles {
			if r == allowed {
				c.Next()
				return
			}
		}
		abort(c, http.StatusForbidden, "Insufficient permissions", "insufficient_permissions")
	}
}

// UserFromContext returns the authenticated user's id, username and role.
func UserFromContext(c *gin.Context) (uuid.UUID, string, string, bool) {
	id, ok1 := c.Get("user_id")
	name, ok2 := c.Get("username")
	role, ok3 := c.Get("role")
	if !ok1 || !ok2 || !ok3 {
		return uuid.Nil, "", "", false
	}
	uid, a := id.(uuid.UUID)
	n, b := name.(string)
	r, d := role.(string)
	if !a || !b || !d {
		return uuid.Nil, "", "", false
	}
	return uid, n, r, true
}

func abort(c *gin.Context, status int, message, code string) {
	c.JSON(status, models.Fail(message, code))
	c.Abort()
}
