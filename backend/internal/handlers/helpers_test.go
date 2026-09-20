package handlers

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"elevon-backend/internal/models"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

func init() { gin.SetMode(gin.TestMode) }

type actor struct {
	id       uuid.UUID
	username string
	role     string
}

// asActor stands in for AuthMiddleware so handler tests exercise handlers,
// not JWT parsing (the middleware has its own tests).
func asActor(a actor) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Set("user_id", a.id)
		c.Set("username", a.username)
		c.Set("role", a.role)
		c.Next()
	}
}

// seedUser inserts an active user with a MinCost hash and returns its id.
// email nil → NULL.
func seedUser(t *testing.T, db *sql.DB, username, role, password string, email *string) uuid.UUID {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	var id uuid.UUID
	if err := db.QueryRow(`INSERT INTO users (username, email, password_hash, first_name, last_name, role)
		VALUES ($1, $2, $3, 'Test', $1, $4) RETURNING id`, username, email, string(hash), role).Scan(&id); err != nil {
		t.Fatalf("seed %s: %v", username, err)
	}
	return id
}

func strp(s string) *string { return &s }

func doJSON(r http.Handler, method, path string, body any) *httptest.ResponseRecorder {
	var buf bytes.Buffer
	if body != nil {
		_ = json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func decodeEnvelope(t *testing.T, w *httptest.ResponseRecorder) models.APIResponse {
	t.Helper()
	var env models.APIResponse
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode %q: %v", w.Body.String(), err)
	}
	return env
}

func errCode(env models.APIResponse) string {
	if env.Error == nil {
		return ""
	}
	return *env.Error
}

// dataAs re-decodes env.Data into out (Data is interface{} after decoding).
func dataAs(t *testing.T, env models.APIResponse, out any) {
	t.Helper()
	raw, _ := json.Marshal(env.Data)
	if err := json.Unmarshal(raw, out); err != nil {
		t.Fatalf("data: %v", err)
	}
}

// jsonUnmarshal decodes a PaginatedResponse body (Data stays interface{}).
func jsonUnmarshal(b []byte, out any) error { return json.Unmarshal(b, out) }
