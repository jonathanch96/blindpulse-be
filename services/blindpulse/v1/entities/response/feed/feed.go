// Package feedresponse carries the trader-facing projection of a feed.
//
// The types here are the blinding boundary. They exist as *separate* types rather than as the
// domain feed with fields omitted, because omission is a property somebody has to remember on
// every change, while a type with no field for the symbol cannot leak the symbol however it is
// serialized. If you find yourself adding an instrument id, a symbol, or an absolute timestamp to
// anything in this file, that is the bug — not the thing that fixes one.
package feedresponse

import (
	domainfeed "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/feed"
	"github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/market"
)

// Feed is everything a trader may know about a feed before the reveal.
type Feed struct {
	ID string `json:"id"`
	// The synthetic identifier, e.g. "Asset #842". Minted from a sequence, never derived from the
	// symbol — a hash of a ticker is reversible by anyone holding a list of tickers.
	Alias string `json:"alias"`
	// A coarse bucket, e.g. "FX/CRYPTO MASKED". Deliberately not the real asset class.
	AssetClassHint string `json:"asset_class_hint"`
	// The bar interval, which is not identifying on its own: thousands of instruments trade on a
	// 15-minute chart, and the trader needs it to reason about structure at all.
	Timeframe  string `json:"timeframe"`
	Difficulty string `json:"difficulty"`
	// Counts, not dates. "500 bars" says how long the session is without saying when it was.
	TotalBars     int `json:"total_bars"`
	WarmupBars    int `json:"warmup_bars"`
	TradeableBars int `json:"tradeable_bars"`
}

// Detail adds nothing identifying; it exists so the catalogue list stays small while a single feed
// can carry the extra shape hints a trader uses to choose one.
type Detail struct {
	Feed
	// A qualitative band rather than the realized number: the exact volatility of a window, to
	// eight decimal places, is very nearly a fingerprint.
	VolatilityBand string `json:"volatility_band"`
	StructureBand  string `json:"structure_band"`
}

func FromDomain(entity domainfeed.Feed, class market.AssetClass) Feed {
	return Feed{
		ID:             entity.ID.String(),
		Alias:          entity.AliasLabel,
		AssetClassHint: domainfeed.AssetClassHint(class),
		Timeframe:      string(entity.BaseTimeframe),
		Difficulty:     string(entity.Difficulty),
		TotalBars:      entity.TotalBars,
		WarmupBars:     entity.WarmupBars,
		TradeableBars:  entity.TradeableBars(),
	}
}

func FromDomainDetail(entity domainfeed.Feed, class market.AssetClass) Detail {
	return Detail{
		Feed:           FromDomain(entity, class),
		VolatilityBand: volatilityBand(entity),
		StructureBand:  structureBand(entity),
	}
}

// Bar is the wire form of a blinded candle. It is declared here rather than reused from the domain
// so the wire contract is owned by the package the leak test guards — the same reason Feed is its
// own type. Note what it has instead of a timestamp: an index.
type Bar struct {
	Index  int    `json:"index"`
	Open   string `json:"open"`
	High   string `json:"high"`
	Low    string `json:"low"`
	Close  string `json:"close"`
	Volume string `json:"volume"`
	// True when this higher-timeframe bar is still forming at the cursor. The client must draw it
	// distinctly — hollow or dashed — never as a closed candle.
	Forming bool `json:"forming"`
}

// Bars is the blinded candle payload. Each bar carries an index, never a timestamp.
type Bars struct {
	FeedID string `json:"feed_id"`
	// The timeframe these bars are aggregated to. Indices are in this timeframe's own space, not
	// the feed's base — a 1h view's bar 3 is not the base series' bar 3.
	Timeframe string `json:"timeframe"`
	From      int    `json:"from"`
	To        int    `json:"to"`
	Bars      []Bar  `json:"bars"`
}

// FromDomainBars renders candles for the wire. Prices are strings for the same reason they are
// strings everywhere else in this API: a JSON number is a float on the other side, and a price
// that has been through a float is not the price the server quoted.
func FromDomainBars(bars []domainfeed.Bar) []Bar {
	wire := make([]Bar, 0, len(bars))
	for _, bar := range bars {
		wire = append(wire, Bar{
			Index: bar.Index,
			Open:  bar.Open.String(), High: bar.High.String(),
			Low: bar.Low.String(), Close: bar.Close.String(),
			Volume: bar.Volume.String(), Forming: bar.Forming,
		})
	}
	return wire
}

func volatilityBand(entity domainfeed.Feed) string {
	if entity.RealizedVolatility == nil {
		return "unknown"
	}
	value, _ := entity.RealizedVolatility.Float64()
	switch {
	case value >= 0.012:
		return "extreme"
	case value >= 0.006:
		return "elevated"
	case value <= 0.002:
		return "compressed"
	default:
		return "normal"
	}
}

func structureBand(entity domainfeed.Feed) string {
	if entity.TrendPersistence == nil {
		return "unknown"
	}
	value, _ := entity.TrendPersistence.Float64()
	switch {
	case value >= 0.45:
		return "trending"
	case value <= 0.15:
		return "ranging"
	default:
		return "mixed"
	}
}
