package execution

import (
	"time"

	"github.com/google/uuid"
	domainexec "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/execution"
	"github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/event"
	"github.com/shopspring/decimal"
)

// EventRecord aliases the outbox event so the domain does not import the persistence package.
type EventRecord = event.OutboxEvent

type Dependencies struct {
	Orders    OrderRepository
	Trades    TradeRepository
	Snapshots SnapshotRepository
	Sessions  SessionReader
	Accounts  AccountWriter
	Feeds     FeedReader
	Clock     MarketClock
	Ledger    LedgerWriter
	Outbox    OutboxRepository
	UOW       UnitOfWork
	Topic     func(string) string
	Now       func() time.Time

	// Conditions are the venue's frictions: spread and the slippage bound. Configured rather than
	// constant so a teaching feed can run frictionless and a realistic one cannot.
	Conditions domainexec.Conditions
}

type service struct{ deps Dependencies }

// PlaceInput is one order as the trader described it.
//
// Quantity and RiskPct are alternatives: a trader either names a size or names the fraction of the
// account they are willing to lose, and the server derives the other. Both given is a client bug
// and is refused rather than resolved by precedence, because either precedence would silently
// ignore something the trader typed.
type PlaceInput struct {
	ClientKey  string
	Side       domainexec.Side
	Type       domainexec.OrderType
	Quantity   *decimal.Decimal
	RiskPct    *decimal.Decimal
	LimitPrice *decimal.Decimal
	StopLoss   decimal.Decimal
	TakeProfit *decimal.Decimal
}

type AmendInput struct {
	StopLoss   *decimal.Decimal
	TakeProfit *decimal.Decimal
}

// fillContext is everything one bar's resolution needs, gathered once rather than re-read per
// order. A session advancing ten bars with five resting orders would otherwise make fifty round
// trips for data that cannot change inside the loop.
type fillContext struct {
	sessionID uuid.UUID
	accountID uuid.UUID
	seed      int64
	// conditions are the venue's frictions with the tick size resolved.
	//
	// The configured value cannot carry one: there is no single tick size at configuration time, it
	// is one unit of the blinded display scale. So something has to supply it, and this is that
	// something — the fill engine gets these rather than Dependencies.Conditions, and the same
	// resolved tick then prices a market fill, a resting fill and a manual exit. MarketFill defends
	// itself against a zero tick as well, which is belt and braces rather than the real answer: two
	// copies of a default are two things to keep in step.
	conditions   domainexec.Conditions
	tickSize     decimal.Decimal
	contractSize decimal.Decimal
	barTimes     []time.Time
}

// barTime is the real instant of a bar, used for the drawdown day boundary and for the order row's
// placed_bar_at. It never reaches a response type.
func (c fillContext) barTime(index int) time.Time {
	if index >= 0 && index < len(c.barTimes) {
		return c.barTimes[index]
	}
	return time.Time{}
}
