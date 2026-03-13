package handler

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/afjuiekafdjsf/nexus-notification/db"
	"github.com/gin-gonic/gin"
)

type Notification struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	Type      string    `json:"type"`
	ActorID   string    `json:"actor_id"`
	ActorName string    `json:"actor_name"`
	PostID    string    `json:"post_id,omitempty"`
	CommentID string    `json:"comment_id,omitempty"`
	Content   string    `json:"content,omitempty"`
	IsRead    bool      `json:"is_read"`
	CreatedAt time.Time `json:"created_at"`
}

// SSE client registry
var (
	mu      sync.RWMutex
	clients = map[string][]chan Notification{} // userID -> channels
)

func subscribe(userID string) chan Notification {
	ch := make(chan Notification, 10)
	mu.Lock()
	clients[userID] = append(clients[userID], ch)
	mu.Unlock()
	return ch
}

func unsubscribe(userID string, ch chan Notification) {
	mu.Lock()
	defer mu.Unlock()
	list := clients[userID]
	for i, c := range list {
		if c == ch {
			clients[userID] = append(list[:i], list[i+1:]...)
			break
		}
	}
	close(ch)
}

func broadcast(userID string, n Notification) {
	mu.RLock()
	defer mu.RUnlock()
	for _, ch := range clients[userID] {
		select {
		case ch <- n:
		default:
		}
	}
}

func SaveNotification(typ, actorID, actorName, targetID, postID, commentID, content string) {
	var n Notification
	err := db.DB.QueryRow(
		`INSERT INTO notifications (user_id, type, actor_id, actor_name, post_id, comment_id, content)
		 VALUES ($1,$2,$3,$4,$5,$6,$7)
		 RETURNING id, user_id, type, actor_id, actor_name, post_id, comment_id, content, is_read, created_at`,
		targetID, typ, actorID, actorName, postID, commentID, content,
	).Scan(&n.ID, &n.UserID, &n.Type, &n.ActorID, &n.ActorName, &n.PostID, &n.CommentID, &n.Content, &n.IsRead, &n.CreatedAt)
	if err != nil {
		return
	}
	broadcast(targetID, n)
}

// SSE stream endpoint
func StreamNotifications(c *gin.Context) {
	userID := c.GetString("user_id")
	ch := subscribe(userID)
	defer unsubscribe(userID, ch)

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	// Send any unread notifications first
	rows, _ := db.DB.Query(
		`SELECT id, user_id, type, actor_id, actor_name, post_id, comment_id, content, is_read, created_at
		 FROM notifications WHERE user_id=$1 AND is_read=FALSE ORDER BY created_at DESC LIMIT 20`, userID)
	if rows != nil {
		defer rows.Close()
		for rows.Next() {
			var n Notification
			rows.Scan(&n.ID, &n.UserID, &n.Type, &n.ActorID, &n.ActorName, &n.PostID, &n.CommentID, &n.Content, &n.IsRead, &n.CreatedAt)
			fmt.Fprintf(c.Writer, "data: {\"id\":\"%s\",\"type\":\"%s\",\"actor_name\":\"%s\",\"post_id\":\"%s\",\"created_at\":\"%s\"}\n\n",
				n.ID, n.Type, n.ActorName, n.PostID, n.CreatedAt.Format(time.RFC3339))
		}
		c.Writer.Flush()
	}

	for {
		select {
		case n, ok := <-ch:
			if !ok {
				return
			}
			fmt.Fprintf(c.Writer, "data: {\"id\":\"%s\",\"type\":\"%s\",\"actor_name\":\"%s\",\"post_id\":\"%s\",\"created_at\":\"%s\"}\n\n",
				n.ID, n.Type, n.ActorName, n.PostID, n.CreatedAt.Format(time.RFC3339))
			c.Writer.Flush()
		case <-ticker.C:
			fmt.Fprintf(c.Writer, ": ping\n\n")
			c.Writer.Flush()
		case <-c.Request.Context().Done():
			return
		}
	}
}

func GetNotifications(c *gin.Context) {
	userID := c.GetString("user_id")
	rows, err := db.DB.Query(
		`SELECT id, user_id, type, actor_id, actor_name, post_id, comment_id, content, is_read, created_at
		 FROM notifications WHERE user_id=$1 ORDER BY created_at DESC LIMIT 50`, userID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	defer rows.Close()
	notifications := []Notification{}
	for rows.Next() {
		var n Notification
		rows.Scan(&n.ID, &n.UserID, &n.Type, &n.ActorID, &n.ActorName, &n.PostID, &n.CommentID, &n.Content, &n.IsRead, &n.CreatedAt)
		notifications = append(notifications, n)
	}
	c.JSON(http.StatusOK, notifications)
}

func MarkRead(c *gin.Context) {
	userID := c.GetString("user_id")
	db.DB.Exec(`UPDATE notifications SET is_read=TRUE WHERE user_id=$1 AND is_read=FALSE`, userID)
	c.JSON(http.StatusOK, gin.H{"message": "marked as read"})
}

func GetUnreadCount(c *gin.Context) {
	userID := c.GetString("user_id")
	var count int
	db.DB.QueryRow(`SELECT COUNT(*) FROM notifications WHERE user_id=$1 AND is_read=FALSE`, userID).Scan(&count)
	c.JSON(http.StatusOK, gin.H{"count": count})
}

// Internal endpoint for user-service to push follow events
func InternalEvent(c *gin.Context) {
	var req struct {
		Type      string `json:"type"`
		ActorID   string `json:"actor_id"`
		ActorName string `json:"actor_name"`
		TargetID  string `json:"target_id"`
		PostID    string `json:"post_id"`
		Content   string `json:"content"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.TargetID == "" || req.Type == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing fields"})
		return
	}
	go SaveNotification(req.Type, req.ActorID, req.ActorName, req.TargetID, req.PostID, "", req.Content)
	c.JSON(http.StatusOK, gin.H{"message": "ok"})
}
