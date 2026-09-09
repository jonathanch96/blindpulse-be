package accounts

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type Account struct {
	ID                  uuid.UUID       `gorm:"column:id;primaryKey"`
	UserID              uuid.UUID       `gorm:"column:user_id"`
	RootAccountID       uuid.UUID       `gorm:"column:root_account_id"`
	ParentAccountID     *uuid.UUID      `gorm:"column:parent_account_id"`
	IterationIndex      int             `gorm:"column:iteration_index"`
	Name                string          `gorm:"column:name"`
	StrategyProfile     *string         `gorm:"column:strategy_profile"`
	Currency            string          `gorm:"column:currency"`
	InitialBalance      decimal.Decimal `gorm:"column:initial_balance;type:numeric"`
	CurrentBalance      decimal.Decimal `gorm:"column:current_balance;type:numeric"`
	CurrentEquity       decimal.Decimal `gorm:"column:current_equity;type:numeric"`
	PeakEquity          decimal.Decimal `gorm:"column:peak_equity;type:numeric"`
	RiskPerTradePct     decimal.Decimal `gorm:"column:risk_per_trade_pct;type:numeric"`
	MaxDailyDrawdownPct decimal.Decimal `gorm:"column:max_daily_drawdown_pct;type:numeric"`
	MinRiskReward       decimal.Decimal `gorm:"column:min_risk_reward;type:numeric"`
	MaxOpenPositions    int             `gorm:"column:max_open_positions"`
	Leverage            decimal.Decimal `gorm:"column:leverage;type:numeric"`
	Status              string          `gorm:"column:status"`
	ResetReason         *string         `gorm:"column:reset_reason"`
	ResetAt             *time.Time      `gorm:"column:reset_at"`
	SealedAt            *time.Time      `gorm:"column:sealed_at"`
	RootHash            *string         `gorm:"column:root_hash"`
	CreatedAt           time.Time       `gorm:"column:created_at"`
	UpdatedAt           time.Time       `gorm:"column:updated_at"`
	Version             int             `gorm:"column:version"`
}

func (Account) TableName() string { return "blindpulse.accounts" }
