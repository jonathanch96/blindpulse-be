package instruments

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type Instrument struct {
	ID            uuid.UUID       `gorm:"column:id;primaryKey"`
	Symbol        string          `gorm:"column:symbol"`
	DisplayName   string          `gorm:"column:display_name"`
	AssetClass    string          `gorm:"column:asset_class"`
	Venue         *string         `gorm:"column:venue"`
	QuoteCurrency string          `gorm:"column:quote_currency"`
	TickSize      decimal.Decimal `gorm:"column:tick_size;type:numeric"`
	ContractSize  decimal.Decimal `gorm:"column:contract_size;type:numeric"`
	CreatedAt     time.Time       `gorm:"column:created_at"`
	UpdatedAt     time.Time       `gorm:"column:updated_at"`
}

func (Instrument) TableName() string { return "blindpulse.instruments" }
