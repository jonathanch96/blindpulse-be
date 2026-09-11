package session_reveals

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/shopspring/decimal"
)

type SessionReveal struct {
	SessionID    uuid.UUID `gorm:"column:session_id;primaryKey"`
	InstrumentID uuid.UUID `gorm:"column:instrument_id"`
	RevealedAt   time.Time `gorm:"column:revealed_at"`

	Symbol      string    `gorm:"column:symbol"`
	Timeframe   string    `gorm:"column:timeframe"`
	WindowStart time.Time `gorm:"column:window_start"`
	WindowEnd   time.Time `gorm:"column:window_end"`

	MacroLabel *string        `gorm:"column:macro_label"`
	MacroNotes *string        `gorm:"column:macro_notes"`
	MacroTags  pq.StringArray `gorm:"column:macro_tags;type:text[]"`

	BenchmarkLabel     string          `gorm:"column:benchmark_label"`
	StrategyReturnPct  decimal.Decimal `gorm:"column:strategy_return_pct;type:numeric"`
	BenchmarkReturnPct decimal.Decimal `gorm:"column:benchmark_return_pct;type:numeric"`
	AlphaPct           decimal.Decimal `gorm:"column:alpha_pct;type:numeric"`
	DisciplineIndex    int             `gorm:"column:discipline_index"`
	// Metrics is Sprint 06's projector output, frozen beside the reveal. Untouched here: the
	// column exists and this slice does not write it, which is honest about who owns it.
	Metrics json.RawMessage `gorm:"column:metrics;type:jsonb"`
}

func (SessionReveal) TableName() string { return "blindpulse.session_reveals" }
