package account

import (
	"time"

	"github.com/shopspring/decimal"
)

type Dependencies struct {
	Repo   Repository
	Ledger LedgerRepository
	Outbox OutboxRepository
	UOW    UnitOfWork
	Topic  func(string) string
	Clock  func() time.Time
}

type service struct{ deps Dependencies }

type OpenInput struct {
	Name            string
	StrategyProfile string
	Currency        string
	InitialBalance  decimal.Decimal
	Risk            RiskInput
}

// RiskInput carries the gates as optional overrides. A nil field means "keep the platform
// default" rather than "set it to zero", which is why these are pointers.
type RiskInput struct {
	RiskPerTradePct     *decimal.Decimal
	MaxDailyDrawdownPct *decimal.Decimal
	MinRiskReward       *decimal.Decimal
	MaxOpenPositions    *int
	Leverage            *decimal.Decimal
}

type ResetInput struct {
	Reason          string
	Name            string
	StrategyProfile string
	InitialBalance  *decimal.Decimal
	Risk            RiskInput
}

// Verification is the result of recomputing an account's ledger chain. BrokenAtSequence names the
// first entry whose stored hash disagrees with the recomputed one, so an integrity failure points
// at a row rather than just saying "invalid".
type Verification struct {
	AccountID        string
	Entries          int
	RootHash         string
	StoredRootHash   string
	Valid            bool
	BrokenAtSequence *int64
}
