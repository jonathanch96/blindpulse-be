package execution

import (
	"github.com/shopspring/decimal"
)

// The pre-entry gate (BR-03, BR-04, BR-05, BR-09).
//
// Two rules hold everywhere in this file:
//
//  1. **The checks run in a fixed order and the first failure is the answer.** Order matters for
//     the message the trader reads: telling somebody their risk-to-reward is too low when they have
//     not set a stop at all is noise, and a gate that reported every failure at once would bury the
//     one they need to fix.
//
//  2. **A rejection is never a silent correction.** No check clamps a quantity or widens a target to
//     make an order acceptable. A trader who learns that oversized orders quietly shrink learns
//     nothing about sizing — and the refused attempt is itself the data Sprint 06 grades.

// GateInput is everything the ten checks read. It is a value rather than a set of service lookups so
// the gate is a pure function: the whole rule set is testable as a table, with no database and no
// clock.
type GateInput struct {
	// SessionLive is false for a closed or abandoned session.
	SessionLive bool
	// Halted is true once the daily drawdown gate has breached (BR-05).
	Halted bool

	Side       Side
	Type       OrderType
	Quantity   decimal.Decimal
	EntryPrice decimal.Decimal
	StopLoss   decimal.Decimal
	TakeProfit *decimal.Decimal

	// TickSize and ContractSize come from the instrument. ContractSize is units of the base asset,
	// not lots (SP4-3).
	TickSize     decimal.Decimal
	ContractSize decimal.Decimal

	RiskPerTradePct  decimal.Decimal
	MinRiskReward    decimal.Decimal
	MaxOpenPositions int
	Leverage         decimal.Decimal

	OpenPositions int
	// AccountEquity is balance plus unrealized PnL on open positions: an underwater position
	// reduces what the next order may size against, which is what stops a trader pyramiding into a
	// loser (SP4-3).
	AccountEquity decimal.Decimal
	// CommittedMargin is what the open positions already tie up.
	CommittedMargin decimal.Decimal
}

// Gate codes, in evaluation order. Exported so the frontend's compliance panel and the tests name
// the same things the server does.
const (
	CodeSessionClosed      = "SESSION_CLOSED"
	CodeStopRequired       = "ORDER_STOP_REQUIRED"
	CodeStopInvalid        = "ORDER_STOP_INVALID"
	CodeTargetInvalid      = "ORDER_TARGET_INVALID"
	CodeQuantityInvalid    = "ORDER_QUANTITY_INVALID"
	CodeStopTooWide        = "RISK_STOP_TOO_WIDE"
	CodeRiskRewardTooLow   = "RISK_REWARD_TOO_LOW"
	CodeMaxPositions       = "MAX_POSITIONS_REACHED"
	CodeInsufficientMargin = "INSUFFICIENT_MARGIN"
	CodeDrawdownBreached   = "DAILY_DRAWDOWN_BREACHED"
)

// Evaluate runs the gate and returns the first failing code, or "" if the order passes.
//
// The drawdown check is last rather than first even though a halted session refuses everything.
// That is deliberate: a trader whose account is halted *and* whose order has no stop should be told
// about the stop, because the halt is a state they already know about and the missing stop is the
// habit worth correcting. A halted session with an otherwise-valid order gets the halt.
func Evaluate(in GateInput) string {
	if !in.SessionLive {
		return CodeSessionClosed
	}
	// BR-03. The schema makes this NOT NULL as well; a zero here means the client sent one.
	if in.StopLoss.IsZero() {
		return CodeStopRequired
	}
	// A long's stop is below its entry and a short's is above. An equal stop is refused too: a
	// zero-distance stop makes risk zero, which would divide by zero in the size calculation and
	// report an infinite R-multiple afterwards.
	if in.Side.Long() && !in.StopLoss.LessThan(in.EntryPrice) {
		return CodeStopInvalid
	}
	if !in.Side.Long() && !in.StopLoss.GreaterThan(in.EntryPrice) {
		return CodeStopInvalid
	}
	if in.TakeProfit != nil {
		if in.Side.Long() && !in.TakeProfit.GreaterThan(in.EntryPrice) {
			return CodeTargetInvalid
		}
		if !in.Side.Long() && !in.TakeProfit.LessThan(in.EntryPrice) {
			return CodeTargetInvalid
		}
	}
	if !in.Quantity.IsPositive() || !Quantized(in.Quantity, in.TickSize) {
		return CodeQuantityInvalid
	}

	// Risk per trade. The refusal is on the *money at risk*, not on the stop distance alone: a wide
	// stop with a small size risks little, and refusing it would teach the opposite of position
	// sizing. The code's name is historical.
	risk := RiskAmount(in.Quantity, in.EntryPrice, in.StopLoss, in.ContractSize)
	allowed := percentOf(in.AccountEquity, in.RiskPerTradePct)
	if risk.GreaterThan(allowed) {
		return CodeStopTooWide
	}

	if in.TakeProfit != nil && in.MinRiskReward.IsPositive() {
		reward := in.TakeProfit.Sub(in.EntryPrice).Abs()
		distance := in.EntryPrice.Sub(in.StopLoss).Abs()
		if distance.IsPositive() && reward.Div(distance).LessThan(in.MinRiskReward) {
			return CodeRiskRewardTooLow
		}
	}

	if in.MaxOpenPositions > 0 && in.OpenPositions >= in.MaxOpenPositions {
		return CodeMaxPositions
	}

	if required := RequiredMargin(in.Quantity, in.EntryPrice, in.ContractSize, in.Leverage); required.GreaterThan(
		in.AccountEquity.Sub(in.CommittedMargin)) {
		return CodeInsufficientMargin
	}

	if in.Halted {
		return CodeDrawdownBreached
	}
	return ""
}

// Notional is quantity × price × contract size.
//
// ContractSize is units of the base asset rather than lots (SP4-3). Reading a unit as a lot inflates
// this by a factor of 100,000, and every downstream number stays plausible while being wrong.
func Notional(quantity, price, contractSize decimal.Decimal) decimal.Decimal {
	if !contractSize.IsPositive() {
		contractSize = decimal.NewFromInt(1)
	}
	return quantity.Mul(price).Mul(contractSize).Abs()
}

// RequiredMargin is notional ÷ leverage.
//
// Per-asset-class initial margin (FX 2%, index 5%, crypto 20%) is the more realistic model and is
// deliberately not adopted: it needs a per-contract margin table the reference data does not have,
// and it substitutes for this divisor later without changing the gate's shape or its error code.
// Leverage defaults to 1, which makes a new account cash-only.
func RequiredMargin(quantity, price, contractSize, leverage decimal.Decimal) decimal.Decimal {
	notional := Notional(quantity, price, contractSize)
	if !leverage.IsPositive() {
		return notional
	}
	return notional.Div(leverage).Round(MoneyScale)
}

// RiskAmount is the money between the entry and the stop: what this trade loses if it is wrong.
func RiskAmount(quantity, entry, stop, contractSize decimal.Decimal) decimal.Decimal {
	if !contractSize.IsPositive() {
		contractSize = decimal.NewFromInt(1)
	}
	return entry.Sub(stop).Abs().Mul(quantity).Mul(contractSize).Round(MoneyScale)
}

// SizeFromRisk converts "risk this percentage of my account" into a quantity.
//
// This is where the blinding cancels. The stop distance is in blinded price units, so the scale
// divides out of the quantity; realized PnL multiplies it back in. A trader risking 1% makes or
// loses the same 1% whatever scale the feed was blinded with — which is the property that makes a
// blinded simulator produce meaningful numbers rather than arbitrary ones.
//
// The result is floored to the tick, never rounded up: rounding up would make a size the gate then
// refuses for exceeding the very limit it was derived from.
func SizeFromRisk(equity, riskPct, entry, stop, contractSize, tickSize decimal.Decimal) decimal.Decimal {
	distance := entry.Sub(stop).Abs()
	if !distance.IsPositive() || !equity.IsPositive() || !riskPct.IsPositive() {
		return decimal.Zero
	}
	if !contractSize.IsPositive() {
		contractSize = decimal.NewFromInt(1)
	}
	raw := percentOf(equity, riskPct).Div(distance).Div(contractSize)
	return FloorToTick(raw, tickSize)
}

// FloorToTick snaps a quantity down to a whole multiple of the tick.
func FloorToTick(value, tick decimal.Decimal) decimal.Decimal {
	if !tick.IsPositive() {
		return value
	}
	steps := value.Div(tick).Floor()
	return steps.Mul(tick)
}

// Quantized reports whether a value sits on the instrument's tick grid.
//
// An order for 1.234567 units of something quoted in 0.00001 steps is not a precision issue, it is a
// client that has not been told what the instrument trades in — so it is refused rather than
// rounded, for the same reason no other check silently corrects.
func Quantized(value, tick decimal.Decimal) bool {
	if !tick.IsPositive() {
		return true
	}
	return value.Div(tick).Sub(value.Div(tick).Round(0)).IsZero()
}

func percentOf(value, pct decimal.Decimal) decimal.Decimal {
	return value.Mul(pct).Div(decimal.NewFromInt(100))
}
