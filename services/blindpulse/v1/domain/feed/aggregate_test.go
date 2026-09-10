package feed

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/market"
	"github.com/shopspring/decimal"
)

func minuteBars(count int, start time.Time) []market.Bar {
	id := uuid.New()
	bars := make([]market.Bar, 0, count)
	for i := 0; i < count; i++ {
		price := decimal.NewFromInt(int64(100 + i))
		bars = append(bars, market.Bar{
			InstrumentID: id, Timeframe: market.TF1m,
			OpenedAt: start.Add(time.Duration(i) * time.Minute),
			Open:     price,
			High:     price.Add(decimal.NewFromInt(2)),
			Low:      price.Sub(decimal.NewFromInt(1)),
			Close:    price.Add(decimal.NewFromInt(1)),
			Volume:   decimal.NewFromInt(10),
		})
	}
	return bars
}

func TestAggregateRollsUpOHLCV(t *testing.T) {
	t.Parallel()
	start := time.Date(2023, 3, 14, 0, 0, 0, 0, time.UTC)

	rolled := Aggregate(minuteBars(15, start), market.TF5m)

	if len(rolled) != 3 {
		t.Fatalf("bar count = %d, want 3 complete 5m bars", len(rolled))
	}
	first := rolled[0]
	// Open is the first minute's open, close the last minute's close, high and low the extremes,
	// volume the sum. Getting any of these backwards is invisible on a chart until somebody
	// trades against it.
	if !first.Open.Equal(decimal.NewFromInt(100)) {
		t.Fatalf("open = %s, want 100", first.Open)
	}
	if !first.Close.Equal(decimal.NewFromInt(105)) {
		t.Fatalf("close = %s, want 105 (the 5th minute's close)", first.Close)
	}
	if !first.High.Equal(decimal.NewFromInt(106)) {
		t.Fatalf("high = %s, want 106", first.High)
	}
	if !first.Low.Equal(decimal.NewFromInt(99)) {
		t.Fatalf("low = %s, want 99", first.Low)
	}
	if !first.Volume.Equal(decimal.NewFromInt(50)) {
		t.Fatalf("volume = %s, want 50", first.Volume)
	}
}

// Buckets align to the UTC epoch, not to wherever the input happens to start. A 1h series that
// began at 00:07 must still produce bars on the hour, or every higher timeframe in the system is
// offset from every chart the trader has ever seen.
func TestAggregateAlignsBucketsToUTC(t *testing.T) {
	t.Parallel()
	start := time.Date(2023, 3, 14, 0, 7, 0, 0, time.UTC)

	rolled := Aggregate(minuteBars(180, start), market.TF1h)

	if len(rolled) == 0 {
		t.Fatal("no bars produced")
	}
	for _, bar := range rolled {
		if bar.OpenedAt.Minute() != 0 || bar.OpenedAt.Second() != 0 {
			t.Fatalf("1h bar opened at %s, which is not on the hour", bar.OpenedAt.Format(time.RFC3339))
		}
	}
}

// A bucket the input only partly covered is not a bar. Publishing it as one shows the trader a
// candle that never closed — and in a replay, a partial higher-timeframe bar leaks the future.
func TestAggregateDropsATrailingPartialBucket(t *testing.T) {
	t.Parallel()
	start := time.Date(2023, 3, 14, 0, 0, 0, 0, time.UTC)

	// 7 minutes of input: one complete 5m bucket, then a 2-minute remainder.
	rolled := Aggregate(minuteBars(7, start), market.TF5m)

	if len(rolled) != 1 {
		t.Fatalf("bar count = %d, want only the complete bucket", len(rolled))
	}
}

func TestValidateSeriesReportsEveryProblemInOnePass(t *testing.T) {
	t.Parallel()
	start := time.Date(2023, 3, 14, 0, 0, 0, 0, time.UTC)
	bars := minuteBars(3, start)
	bars[0].High = decimal.NewFromInt(1) // high below the low
	bars[2].OpenedAt = bars[1].OpenedAt  // not strictly increasing

	problems := ValidateSeries(bars)

	// Two distinct problems, both reported. Stopping at the first means a bad file takes one run
	// per bad row to diagnose.
	if len(problems) != 2 {
		t.Fatalf("problems = %v, want two distinct reports", problems)
	}
}
