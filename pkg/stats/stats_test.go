package stats

import (
	"math"
	"testing"

	"github.com/shopspring/decimal"
)

// Review finding BE-02-4. This kernel decides a feed's difficulty band — what the trader sees in
// the catalogue — and in Sprint 08 it becomes the weight the stratified sampler draws against. It
// had no tests at all.
//
// These are mostly property assertions rather than fixed expected values: the properties are what
// the callers rely on, and a fixed value would pin an implementation detail instead.

func decimals(values ...float64) []decimal.Decimal {
	out := make([]decimal.Decimal, 0, len(values))
	for _, value := range values {
		out = append(out, decimal.NewFromFloat(value))
	}
	return out
}

// A series that never moves has no volatility. If this were non-zero, every flat window would be
// banded as if it had structure.
func TestVolatilityOfAFlatSeriesIsZero(t *testing.T) {
	t.Parallel()
	if got := Volatility(decimals(100, 100, 100, 100)); !got.IsZero() {
		t.Errorf("Volatility(flat) = %s, want 0", got)
	}
}

// Constant *returns* are also zero volatility — volatility is the spread of returns, not their
// size. A series compounding steadily at 1% a bar is trending, not volatile, and banding it as
// volatile would mislabel the calmest kind of trend there is.
func TestVolatilityOfAConstantGrowthSeriesIsZero(t *testing.T) {
	t.Parallel()
	series := make([]decimal.Decimal, 0, 20)
	value := 100.0
	for i := 0; i < 20; i++ {
		series = append(series, decimal.NewFromFloat(value))
		value *= 1.01
	}
	if got := Volatility(series); got.GreaterThan(decimal.NewFromFloat(1e-6)) {
		t.Errorf("Volatility(constant growth) = %s, want ~0", got)
	}
}

func TestVolatilityRisesWithDispersion(t *testing.T) {
	t.Parallel()
	calm := Volatility(decimals(100, 100.1, 99.9, 100.05, 99.95))
	wild := Volatility(decimals(100, 110, 92, 108, 90))
	if !wild.GreaterThan(calm) {
		t.Errorf("Volatility(wild)=%s is not greater than Volatility(calm)=%s", wild, calm)
	}
}

func TestVolatilityIsNeverNegative(t *testing.T) {
	t.Parallel()
	for _, series := range [][]decimal.Decimal{
		decimals(100, 90, 110, 80, 120),
		decimals(5, 4, 3, 2, 1),
		decimals(1, 2),
	} {
		if Volatility(series).IsNegative() {
			t.Errorf("Volatility(%v) is negative", series)
		}
	}
}

// Degenerate input must return a value, not panic: a feed window can legitimately be short, and a
// zero close is a data problem the builder should survive rather than crash on.
func TestVolatilityHandlesDegenerateInput(t *testing.T) {
	t.Parallel()
	for name, series := range map[string][]decimal.Decimal{
		"empty":     {},
		"one bar":   decimals(100),
		"zero open": decimals(0, 100, 101),
		"all zero":  decimals(0, 0, 0),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := Volatility(series); got.IsNegative() {
				t.Errorf("Volatility(%s) = %s", name, got)
			}
		})
	}
}

// The two ends of the scale the difficulty bands are read off.
func TestTrendPersistenceIsOneForAMonotonicSeries(t *testing.T) {
	t.Parallel()
	if got := TrendPersistence(decimals(100, 101, 102, 103, 104)); !got.Equal(decimal.NewFromInt(1)) {
		t.Errorf("TrendPersistence(monotonic) = %s, want 1", got)
	}
}

func TestTrendPersistenceIsZeroWhenTheSeriesEndsWhereItStarted(t *testing.T) {
	t.Parallel()
	if got := TrendPersistence(decimals(100, 110, 100)); !got.IsZero() {
		t.Errorf("TrendPersistence(round trip) = %s, want 0", got)
	}
}

func TestTrendPersistenceStaysWithinItsStatedRange(t *testing.T) {
	t.Parallel()
	one := decimal.NewFromInt(1)
	for _, series := range [][]decimal.Decimal{
		decimals(100, 105, 98, 112, 90, 130),
		decimals(50, 49, 51, 48, 52, 47),
		decimals(1, 1000),
		decimals(1000, 1),
	} {
		got := TrendPersistence(series)
		if got.IsNegative() || got.GreaterThan(one) {
			t.Errorf("TrendPersistence(%v) = %s, outside [0,1]", series, got)
		}
	}
}

// Direction must not change the reading: a clean downtrend is exactly as persistent as a clean
// uptrend, and a difficulty band that treated one as harder would be describing the trader's bias
// rather than the market's shape.
func TestTrendPersistenceIsDirectionless(t *testing.T) {
	t.Parallel()
	up := TrendPersistence(decimals(100, 102, 105, 109, 114))
	down := TrendPersistence(decimals(114, 109, 105, 102, 100))
	if !up.Equal(down) {
		t.Errorf("up = %s but down = %s; persistence must not depend on direction", up, down)
	}
}

func TestTrendPersistenceHandlesDegenerateInput(t *testing.T) {
	t.Parallel()
	for name, series := range map[string][]decimal.Decimal{
		"empty":   {},
		"one bar": decimals(100),
		"flat":    decimals(100, 100, 100),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := TrendPersistence(series); !got.IsZero() {
				t.Errorf("TrendPersistence(%s) = %s, want 0", name, got)
			}
		})
	}
}

func TestBetweenStaysInRange(t *testing.T) {
	t.Parallel()
	low, high := decimal.NewFromInt(10), decimal.NewFromInt(20)
	for _, draw := range []float64{0, 0.5, 0.999999} {
		got := Between(func() float64 { return draw }, low, high)
		if got.LessThan(low) || got.GreaterThanOrEqual(high) {
			t.Errorf("Between(%v) = %s, outside [10,20)", draw, got)
		}
	}
}

func TestBetweenDegradesRatherThanPanicking(t *testing.T) {
	t.Parallel()
	low, high := decimal.NewFromInt(10), decimal.NewFromInt(20)
	if got := Between(nil, low, high); !got.Equal(low) {
		t.Errorf("Between(nil source) = %s, want the low bound", got)
	}
	// An inverted range is a caller bug; returning the low bound keeps a feed build going rather
	// than failing it over a degenerate window.
	if got := Between(func() float64 { return 0.5 }, high, low); !got.Equal(high) {
		t.Errorf("Between(inverted) = %s, want the first bound", got)
	}
}

// Index feeds the "randomize an unseen feed" pick. Returning count would index past the end of the
// candidate slice, which is a panic in the one place a trader touches every session.
func TestIndexNeverReturnsTheCount(t *testing.T) {
	t.Parallel()
	for _, count := range []int{1, 2, 7, 1200} {
		for _, draw := range []float64{0, 0.5, 0.999999999, 1} {
			got := Index(func() float64 { return draw }, count)
			if got < 0 || got >= count {
				t.Errorf("Index(%v, %d) = %d, outside [0,%d)", draw, count, got, count)
			}
		}
	}
}

func TestIndexHandlesAnEmptyRange(t *testing.T) {
	t.Parallel()
	if got := Index(func() float64 { return 0.5 }, 0); got != 0 {
		t.Errorf("Index(empty) = %d, want 0", got)
	}
	if got := Index(nil, 10); got != 0 {
		t.Errorf("Index(nil source) = %d, want 0", got)
	}
}

// Uniformity, roughly: a biased picker would quietly curate which feeds a trader meets, which is
// the curve-fitting the product exists to prevent.
func TestIndexIsApproximatelyUniform(t *testing.T) {
	t.Parallel()
	const buckets, draws = 8, 80_000
	counts := make([]int, buckets)
	position := 0
	source := func() float64 {
		// A deterministic sweep rather than a random source: it makes the assertion exact and the
		// test non-flaky, while still exercising the whole [0,1) range.
		position++
		return math.Mod(float64(position)*0.0001234567, 1)
	}
	for i := 0; i < draws; i++ {
		counts[Index(source, buckets)]++
	}
	expected := draws / buckets
	for bucket, count := range counts {
		if math.Abs(float64(count-expected)) > float64(expected)*0.1 {
			t.Errorf("bucket %d got %d draws, want within 10%% of %d", bucket, count, expected)
		}
	}
}
