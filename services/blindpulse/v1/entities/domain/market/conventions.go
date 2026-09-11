package market

import (
	"strings"

	"github.com/shopspring/decimal"
)

// Instrument conventions.
//
// The loader used to stamp every instrument with `TickSize 0.00001` and `QuoteCurrency "USD"`
// whatever it was ingesting. That is right for EURUSD and wrong for USDJPY (0.001), an equity
// (0.01) and BTC — and it has been harmless only because nothing reads tick size yet. Sprint 04's
// order gate is the first consumer: stop distance, position size and R:R all quantize to it, so a
// wrong tick produces a plausible-looking number that is off by a factor of a hundred.
//
// These are *defaults*, not facts. A real venue's tick table is per-contract and belongs in
// reference data; what this does is stop the obvious errors and give the loader something better to
// fall back on than one FX convention applied to everything.

// Conventions is what an instrument is quoted and traded in.
type Conventions struct {
	QuoteCurrency string
	TickSize      decimal.Decimal
	// ContractSize is expressed in **units of the base asset**, not lots — one unit of EUR rather
	// than a 100,000-unit standard lot. Sprint 04's margin model settled on that reading (review
	// finding SP4-3): notional is quantity x price x contract size, so reading a unit as a lot would
	// inflate every margin requirement by a factor of 100,000 and the gate would refuse everything.
	ContractSize decimal.Decimal
}

// quoteAssets are the trailing symbols a crypto pair is quoted in, longest first so USDT matches
// before USD would.
var quoteAssets = []string{"USDT", "USDC", "TUSD", "BUSD", "USD", "EUR", "GBP", "JPY", "BTC", "ETH"}

// DeriveConventions infers how an instrument is quoted from its asset class and symbol.
//
// It never guesses silently past what the symbol supports: an unrecognized shape falls back to the
// asset class default, which is a conservative tick rather than an FX one.
func DeriveConventions(symbol string, class AssetClass) Conventions {
	upper := strings.ToUpper(strings.TrimSpace(symbol))
	switch class {
	case AssetClassFX:
		quote := fxQuote(upper)
		return Conventions{
			QuoteCurrency: quote,
			// The five-decimal convention, except against the yen. A JPY pair prints to three
			// decimals because a pip is 0.01 rather than 0.0001, and using the FX default there
			// makes every stop distance a hundred times too fine.
			TickSize:     tickForFX(quote),
			ContractSize: decimal.NewFromInt(1),
		}
	case AssetClassCrypto:
		return Conventions{
			QuoteCurrency: cryptoQuote(upper),
			// Venue- and pair-specific in reality; a cent is the common case for a USD-quoted
			// major and is not wrong by orders of magnitude for the rest.
			TickSize:     decimal.RequireFromString("0.01"),
			ContractSize: decimal.NewFromInt(1),
		}
	case AssetClassEquity:
		return Conventions{
			QuoteCurrency: "USD",
			TickSize:      decimal.RequireFromString("0.01"),
			ContractSize:  decimal.NewFromInt(1),
		}
	case AssetClassIndex, AssetClassCommodity, AssetClassFutures:
		// A futures tick is per-contract — 0.25 on ES, 0.01 on CL, 0.10 on GC — and cannot be
		// derived from a symbol. A cent is the safe default because it is finer than every real
		// tick here, so a stop quantizes to something valid rather than something too coarse to
		// place. Pass the real value explicitly when it matters.
		return Conventions{
			QuoteCurrency: "USD",
			TickSize:      decimal.RequireFromString("0.01"),
			ContractSize:  decimal.NewFromInt(1),
		}
	default:
		return Conventions{
			QuoteCurrency: "USD",
			TickSize:      decimal.RequireFromString("0.01"),
			ContractSize:  decimal.NewFromInt(1),
		}
	}
}

// fxQuote takes the quote currency from a six-letter pair. Anything else keeps USD rather than
// slicing a symbol it does not understand.
func fxQuote(symbol string) string {
	trimmed := strings.ReplaceAll(strings.ReplaceAll(symbol, "/", ""), "_", "")
	if len(trimmed) == 6 {
		return trimmed[3:]
	}
	return "USD"
}

func tickForFX(quote string) decimal.Decimal {
	if quote == "JPY" {
		return decimal.RequireFromString("0.001")
	}
	return decimal.RequireFromString("0.00001")
}

// cryptoQuote matches the longest known quote asset the symbol ends with, so BTCUSDT is quoted in
// USDT rather than in USD with a stray T.
func cryptoQuote(symbol string) string {
	trimmed := strings.ReplaceAll(strings.ReplaceAll(symbol, "/", ""), "-", "")
	for _, quote := range quoteAssets {
		if len(trimmed) > len(quote) && strings.HasSuffix(trimmed, quote) {
			return quote
		}
	}
	return "USD"
}
