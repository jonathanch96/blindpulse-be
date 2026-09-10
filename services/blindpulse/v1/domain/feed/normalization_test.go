package feed

import (
	"math"
	"testing"
	"time"

	"github.com/google/uuid"
	domainfeed "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/feed"
	"github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/market"
	"github.com/shopspring/decimal"
)

func dec(value string) decimal.Decimal { return decimal.RequireFromString(value) }

func fibLevel(high, low decimal.Decimal, ratio string) decimal.Decimal {
	return high.Sub(dec(ratio).Mul(high.Sub(low)))
}

// The blinding map is affine, and every technique the product teaches is linear in price. This is
// the test that says so: fib levels computed on blinded prices are exactly the blinded versions of
// the levels computed on real ones. If this fails, the trader's analysis no longer describes the
// chart they were shown, and the whole exercise is worthless.
func TestNormalizationPreservesFibonacciLevels(t *testing.T) {
	t.Parallel()
	normalization := domainfeed.Normalization{
		Offset: dec("0.4137"), Scale: dec("3.72"), VolumeScale: dec("1.5"),
	}
	realHigh, realLow := dec("1.0955"), dec("1.0788")
	blindHigh, blindLow := normalization.Apply(realHigh), normalization.Apply(realLow)

	for _, ratio := range []string{"0.236", "0.382", "0.5", "0.618", "0.786"} {
		want := normalization.Apply(fibLevel(realHigh, realLow, ratio))
		got := fibLevel(blindHigh, blindLow, ratio)
		if want.Sub(got).Abs().GreaterThan(dec("0.000000001")) {
			t.Fatalf("fib %s: blinded %s, want %s", ratio, got, want)
		}
	}
}

// Risk-to-reward is a ratio of price distances, so the offset cancels and the scale divides out.
// It must survive exactly, because the gate in Sprint 04 rejects orders on this number.
func TestNormalizationPreservesRiskReward(t *testing.T) {
	t.Parallel()
	normalization := domainfeed.Normalization{Offset: dec("12.5"), Scale: dec("0.031"), VolumeScale: dec("1")}
	entry, stop, target := dec("1.0850"), dec("1.0788"), dec("1.0955")

	realRR := target.Sub(entry).Abs().Div(entry.Sub(stop).Abs())
	e, s, tp := normalization.Apply(entry), normalization.Apply(stop), normalization.Apply(target)
	blindRR := tp.Sub(e).Abs().Div(e.Sub(s).Abs())

	if realRR.Sub(blindRR).Abs().GreaterThan(dec("0.000000001")) {
		t.Fatalf("R:R changed under normalization: real %s, blinded %s", realRR, blindRR)
	}
}

// The percentage-return series is a fingerprint: preserve it and anyone with a price database can
// identify the instrument by correlation. Breaking it is the reason the map has an offset at all,
// so assert it is actually broken rather than assuming it.
func TestNormalizationBreaksThePercentageReturnFingerprint(t *testing.T) {
	t.Parallel()
	normalization := domainfeed.Normalization{Offset: dec("0.4137"), Scale: dec("3.72"), VolumeScale: dec("1")}
	previous, current := dec("1.0850"), dec("1.0912")

	realPct := current.Sub(previous).Div(previous)
	blindPct := normalization.Apply(current).Sub(normalization.Apply(previous)).Div(normalization.Apply(previous))

	if realPct.Sub(blindPct).Abs().LessThan(dec("0.0001")) {
		t.Fatalf("percentage returns survived normalization (real %s, blinded %s) — the feed is fingerprintable", realPct, blindPct)
	}
}

func TestNormalizationRoundTrips(t *testing.T) {
	t.Parallel()
	normalization := domainfeed.Normalization{Offset: dec("7.25"), Scale: dec("0.44"), VolumeScale: dec("2")}
	price := dec("61250.75")

	restored := normalization.Invert(normalization.Apply(price))

	if restored.Sub(price).Abs().GreaterThan(dec("0.00000001")) {
		t.Fatalf("round trip lost the price: got %s, want %s", restored, price)
	}
}

// A blinded candle must stay a candle: the map is monotonic increasing, so the high stays the high.
func TestNormalizationKeepsCandleOrdering(t *testing.T) {
	t.Parallel()
	normalization := domainfeed.Normalization{Offset: dec("0.5"), Scale: dec("2.5"), VolumeScale: dec("1")}
	bar := market.Bar{
		Open: dec("1.0850"), High: dec("1.0955"), Low: dec("1.0788"), Close: dec("1.0912"),
		Volume: dec("18420"),
	}

	blinded := normalization.ApplyBar(bar, 7)

	if blinded.High.LessThan(blinded.Open) || blinded.High.LessThan(blinded.Close) ||
		blinded.Low.GreaterThan(blinded.Open) || blinded.Low.GreaterThan(blinded.Close) {
		t.Fatalf("normalization broke the candle: %+v", blinded)
	}
	if blinded.Index != 7 {
		t.Fatalf("bar index = %d, want 7", blinded.Index)
	}
}

// A feed's prices must not betray its asset class. EUR/USD near 1.08 and BTC near 60,000 have to
// come out of the builder looking like the same kind of thing, or the magnitude alone tells the
// trader what they are looking at and the blinding is decorative.
func TestDrawNormalizationHidesAssetClassMagnitude(t *testing.T) {
	t.Parallel()
	fxBuilder := &service{deps: Dependencies{Rand: sequenceRand(0.3, 0.6, 0.45, 0.8)}}
	cryptoBuilder := &service{deps: Dependencies{Rand: sequenceRand(0.3, 0.6, 0.45, 0.8)}}

	fx := fxBuilder.drawNormalization(syntheticBars(300, 1.0850, 0.0004))
	btc := cryptoBuilder.drawNormalization(syntheticBars(300, 61250.0, 25.0))

	fxMid, _ := fx.Apply(dec("1.0850")).Float64()
	btcMid, _ := btc.Apply(dec("61250.0")).Float64()

	low, _ := targetMagnitudeLow.Float64()
	high, _ := targetMagnitudeHigh.Float64()
	if fxMid < low*0.5 || fxMid > high*2 {
		t.Fatalf("fx normalized magnitude %f is outside the shared band [%f, %f]", fxMid, low*0.5, high*2)
	}
	if btcMid < low*0.5 || btcMid > high*2 {
		t.Fatalf("crypto normalized magnitude %f is outside the shared band [%f, %f]", btcMid, low*0.5, high*2)
	}
	// The two must be within one order of magnitude of each other, which is the property that
	// actually stops a trader separating them by eye.
	ratio := math.Max(fxMid, btcMid) / math.Min(fxMid, btcMid)
	if ratio > 10 {
		t.Fatalf("normalized magnitudes differ by %.1fx — asset class is inferable from price alone", ratio)
	}
}

func TestDifficultyClassification(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		volatility  string
		persistence string
		want        domainfeed.Difficulty
	}{
		{"crisis volatility", "0.020", "0.6", domainfeed.DifficultyCrisis},
		{"elevated volatility", "0.008", "0.3", domainfeed.DifficultyVolatile},
		{"compressed and directionless", "0.001", "0.05", domainfeed.DifficultyCalm},
		{"ordinary", "0.004", "0.3", domainfeed.DifficultyStandard},
		// Compressed volatility but a clean trend is not "calm" to trade — it is standard, and
		// mislabelling it would send beginners into the wrong window.
		{"compressed but trending", "0.001", "0.7", domainfeed.DifficultyStandard},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			got := difficultyFor(dec(test.volatility), dec(test.persistence))
			if got != test.want {
				t.Fatalf("difficultyFor(%s, %s) = %s, want %s", test.volatility, test.persistence, got, test.want)
			}
		})
	}
}

func sequenceRand(values ...float64) func() float64 {
	index := 0
	return func() float64 {
		value := values[index%len(values)]
		index++
		return value
	}
}

func syntheticBars(count int, base, step float64) []market.Bar {
	bars := make([]market.Bar, 0, count)
	start := time.Date(2023, 3, 14, 0, 0, 0, 0, time.UTC)
	id := uuid.New()
	for i := 0; i < count; i++ {
		price := base + float64(i%20)*step
		bars = append(bars, market.Bar{
			InstrumentID: id, Timeframe: market.TF15m,
			OpenedAt: start.Add(time.Duration(i) * 15 * time.Minute),
			Open:     decimal.NewFromFloat(price),
			High:     decimal.NewFromFloat(price + step),
			Low:      decimal.NewFromFloat(price - step),
			Close:    decimal.NewFromFloat(price + step/2),
			Volume:   decimal.NewFromInt(1000),
		})
	}
	return bars
}
