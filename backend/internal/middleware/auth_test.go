package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func TestGenerateAndValidateToken_RoundTrip(t *testing.T) {
	id := uuid.New()
	tok, err := GenerateToken(id, "owner", "admin")
	if err != nil {
		t.Fatal(err)
	}
	claims, err := ValidateToken(tok)
	if err != nil {
		t.Fatal(err)
	}
	if claims.UserID != id || claims.Username != "owner" || claims.Role != "admin" || claims.IssuedAt == nil {
		t.Fatalf("claims mismatch: %+v", claims)
	}
}

func TestValidateToken_RejectsGarbage(t *testing.T) {
	if _, err := ValidateToken("not-a-token"); err == nil {
		t.Fatal("garbage must not validate")
	}
}

func TestAuthMiddleware_MissingHeaderIs401WithCode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/x", AuthMiddleware(nil), func(c *gin.Context) { c.Status(200) })
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/x", nil))
	if w.Code != 401 || !strings.Contains(w.Body.String(), `"error":"missing_auth_header"`) {
		t.Fatalf("got %d %s", w.Code, w.Body.String())
	}
}

func TestAuthMiddleware_AcceptsFallbackHeader(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tok, _ := GenerateToken(uuid.New(), "till", "counter")
	r := gin.New()
	r.GET("/x", AuthMiddleware(nil), func(c *gin.Context) { c.String(200, c.GetString("role")) })
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set(FallbackJWTHeader, tok)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 200 || w.Body.String() != "counter" {
		t.Fatalf("got %d %s", w.Code, w.Body.String())
	}
}

func TestRequireRoles_ForbidsOtherRole(t *testing.T) {
	gin.SetMode(gin.TestMode)
	tok, _ := GenerateToken(uuid.New(), "till", "counter")
	r := gin.New()
	r.GET("/admin-only", AuthMiddleware(nil), RequireRoles([]string{"admin"}), func(c *gin.Context) { c.Status(200) })
	req := httptest.NewRequest(http.MethodGet, "/admin-only", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 403 || !strings.Contains(w.Body.String(), `"error":"insufficient_permissions"`) {
		t.Fatalf("got %d %s", w.Code, w.Body.String())
	}
}
