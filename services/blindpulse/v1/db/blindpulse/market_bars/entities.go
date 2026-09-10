package market_bars

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type MarketBar struct {
	InstrumentID uuid.UUID       `gorm:"column:instrument_id;primaryKey"`
	Timeframe    string          `gorm:"column:timeframe;primaryKey"`
	OpenedAt     time.Time       `gorm:"column:opened_at;primaryKey"`
	Open         decimal.Decimal `gorm:"column:open;type:numeric"`
	High         decimal.Decimal `gorm:"column:high;type:numeric"`
	Low          decimal.Decimal `gorm:"column:low;type:numeric"`
	Close        decimal.Decimal `gorm:"column:close;type:numeric"`
	Volume       decimal.Decimal `gorm:"column:volume;type:numeric"`
}

func (MarketBar) TableName() string { return "blindpulse.market_bars" }
