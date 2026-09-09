package account_ledger_entries

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type LedgerEntry struct {
	ID            uuid.UUID       `gorm:"column:id;primaryKey"`
	AccountID     uuid.UUID       `gorm:"column:account_id"`
	Sequence      int64           `gorm:"column:sequence"`
	Kind          string          `gorm:"column:kind"`
	ReferenceType *string         `gorm:"column:reference_type"`
	ReferenceID   *uuid.UUID      `gorm:"column:reference_id"`
	Amount        decimal.Decimal `gorm:"column:amount;type:numeric"`
	BalanceAfter  decimal.Decimal `gorm:"column:balance_after;type:numeric"`
	EquityAfter   decimal.Decimal `gorm:"column:equity_after;type:numeric"`
	Payload       json.RawMessage `gorm:"column:payload;type:json"`
	PreviousHash  *string         `gorm:"column:previous_hash"`
	EntryHash     string          `gorm:"column:entry_hash"`
	RecordedAt    time.Time       `gorm:"column:recorded_at"`
}

func (LedgerEntry) TableName() string { return "blindpulse.account_ledger_entries" }
