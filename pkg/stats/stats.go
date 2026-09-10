// Package stats holds the numerical kernel the domain needs but must not contain.
//
// `tools/archlint` forbids float32 and float64 anywhere under a domain package, because a float
// that touches money silently loses precision and money is what this system moves. That rule is
// worth keeping absolute, so the arithmetic that genuinely wants floating point — standard
// deviations, square roots, drawing a random value in a range — lives here instead, behind an
// interface that takes decimals in and hands decimals back.
//
// Nothing in this package should ever be used for a price, a balance or a PnL. It exists for
// descriptive statistics about a series and for sampling.
package stats

import (
	"math"

	"github.com/shopspring/decimal"
)

// Source produces a uniform value in [0,1). Injected rather than global so a feed build is
// reproducible from a recorded seed.
type Source func() float64

// Volatility is the sample standard deviation of bar-to-bar returns over the closes. It is a shape
// descriptor for a window, never a risk number a trade is sized from.
func Volatility(closes []decimal.Decimal) decimal.Decimal {
	if len(closes) < 2 {
		return decimal.Zero
	}
	returns := make([]float64, 0, len(closes)-1)
	for i := 1; i < len(closes); i++ {
		previous, _ := closes[i-1].Float64()
		current, _ := closes[i].Float64()
		if previous == 0 {
			continue
		}
		returns = append(returns, (current-previous)/previous)
	}
	if len(returns) == 0 {
		return decimal.Zero
	}
	var mean float64
	for _, value := range returns {
		mean += value
	}
	mean /= float64(len(returns))
	var variance float64
	for _, value := range returns {
		variance += (value - mean) * (value - mean)
	}
	return decimal.NewFromFloat(math.Sqrt(variance / float64(len(returns)))).Round(8)
}

// TrendPersistence is the net move across the series divided by the total path travelled. It sits
// in [0,1]: near 1 is a clean directional trend, near 0 is chop that ends where it started.
func TrendPersistence(closes []decimal.Decimal) decimal.Decimal {
	if len(closes) < 2 {
		return decimal.Zero
	}
	var path float64
	for i := 1; i < len(closes); i++ {
		previous, _ := closes[i-1].Float64()
		current, _ := closes[i].Float64()
		path += math.Abs(current - previous)
	}
	if path == 0 {
		return decimal.Zero
	}
	first, _ := closes[0].Float64()
	last, _ := closes[len(closes)-1].Float64()
	return decimal.NewFromFloat(math.Abs(last-first) / path).Round(8)
}

// Between draws a decimal uniformly in [low, high).
func Between(source Source, low, high decimal.Decimal) decimal.Decimal {
	if source == nil || high.LessThanOrEqual(low) {
		return low
	}
	return low.Add(high.Sub(low).Mul(decimal.NewFromFloat(source())))
}

// Index draws a uniform index in [0, count). It returns 0 for an empty range rather than panicking,
// so a caller that has already checked for emptiness is not forced to check again.
func Index(source Source, count int) int {
	if source == nil || count <= 0 {
		return 0
	}
	picked := int(source() * float64(count))
	if picked >= count {
		picked = count - 1
	}
	return picked
}
