package execution

import (
	"testing"

	domainfeed "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/feed"
	"github.com/shopspring/decimal"
)

func bar(open, high, low, close string) domainfeed.Bar {
	return domainfeed.Bar{Open: dec(open), High: dec(high), Low: dec(low), Close: dec(close)}
}

var conditions = Conditions{SpreadTicks: 1, MaxSlippageTicks: 2, TickSize: dec("0.00001")}

// A fill at the current bar's close would be a fill at a price the trader had already seen, which
// is the hindsight the whole product removes. The next open is the first price that exists after
// the decision.
func TestMarketFillCrossesTheSpreadAtTheNextOpenAndSlippageOnlyCosts(t *testing.T) {
	next := bar("1.10000", "1.10500", "1.09500", "1.10200")

	// One tick of half-spread, no slippage: a buy pays up, a sell receives down.
	if got := MarketFill(next, SideBuy, conditions, 0).String(); got != "1.10001" {
		t.Errorf("buy = %s, want 1.10001", got)
	}
	if got := MarketFill(next, SideSell, conditions, 0).String(); got != "1.09999" {
		t.Errorf("sell = %s, want 1.09999", got)
	}

	// Slippage is adverse for both sides. Modelling it two-sided would let the simulator's random
	// number generator lift a trader's expectancy, which is the one thing a practice tool must not
	// do.
	buy := MarketFill(next, SideBuy, conditions, 2)
	sell := MarketFill(next, SideSell, conditions, 2)
	if !buy.GreaterThan(next.Open) || !sell.LessThan(next.Open) {
		t.Errorf("slippage paid the trader: buy %s, sell %s, open %s", buy, sell, next.Open)
	}
	if got := buy.String(); got != "1.10003" {
		t.Errorf("buy with 2 ticks = %s, want 1.10003", got)
	}
}

func TestRestingOrdersFillWhenPriceComesToThem(t *testing.T) {
	limitBuy := Order{Type: OrderLimit, Side: SideBuy, LimitPrice: ptr("1.09000")}
	// Price never reaches it.
	if _, filled := RestingFill(limitBuy, bar("1.10000", "1.10500", "1.09500", "1.10200")); filled {
		t.Error("a limit buy filled without price trading down to it")
	}
	// Price trades through it: filled at the limit.
	price, filled := RestingFill(limitBuy, bar("1.10000", "1.10500", "1.08800", "1.09200"))
	if !filled || price.String() != "1.09" {
		t.Errorf("limit buy = %s filled=%v, want 1.09", price, filled)
	}
}

// The asymmetry is real and is the reason a stop is not a guarantee of price: a gap past a limit
// pays better than asked, a gap through a stop pays worse.
func TestAGapPastARestingOrderFillsAtTheGapPrice(t *testing.T) {
	limitBuy := Order{Type: OrderLimit, Side: SideBuy, LimitPrice: ptr("1.09000")}
	price, filled := RestingFill(limitBuy, bar("1.08500", "1.08900", "1.08000", "1.08700"))
	if !filled || price.String() != "1.085" {
		t.Errorf("gapped limit buy = %s filled=%v, want the open 1.085", price, filled)
	}

	stopBuy := Order{Type: OrderStop, Side: SideBuy, LimitPrice: ptr("1.11000")}
	price, filled = RestingFill(stopBuy, bar("1.11500", "1.12000", "1.11400", "1.11800"))
	if !filled || price.String() != "1.115" {
		t.Errorf("gapped stop buy = %s filled=%v, want the open 1.115 — worse than the stop", price, filled)
	}
}

// 04-AC-6. With OHLC data the path inside the bar is unknown, so this is an assumption either way —
// and the favourable one flatters every result the product produces.
func TestABarCoveringBothStopAndTargetTakesTheStop(t *testing.T) {
	trade := Trade{Side: SideBuy, Quantity: dec("10000"), EntryPrice: dec("1.10000"),
		StopLoss: dec("1.09000"), TakeProfit: ptr("1.11000")}

	// A bar whose range covers both.
	got := ResolveOpen(trade, bar("1.10000", "1.11500", "1.08500", "1.10000"))
	if !got.Closed || got.Exit != ExitStop {
		t.Fatalf("resolution = %+v, want the stop", got)
	}
	if got.Price.String() != "1.09" {
		t.Errorf("stop price = %s, want 1.09", got.Price)
	}

	// A bar that only reaches the target closes at the target.
	got = ResolveOpen(trade, bar("1.10000", "1.11500", "1.09500", "1.11200"))
	if !got.Closed || got.Exit != ExitTarget {
		t.Errorf("resolution = %+v, want the target", got)
	}
	// A bar that reaches neither leaves it open.
	if ResolveOpen(trade, bar("1.10000", "1.10500", "1.09500", "1.10200")).Closed {
		t.Error("a position closed on a bar that touched neither level")
	}
}

func TestAGapThroughAStopFillsAtTheGapNotTheStop(t *testing.T) {
	trade := Trade{Side: SideBuy, Quantity: dec("10000"), EntryPrice: dec("1.10000"), StopLoss: dec("1.09000")}
	got := ResolveOpen(trade, bar("1.08000", "1.08500", "1.07500", "1.08200"))
	if !got.Closed || got.Exit != ExitStop {
		t.Fatalf("resolution = %+v, want the stop", got)
	}
	// A stop is an instruction to leave, not a promise about where. Filling at 1.09 here would be
	// the simulator inventing liquidity that was not there.
	if got.Price.String() != "1.08" {
		t.Errorf("price = %s, want the gap open 1.08", got.Price)
	}
}

func TestAShortsLevelsAreTheMirrorOfALongs(t *testing.T) {
	short := Trade{Side: SideSell, Quantity: dec("10000"), EntryPrice: dec("1.10000"),
		StopLoss: dec("1.11000"), TakeProfit: ptr("1.09000")}

	// Price rising takes a short's stop.
	if got := ResolveOpen(short, bar("1.10000", "1.11500", "1.09800", "1.11200")); got.Exit != ExitStop {
		t.Errorf("resolution = %+v, want the stop", got)
	}
	// Price falling takes its target.
	if got := ResolveOpen(short, bar("1.10000", "1.10200", "1.08500", "1.08800")); got.Exit != ExitTarget {
		t.Errorf("resolution = %+v, want the target", got)
	}
	// And a short profits when price falls.
	if got := RealizedPnL(short, dec("1.09000"), dec("1")).String(); got != "100" {
		t.Errorf("short PnL = %s, want 100", got)
	}
}

func TestRMultipleIsSignedAndScaledByTheRiskTaken(t *testing.T) {
	trade := Trade{Side: SideBuy, Quantity: dec("10000"), EntryPrice: dec("1.10000"), StopLoss: dec("1.09000")}
	if got := RMultiple(trade, dec("1.12000")).String(); got != "2" {
		t.Errorf("a 2R win = %s, want 2", got)
	}
	if got := RMultiple(trade, dec("1.09000")).String(); got != "-1" {
		t.Errorf("a stop-out = %s, want -1", got)
	}
	// A zero-distance stop would divide by zero. The gate refuses one, and this is the second line
	// of defence: an infinite R-multiple in the ledger would poison every aggregate above it.
	if got := RMultiple(Trade{Side: SideBuy, EntryPrice: dec("1.1"), StopLoss: dec("1.1")}, dec("1.2")); !got.IsZero() {
		t.Errorf("R with no risk = %s, want 0", got)
	}
}

// The excursions are what separate "the idea was wrong" from "the idea was right and the stop was
// in the wrong place", so they are measured at the bar's extremes rather than at its close.
// Excursions are non-negative price distances, in the same units as the stop.
//
// They used to be money figures signed by direction, so an adverse excursion was negative and could
// not be compared to the stop distance it exists to be compared to. The sentence MAE is for is "it
// went 0.6 of my stop against me before it worked", and that needs a distance: money is recoverable
// by multiplying through by the size, while the reverse needs a size the post-mortem may be varying
// across the positions it is comparing.
func TestExcursionsAreMeasuredAtTheExtremesNotTheClose(t *testing.T) {
	trade := Trade{Side: SideBuy, Quantity: dec("10000"), EntryPrice: dec("1.10000"), StopLoss: dec("1.09000")}
	adverse, favorable := ExcursionsFor(trade, bar("1.10000", "1.10800", "1.09400", "1.10000"))
	// The low is 60 ticks below the entry, which is 0.6 of a 100-tick stop: the trade was well
	// inside its stop at the worst point, even though the bar closed flat.
	if got := adverse.String(); got != "0.006" {
		t.Errorf("adverse = %s, want 0.006 (the distance to the low, not the flat close)", got)
	}
	if got := favorable.String(); got != "0.008" {
		t.Errorf("favorable = %s, want 0.008 (the distance to the high)", got)
	}
	// And the same bar against a short: the extremes swap, so what was adverse for the long is the
	// favourable side here. Without this the sign convention could be inverted and the long case
	// alone would not notice.
	short := Trade{Side: SideSell, Quantity: dec("10000"), EntryPrice: dec("1.10000"), StopLoss: dec("1.11000")}
	adverseShort, favorableShort := ExcursionsFor(short, bar("1.10000", "1.10800", "1.09400", "1.10000"))
	if got := adverseShort.String(); got != "0.008" {
		t.Errorf("short adverse = %s, want 0.008 (the high is against a short)", got)
	}
	if got := favorableShort.String(); got != "0.006" {
		t.Errorf("short favorable = %s, want 0.006 (the low is in a short's favour)", got)
	}
	// A bar that only went the trader's way has no adverse excursion, not a positive one.
	adverse, _ = ExcursionsFor(trade, bar("1.10100", "1.10800", "1.10050", "1.10700"))
	if !adverse.IsZero() {
		t.Errorf("adverse = %s on a bar that never went against, want 0", adverse)
	}
}

func TestUnrealizedPnLIsSignedBySide(t *testing.T) {
	long := Trade{Side: SideBuy, Quantity: dec("10000"), EntryPrice: dec("1.10000")}
	short := Trade{Side: SideSell, Quantity: dec("10000"), EntryPrice: dec("1.10000")}
	at := dec("1.10500")
	if got := long.UnrealizedPnL(at).String(); got != "50" {
		t.Errorf("long = %s, want 50", got)
	}
	if got := short.UnrealizedPnL(at).String(); got != "-50" {
		t.Errorf("short = %s, want -50", got)
	}
	if !long.UnrealizedPnL(at).Equal(short.UnrealizedPnL(at).Neg()) {
		t.Error("the two sides are not mirrors of each other")
	}
}

func TestDecimalThroughout(t *testing.T) {
	// NFR-08. Guarded here because this package is where money arithmetic concentrates, and the
	// classic failure is a single float64 helper that rounds a cent away per trade.
	var _ decimal.Decimal = RealizedPnL(Trade{Side: SideBuy, Quantity: dec("1"), EntryPrice: dec("1")}, dec("2"), dec("1"))
}

// TestRMultipleIsMeasuredAgainstTheStopThePositionWasSizedOn is what makes breakeven usable.
//
// 1R is fixed when the position opens, because that is the distance the size was derived from. Read
// from the *live* stop instead, a trader who moved their stop to entry had a risk of zero, and every
// R-multiple after that divided by zero and came back 0.0000 — so a trade that lost real money
// reported as a scratch, and the discipline index's central number was silently destroyed by the one
// action it most wants to reward.
func TestRMultipleIsMeasuredAgainstTheStopThePositionWasSizedOn(t *testing.T) {
	trade := Trade{
		Side: SideBuy, Quantity: dec("10000"), EntryPrice: dec("1.10000"),
		InitialStopLoss: dec("1.09000"),
		// Moved to breakeven after the trade went the trader's way.
		StopLoss: dec("1.10000"),
	}
	// Stopped out at breakeven, minus a tick of friction: a small real loss, not a 0R scratch and
	// certainly not a division by zero.
	if got := RMultiple(trade, dec("1.09990")).String(); got != "-0.01" {
		t.Errorf("R at breakeven = %s, want -0.01 of the original 100-tick risk", got)
	}
	// And a winner is still measured in the same units.
	if got := RMultiple(trade, dec("1.12000")).String(); got != "2" {
		t.Errorf("R at +200 ticks = %s, want 2", got)
	}
	// Risk() is 1R and does not move with the stop; LiveRisk() is what is still on the table.
	if got := trade.Risk().String(); got != "0.01" {
		t.Errorf("Risk() = %s, want the original 0.01", got)
	}
	if !trade.LiveRisk().IsZero() {
		t.Errorf("LiveRisk() = %s at breakeven, want 0", trade.LiveRisk())
	}
}

// A row written before initial_stop_loss existed carries a zero there. Falling back to the live stop
// is the best available answer and is exactly right for any position whose stop never moved.
func TestRMultipleFallsBackToTheLiveStopForAnOlderTrade(t *testing.T) {
	legacy := Trade{Side: SideBuy, Quantity: dec("10000"), EntryPrice: dec("1.10000"), StopLoss: dec("1.09000")}
	if got := RMultiple(legacy, dec("1.12000")).String(); got != "2" {
		t.Errorf("R = %s on a trade with no recorded initial stop, want 2", got)
	}
}
