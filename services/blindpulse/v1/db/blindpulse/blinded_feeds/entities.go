package blinded_feeds

import (
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/shopspring/decimal"
)

type BlindedFeed struct {
	ID                 uuid.UUID        `gorm:"column:id;primaryKey"`
	InstrumentID       uuid.UUID        `gorm:"column:instrument_id"`
	AliasLabel         string           `gorm:"column:alias_label"`
	BaseTimeframe      string           `gorm:"column:base_timeframe"`
	WindowStart        time.Time        `gorm:"column:window_start"`
	WindowEnd          time.Time        `gorm:"column:window_end"`
	WarmupBars         int              `gorm:"column:warmup_bars"`
	TotalBars          int              `gorm:"column:total_bars"`
	PriceScale         decimal.Decimal  `gorm:"column:price_scale;type:numeric"`
	PriceOffset        decimal.Decimal  `gorm:"column:price_offset;type:numeric"`
	VolumeScale        decimal.Decimal  `gorm:"column:volume_scale;type:numeric"`
	Difficulty         string           `gorm:"column:difficulty"`
	MacroLabel         *string          `gorm:"column:macro_label"`
	MacroNotes         *string          `gorm:"column:macro_notes"`
	MacroTags          pq.StringArray   `gorm:"column:macro_tags;type:text[]"`
	IsPublished        bool             `gorm:"column:is_published"`
	RealizedVolatility *decimal.Decimal `gorm:"column:realized_volatility;type:numeric"`
	TrendPersistence   *decimal.Decimal `gorm:"column:trend_persistence;type:numeric"`
	BuilderVersion     int              `gorm:"column:builder_version"`
	BuiltAt            time.Time        `gorm:"column:built_at"`
	CreatedAt          time.Time        `gorm:"column:created_at"`
	UpdatedAt          time.Time        `gorm:"column:updated_at"`
	Version            int              `gorm:"column:version"`
}

func (BlindedFeed) TableName() string { return "blindpulse.blinded_feeds" }
