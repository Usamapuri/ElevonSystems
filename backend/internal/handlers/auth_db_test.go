package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"elevon-backend/internal/middleware"
	"elevon-backend/internal/models"
	"elevon-backend/internal/testdb"

	"github.com/gin-gonic/gin"
)

func TestLogin_RateLimitsAfterTenFailures(t *testing.T) {
	db := testdb.Fresh(t)
	seedUser(t, db, "cashier", "counter", "right-password", nil)
	r := gin.New()
	r.POST("/auth/login", NewAuthHandler(db, nil).Login)
	for i := 0; i < 10; i++ {
		if w := doJSON(r, http.MethodPost, "/auth/login", models.LoginRequest{Username: "cashier", Password: "wrong"}); w.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: got %d", i+1, w.Code)
		}
	}
	w := doJSON(r, http.MethodPost, "/auth/login", models.LoginRequest{Username: "cashier", Password: "right-password"})
	if w.Code != http.StatusTooManyRequests || errCode(decodeEnvelope(t, w)) != "rate_limited" {
		t.Fatalf("expected 429 rate_limited, got %d %s", w.Code, w.Body.String())
	}
}

func TestLogin_SuccessDoesNotConsumeBucket(t *testing.T) {
	db := testdb.Fresh(t)
	seedUser(t, db, "cashier", "counter", "right-password", nil)
	r := gin.New()
	r.POST("/auth/login", NewAuthHandler(db, nil).Login)
	for i := 0; i < 9; i++ {
		doJSON(r, http.MethodPost, "/auth/login", models.LoginRequest{Username: "cashier", Password: "wrong"})
	}
	if w := doJSON(r, http.MethodPost, "/auth/login", models.LoginRequest{Username: "cashier", Password: "right-password"}); w.Code != http.StatusOK {
		t.Fatalf("9 failures then success must sign in: %d %s", w.Code, w.Body.String())
	}
	if w := doJSON(r, http.MethodPost, "/auth/login", models.LoginRequest{Username: "cashier", Password: "wrong"}); w.Code != http.StatusUnauthorized {
		t.Fatalf("10th failure is still under the limit: %d", w.Code)
	}
}

type captureMailer struct{ urls chan string }

func (m *captureMailer) SendPasswordReset(_ context.Context, _, _, resetURL, _ string) error {
	m.urls <- resetURL
	return nil
}

func passwordRouter(h *AuthHandler, staff actor) *gin.Engine {
	r := gin.New()
	r.POST("/auth/login", h.Login)
	r.POST("/auth/forgot-password", h.ForgotPassword)
	r.POST("/auth/reset-password", h.ResetPassword)
	r.POST("/auth/change-password", asActor(staff), h.ChangePassword)
	return r
}

func TestForgotThenReset_RoundTrip(t *testing.T) {
	db := testdb.Fresh(t)
	t.Setenv("APP_URL", "https://pos.example.com/")
	id := seedUser(t, db, "owner", "admin", "old-password-1", strp("Owner@Example.com"))
	mail := &captureMailer{urls: make(chan string, 1)}
	h := NewAuthHandler(db, mail)
	r := passwordRouter(h, actor{})

	if w := doJSON(r, http.MethodPost, "/auth/forgot-password", models.ForgotPasswordRequest{Email: "owner@example.com"}); w.Code != http.StatusOK {
		t.Fatalf("forgot: %d %s", w.Code, w.Body.String())
	}
	var link string
	select {
	case link = <-mail.urls:
	case <-time.After(5 * time.Second):
		t.Fatal("no reset email was sent")
	}
	if !strings.HasPrefix(link, "https://pos.example.com/reset-password?token=") {
		t.Fatalf("unexpected link %q", link)
	}
	u, _ := url.Parse(link)
	token := u.Query().Get("token")

	if w := doJSON(r, http.MethodPost, "/auth/reset-password", models.ResetPasswordRequest{Token: token, NewPassword: "short"}); errCode(decodeEnvelope(t, w)) != "weak_password" {
		t.Fatalf("policy must apply on reset: %s", w.Body.String())
	}
	if w := doJSON(r, http.MethodPost, "/auth/reset-password", models.ResetPasswordRequest{Token: token, NewPassword: "new-password-1"}); w.Code != http.StatusOK {
		t.Fatalf("reset: %d %s", w.Code, w.Body.String())
	}
	if w := doJSON(r, http.MethodPost, "/auth/reset-password", models.ResetPasswordRequest{Token: token, NewPassword: "new-password-2"}); errCode(decodeEnvelope(t, w)) != "invalid_or_expired_token" {
		t.Fatalf("token must be single-use: %s", w.Body.String())
	}
	if w := doJSON(r, http.MethodPost, "/auth/login", models.LoginRequest{Username: "owner", Password: "new-password-1"}); w.Code != http.StatusOK {
		t.Fatalf("login with new password: %d %s", w.Code, w.Body.String())
	}
	// A token issued before the reset is revoked (iat is whole-second, so look 2s back).
	if err := middleware.CheckTokenNotRevoked(db, id, time.Now().Add(-2*time.Second)); !errors.Is(err, middleware.ErrTokenRevoked) {
		t.Fatalf("old sessions must be revoked, got %v", err)
	}
}

func TestForgotPassword_UnknownEmailIsSilentAndThrottled(t *testing.T) {
	db := testdb.Fresh(t)
	mail := &captureMailer{urls: make(chan string, 1)}
	r := passwordRouter(NewAuthHandler(db, mail), actor{})
	for i := 0; i < 5; i++ {
		if w := doJSON(r, http.MethodPost, "/auth/forgot-password", models.ForgotPasswordRequest{Email: "nobody@example.com"}); w.Code != http.StatusOK {
			t.Fatalf("unknown email must look like success: %d", w.Code)
		}
	}
	if w := doJSON(r, http.MethodPost, "/auth/forgot-password", models.ForgotPasswordRequest{Email: "nobody@example.com"}); w.Code != http.StatusTooManyRequests {
		t.Fatalf("6th request in 5 min must be 429, got %d", w.Code)
	}
	select {
	case <-mail.urls:
		t.Fatal("no mail may be sent for an unknown address")
	default:
	}
}

func TestChangePassword_RequiresCurrentAndRevokes(t *testing.T) {
	db := testdb.Fresh(t)
	id := seedUser(t, db, "owner", "admin", "old-password-1", nil)
	r := passwordRouter(NewAuthHandler(db, nil), actor{id: id, username: "owner", role: "admin"})
	if w := doJSON(r, http.MethodPost, "/auth/change-password", models.ChangePasswordRequest{CurrentPassword: "nope", NewPassword: "new-password-1"}); w.Code != http.StatusBadRequest || errCode(decodeEnvelope(t, w)) != "invalid_current_password" {
		t.Fatalf("wrong current password: %d %s", w.Code, w.Body.String())
	}
	if w := doJSON(r, http.MethodPost, "/auth/change-password", models.ChangePasswordRequest{CurrentPassword: "old-password-1", NewPassword: "old-password-1"}); errCode(decodeEnvelope(t, w)) != "password_unchanged" {
		t.Fatalf("same password: %s", w.Body.String())
	}
	if w := doJSON(r, http.MethodPost, "/auth/change-password", models.ChangePasswordRequest{CurrentPassword: "old-password-1", NewPassword: "new-password-1"}); w.Code != http.StatusOK {
		t.Fatalf("change: %d %s", w.Code, w.Body.String())
	}
	if err := middleware.CheckTokenNotRevoked(db, id, time.Now().Add(-2*time.Second)); !errors.Is(err, middleware.ErrTokenRevoked) {
		t.Fatalf("old sessions must be revoked, got %v", err)
	}
	if w := doJSON(r, http.MethodPost, "/auth/login", models.LoginRequest{Username: "owner", Password: "new-password-1"}); w.Code != http.StatusOK {
		t.Fatalf("login with new password: %d", w.Code)
	}
}
