// Package market holds the unblinded truth: real instruments and their real bars. Nothing in this
// package is ever serialized to a trader directly. It reaches them only through a feed, which
// strips the identity and rewrites the prices.
package market

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type AssetClass string

const (
	AssetClassFX        AssetClass = "fx"
	AssetClassEquity    AssetClass = "equity"
	AssetClassCrypto    AssetClass = "crypto"
	AssetClassFutures   AssetClass = "futures"
	AssetClassIndex     AssetClass = "index"
	AssetClassCommodity AssetClass = "commodity"
)

// Timeframe is the bar interval. The set is closed because it is also a database CHECK constraint
// and a partition of the derivation tree — adding one means a migration, not a constant.
type Timeframe string

const (
	TF1m  Timeframe = "1m"
	TF5m  Timeframe = "5m"
	TF15m Timeframe = "15m"
	TF30m Timeframe = "30m"
	TF1h  Timeframe = "1h"
	TF4h  Timeframe = "4h"
	TF1d  Timeframe = "1d"
	TF1w  Timeframe = "1w"
)

var timeframeDuration = map[Timeframe]time.Duration{
	TF1m: time.Minute, TF5m: 5 * time.Minute, TF15m: 15 * time.Minute, TF30m: 30 * time.Minute,
	TF1h: time.Hour, TF4h: 4 * time.Hour, TF1d: 24 * time.Hour, TF1w: 7 * 24 * time.Hour,
}

func (t Timeframe) Duration() (time.Duration, bool) {
	d, ok := timeframeDuration[t]
	return d, ok
}

func (t Timeframe) Valid() bool {
	_, ok := timeframeDuration[t]
	return ok
}

// DerivedTimeframes are the intervals built from the 1m base at load time. Aggregating on read
// would put the cost inside the replay hot path, where the 15ms budget lives.
func DerivedTimeframes() []Timeframe {
	return []Timeframe{TF5m, TF15m, TF30m, TF1h, TF4h, TF1d}
}

type Instrument struct {
	ID            uuid.UUID
	Symbol        string
	DisplayName   string
	AssetClass    AssetClass
	Venue         *string
	QuoteCurrency string
	TickSize      decimal.Decimal
	ContractSize  decimal.Decimal
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// Bar is one OHLCV candle of real, unmodified market data.
type Bar struct {
	InstrumentID uuid.UUID
	Timeframe    Timeframe
	OpenedAt     time.Time
	Open         decimal.Decimal
	High         decimal.Decimal
	Low          decimal.Decimal
	Close        decimal.Decimal
	Volume       decimal.Decimal
}

// Valid reports whether the candle is internally coherent. A bar failing this is rejected at
// ingest rather than stored: a high below the close is not a data point, it is a parsing bug, and
// storing it means every downstream indicator inherits it.
func (b Bar) Valid() bool {
	if b.High.LessThan(b.Low) || b.High.LessThan(b.Open) || b.High.LessThan(b.Close) {
		return false
	}
	if b.Low.GreaterThan(b.Open) || b.Low.GreaterThan(b.Close) {
		return false
	}
	return !b.Volume.IsNegative()
}
