// Package account holds the trading-account aggregate as the domain sees it: a node in a reset
// tree, never a mutable balance. Persistence models live in db/blindpulse/accounts; this type is
// what crosses controller and domain boundaries.
package account

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type Status string

const (
	StatusActive   Status = "active"
	StatusReset    Status = "reset"
	StatusArchived Status = "archived"
)

// RiskPolicy is the account's contract with itself. The bracket dock renders it; the order gate
// enforces it. Both read the same values, so what the trader was promised and what the simulator
// applies cannot drift apart.
type RiskPolicy struct {
	RiskPerTradePct     decimal.Decimal
	MaxDailyDrawdownPct decimal.Decimal
	MinRiskReward       decimal.Decimal
	MaxOpenPositions    int
	Leverage            decimal.Decimal
}

type Account struct {
	ID              uuid.UUID
	UserID          uuid.UUID
	RootAccountID   uuid.UUID
	ParentAccountID *uuid.UUID
	IterationIndex  int
	Name            string
	StrategyProfile *string
	Currency        string
	InitialBalance  decimal.Decimal
	CurrentBalance  decimal.Decimal
	CurrentEquity   decimal.Decimal
	PeakEquity      decimal.Decimal
	Risk            RiskPolicy
	Status          Status
	ResetReason     *string
	ResetAt         *time.Time
	SealedAt        *time.Time
	RootHash        *string
	CreatedAt       time.Time
	UpdatedAt       time.Time
	Version         int
}

func (a Account) IsActive() bool { return a.Status == StatusActive }

// NetReturnPct is the number the accounts screen prints beside each iteration. It is computed
// from equity rather than balance so an iteration with open positions still reports honestly.
func (a Account) NetReturnPct() decimal.Decimal {
	if a.InitialBalance.IsZero() {
		return decimal.Zero
	}
	return a.CurrentEquity.Sub(a.InitialBalance).Div(a.InitialBalance).Mul(decimal.NewFromInt(100))
}

// DrawdownPct measures the fall from the iteration's own high-water mark, which is what the daily
// drawdown gate trips on - measuring from the starting balance would let a profitable account give
// back an unlimited amount before the gate noticed.
func (a Account) DrawdownPct() decimal.Decimal {
	if a.PeakEquity.IsZero() {
		return decimal.Zero
	}
	drawdown := a.PeakEquity.Sub(a.CurrentEquity).Div(a.PeakEquity).Mul(decimal.NewFromInt(100))
	if drawdown.IsNegative() {
		return decimal.Zero
	}
	return drawdown
}

// LedgerKind names the reason a ledger row exists. Every balance movement is one of these.
type LedgerKind string

const (
	LedgerOpen       LedgerKind = "open"
	LedgerTrade      LedgerKind = "trade"
	LedgerFee        LedgerKind = "fee"
	LedgerAdjustment LedgerKind = "adjustment"
	LedgerReset      LedgerKind = "reset"
	LedgerSeal       LedgerKind = "seal"
)

// LedgerEntry is append-only and hash-chained. PreviousHash is the entry before it in the same
// account; EntryHash covers this row's own fields plus that link, so altering any historical row
// invalidates every hash after it and the account's published RootHash stops matching.
type LedgerEntry struct {
	ID            uuid.UUID
	AccountID     uuid.UUID
	Sequence      int64
	Kind          LedgerKind
	ReferenceType *string
	ReferenceID   *uuid.UUID
	Amount        decimal.Decimal
	BalanceAfter  decimal.Decimal
	EquityAfter   decimal.Decimal
	Payload       []byte
	PreviousHash  *string
	EntryHash     string
	RecordedAt    time.Time
}

// Tree is one root account and every iteration under it, ordered oldest first. It is the shape
// the Accounts & Resets screen reads.
type Tree struct {
	RootAccountID uuid.UUID
	Iterations    []Account
}

func (t Tree) Active() *Account {
	for i := range t.Iterations {
		if t.Iterations[i].IsActive() {
			return &t.Iterations[i]
		}
	}
	return nil
}
