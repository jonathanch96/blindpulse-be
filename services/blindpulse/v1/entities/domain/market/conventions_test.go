package market

import (
	"testing"
)

// Review finding BE-02-3. Every instrument was stamped with an FX tick and a USD quote regardless
// of what it was. Harmless so far because nothing reads tick size — and a correctness bug the
// moment Sprint 04's order gate starts quantizing stops and sizes against it.

func TestDeriveConventionsQuoteCurrency(t *testing.T) {
	t.Parallel()
	cases := []struct {
		symbol string
		class  AssetClass
		want   string
	}{
		{"EURUSD", AssetClassFX, "USD"},
		{"USDJPY", AssetClassFX, "JPY"},
		{"GBPCHF", AssetClassFX, "CHF"},
		{"EUR/USD", AssetClassFX, "USD"},
		// Longest match wins, or BTCUSDT would come back quoted in USD with a stray T.
		{"BTCUSDT", AssetClassCrypto, "USDT"},
		{"BTCUSD", AssetClassCrypto, "USD"},
		{"ETHBTC", AssetClassCrypto, "BTC"},
		{"SOL-USDC", AssetClassCrypto, "USDC"},
		{"AAPL", AssetClassEquity, "USD"},
	}
	for _, testCase := range cases {
		t.Run(testCase.symbol, func(t *testing.T) {
			t.Parallel()
			if got := DeriveConventions(testCase.symbol, testCase.class).QuoteCurrency; got != testCase.want {
				t.Errorf("DeriveConventions(%q, %q).QuoteCurrency = %q, want %q",
					testCase.symbol, testCase.class, got, testCase.want)
			}
		})
	}
}

// The specific defect: a yen pair prints to three decimals because a pip is 0.01 rather than
// 0.0001. Using the FX default there makes every stop distance a hundred times too fine.
func TestDeriveConventionsTickSize(t *testing.T) {
	t.Parallel()
	cases := []struct {
		symbol string
		class  AssetClass
		want   string
	}{
		{"EURUSD", AssetClassFX, "0.00001"},
		{"USDJPY", AssetClassFX, "0.001"},
		{"EURJPY", AssetClassFX, "0.001"},
		{"AAPL", AssetClassEquity, "0.01"},
		{"BTCUSDT", AssetClassCrypto, "0.01"},
		{"SPX", AssetClassIndex, "0.01"},
		{"CL", AssetClassFutures, "0.01"},
	}
	for _, testCase := range cases {
		t.Run(testCase.symbol, func(t *testing.T) {
			t.Parallel()
			if got := DeriveConventions(testCase.symbol, testCase.class).TickSize.String(); got != testCase.want {
				t.Errorf("DeriveConventions(%q, %q).TickSize = %s, want %s",
					testCase.symbol, testCase.class, got, testCase.want)
			}
		})
	}
}

// The regression, stated plainly: an equity must not inherit the FX tick.
func TestAnEquityDoesNotGetTheFxTick(t *testing.T) {
	t.Parallel()
	equity := DeriveConventions("AAPL", AssetClassEquity)
	fx := DeriveConventions("EURUSD", AssetClassFX)
	if equity.TickSize.Equal(fx.TickSize) {
		t.Errorf("an equity and an FX pair share tick size %s", equity.TickSize)
	}
}

// A symbol that is not the shape the rule expects must fall back rather than slice blindly — a
// three-letter "pair" sliced at position 3 would produce an empty quote currency.
func TestAnUnexpectedSymbolShapeFallsBack(t *testing.T) {
	t.Parallel()
	for _, symbol := range []string{"", "EUR", "EURUSDEXTRA"} {
		got := DeriveConventions(symbol, AssetClassFX)
		if got.QuoteCurrency == "" {
			t.Errorf("DeriveConventions(%q, fx) produced an empty quote currency", symbol)
		}
		if !got.TickSize.IsPositive() {
			t.Errorf("DeriveConventions(%q, fx) produced a non-positive tick %s", symbol, got.TickSize)
		}
	}
}

// Every path must produce something usable: a zero tick would make quantization divide by zero in
// Sprint 04, and an empty quote currency would render as a bare number with no unit.
func TestEveryAssetClassProducesUsableConventions(t *testing.T) {
	t.Parallel()
	classes := []AssetClass{
		AssetClassFX, AssetClassEquity, AssetClassCrypto,
		AssetClassFutures, AssetClassIndex, AssetClassCommodity, AssetClass("unknown"),
	}
	for _, class := range classes {
		got := DeriveConventions("TEST", class)
		if !got.TickSize.IsPositive() {
			t.Errorf("%q produced tick %s, want a positive value", class, got.TickSize)
		}
		if got.QuoteCurrency == "" {
			t.Errorf("%q produced no quote currency", class)
		}
		if !got.ContractSize.IsPositive() {
			t.Errorf("%q produced contract size %s, want a positive value", class, got.ContractSize)
		}
	}
}
