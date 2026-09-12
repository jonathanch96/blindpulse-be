package orders

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// Order is the orders row.
//
// PlacedBarAt is a real instant and it lives here rather than on the domain's response path for one
// reason: the drawdown's day boundary is a market day (SP4-1). It is written, it is read by the
// gate, and it is never mapped into anything a client sees.
type Order struct {
	ID             uuid.UUID        `gorm:"column:id;primaryKey"`
	SessionID      uuid.UUID        `gorm:"column:session_id"`
	AccountID      uuid.UUID        `gorm:"column:account_id"`
	ClientKey      string           `gorm:"column:client_key"`
	Side           string           `gorm:"column:side"`
	OrderType      string           `gorm:"column:order_type"`
	Quantity       decimal.Decimal  `gorm:"column:quantity;type:numeric"`
	LimitPrice     *decimal.Decimal `gorm:"column:limit_price;type:numeric"`
	StopLoss       decimal.Decimal  `gorm:"column:stop_loss;type:numeric"`
	TakeProfit     *decimal.Decimal `gorm:"column:take_profit;type:numeric"`
	RiskReward     *decimal.Decimal `gorm:"column:risk_reward;type:numeric"`
	RiskAmount     *decimal.Decimal `gorm:"column:risk_amount;type:numeric"`
	Status         string           `gorm:"column:status"`
	RejectionCode  *string          `gorm:"column:rejection_code"`
	PlacedBarIndex int              `gorm:"column:placed_bar_index"`
	PlacedBarAt    time.Time        `gorm:"column:placed_bar_at"`
	FilledBarIndex *int             `gorm:"column:filled_bar_index"`
	FilledPrice    *decimal.Decimal `gorm:"column:filled_price;type:numeric"`
	Slippage       decimal.Decimal  `gorm:"column:slippage;type:numeric"`
	CreatedAt      time.Time        `gorm:"column:created_at"`
	UpdatedAt      time.Time        `gorm:"column:updated_at"`
	Version        int              `gorm:"column:version"`
}

func (Order) TableName() string { return "blindpulse.orders" }
