package kafka

import (
	"context"
	"encoding/json"
	"log"
	"os"
	"time"

	"github.com/afjuiekafdjsf/nexus-notification/handler"
	kafkago "github.com/segmentio/kafka-go"
)

type Event struct {
	Type      string `json:"type"`
	ActorID   string `json:"actor_id"`
	ActorName string `json:"actor_name"`
	TargetID  string `json:"target_id"`
	PostID    string `json:"post_id"`
	CommentID string `json:"comment_id"`
	Content   string `json:"content"`
}

func StartConsumer() {
	broker := os.Getenv("KAFKA_BROKER")
	if broker == "" {
		broker = "nexuskafka:9092"
	}

	// Wait for Kafka
	for i := 0; i < 15; i++ {
		conn, err := kafkago.Dial("tcp", broker)
		if err == nil {
			conn.Close()
			break
		}
		log.Printf("waiting for kafka... (%d/15)", i+1)
		time.Sleep(5 * time.Second)
	}

	reader := kafkago.NewReader(kafkago.ReaderConfig{
		Brokers:     []string{broker},
		Topic:       "events",
		GroupID:     "notification-service-v1",
		MinBytes:    1,
		MaxBytes:    10e6,
		StartOffset: kafkago.FirstOffset,
	})
	defer reader.Close()

	log.Println("Kafka consumer started, listening on 'events' topic...")
	for {
		msg, err := reader.ReadMessage(context.Background())
		if err != nil {
			log.Println("kafka read error:", err)
			time.Sleep(time.Second)
			continue
		}

		var event Event
		if err := json.Unmarshal(msg.Value, &event); err != nil {
			continue
		}

		if event.TargetID == "" {
			continue
		}

		handler.SaveNotification(event.Type, event.ActorID, event.ActorName,
			event.TargetID, event.PostID, event.CommentID, event.Content)
	}
}
