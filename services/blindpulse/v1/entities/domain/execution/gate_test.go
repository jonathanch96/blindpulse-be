package execution

import (
	"testing"

	"github.com/shopspring/decimal"
)

func dec(value string) decimal.Decimal { return decimal.RequireFromString(value) }

func ptr(value string) *decimal.Decimal {
	parsed := dec(value)
	return &parsed
}

// valid is an order that passes every check: a long at 1.10000 risking 100 of a 10,000 account
// (1%) with a 2:1 target. Each case below breaks exactly one thing, so a failure names the check
// that moved rather than the scenario.
func valid() GateInput {
	return GateInput{
		SessionLive: true,
		Side:        SideBuy,
		Type:        OrderMarket,
		// 0.01 of stop distance at 10,000 units is 100 of risk: 1% of the account.
		Quantity:   dec("10000"),
		EntryPrice: dec("1.10000"),
		StopLoss:   dec("1.09000"),
		TakeProfit: ptr("1.12000"),

		TickSize:     dec("0.00001"),
		ContractSize: dec("1"),

		RiskPerTradePct:  dec("1"),
		MinRiskReward:    dec("2"),
		MaxOpenPositions: 5,
		Leverage:         dec("100"),

		OpenPositions: 0,
		AccountEquity: dec("10000"),
	}
}

func TestTheGateAcceptsAWellFormedOrder(t *testing.T) {
	if code := Evaluate(valid()); code != "" {
		t.Fatalf("a valid order was refused with %s", code)
	}
}

// The ten checks, each triggered on its own. The table is the specification: a check that stops
// firing, or fires for the wrong reason, shows up here as a named row rather than as a subtly
// different rejection somewhere downstream.
func TestEachCheckRefusesItsOwnCase(t *testing.T) {
	cases := []struct {
		name   string
		breaks func(*GateInput)
		want   string
	}{
		{"a closed session", func(in *GateInput) { in.SessionLive = false }, CodeSessionClosed},
		{"no stop loss", func(in *GateInput) { in.StopLoss = decimal.Zero }, CodeStopRequired},
		{"a long's stop above its entry", func(in *GateInput) { in.StopLoss = dec("1.11000") }, CodeStopInvalid},
		{"a long's stop at its entry", func(in *GateInput) { in.StopLoss = in.EntryPrice }, CodeStopInvalid},
		{"a short's stop below its entry", func(in *GateInput) {
			in.Side = SideSell
			in.StopLoss = dec("1.09000")
			in.TakeProfit = ptr("1.08000")
		}, CodeStopInvalid},
		{"a long's target below its entry", func(in *GateInput) { in.TakeProfit = ptr("1.09500") }, CodeTargetInvalid},
		{"a zero quantity", func(in *GateInput) { in.Quantity = decimal.Zero }, CodeQuantityInvalid},
		{"a negative quantity", func(in *GateInput) { in.Quantity = dec("-10000") }, CodeQuantityInvalid},
		{"a quantity off the tick grid", func(in *GateInput) { in.Quantity = dec("10000.000005") }, CodeQuantityInvalid},
		{"risk above the per-trade limit", func(in *GateInput) { in.Quantity = dec("20000") }, CodeStopTooWide},
		{"a target below the minimum R:R", func(in *GateInput) { in.TakeProfit = ptr("1.11000") }, CodeRiskRewardTooLow},
		{"the open-position cap", func(in *GateInput) { in.OpenPositions = 5 }, CodeMaxPositions},
		{"margin the account cannot cover", func(in *GateInput) { in.Leverage = dec("1") }, CodeInsufficientMargin},
		{"a halted account", func(in *GateInput) { in.Halted = true }, CodeDrawdownBreached},
	}
	for _, testCase := range cases {
		input := valid()
		testCase.breaks(&input)
		if got := Evaluate(input); got != testCase.want {
			t.Errorf("%s: got %q, want %q", testCase.name, got, testCase.want)
		}
	}
}

// 04-AC-3's sibling: when two checks would both fail, only the first is reported. The order is the
// point — telling somebody their R:R is too low when they have not set a stop at all is noise.
func TestOnlyTheFirstFailingCheckIsReported(t *testing.T) {
	input := valid()
	input.StopLoss = decimal.Zero   // check 2
	input.Quantity = dec("1000000") // check 5 and 6
	input.OpenPositions = 99        // check 8
	input.Halted = true             // check 10
	if got := Evaluate(input); got != CodeStopRequired {
		t.Errorf("got %q, want the earliest failure %q", got, CodeStopRequired)
	}

	// And the halt does not pre-empt the habit worth correcting: a halted account submitting an
	// order with no stop is told about the stop.
	halted := valid()
	halted.Halted = true
	halted.StopLoss = decimal.Zero
	if got := Evaluate(halted); got != CodeStopRequired {
		t.Errorf("got %q, want %q — the halt should not hide a missing stop", got, CodeStopRequired)
	}
}

// SP4-3's decision, and the behaviour it exists to prevent. Sizing against balance would let a
// trader keep adding to a position that is deep underwater, because the paper loss never reaches
// the check — which is exactly what Sprint 06's discipline index is built to flag.
func TestAnUnderwaterPositionShrinksWhatTheNextOrderCanSize(t *testing.T) {
	input := valid()
	input.Leverage = dec("1")
	// Risk-per-trade is set wide enough that it cannot be the binding check. Equity is the
	// denominator of *both* limits, so lowering it moves the risk budget too — and the first check
	// to fail is the one reported. Isolating margin means taking risk out of contention.
	input.RiskPerTradePct = dec("5")
	input.AccountEquity = dec("11000") // fully funded at 1:1 leverage
	input.Quantity = dec("10000")
	if code := Evaluate(input); code != "" {
		t.Fatalf("a fully funded order was refused with %s", code)
	}

	// The same order after the open position has lost 2,000 on paper. Risk is 100 against a budget
	// of 450, so nothing before the margin check has anything to say about it.
	input.AccountEquity = dec("9000")
	if got := Evaluate(input); got != CodeInsufficientMargin {
		t.Errorf("got %q, want %q — unrealized losses must reduce what the next order can use", got, CodeInsufficientMargin)
	}
}

func TestCommittedMarginIsNotAvailableTwice(t *testing.T) {
	input := valid()
	input.Leverage = dec("1")
	input.RiskPerTradePct = dec("5")
	input.AccountEquity = dec("11000")
	input.Quantity = dec("10000")
	if code := Evaluate(input); code != "" {
		t.Fatalf("the first order was refused with %s", code)
	}
	// The same equity, with the first position's margin already tied up.
	input.CommittedMargin = dec("11000")
	if got := Evaluate(input); got != CodeInsufficientMargin {
		t.Errorf("got %q, want %q — committed margin cannot back a second position", got, CodeInsufficientMargin)
	}
}

// A target is optional: a trader running a stop and managing the exit by hand is not breaking a
// rule, and a gate that demanded one would be inventing a policy the PRD does not state.
func TestAnOrderWithoutATargetSkipsTheRiskRewardCheck(t *testing.T) {
	input := valid()
	input.TakeProfit = nil
	if code := Evaluate(input); code != "" {
		t.Errorf("an order with no target was refused with %s", code)
	}
}

func TestNotionalReadsContractSizeAsUnitsNotLots(t *testing.T) {
	// SP4-3. 10,000 units of something at 1.10 is 11,000 of notional. Reading the contract size as a
	// 100,000-unit lot would make it 1.1 billion — and every margin check would refuse everything
	// while each individual number still looked like a number.
	if got := Notional(dec("10000"), dec("1.10"), dec("1")).String(); got != "11000" {
		t.Errorf("notional = %s, want 11000", got)
	}
	if got := RequiredMargin(dec("10000"), dec("1.10"), dec("1"), dec("100")).String(); got != "110" {
		t.Errorf("margin at 100:1 = %s, want 110", got)
	}
	// Leverage 1 is cash-only: the whole notional has to be funded.
	if got := RequiredMargin(dec("10000"), dec("1.10"), dec("1"), dec("1")).String(); got != "11000" {
		t.Errorf("margin at 1:1 = %s, want 11000", got)
	}
}

// The property that makes a blinded simulator mean anything: the scale the feed was blinded with
// cancels out, so the trader's dollar risk and percentage outcome are those of the real series.
func TestPositionSizingIsInvariantUnderBlinding(t *testing.T) {
	equity, riskPct := dec("10000"), dec("1")
	contract, tick := dec("1"), dec("0.00001")

	// The same real setup seen through two different blinding scales. Prices differ by 1000x; the
	// dollars must not.
	small := SizeFromRisk(equity, riskPct, dec("1.10000"), dec("1.09000"), contract, tick)
	large := SizeFromRisk(equity, riskPct, dec("1100.00000"), dec("1090.00000"), contract, tick)

	riskSmall := RiskAmount(small, dec("1.10000"), dec("1.09000"), contract)
	riskLarge := RiskAmount(large, dec("1100.00000"), dec("1090.00000"), contract)
	if !riskSmall.Equal(riskLarge) {
		t.Errorf("risk differs with the blinding scale: %s vs %s", riskSmall, riskLarge)
	}
	if got := riskSmall.String(); got != "100" {
		t.Errorf("risk = %s, want 100 (1%% of 10,000)", got)
	}

	// And a 1R win pays the same in both.
	winSmall := RealizedPnL(Trade{Side: SideBuy, Quantity: small, EntryPrice: dec("1.10000"), StopLoss: dec("1.09000")}, dec("1.11000"), contract)
	winLarge := RealizedPnL(Trade{Side: SideBuy, Quantity: large, EntryPrice: dec("1100.00000"), StopLoss: dec("1090.00000")}, dec("1110.00000"), contract)
	if !winSmall.Equal(winLarge) {
		t.Errorf("a 1R win pays differently by scale: %s vs %s", winSmall, winLarge)
	}
}

// Rounding up would produce a size the very next check refuses for exceeding the limit it was
// derived from — a loop the trader cannot escape without understanding the rounding.
func TestSizeFromRiskFloorsToTheTickAndItsResultPassesTheGate(t *testing.T) {
	input := valid()
	// A stop distance that does not divide evenly into the risk budget.
	input.StopLoss = dec("1.09700")
	input.Quantity = SizeFromRisk(input.AccountEquity, input.RiskPerTradePct,
		input.EntryPrice, input.StopLoss, input.ContractSize, input.TickSize)
	input.TakeProfit = ptr("1.11000")
	if !input.Quantity.IsPositive() {
		t.Fatalf("sizing produced %s", input.Quantity)
	}
	if code := Evaluate(input); code != "" {
		t.Errorf("a size derived from the risk limit was refused with %s", code)
	}
}
