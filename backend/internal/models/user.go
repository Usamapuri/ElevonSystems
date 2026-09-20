package models

import (
	"time"

	"github.com/google/uuid"
)

// User is the public shape of a staff account. Never carries the hash.
type User struct {
	ID          uuid.UUID  `json:"id"`
	Username    string     `json:"username"`
	Email       *string    `json:"email"`
	FirstName   string     `json:"first_name"`
	LastName    string     `json:"last_name"`
	Role        string     `json:"role"`
	IsActive    bool       `json:"is_active"`
	HasPin      bool       `json:"has_pin"`
	LastLoginAt *time.Time `json:"last_login_at"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// LoginRequest accepts a username or an email in the username field.
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// LoginResponse is the payload of a successful login.
type LoginResponse struct {
	Token string `json:"token"`
	User  User   `json:"user"`
}

// ForgotPasswordRequest starts a reset by email.
type ForgotPasswordRequest struct {
	Email string `json:"email"`
}

// ResetPasswordRequest completes a reset with the emailed token.
type ResetPasswordRequest struct {
	Token       string `json:"token"`
	NewPassword string `json:"new_password"`
}

// ChangePasswordRequest rotates the caller's own password.
type ChangePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}
