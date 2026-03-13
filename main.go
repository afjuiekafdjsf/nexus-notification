package main

import (
	"log"
	"os"

	"github.com/afjuiekafdjsf/nexus-notification/db"
	"github.com/afjuiekafdjsf/nexus-notification/handler"
	"github.com/afjuiekafdjsf/nexus-notification/kafka"
	"github.com/afjuiekafdjsf/nexus-notification/middleware"
	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"
)

func main() {
	godotenv.Load()
	db.Init()

	go kafka.StartConsumer()

	r := gin.Default()
	r.Use(cors())

	// Internal endpoint (no auth)
	r.POST("/internal/events", handler.InternalEvent)

	// Protected endpoints
	auth := r.Group("/")
	auth.Use(middleware.Auth())
	auth.GET("/notifications/stream", handler.StreamNotifications)
	auth.GET("/notifications", handler.GetNotifications)
	auth.GET("/notifications/unread-count", handler.GetUnreadCount)
	auth.POST("/notifications/read", handler.MarkRead)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("notification-service on :%s", port)
	r.Run(":" + port)
}

func cors() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET,POST,PUT,DELETE,OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Content-Type,Authorization")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	}
}
