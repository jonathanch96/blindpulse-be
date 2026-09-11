package chart_drawings

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

type ChartDrawing struct {
	ID              uuid.UUID       `gorm:"column:id;primaryKey"`
	SessionID       uuid.UUID       `gorm:"column:session_id"`
	Kind            string          `gorm:"column:kind"`
	Timeframe       string          `gorm:"column:timeframe"`
	Payload         json.RawMessage `gorm:"column:payload;type:jsonb"`
	CreatedBarIndex int             `gorm:"column:created_bar_index"`
	CreatedAt       time.Time       `gorm:"column:created_at"`
	UpdatedAt       time.Time       `gorm:"column:updated_at"`
	Version         int             `gorm:"column:version"`
}

func (ChartDrawing) TableName() string { return "blindpulse.chart_drawings" }
