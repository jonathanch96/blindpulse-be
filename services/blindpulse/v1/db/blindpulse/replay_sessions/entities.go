package replay_sessions

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type ReplaySession struct {
	ID                  uuid.UUID       `gorm:"column:id;primaryKey"`
	UserID              uuid.UUID       `gorm:"column:user_id"`
	AccountID           uuid.UUID       `gorm:"column:account_id"`
	FeedID              uuid.UUID       `gorm:"column:feed_id"`
	Status              string          `gorm:"column:status"`
	Timeframe           string          `gorm:"column:timeframe"`
	PlaybackSpeed       decimal.Decimal `gorm:"column:playback_speed;type:numeric"`
	CursorIndex         int             `gorm:"column:cursor_index"`
	RevealedIndex       int             `gorm:"column:revealed_index"`
	CursorAt            *time.Time      `gorm:"column:cursor_at"`
	Seed                int64           `gorm:"column:seed"`
	LastCheckpointIndex int             `gorm:"column:last_checkpoint_index"`
	StartedAt           time.Time       `gorm:"column:started_at"`
	LastActiveAt        time.Time       `gorm:"column:last_active_at"`
	ClosedAt            *time.Time      `gorm:"column:closed_at"`
	RevealedAt          *time.Time      `gorm:"column:revealed_at"`
	RootHash            *string         `gorm:"column:root_hash"`
	CreatedAt           time.Time       `gorm:"column:created_at"`
	UpdatedAt           time.Time       `gorm:"column:updated_at"`
	Version             int             `gorm:"column:version"`
}

func (ReplaySession) TableName() string { return "blindpulse.replay_sessions" }
