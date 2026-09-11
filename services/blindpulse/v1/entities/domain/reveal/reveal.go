// Package reveal holds the unblinding: the moment the product pays off.
//
// Everything else in this system is built to keep the instrument, the window and the outcome away
// from the trader. This is the one type that carries all three, which is why it is a separate
// package with a separate response type rather than a flag on the session — a conditional field is
// one refactor away from being switched on too early, and there is no undoing an early reveal.
package reveal

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// BenchmarkBuyAndHold is the only benchmark defined so far, and its label travels with the numbers.
// "You beat the market" means nothing without saying which market and how it was measured, and a
// trader who disagrees with the comparison should be able to see what it was before arguing.
const BenchmarkBuyAndHold = "buy_and_hold"

// Reveal is written once and never recomputed.
//
// Frozen rather than derived on read so the comparison always reflects the session as it was
// actually traded. A feed can be rebuilt, an instrument's history can be corrected, and a
// normalization can change — none of which should be able to alter what a trader was told about a
// session they finished months ago.
type Reveal struct {
	SessionID    uuid.UUID
	InstrumentID uuid.UUID
	RevealedAt   time.Time

	Symbol      string
	Timeframe   string
	WindowStart time.Time
	WindowEnd   time.Time

	MacroLabel *string
	MacroNotes *string
	MacroTags  []string

	BenchmarkLabel string
	// StrategyReturnPct is what the trader made, as a percentage of the balance they started with.
	// It is 0 for a session with no closed trades, which is the right answer for watching without
	// acting rather than a placeholder.
	StrategyReturnPct decimal.Decimal
	// BenchmarkReturnPct is buy-and-hold over the identical window: in at the first tradeable
	// bar's open, out at the last bar's close, unlevered.
	//
	// It is computed on *real* prices, and it has to be. The blinding map is affine, which
	// preserves every linear technique but deliberately does not preserve the percentage-return
	// series — so a benchmark computed on the blinded series would be a different number about a
	// different asset.
	BenchmarkReturnPct decimal.Decimal
	AlphaPct           decimal.Decimal

	DisciplineIndex int
}

// Alpha is strategy minus benchmark. Trivial, and it lives here so the two places that need it
// (writing the reveal, and any later recomputation) cannot disagree about the sign.
func Alpha(strategy, benchmark decimal.Decimal) decimal.Decimal {
	return strategy.Sub(benchmark)
}

// PercentScale is the precision the numbers are stored and compared at. It matches the column's
// NUMERIC(12,4), so what a test computes by hand is what the database holds.
const PercentScale = 4

// ReturnPct is (exit − entry) / entry × 100, rounded to the stored precision. A zero or negative
// entry price yields zero rather than an error or an infinity: it means the window is unusable,
// and a reveal that failed because of bad reference data would deny the trader the rest of the
// unblinding over a number they did not ask for.
func ReturnPct(entry, exit decimal.Decimal) decimal.Decimal {
	if !entry.IsPositive() {
		return decimal.Zero
	}
	return exit.Sub(entry).Div(entry).Mul(decimal.NewFromInt(100)).Round(PercentScale)
}
