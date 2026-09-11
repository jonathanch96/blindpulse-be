package journal_entries

import (
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

type JournalEntry struct {
	ID         uuid.UUID      `gorm:"column:id;primaryKey"`
	SessionID  uuid.UUID      `gorm:"column:session_id"`
	TradeID    *uuid.UUID     `gorm:"column:trade_id"`
	UserID     uuid.UUID      `gorm:"column:user_id"`
	BarIndex   int            `gorm:"column:bar_index"`
	Thesis     *string        `gorm:"column:thesis"`
	Note       *string        `gorm:"column:note"`
	Emotion    *string        `gorm:"column:emotion"`
	Conviction *int           `gorm:"column:conviction"`
	Tags       pq.StringArray `gorm:"column:tags;type:text[]"`
	MediaKey   *string        `gorm:"column:media_key"`
	CreatedAt  time.Time      `gorm:"column:created_at"`
	UpdatedAt  time.Time      `gorm:"column:updated_at"`
	Version    int            `gorm:"column:version"`
}

func (JournalEntry) TableName() string { return "blindpulse.journal_entries" }

type JournalEntryRevision struct {
	ID           uuid.UUID      `gorm:"column:id;primaryKey"`
	EntryID      uuid.UUID      `gorm:"column:entry_id"`
	Version      int            `gorm:"column:version"`
	BarIndex     int            `gorm:"column:bar_index"`
	Thesis       *string        `gorm:"column:thesis"`
	Note         *string        `gorm:"column:note"`
	Emotion      *string        `gorm:"column:emotion"`
	Conviction   *int           `gorm:"column:conviction"`
	Tags         pq.StringArray `gorm:"column:tags;type:text[]"`
	SupersededAt time.Time      `gorm:"column:superseded_at"`
}

func (JournalEntryRevision) TableName() string { return "blindpulse.journal_entry_revisions" }
