// Package revealresponse carries the unblinding.
//
// This is the one response package in the system that is *allowed* to name the instrument and the
// dates, and it is deliberately a separate type rather than a flag on the blinded one. A field that
// appears conditionally is one refactor away from appearing unconditionally, and the mistake would
// be discovered by a trader who has just been shown the answer to a session they were still
// trading. There is no undoing that, so the two shapes never share a struct.
package revealresponse

import (
	"time"

	"github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/market"
	domainreveal "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/reveal"
)

type Reveal struct {
	SessionID  string    `json:"session_id"`
	RevealedAt time.Time `json:"revealed_at"`

	// The three things the product spent the whole session withholding.
	Symbol      string    `json:"symbol"`
	Timeframe   string    `json:"timeframe"`
	WindowStart time.Time `json:"window_start"`
	WindowEnd   time.Time `json:"window_end"`

	MacroLabel *string  `json:"macro_label"`
	MacroNotes *string  `json:"macro_notes"`
	MacroTags  []string `json:"macro_tags"`

	// BenchmarkLabel travels with the numbers rather than being assumed. "You beat the market"
	// means nothing without saying which market and how it was measured, and a trader who disputes
	// the comparison should be able to read what it was.
	BenchmarkLabel string `json:"benchmark_label"`
	// Decimal strings, like every other number crossing this boundary. A return rendered through a
	// JSON float would be fine here and wrong somewhere else, so the rule holds everywhere.
	StrategyReturnPct  string `json:"strategy_return_pct"`
	BenchmarkReturnPct string `json:"benchmark_return_pct"`
	AlphaPct           string `json:"alpha_pct"`

	DisciplineIndex int `json:"discipline_index"`
}

func FromDomain(entity domainreveal.Reveal) Reveal {
	return Reveal{
		SessionID: entity.SessionID.String(), RevealedAt: entity.RevealedAt,
		Symbol: entity.Symbol, Timeframe: entity.Timeframe,
		WindowStart: entity.WindowStart, WindowEnd: entity.WindowEnd,
		MacroLabel: entity.MacroLabel, MacroNotes: entity.MacroNotes, MacroTags: tags(entity.MacroTags),
		BenchmarkLabel:     entity.BenchmarkLabel,
		StrategyReturnPct:  entity.StrategyReturnPct.String(),
		BenchmarkReturnPct: entity.BenchmarkReturnPct.String(),
		AlphaPct:           entity.AlphaPct.String(),
		DisciplineIndex:    entity.DisciplineIndex,
	}
}

func tags(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

// DisclosedBar is a real candle, with its real timestamp, from a session the trader has chosen to
// unblind.
//
// It is a separate type from `feedresponse.Bar`, and that is the whole design of 05.4. The blinded
// bar has no timestamp field at all — not an omitted one, not a nil one — so there is no switch
// anywhere that could turn disclosure on early. Adding a timestamp to that type instead would have
// made every pre-reveal response one conditional away from the leak the product exists to prevent.
type DisclosedBar struct {
	Index int `json:"index"`
	// The real instant. Present here and nowhere else in the system.
	Timestamp time.Time `json:"timestamp"`
	// Real, unnormalized prices: the blinding map is not applied, because there is nothing left to
	// blind. A trader comparing this against a chart elsewhere should see the same numbers.
	Open   string `json:"open"`
	High   string `json:"high"`
	Low    string `json:"low"`
	Close  string `json:"close"`
	Volume string `json:"volume"`
}

// Disclosure is the post-reveal view: the unblinding beside the real series it describes.
type Disclosure struct {
	Reveal Reveal         `json:"reveal"`
	Bars   []DisclosedBar `json:"bars"`
}

func DisclosureFromDomain(entity domainreveal.Reveal, bars []market.Bar) Disclosure {
	disclosed := make([]DisclosedBar, 0, len(bars))
	for index, bar := range bars {
		disclosed = append(disclosed, DisclosedBar{
			Index: index, Timestamp: bar.OpenedAt,
			Open: bar.Open.String(), High: bar.High.String(), Low: bar.Low.String(),
			Close: bar.Close.String(), Volume: bar.Volume.String(),
		})
	}
	return Disclosure{Reveal: FromDomain(entity), Bars: disclosed}
}
