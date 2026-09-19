package main

import (
	"database/sql"
	"log"
	"os"
	"strings"

	"elevon-backend/internal/database"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
)

func main() {
	if err := godotenv.Load(); err != nil {
		log.Println("no .env file, using environment")
	}

	db, err := openDB()
	if err != nil {
		log.Fatalf("database: %v", err)
	}
	defer db.Close()

	if err := database.Migrate(db); err != nil {
		log.Fatalf("migrations: %v", err)
	}
	database.EnsureInitialAdmin(db)

	gin.SetMode(getEnv("GIN_MODE", "release"))
	router := gin.New()
	// Default-deny proxy headers so c.ClientIP() is the TCP peer and IP-keyed
	// rate limits cannot be spoofed via X-Forwarded-For.
	if err := router.SetTrustedProxies(nil); err != nil {
		log.Fatalf("trusted proxies: %v", err)
	}
	router.Use(gin.Logger(), gin.Recovery())
	router.Use(cors.New(corsConfig()))

	router.GET("/health", func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "healthy", "message": "Elevon POS API is running"})
	})

	port := getEnv("PORT", "8080")
	log.Printf("listening on :%s", port)
	if err := router.Run(":" + port); err != nil {
		log.Fatalf("server: %v", err)
	}
}

func openDB() (*sql.DB, error) {
	if dsn := os.Getenv("DATABASE_URL"); dsn != "" {
		return database.OpenPostgres(dsn)
	}
	return database.Connect(database.Config{
		Host:     getEnv("DB_HOST", "postgres"),
		Port:     getEnv("DB_PORT", "5432"),
		User:     getEnv("DB_USER", "postgres"),
		Password: getEnv("DB_PASSWORD", "postgres123"),
		DBName:   getEnv("DB_NAME", "elevon_pos"),
		SSLMode:  getEnv("DB_SSLMODE", "disable"),
	})
}

func corsConfig() cors.Config {
	origins := strings.Split(getEnv("CORS_ORIGINS", "http://localhost:3000,http://127.0.0.1:3000"), ",")
	for i := range origins {
		origins[i] = strings.TrimSpace(origins[i])
	}
	return cors.Config{
		AllowOrigins:     origins,
		AllowMethods:     []string{"GET", "POST", "PUT", "DELETE", "PATCH", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Content-Length", "Accept", "Authorization", "X-POS-JWT", "Cache-Control", "X-Requested-With"},
		ExposeHeaders:    []string{"Content-Disposition"}, // report downloads read the server filename
		AllowCredentials: true,
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
