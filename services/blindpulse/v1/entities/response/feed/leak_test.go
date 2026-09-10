package feedresponse

import (
	"encoding/json"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	domainfeed "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/feed"
	"github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/market"
	"github.com/shopspring/decimal"
)

// This file is the NFR-05 guard. The product's central promise (BR-01) is that a trader cannot
// identify the instrument, the calendar window, or the venue before they choose to reveal it.
//
// These tests assert that over the *serialized* payload rather than over a list of field names,
// because the failure mode is always a field somebody added without thinking about this file.

func sampleFeed() domainfeed.Feed {
	volatility := decimal.RequireFromString("0.0074")
	persistence := decimal.RequireFromString("0.52")
	return domainfeed.Feed{
		ID:            uuid.MustParse("2f1c4e10-9a3d-4c77-8b21-6d5e0f3a9c11"),
		InstrumentID:  uuid.MustParse("aa11bb22-cc33-dd44-ee55-ff6677889900"),
		AliasLabel:    "Asset #842",
		BaseTimeframe: market.TF15m,
		// A real, recognizable window: SVB collapse week. If any of this reaches the payload the
		// trader can date the session, and dating it identifies it.
		WindowStart: time.Date(2023, 3, 14, 8, 30, 0, 0, time.UTC),
		WindowEnd:   time.Date(2023, 3, 17, 16, 0, 0, 0, time.UTC),
		WarmupBars:  200,
		TotalBars:   500,
		Normalization: domainfeed.Normalization{
			Offset:      decimal.RequireFromString("0.4137"),
			Scale:       decimal.RequireFromString("3.72"),
			VolumeScale: decimal.RequireFromString("1.5"),
		},
		Difficulty:         domainfeed.DifficultyVolatile,
		MacroLabel:         strPtr("SVB Contagion"),
		IsPublished:        true,
		RealizedVolatility: &volatility,
		TrendPersistence:   &persistence,
		BuilderVersion:     1,
		BuiltAt:            time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
	}
}

// forbidden is everything that would let a trader identify what they are trading. The macro label
// is included: "SVB Contagion" dates the window to a single week as surely as the date does.
var forbidden = []string{
	"EURUSD", "EUR/USD", "aa11bb22", "SVB", "Contagion",
	"2023", "03-14", "08:30", "instrument", "symbol", "window", "macro",
	"price_scale", "price_offset", "0.4137", "3.72",
}

// No trailing \b: in "2026-09-10T07:15:30Z" the digit and the T are both word
// characters, so there is no boundary between them and a trailing \b would miss the
// single most likely way a date reaches the wire.
var isoDate = regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}`)

func assertClean(t *testing.T, label string, payload any) {
	t.Helper()
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal %s: %v", label, err)
	}
	body := string(encoded)
	for _, needle := range forbidden {
		if strings.Contains(strings.ToLower(body), strings.ToLower(needle)) {
			t.Errorf("%s leaks %q\n%s", label, needle, body)
		}
	}
	// Any ISO date at all, not just the known one — a future field carrying a different date is
	// exactly as identifying.
	if match := isoDate.FindString(body); match != "" {
		t.Errorf("%s contains a calendar date %q\n%s", label, match, body)
	}
}

func TestCatalogueResponseCarriesNoIdentity(t *testing.T) {
	t.Parallel()
	assertClean(t, "Feed", FromDomain(sampleFeed(), market.AssetClassFX))
}

func TestDetailResponseCarriesNoIdentity(t *testing.T) {
	t.Parallel()
	assertClean(t, "Detail", FromDomainDetail(sampleFeed(), market.AssetClassFX))
}

func TestBarsResponseCarriesNoTimestamps(t *testing.T) {
	t.Parallel()
	entity := sampleFeed()
	bars := []domainfeed.Bar{
		entity.Normalization.ApplyBar(market.Bar{
			OpenedAt: entity.WindowStart,
			Open:     decimal.RequireFromString("1.0850"),
			High:     decimal.RequireFromString("1.0955"),
			Low:      decimal.RequireFromString("1.0788"),
			Close:    decimal.RequireFromString("1.0912"),
			Volume:   decimal.RequireFromString("18420"),
		}, 0),
	}

	assertClean(t, "Bars", Bars{FeedID: entity.ID.String(), From: 0, To: 0, Bars: FromDomainBars(bars)})
}

// The structural guard behind the payload ones: the response types must have no field that could
// ever carry an identity, so a future change has to delete this test on purpose rather than
// forget an omission. A time.Time anywhere in this package is by itself a bug.
func TestResponseTypesHaveNoIdentityBearingFields(t *testing.T) {
	t.Parallel()
	banned := []string{
		"instrumentid", "symbol", "venue", "windowstart", "windowend",
		"macrolabel", "priceScale", "priceoffset", "openedat", "builtat", "timestamp",
	}
	for _, target := range []any{Feed{}, Detail{}, Bars{}} {
		walkFields(t, reflect.TypeOf(target), banned)
	}
}

func walkFields(t *testing.T, typ reflect.Type, banned []string) {
	t.Helper()
	if typ.Kind() == reflect.Slice || typ.Kind() == reflect.Ptr {
		walkFields(t, typ.Elem(), banned)
		return
	}
	if typ.Kind() != reflect.Struct {
		return
	}
	if typ == reflect.TypeOf(time.Time{}) {
		t.Errorf("%s carries a time.Time; a blinded response must not carry an absolute time", typ.Name())
		return
	}
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		name := strings.ToLower(field.Name)
		for _, needle := range banned {
			if name == strings.ToLower(needle) {
				t.Errorf("%s.%s is an identity-bearing field on a blinded response", typ.Name(), field.Name)
			}
		}
		walkFields(t, field.Type, banned)
	}
}

func strPtr(value string) *string { return &value }
