package feed

import (
	"time"

	"github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/market"
	"github.com/shopspring/decimal"
)

// Aggregate rolls a 1m base series up to a higher timeframe. This runs at load time, not at query
// time: aggregating on read would put the cost inside the replay hot path, where the 15ms budget
// lives (NFR-01).
//
// Buckets align to the UTC epoch, so a 1h bar always starts on the hour and a 1d bar always starts
// at UTC midnight regardless of where the input happens to begin. A trailing partial bucket is
// dropped — a bar that only saw part of its interval is not a bar, and publishing it as one would
// show a trader a candle that never closed.
func Aggregate(bars []market.Bar, target market.Timeframe) []market.Bar {
	interval, ok := target.Duration()
	if !ok || len(bars) == 0 {
		return nil
	}
	seconds := int64(interval / time.Second)

	aggregated := make([]market.Bar, 0, len(bars)/int(max64(seconds/60, 1))+1)
	var current *market.Bar
	var bucketStart int64

	flush := func() {
		if current != nil {
			aggregated = append(aggregated, *current)
			current = nil
		}
	}

	for _, bar := range bars {
		start := bar.OpenedAt.UTC().Unix() / seconds * seconds
		if current == nil || start != bucketStart {
			flush()
			bucketStart = start
			opened := time.Unix(start, 0).UTC()
			current = &market.Bar{
				InstrumentID: bar.InstrumentID, Timeframe: target, OpenedAt: opened,
				Open: bar.Open, High: bar.High, Low: bar.Low, Close: bar.Close, Volume: bar.Volume,
			}
			continue
		}
		if bar.High.GreaterThan(current.High) {
			current.High = bar.High
		}
		if bar.Low.LessThan(current.Low) {
			current.Low = bar.Low
		}
		current.Close = bar.Close
		current.Volume = current.Volume.Add(bar.Volume)
	}

	// The final bucket is kept only if the input actually covered it. Without this the last bar of
	// every load would be a partial candle masquerading as a complete one.
	if current != nil {
		last := bars[len(bars)-1].OpenedAt.UTC().Unix()
		baseInterval, hasBase := bars[0].Timeframe.Duration()
		covered := last + int64(baseInterval/time.Second)
		if hasBase && covered >= bucketStart+seconds {
			aggregated = append(aggregated, *current)
		}
	}
	return aggregated
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// ValidateSeries reports the problems in a candidate series without stopping at the first one, so
// a bad file is diagnosed in one pass rather than one row per run.
func ValidateSeries(bars []market.Bar) []string {
	problems := make([]string, 0)
	var previous *market.Bar
	for index, bar := range bars {
		if !bar.Valid() {
			problems = append(problems, formatProblem(index, bar, "OHLC values are inconsistent"))
		}
		if bar.Open.IsNegative() || bar.Close.IsNegative() {
			problems = append(problems, formatProblem(index, bar, "price is negative"))
		}
		if previous != nil {
			if !bar.OpenedAt.After(previous.OpenedAt) {
				problems = append(problems, formatProblem(index, bar, "timestamp is not strictly increasing"))
			}
		}
		current := bar
		previous = &current
	}
	return problems
}

func formatProblem(index int, bar market.Bar, reason string) string {
	return "row " + decimal.NewFromInt(int64(index)).String() + " (" + bar.OpenedAt.UTC().Format(time.RFC3339) + "): " + reason
}
