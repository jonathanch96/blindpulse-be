package outbox_events

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type OutboxEvent struct {
	ID            uuid.UUID       `gorm:"column:id;primaryKey"`
	AggregateType string          `gorm:"column:aggregate_type"`
	AggregateID   uuid.UUID       `gorm:"column:aggregate_id"`
	EventType     string          `gorm:"column:event_type"`
	Topic         string          `gorm:"column:topic"`
	PartitionKey  string          `gorm:"column:partition_key"`
	Payload       json.RawMessage `gorm:"column:payload;type:jsonb"`
	Headers       json.RawMessage `gorm:"column:headers;type:jsonb"`
	Status        string          `gorm:"column:status"`
	Attempts      int             `gorm:"column:attempts"`
	AvailableAt   time.Time       `gorm:"column:available_at"`
	PublishedAt   *time.Time      `gorm:"column:published_at"`
	LastError     *string         `gorm:"column:last_error"`
	CreatedAt     time.Time       `gorm:"column:created_at"`
}

func (OutboxEvent) TableName() string { return "blindpulse.outbox_events" }
