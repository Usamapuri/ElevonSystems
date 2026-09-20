package handlers

import (
	"net/http"
	"testing"

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
