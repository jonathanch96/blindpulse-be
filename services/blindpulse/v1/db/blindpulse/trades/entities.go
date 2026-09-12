package trades

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type Trade struct {
	ID           uuid.UUID  `gorm:"column:id;primaryKey"`
	SessionID    uuid.UUID  `gorm:"column:session_id"`
	AccountID    uuid.UUID  `gorm:"column:account_id"`
	EntryOrderID uuid.UUID  `gorm:"column:entry_order_id"`
	ExitOrderID  *uuid.UUID `gorm:"column:exit_order_id"`
	Side         string     `gorm:"column:side"`

	Quantity   decimal.Decimal  `gorm:"column:quantity;type:numeric"`
	EntryPrice decimal.Decimal  `gorm:"column:entry_price;type:numeric"`
	ExitPrice  *decimal.Decimal `gorm:"column:exit_price;type:numeric"`
	StopLoss   decimal.Decimal  `gorm:"column:stop_loss;type:numeric"`
	// The stop the position was sized on. Never updated — Update deliberately omits it. A pointer
	// because NULL is meaningful: a row that predates the column, whose stop had already moved, has
	// no recoverable original distance, and zero would claim it risked nothing.
	InitialStopLoss *decimal.Decimal `gorm:"column:initial_stop_loss;type:numeric"`
	TakeProfit      *decimal.Decimal `gorm:"column:take_profit;type:numeric"`

	Status      string  `gorm:"column:status"`
	ExitReason  *string `gorm:"column:exit_reason"`
	BehaviorTag *string `gorm:"column:behavior_tag"`

	RealizedPnL           *decimal.Decimal `gorm:"column:realized_pnl;type:numeric"`
	RMultiple             *decimal.Decimal `gorm:"column:r_multiple;type:numeric"`
	MaxAdverseExcursion   *decimal.Decimal `gorm:"column:max_adverse_excursion;type:numeric"`
	MaxFavorableExcursion *decimal.Decimal `gorm:"column:max_favorable_excursion;type:numeric"`

	OpenedBarIndex int        `gorm:"column:opened_bar_index"`
	ClosedBarIndex *int       `gorm:"column:closed_bar_index"`
	BarsHeld       *int       `gorm:"column:bars_held"`
	OpenedAt       time.Time  `gorm:"column:opened_at"`
	ClosedAt       *time.Time `gorm:"column:closed_at"`
	CreatedAt      time.Time  `gorm:"column:created_at"`
	UpdatedAt      time.Time  `gorm:"column:updated_at"`
	Version        int        `gorm:"column:version"`
}

func (Trade) TableName() string { return "blindpulse.trades" }
