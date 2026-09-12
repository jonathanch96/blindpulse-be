package equity_snapshots

import (
	"time"

	"github.com/google/uuid"
	domainexec "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/execution"
	"github.com/shopspring/decimal"
)

type EquitySnapshot struct {
	SessionID     uuid.UUID       `gorm:"column:session_id;primaryKey"`
	BarIndex      int             `gorm:"column:bar_index;primaryKey"`
	BarAt         time.Time       `gorm:"column:bar_at"`
	Balance       decimal.Decimal `gorm:"column:balance;type:numeric"`
	Equity        decimal.Decimal `gorm:"column:equity;type:numeric"`
	DrawdownPct   decimal.Decimal `gorm:"column:drawdown_pct;type:numeric"`
	OpenPositions int             `gorm:"column:open_positions"`
}

func (EquitySnapshot) TableName() string { return "blindpulse.equity_snapshots" }

func fromDomain(entity domainexec.EquitySnapshot) EquitySnapshot {
	return EquitySnapshot{
		SessionID: entity.SessionID, BarIndex: entity.BarIndex, BarAt: entity.BarAt,
		Balance: entity.Balance, Equity: entity.Equity,
		DrawdownPct: entity.DrawdownPct, OpenPositions: entity.OpenPositions,
	}
}

func toDomain(model EquitySnapshot) domainexec.EquitySnapshot {
	return domainexec.EquitySnapshot{
		SessionID: model.SessionID, BarIndex: model.BarIndex, BarAt: model.BarAt,
		Balance: model.Balance, Equity: model.Equity,
		DrawdownPct: model.DrawdownPct, OpenPositions: model.OpenPositions,
	}
}
