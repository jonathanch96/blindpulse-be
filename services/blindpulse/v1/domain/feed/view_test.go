package feed

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/market"
	"github.com/shopspring/decimal"
)

// A higher-timeframe view is the one place this sprint could reintroduce hindsight: a completed
// 1h candle implies the whole hour, so returning one before the hour has elapsed for the trader
// would show them 59 minutes they have not been shown.

func viewBars(count int, start time.Time) []market.Bar {
	id := uuid.New()
	bars := make([]market.Bar, 0, count)
	for i := 0; i < count; i++ {
		price := decimal.NewFromInt(int64(100 + i))
		bars = append(bars, market.Bar{
			InstrumentID: id, Timeframe: market.TF15m,
			OpenedAt: start.Add(time.Duration(i) * 15 * time.Minute),
			Open:     price,
			High:     price.Add(decimal.NewFromInt(3)),
			Low:      price.Sub(decimal.NewFromInt(2)),
			Close:    price.Add(decimal.NewFromInt(1)),
			Volume:   decimal.NewFromInt(10),
		})
	}
	return bars
}

func TestAggregateViewFlagsTheFormingBar(t *testing.T) {
	t.Parallel()
	start := time.Date(2023, 3, 14, 0, 0, 0, 0, time.UTC)

	// Six 15m bars: one complete hour, then half of the next.
	rolled, forming := AggregateView(viewBars(6, start), market.TF1h)

	if len(rolled) != 2 {
		t.Fatalf("bars = %d, want the complete hour plus the forming one", len(rolled))
	}
	if !forming {
		t.Fatal("the trailing bucket covers only 2 of 4 quarters but was not flagged as forming")
	}
	// The complete bar must summarize exactly its own four quarters and nothing after them.
	if !rolled[0].Close.Equal(decimal.NewFromInt(104)) {
		t.Fatalf("completed hour close = %s, want 104 (the 4th quarter's close)", rolled[0].Close)
	}
}

func TestAggregateViewMarksAnExactBoundaryComplete(t *testing.T) {
	t.Parallel()
	start := time.Date(2023, 3, 14, 0, 0, 0, 0, time.UTC)

	// Exactly eight 15m bars: two whole hours, nothing forming.
	rolled, forming := AggregateView(viewBars(8, start), market.TF1h)

	if len(rolled) != 2 || forming {
		t.Fatalf("bars = %d, forming = %v; want 2 complete hours and nothing forming", len(rolled), forming)
	}
}

// The safety property stated plainly: whatever the trader is shown at a higher timeframe must be
// derivable from the bars they have already been given. This walks a cursor through a series and
// checks that at every position, no rolled-up bar summarizes a base bar past it.
func TestHigherTimeframeViewNeverSummarizesUnrevealedBars(t *testing.T) {
	t.Parallel()
	start := time.Date(2023, 3, 14, 0, 0, 0, 0, time.UTC)
	all := viewBars(40, start)

	for cursor := 0; cursor < len(all); cursor++ {
		revealed := all[:cursor+1]
		rolled, forming := AggregateView(revealed, market.TF1h)
		if len(rolled) == 0 {
			continue
		}

		// The highest close any rolled bar reports must exist among the revealed base bars.
		revealedCloses := make(map[string]bool, len(revealed))
		for _, bar := range revealed {
			revealedCloses[bar.Close.String()] = true
		}
		for index, bar := range rolled {
			if !revealedCloses[bar.Close.String()] {
				t.Fatalf("cursor %d: rolled bar %d closes at %s, which is not among the revealed bars",
					cursor, index, bar.Close)
			}
		}

		// A bucket is reported complete only when the cursor has actually passed its end.
		last := rolled[len(rolled)-1]
		bucketEnd := last.OpenedAt.Add(time.Hour)
		cursorEnd := revealed[len(revealed)-1].OpenedAt.Add(15 * time.Minute)
		complete := !cursorEnd.Before(bucketEnd)
		if complete == forming {
			t.Fatalf("cursor %d: bucket ending %s with the cursor at %s reported forming=%v",
				cursor, bucketEnd.Format(time.RFC3339), cursorEnd.Format(time.RFC3339), forming)
		}
	}
}

func TestAggregateViewIsEmptyForNoInput(t *testing.T) {
	t.Parallel()
	rolled, forming := AggregateView(nil, market.TF1h)
	if len(rolled) != 0 || forming {
		t.Fatalf("empty input produced %d bars, forming = %v", len(rolled), forming)
	}
}
