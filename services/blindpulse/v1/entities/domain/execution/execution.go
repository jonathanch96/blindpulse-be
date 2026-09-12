// Package execution holds the simulator's orders, fills and positions.
//
// One property underpins everything here and is worth stating before any of the types: **the
// simulator works entirely in blinded prices, and the dollars it produces are nevertheless the
// dollars the trader would have made on the real series.**
//
// The blinding map is affine — displayed = (real + offset) × scale — so a stop distance in blinded
// space is the real distance times the scale. Position size is derived from the trader's dollar
// risk divided by that stop distance, so the scale appears in the denominator; realized PnL is the
// blinded price move times the size, so it appears in the numerator. It cancels exactly. A trader
// risking 1% on a 20-tick stop makes or loses the same 1% whatever scale the feed was blinded with,
// and R-multiples and percentage account moves are identical to the unblinded series.
//
// That is what makes a blinded simulator meaningful rather than a game with arbitrary units, and
// `SizeFromRisk` is where it is enforced. It is asserted in the tests, because the day somebody
// sizes a position from a notional constant instead, the numbers stay plausible and stop meaning
// anything.
package execution

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type Side string

const (
	SideBuy  Side = "buy"
	SideSell Side = "sell"
)

func (s Side) Valid() bool { return s == SideBuy || s == SideSell }

// Long reports the direction as a sign, so price comparisons can be written once rather than twice.
func (s Side) Long() bool { return s == SideBuy }

// Opposite is the side that closes this one.
func (s Side) Opposite() Side {
	if s == SideBuy {
		return SideSell
	}
	return SideBuy
}

type OrderType string

const (
	OrderMarket OrderType = "market"
	OrderLimit  OrderType = "limit"
	OrderStop   OrderType = "stop"
)

func (t OrderType) Valid() bool {
	return t == OrderMarket || t == OrderLimit || t == OrderStop
}

// Resting reports whether the order waits for price to come to it.
func (t OrderType) Resting() bool { return t == OrderLimit || t == OrderStop }

type OrderStatus string

const (
	StatusPending   OrderStatus = "pending"
	StatusFilled    OrderStatus = "filled"
	StatusCancelled OrderStatus = "cancelled"
	StatusRejected  OrderStatus = "rejected"
	StatusExpired   OrderStatus = "expired"
)

type ExitReason string

const (
	ExitStop         ExitReason = "stop"
	ExitTarget       ExitReason = "target"
	ExitManual       ExitReason = "manual"
	ExitSessionEnd   ExitReason = "session_end"
	ExitDrawdownHalt ExitReason = "drawdown_halt"
)

// Order is one instruction, whether it filled or not.
//
// A rejected order is a row like any other (BR-09). Sprint 06's discipline index is built largely
// from what the trader *tried* to do, and discarding rejections would erase the most interesting
// data the product collects — an oversized order refused is a fact about the trader, not a
// non-event.
type Order struct {
	ID        uuid.UUID
	SessionID uuid.UUID
	AccountID uuid.UUID
	// ClientKey makes a retry a no-op. The unique index on (session_id, client_key) is what turns
	// an at-least-once network into exactly-once execution.
	ClientKey  string
	Side       Side
	Type       OrderType
	Quantity   decimal.Decimal
	LimitPrice *decimal.Decimal
	// StopLoss is not a pointer, and the column is NOT NULL. "No entry without a hard stop" (BR-03)
	// is the product's central discipline rule, so it is a database guarantee rather than a
	// validation somebody can route around.
	StopLoss      decimal.Decimal
	TakeProfit    *decimal.Decimal
	RiskReward    *decimal.Decimal
	RiskAmount    *decimal.Decimal
	Status        OrderStatus
	RejectionCode *string
	// PlacedBarIndex is where the trader was when they pressed the button. PlacedBarAt is the bar's
	// real instant — it lives on the row for the drawdown day boundary and must never reach a
	// response type (SP4-1).
	PlacedBarIndex int
	PlacedBarAt    time.Time
	FilledBarIndex *int
	FilledPrice    *decimal.Decimal
	Slippage       decimal.Decimal
	CreatedAt      time.Time
	UpdatedAt      time.Time
	Version        int
}

// Trade is a position: open once its entry fills, closed when something takes it out.
type Trade struct {
	ID           uuid.UUID
	SessionID    uuid.UUID
	AccountID    uuid.UUID
	EntryOrderID uuid.UUID
	ExitOrderID  *uuid.UUID
	Side         Side
	Quantity     decimal.Decimal
	EntryPrice   decimal.Decimal
	ExitPrice    *decimal.Decimal
	StopLoss     decimal.Decimal
	TakeProfit   *decimal.Decimal
	Status       string
	ExitReason   *ExitReason
	BehaviorTag  *string
	RealizedPnL  *decimal.Decimal
	RMultiple    *decimal.Decimal
	// Excursions separate "the idea was wrong" from "the idea was right and the stop was in the
	// wrong place", which is the single most useful thing a post-mortem can tell a trader.
	MaxAdverseExcursion   *decimal.Decimal
	MaxFavorableExcursion *decimal.Decimal
	OpenedBarIndex        int
	ClosedBarIndex        *int
	BarsHeld              *int
	OpenedAt              time.Time
	ClosedAt              *time.Time
	CreatedAt             time.Time
	UpdatedAt             time.Time
	Version               int
}

const (
	TradeOpen   = "open"
	TradeClosed = "closed"
)

// Open reports whether this position is still exposed.
func (t Trade) Open() bool { return t.Status == TradeOpen }

// Risk is the distance from entry to stop, always positive. It is the denominator of every
// R-multiple and the basis of every size calculation.
func (t Trade) Risk() decimal.Decimal { return t.EntryPrice.Sub(t.StopLoss).Abs() }

// UnrealizedPnL values an open position at a price.
//
// Signed by side rather than by branching on it at each call site: a long gains when price rises, a
// short when it falls, and writing that once is the difference between one rule and four places to
// get it backwards.
func (t Trade) UnrealizedPnL(at decimal.Decimal) decimal.Decimal {
	move := at.Sub(t.EntryPrice)
	if !t.Side.Long() {
		move = move.Neg()
	}
	return move.Mul(t.Quantity)
}

// EquitySnapshot is one bar's mark-to-market, written per bar while a session is live.
//
// BarAt is the bar's real instant and, like the order's, stays server-side: it is here because the
// drawdown's day boundary is a market day (SP4-1), and a client that could read these timestamps
// could date the window.
type EquitySnapshot struct {
	SessionID     uuid.UUID
	BarIndex      int
	BarAt         time.Time
	Balance       decimal.Decimal
	Equity        decimal.Decimal
	DrawdownPct   decimal.Decimal
	OpenPositions int
}

// PriceScale is the precision prices are quantized to, matching the feed's display scale. Working
// to more digits than the trader can see would produce fills at prices that never appeared on the
// chart.
const PriceScale = 5

// MoneyScale is the precision balances and PnL are held at.
const MoneyScale = 8
