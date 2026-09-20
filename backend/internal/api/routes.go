// Package api registers every HTTP route. Every route except POST
// /auth/login, POST /auth/forgot-password and POST /auth/reset-password is
// registered on a group that carries middleware.RequireRoles — pinned by
// routes_role_gates_contract_test.go.
package api

import (
	"database/sql"

	"elevon-backend/internal/email"
	"elevon-backend/internal/handlers"
	"elevon-backend/internal/middleware"
	"elevon-backend/internal/util"

	"github.com/gin-gonic/gin"
)

// SetupRoutes mounts the API under r (normally /api/v1).
func SetupRoutes(r *gin.RouterGroup, db *sql.DB, auth gin.HandlerFunc) {
	authH := handlers.NewAuthHandler(db, email.NewClientFromEnv())

	// Unauthenticated: sign-in and the two halves of the password reset.
	public := r.Group("/auth")
	public.POST("/login", authH.Login)
	public.POST("/forgot-password", authH.ForgotPassword)
	public.POST("/reset-password", authH.ResetPassword)

	// Any signed-in staff member.
	staff := r.Group("", auth, middleware.RequireRoles(util.AllRoles))
	staff.GET("/auth/me", authH.Me)
	staff.POST("/auth/change-password", authH.ChangePassword)

	// Admin only.
	admin := r.Group("/admin", auth, middleware.RequireRoles([]string{util.RoleAdmin}))
	usersH := handlers.NewUsersHandler(db)
	admin.GET("/users", usersH.List)
	admin.POST("/users", usersH.Create)
	admin.PUT("/users/:id", usersH.Update)
	admin.PUT("/users/:id/pin", usersH.SetPin)

	settingsH := handlers.NewSettingsHandler(db)
	staff.GET("/settings", settingsH.GetAll)
	admin.PUT("/settings", settingsH.Update)

	productsH := handlers.NewProductsHandler(db)
	staff.GET("/products", productsH.List)
	admin.POST("/products", productsH.Create)
	admin.PUT("/products/:id", productsH.Update)
	admin.PUT("/rates", productsH.UpdateRates)
	admin.GET("/rates/history", productsH.RateHistory)

	dayH := handlers.NewDayHandler(db)
	staff.GET("/day/current", dayH.Current)
	staff.POST("/day/open", dayH.Open)
	staff.POST("/day/movements", dayH.AddMovement)
	staff.POST("/day/close", dayH.Close)
	staff.GET("/day/:id/z", dayH.ZReport)
	admin.POST("/day/reopen", dayH.Reopen)
	admin.POST("/day/force-close", dayH.ForceClose)
	admin.GET("/day/history", dayH.History)

	customersH := handlers.NewCustomersHandler(db)
	staff.GET("/customers", customersH.List)
	staff.GET("/customers/:id", customersH.Get)
	staff.GET("/customers/:id/statement", customersH.Statement)
	admin.POST("/customers", customersH.Create)
	admin.PUT("/customers/:id", customersH.Update)

	// Receipts are staff work: taking money against an account happens at the
	// counter. The void needs an admin PIN in the body, which is the
	// authority — not the route's role (spec §6.5).
	staff.GET("/customers/:id/receipts", customersH.ListReceipts)
	staff.POST("/customers/:id/receipts", customersH.CreateReceipt)
	staff.POST("/customers/:id/receipts/:rid/void", customersH.VoidReceipt)

	// The money core. Voids are on staff for the same reason: the PIN in the
	// body identifies the admin who authorised it (spec §6.4).
	invoicesH := handlers.NewInvoicesHandler(db)
	staff.POST("/invoices", invoicesH.Create)
	staff.GET("/invoices", invoicesH.List)
	staff.GET("/invoices/recent", invoicesH.Recent)
	staff.GET("/invoices/:id", invoicesH.Get)
	staff.POST("/invoices/:id/void", invoicesH.Void)
}
