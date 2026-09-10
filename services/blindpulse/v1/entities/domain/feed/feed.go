// Package feed holds the trader-facing view of market data: an alias, a window, and the affine
// map that makes a real series unrecognizable.
//
// The blinding rule this package exists to enforce: a trader learns the instrument only at the
// reveal. That is achieved by *never sending* the identity, not by hiding it in the UI — a masked
// field in a JSON response is not masked at all.
package feed

import (
	"time"

	"github.com/google/uuid"
	"github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/market"
	"github.com/shopspring/decimal"
)

type Difficulty string

const (
	DifficultyCalm     Difficulty = "calm"
	DifficultyStandard Difficulty = "standard"
	DifficultyVolatile Difficulty = "volatile"
	DifficultyCrisis   Difficulty = "crisis"
)

func (d Difficulty) Valid() bool {
	switch d {
	case DifficultyCalm, DifficultyStandard, DifficultyVolatile, DifficultyCrisis:
		return true
	}
	return false
}

// Normalization is the affine map applied to every price in a feed: displayed = (real + Offset) × Scale.
//
// Affine is chosen deliberately. Every technique the product teaches is linear in price, so all of
// it survives the map exactly: fibonacci levels, trendlines, support and resistance, EMAs, RSI
// (which reads differences), and risk-to-reward (a ratio of price distances, so Offset cancels and
// Scale divides out).
//
// What an affine map does not preserve is the percentage-return series — and that is the point. An
// identical return series is a fingerprint that can be matched against a database of real assets,
// so preserving it would leave the feed identifiable to anyone willing to run the comparison. The
// price of that is that a logarithmic scale is meaningful only within the normalized series, which
// is the only series a client ever sees.
type Normalization struct {
	Offset      decimal.Decimal
	Scale       decimal.Decimal
	VolumeScale decimal.Decimal
}

// Apply maps a real price into the blinded space.
func (n Normalization) Apply(price decimal.Decimal) decimal.Decimal {
	return price.Add(n.Offset).Mul(n.Scale)
}

// Invert maps a blinded price back to the real one. Used only after a reveal and by the builder's
// own round-trip test — never on a pre-reveal request path.
func (n Normalization) Invert(price decimal.Decimal) decimal.Decimal {
	if n.Scale.IsZero() {
		return decimal.Zero
	}
	return price.Div(n.Scale).Sub(n.Offset)
}

func (n Normalization) ApplyVolume(volume decimal.Decimal) decimal.Decimal {
	return volume.Mul(n.VolumeScale)
}

// Display precision for blinded output. Multiplying by an arbitrary scale produces a long decimal
// tail that no real feed would ever quote, and those trailing digits are pure artifact — they say
// something about the multiplier rather than about the market. Quantizing here is the same thing a
// venue's tick size does, and it keeps the wire format readable.
const (
	priceDisplayScale  = 5
	volumeDisplayScale = 2
)

// ApplyBar maps a whole candle onto the wire. Because the map is monotonic increasing (Scale is
// always positive) and rounding is monotonic too, the high stays the high and the low stays the
// low — a negative scale would silently invert every candle in the feed.
//
// Prices are quantized here rather than inside Apply, so the exact affine map stays available for
// arithmetic (fib levels, R:R) while only the serialized form is rounded.
func (n Normalization) ApplyBar(bar market.Bar, index int) Bar {
	return Bar{
		Index:  index,
		Open:   n.Apply(bar.Open).Round(priceDisplayScale),
		High:   n.Apply(bar.High).Round(priceDisplayScale),
		Low:    n.Apply(bar.Low).Round(priceDisplayScale),
		Close:  n.Apply(bar.Close).Round(priceDisplayScale),
		Volume: n.ApplyVolume(bar.Volume).Round(volumeDisplayScale),
	}
}

// Bar is a blinded candle. It carries an *index*, never a timestamp: an absolute time identifies
// the window, and a window plus a shape identifies the instrument.
type Bar struct {
	Index  int             `json:"index"`
	Open   decimal.Decimal `json:"open"`
	High   decimal.Decimal `json:"high"`
	Low    decimal.Decimal `json:"low"`
	Close  decimal.Decimal `json:"close"`
	Volume decimal.Decimal `json:"volume"`
}

// Feed is the full record, including the parts a trader must not see before the reveal. It never
// crosses the HTTP boundary as-is; `entities/response/feed` carries the blinded projection, and it
// is a separate type with no identity field rather than this one with fields omitted.
type Feed struct {
	ID            uuid.UUID
	InstrumentID  uuid.UUID
	AliasLabel    string
	BaseTimeframe market.Timeframe
	WindowStart   time.Time
	WindowEnd     time.Time
	WarmupBars    int
	TotalBars     int
	Normalization Normalization
	Difficulty    Difficulty
	MacroLabel    *string
	IsPublished   bool

	RealizedVolatility *decimal.Decimal
	TrendPersistence   *decimal.Decimal

	BuilderVersion int
	BuiltAt        time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
	Version        int
}

// TradeableBars excludes the lookback the trader is shown before the cursor starts moving.
func (f Feed) TradeableBars() int {
	tradeable := f.TotalBars - f.WarmupBars
	if tradeable < 0 {
		return 0
	}
	return tradeable
}

// AssetClassHint is the only thing a trader is told about what they are trading, and it is
// deliberately coarse. "fx" and "crypto" are collapsed into one bucket because a 24/5 session gap
// pattern already distinguishes FX from crypto on the chart, so naming them separately would give
// away what the gaps only hint at.
func AssetClassHint(class market.AssetClass) string {
	switch class {
	case market.AssetClassFX, market.AssetClassCrypto:
		return "FX/CRYPTO MASKED"
	case market.AssetClassEquity, market.AssetClassIndex:
		return "EQUITY/INDEX MASKED"
	case market.AssetClassCommodity, market.AssetClassFutures:
		return "COMMODITY/FUTURES MASKED"
	}
	return "MASKED"
}
