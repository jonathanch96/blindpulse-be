package revealresponse

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	feedresponse "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/response/feed"

	"github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/market"
	domainreveal "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/reveal"
	"github.com/shopspring/decimal"
)

// This package is the one place in the system allowed to name the instrument and the dates, so its
// guard runs the other way from every other leak test: it asserts that the disclosure really is a
// *separate type* from the blinded one, rather than the blinded one with fields switched on.
//
// The failure this prevents is not subtle and has no recovery: a conditional field that starts
// appearing unconditionally hands a trader the answer to a session they are still trading.

func sampleReveal() domainreveal.Reveal {
	label, notes := "SVB Contagion", "Regional banking stress."
	return domainreveal.Reveal{
		SessionID:    uuid.MustParse("2f1c4e10-9a3d-4c77-8b21-6d5e0f3a9c11"),
		InstrumentID: uuid.MustParse("aa11bb22-cc33-dd44-ee55-ff6677889900"),
		RevealedAt:   time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC),
		Symbol:       "EURUSD", Timeframe: "15m",
		WindowStart: time.Date(2023, 3, 14, 8, 30, 0, 0, time.UTC),
		WindowEnd:   time.Date(2023, 3, 17, 16, 0, 0, 0, time.UTC),
		MacroLabel:  &label, MacroNotes: &notes, MacroTags: []string{"#SVB_COLLAPSE"},
		BenchmarkLabel:     domainreveal.BenchmarkBuyAndHold,
		StrategyReturnPct:  decimal.RequireFromString("2.5"),
		BenchmarkReturnPct: decimal.RequireFromString("10"),
		AlphaPct:           decimal.RequireFromString("-7.5"),
	}
}

// The blinded bar is the type every pre-reveal path serves. It must have no field that could hold
// an instant — not an omitted one, not a pointer, not a zero value. A disclosure switch cannot be
// added to a type with nowhere to put it.
func TestTheBlindedBarHasNowhereToPutATimestamp(t *testing.T) {
	blinded := reflect.TypeOf(feedresponse.Bar{})
	for i := 0; i < blinded.NumField(); i++ {
		field := blinded.Field(i)
		if field.Type == reflect.TypeOf(time.Time{}) || field.Type == reflect.TypeOf(&time.Time{}) {
			t.Errorf("the blinded bar carries a time field %q; the disclosure is supposed to be a separate type", field.Name)
		}
		name := strings.ToLower(field.Name)
		for _, banned := range []string{"time", "date", "at", "symbol", "instrument"} {
			if name == banned {
				t.Errorf("the blinded bar carries %q", field.Name)
			}
		}
	}
	// And the disclosed one does carry an instant, which is the point: two shapes, not one shape
	// with a switch. A test that only checked the blinded side would still pass if somebody
	// deleted the disclosed type and added a flag instead.
	disclosed := reflect.TypeOf(DisclosedBar{})
	if _, ok := disclosed.FieldByName("Timestamp"); !ok {
		t.Error("the disclosed bar has no Timestamp; the two types have been collapsed")
	}
	if blinded == disclosed {
		t.Error("the blinded and disclosed bars are the same type")
	}
}

func TestDisclosureCarriesTheIdentityItIsSupposedTo(t *testing.T) {
	bars := []market.Bar{{
		OpenedAt: time.Date(2023, 3, 14, 8, 30, 0, 0, time.UTC),
		Open:     decimal.RequireFromString("1.0"), High: decimal.RequireFromString("1.2"),
		Low: decimal.RequireFromString("0.9"), Close: decimal.RequireFromString("1.05"),
		Volume: decimal.RequireFromString("1200"),
	}}
	payload, err := json.Marshal(DisclosureFromDomain(sampleReveal(), bars))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	body := string(payload)
	// Everything the product spent the session withholding, now present on purpose. Asserted so a
	// future change that quietly drops the symbol or the window turns the reveal into a screen
	// that unblinds nothing.
	for _, expected := range []string{"EURUSD", "2023-03-14", "SVB Contagion", "#SVB_COLLAPSE", "buy_and_hold"} {
		if !strings.Contains(body, expected) {
			t.Errorf("the disclosure is missing %q: %s", expected, body)
		}
	}
	// Real prices, not normalized ones: a trader checking this against a chart elsewhere should
	// see the same numbers.
	if !strings.Contains(body, `"open":"1"`) {
		t.Errorf("the disclosed bar is not carrying real prices: %s", body)
	}
}

// The returns cross the wire as decimal strings like every other number in this system. A float
// here would be harmless and would make the rule "decimal except where it did not matter", which is
// not a rule anybody can follow.
func TestReturnsAreDecimalStrings(t *testing.T) {
	wire := reflect.TypeOf(Reveal{})
	for _, name := range []string{"StrategyReturnPct", "BenchmarkReturnPct", "AlphaPct"} {
		field, ok := wire.FieldByName(name)
		if !ok {
			t.Fatalf("%s is missing from the reveal response", name)
		}
		if field.Type.Kind() != reflect.String {
			t.Errorf("%s is %s, want a decimal string", name, field.Type.Kind())
		}
	}
}
