package execution

import (
	domainfeed "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/feed"
	"github.com/shopspring/decimal"
)

// The fill engine (FR-EXEC-09).
//
// Every ambiguous case here resolves against the trader. That is a product decision, not
// conservatism for its own sake: the alternative flatters every result, and a simulator that
// flatters is one whose lessons do not survive contact with a real venue. Each such choice is
// commented where it is made, so a future reader can disagree with the reasoning rather than
// discover the behaviour.

// Conditions are the venue's frictions.
type Conditions struct {
	// SpreadTicks is the full bid-ask width. A buy pays half of it above the mid and a sell
	// receives half below, which is what a mid-priced OHLC series implies.
	SpreadTicks int
	// MaxSlippageTicks bounds the seeded draw. Zero means no slippage, which is a fine setting for
	// a teaching feed and a dishonest one for a fast market.
	MaxSlippageTicks int
	TickSize         decimal.Decimal
}

// DefaultConditions are a liquid-major's frictions: a one-tick spread and up to two ticks of
// slippage. Deliberately mild — the point is that friction exists and is never favourable, not that
// it is punishing.
var DefaultConditions = Conditions{SpreadTicks: 1, MaxSlippageTicks: 2}

// MarketFill prices an order that crosses the spread at the next bar's open.
//
// The *next* bar's open, never this bar's close. A trader pressing buy has seen the current bar; a
// fill at its close would be a fill at a price they already knew, which is the hindsight this whole
// product removes. The next open is the first price that exists after the decision.
//
// Slippage is applied in the adverse direction only. Real slippage is two-sided, and modelling it
// that way here would let a trader's expectancy be lifted by the simulator's random number
// generator — so it costs and never pays.
func MarketFill(next domainfeed.Bar, side Side, conditions Conditions, draw int) decimal.Decimal {
	tick := conditions.TickSize
	if !tick.IsPositive() {
		tick = decimal.New(1, -PriceScale)
	}
	// Half the spread each way, rounded up to a whole tick so a one-tick spread costs a tick rather
	// than disappearing into the price scale.
	half := decimal.NewFromInt(int64(conditions.SpreadTicks)).Div(decimal.NewFromInt(2)).Ceil()
	adverse := half.Add(decimal.NewFromInt(int64(draw))).Mul(tick)
	if side.Long() {
		return next.Open.Add(adverse).Round(PriceScale)
	}
	return next.Open.Sub(adverse).Round(PriceScale)
}

// RestingFill reports whether a resting order is taken out by a bar, and at what price.
//
// The two order types mean opposite things about price direction, and both have a gap case:
//
//   - A **limit** buy sits below the market and fills when price trades down to it. If the bar opens
//     *below* the limit the market has gapped past it, and the fill is at the open — better than the
//     limit, which is what actually happens at a venue.
//   - A **stop** buy sits above the market and fills when price trades up to it. A gap through it
//     fills at the open — worse than the stop. That asymmetry is real and is the reason a stop is
//     not a guarantee of price.
func RestingFill(order Order, bar domainfeed.Bar) (decimal.Decimal, bool) {
	if order.LimitPrice == nil {
		return decimal.Zero, false
	}
	trigger := *order.LimitPrice
	switch {
	case order.Type == OrderLimit && order.Side.Long():
		if bar.Open.LessThanOrEqual(trigger) {
			return bar.Open, true // gapped below the limit: filled better, at the open
		}
		return trigger, bar.Low.LessThanOrEqual(trigger)
	case order.Type == OrderLimit && !order.Side.Long():
		if bar.Open.GreaterThanOrEqual(trigger) {
			return bar.Open, true
		}
		return trigger, bar.High.GreaterThanOrEqual(trigger)
	case order.Type == OrderStop && order.Side.Long():
		if bar.Open.GreaterThanOrEqual(trigger) {
			return bar.Open, true // gapped through the stop: filled worse, at the open
		}
		return trigger, bar.High.GreaterThanOrEqual(trigger)
	default: // stop sell
		if bar.Open.LessThanOrEqual(trigger) {
			return bar.Open, true
		}
		return trigger, bar.Low.LessThanOrEqual(trigger)
	}
}

// Resolution is what a bar did to an open position.
type Resolution struct {
	Exit   ExitReason
	Price  decimal.Decimal
	Closed bool
}

// ResolveOpen decides whether a bar takes a position out, and at what price.
//
// **When a bar's range covers both the stop and the target, the stop is taken first.** With OHLC
// data the path inside the bar is unknown, so this is an assumption either way — and the favourable
// assumption would flatter every result the product produces. It is documented, asserted, and stated
// in the UI rather than left as an implementation detail somebody discovers from a surprising fill.
//
// A gap through the stop fills at the gap price, not the stop price: a stop is an instruction to
// leave, not a promise about where.
func ResolveOpen(trade Trade, bar domainfeed.Bar) Resolution {
	stopHit, stopPrice := touched(bar, trade.StopLoss, !trade.Side.Long())
	if stopHit {
		return Resolution{Exit: ExitStop, Price: stopPrice, Closed: true}
	}
	if trade.TakeProfit != nil {
		targetHit, targetPrice := touched(bar, *trade.TakeProfit, trade.Side.Long())
		if targetHit {
			return Resolution{Exit: ExitTarget, Price: targetPrice, Closed: true}
		}
	}
	return Resolution{}
}

// touched reports whether a bar reached a level from below (upward=true) or above, and the price it
// would fill at — the level itself, or the open when the bar gapped past it.
func touched(bar domainfeed.Bar, level decimal.Decimal, upward bool) (bool, decimal.Decimal) {
	if upward {
		if bar.Open.GreaterThanOrEqual(level) {
			return true, bar.Open
		}
		return bar.High.GreaterThanOrEqual(level), level
	}
	if bar.Open.LessThanOrEqual(level) {
		return true, bar.Open
	}
	return bar.Low.LessThanOrEqual(level), level
}

// ExcursionsFor extends a position's best and worst marks with one more bar.
//
// Measured at the bar's extremes rather than its close, because the question the post-mortem asks is
// "how much heat did this take" — and a trade that went 3R against before coming back was a
// different experience from one that never moved, however identical their closes.
func ExcursionsFor(trade Trade, bar domainfeed.Bar) (adverse, favorable decimal.Decimal) {
	worst, best := bar.Low, bar.High
	if !trade.Side.Long() {
		worst, best = bar.High, bar.Low
	}
	// Price distances, non-negative, in the units the stop is in.
	//
	// They were money figures signed by direction, which made an adverse excursion negative and put
	// it in a different unit from the stop it is meant to be compared against. The whole use of MAE
	// is "the trade went 1.4x my stop distance against me before it worked", and that sentence needs
	// a distance. Money is recoverable from it by multiplying through by size; the reverse needs the
	// size, which the post-mortem may be comparing across positions of different sizes.
	adverse = signedMove(trade.Side, trade.EntryPrice, worst).Neg()
	favorable = signedMove(trade.Side, trade.EntryPrice, best)
	if adverse.IsNegative() {
		adverse = decimal.Zero
	}
	if favorable.IsNegative() {
		favorable = decimal.Zero
	}
	return adverse, favorable
}

// RealizedPnL is the money a closed position made or lost.
func RealizedPnL(trade Trade, exit decimal.Decimal, contractSize decimal.Decimal) decimal.Decimal {
	if !contractSize.IsPositive() {
		contractSize = decimal.NewFromInt(1)
	}
	move := exit.Sub(trade.EntryPrice)
	if !trade.Side.Long() {
		move = move.Neg()
	}
	return move.Mul(trade.Quantity).Mul(contractSize).Round(MoneyScale)
}

// RMultiple expresses a result in units of the risk taken.
//
// The unit the whole product thinks in: "I lost 1R" and "I made 2.4R" compare across instruments,
// account sizes and blinding scales, which a dollar figure does not.
func RMultiple(trade Trade, exit decimal.Decimal) decimal.Decimal {
	risk := trade.Risk()
	if !risk.IsPositive() {
		return decimal.Zero
	}
	move := exit.Sub(trade.EntryPrice)
	if !trade.Side.Long() {
		move = move.Neg()
	}
	return move.Div(risk).Round(4)
}

// signedMove is the price move in the trade's favour: positive when the position is winning at that
// price, negative when it is losing, whichever side it is on. Written once so the four places that
// need the direction cannot each get it backwards.
func signedMove(side Side, entry, at decimal.Decimal) decimal.Decimal {
	move := at.Sub(entry)
	if !side.Long() {
		move = move.Neg()
	}
	return move
}
