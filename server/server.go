package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
)

func main() {
	if err := godotenv.Load(); err != nil {
		log.Fatal("could not load .env file: ", err)
	}

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Fatal("DATABASE_URL is not set. Make sure .env file exists and is populated.")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	db, err := openDatabase(dsn)
	if err != nil {
		log.Fatal("database connection failed: ", err)
	}
	defer db.Close()

	if err := createTables(ctx, db); err != nil {
		log.Fatal("database schema initialization failed: ", err)
	}

	log.Print("connected to database and initialized schema")

	state, err := openBadgerStore()
	if err != nil {
		log.Fatal("Badger initialization failed: ", err)
	}
	defer state.close()

	auth, err := newAuthHandler(db, state)
	if err != nil {
		log.Fatal("OPAQUE initialization failed: ", err)
	}

	router := gin.New()
	router.Use(gin.Logger(), gin.Recovery())
	router.Use(corsMiddleware())
	router.POST("/api/auth/register/start", auth.registerStart)
	router.POST("/api/auth/register/finish", auth.registerFinish)
	router.POST("/api/auth/login/start", auth.loginStart)
	router.POST("/api/auth/login/finish", auth.loginFinish)

	protected := router.Group("/api")
	protected.Use(requireSession(state))
	protected.GET("/auth/me", auth.currentUser)
	protected.POST("/auth/logout", auth.logout)
	protected.POST("/auth/logout-all", auth.logoutAll)

	addr := os.Getenv("HTTP_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	log.Printf("HTTP server listening on %s", addr)
	if err := router.Run(addr); err != nil {
		log.Fatal("HTTP server failed: ", err)
	}

}

func corsMiddleware() gin.HandlerFunc {
	allowedOrigin := os.Getenv("CLIENT_ORIGIN")
	if allowedOrigin == "" {
		allowedOrigin = "http://localhost:5173"
	}

	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", allowedOrigin)
		c.Header("Vary", "Origin")
		c.Header("Access-Control-Allow-Headers", "Content-Type")
		c.Header("Access-Control-Allow-Methods", "POST, OPTIONS")
		c.Header("Access-Control-Allow-Credentials", "true")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	}
}
