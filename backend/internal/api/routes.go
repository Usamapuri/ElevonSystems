// Package api registers every HTTP route. Every route except POST /auth/login
// is registered on a group that carries middleware.RequireRoles — pinned by
// routes_role_gates_contract_test.go.
package api

import (
	"database/sql"

	"elevon-backend/internal/handlers"
	"elevon-backend/internal/middleware"
	"elevon-backend/internal/util"

	"github.com/gin-gonic/gin"
)

// SetupRoutes mounts the API under r (normally /api/v1).
func SetupRoutes(r *gin.RouterGroup, db *sql.DB, auth gin.HandlerFunc) {
	authH := handlers.NewAuthHandler(db, nil)

	public := r.Group("/auth")
	public.POST("/login", authH.Login)

	// Any signed-in staff member.
	staff := r.Group("", auth, middleware.RequireRoles(util.AllRoles))
	staff.GET("/auth/me", authH.Me)

	// Admin only (populated from Phase 1).
	admin := r.Group("/admin", auth, middleware.RequireRoles([]string{util.RoleAdmin}))
	_ = admin
}
