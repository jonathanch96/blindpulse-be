package executionresponse

import (
	"encoding/json"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	executiondomain "github.com/jblabs/blindpulse-be/services/blindpulse/v1/domain/execution"
	domainexec "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/execution"
	"github.com/shopspring/decimal"
)

// The execution half of the NFR-05 / SP4-1 guard.
//
// Execution is where a real instant is hardest to keep off the wire, because the write path genuinely
// needs several. An order row stores placed_bar_at; an equity snapshot stores bar_at; the drawdown's
// day boundary is derived from both. Every one of them is a date in the window the trader is being
// asked to read blind, and the boundary is worse than a single date: a reset pattern with a two-day
// gap every five days is a weekend, which rules out crypto outright.
//
// So the rule is that no response in this package carries a bar's instant or anything derived from
// the day boundary, and these tests are what make that a property rather than a habit.

// No trailing \b: in "2023-03-14T08:30:00Z" the digit and the T are both word characters, so there
// is no boundary between them and a trailing \b would miss the likeliest leak of all.
var isoDate = regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}`)

func dec(value string) decimal.Decimal { return decimal.RequireFromString(value) }

func ptr(value string) *decimal.Decimal {
	parsed := dec(value)
	return &parsed
}

// The SVB collapse week: a window a trader would recognize instantly if a date reached them, which
// is exactly why it is the fixture.
var barInstant = time.Date(2023, 3, 14, 8, 30, 0, 0, time.UTC)

func sampleOrder() domainexec.Order {
	code := domainexec.CodeStopTooWide
	filledBar := 215
	return domainexec.Order{
		ID:        uuid.MustParse("2f1c4e10-9a3d-4c77-8b21-6d5e0f3a9c11"),
		SessionID: uuid.MustParse("8b7d6e55-1c2f-4a90-b3e4-77aa0c1d2e33"),
		AccountID: uuid.New(), ClientKey: "dock-7",
		Side: domainexec.SideBuy, Type: domainexec.OrderLimit,
		Quantity: dec("12500"), LimitPrice: ptr("1.09120"), StopLoss: dec("1.08800"),
		// RiskAmount deliberately carries more significant digits than a float64 holds: routed
		// through a JSON number it would come back 40.1, and the assertion below would catch it.
		TakeProfit: ptr("1.09900"), RiskReward: ptr("2.437"), RiskAmount: ptr("40.100000000000001"),
		Status: domainexec.StatusRejected, RejectionCode: &code,
		PlacedBarIndex: 214, PlacedBarAt: barInstant,
		FilledBarIndex: &filledBar, FilledPrice: ptr("1.09121"), Slippage: dec("0.00001"),
		// The trader's own clock, in their own present. Harmless, and allowed.
		CreatedAt: time.Date(2026, 9, 12, 7, 15, 30, 0, time.UTC),
		UpdatedAt: time.Date(2026, 9, 12, 7, 15, 30, 0, time.UTC),
		Version:   3,
	}
}

func sampleTrade() domainexec.Trade {
	reason := domainexec.ExitStop
	closedBar, held := 231, 17
	return domainexec.Trade{
		ID: uuid.New(), SessionID: uuid.New(), AccountID: uuid.New(), EntryOrderID: uuid.New(),
		Side: domainexec.SideBuy, Quantity: dec("12500"), EntryPrice: dec("1.09121"),
		ExitPrice: ptr("1.08800"), StopLoss: dec("1.08800"), TakeProfit: ptr("1.09900"),
		Status: domainexec.TradeClosed, ExitReason: &reason,
		RealizedPnL: ptr("-40.125"), RMultiple: ptr("-1.0"),
		MaxAdverseExcursion: ptr("0.00321"), MaxFavorableExcursion: ptr("0.00140"),
		OpenedBarIndex: 214, ClosedBarIndex: &closedBar, BarsHeld: &held,
		OpenedAt: time.Date(2026, 9, 12, 7, 15, 30, 0, time.UTC),
		Version:  2,
	}
}

func sampleRisk() executiondomain.RiskState {
	return executiondomain.RiskState{
		Halted: false, RoomRemainingPct: dec("37.5"), OpenPositions: 1,
		Balance: dec("10000"), Equity: dec("9959.875"), CommittedMargin: dec("136.4"),
	}
}

// The order response is the one that matters most: the domain entity it is built from carries
// PlacedBarAt, so a mapper written by copying fields would take the date with it.
func TestTheOrderResponseCarriesNoBarInstant(t *testing.T) {
	t.Parallel()
	encoded, err := json.Marshal(OrderFromDomain(sampleOrder()))
	if err != nil {
		t.Fatalf("marshal order: %v", err)
	}
	body := string(encoded)
	for _, needle := range []string{"2023", "08:30", "placed_bar_at", "placedbarat", "bar_at", "opened_at_bar"} {
		if strings.Contains(strings.ToLower(body), strings.ToLower(needle)) {
			t.Errorf("order response leaks %q\n%s", needle, body)
		}
	}
	// 2026 is the trader's own clock and is allowed, so the date pattern is checked against
	// everything *except* the fields that legitimately hold it.
	stripped := strings.ReplaceAll(body, "2026-09-12T07:15:30Z", "")
	if match := isoDate.FindString(stripped); match != "" {
		t.Errorf("order response contains a calendar date %q\n%s", match, body)
	}
}

// The rejection has to survive the mapping. Without this, a response that dropped rejection_code
// would pass every leak assertion above while making BR-09 invisible to the client: the dock could
// no longer say which rule refused the order.
func TestTheOrderResponseKeepsTheRefusalAndItsNumbers(t *testing.T) {
	t.Parallel()
	wire := OrderFromDomain(sampleOrder())
	if wire.RejectionCode == nil || *wire.RejectionCode != domainexec.CodeStopTooWide {
		t.Errorf("rejection_code = %v, want %s", wire.RejectionCode, domainexec.CodeStopTooWide)
	}
	if wire.PlacedBarIndex != 214 {
		t.Errorf("placed_bar_index = %d, want 214", wire.PlacedBarIndex)
	}
	// Decimals cross as strings, so the client reads the number the server holds. decimal.String
	// renders the exact value and drops insignificant trailing zeros, so 1.09120 arrives as
	// "1.0912" — the same number, and the display precision is the chart's business, not the wire's.
	if wire.LimitPrice == nil || *wire.LimitPrice != "1.0912" {
		t.Errorf("limit_price = %v, want the exact decimal 1.0912", wire.LimitPrice)
	}
	// The one that would actually break. A float64 cannot represent this, so a response that had
	// drifted to JSON numbers would hand back 40.1 and every risk figure would be quietly wrong.
	if wire.RiskAmount == nil || *wire.RiskAmount != "40.100000000000001" {
		t.Errorf("risk_amount = %v, want the full precision preserved", wire.RiskAmount)
	}
}

// SP4-1 in one assertion: the risk readout says how close the gate is and nothing about the window.
func TestTheRiskResponseCarriesNoDayBoundary(t *testing.T) {
	t.Parallel()
	encoded, err := json.Marshal(RiskFromDomain(sampleRisk()))
	if err != nil {
		t.Fatalf("marshal risk: %v", err)
	}
	body := strings.ToLower(string(encoded))
	// Every shape the boundary could take: the instant itself, the ordinal of the day, a countdown,
	// and the allowance expressed as money — which would let a trader divide and recover the window.
	for _, needle := range []string{
		"reset", "day", "boundary", "window", "expires", "next_", "until",
		"seconds", "bars_remaining", "drawdown_limit", "allowance",
	} {
		if strings.Contains(body, needle) {
			t.Errorf("risk response leaks the daily window via %q\n%s", needle, body)
		}
	}
	if match := isoDate.FindString(body); match != "" {
		t.Errorf("risk response contains a calendar date %q\n%s", match, body)
	}
}

// And the readout still has to be useful, or the test above is satisfied by an empty struct.
func TestTheRiskResponseStillReportsTheRoomLeft(t *testing.T) {
	t.Parallel()
	wire := RiskFromDomain(sampleRisk())
	if wire.RoomRemainingPct != "37.5" {
		t.Errorf("room_remaining_pct = %q, want 37.5", wire.RoomRemainingPct)
	}
	if wire.Equity != "9959.875" {
		t.Errorf("equity = %q, want the exact decimal", wire.Equity)
	}
	if wire.OpenPositions != 1 {
		t.Errorf("open_positions = %d, want 1", wire.OpenPositions)
	}
}

func TestTheTradeResponseCarriesNoBarInstant(t *testing.T) {
	t.Parallel()
	encoded, err := json.Marshal(TradeFromDomain(sampleTrade()))
	if err != nil {
		t.Fatalf("marshal trade: %v", err)
	}
	body := string(encoded)
	stripped := strings.ReplaceAll(body, "2026-09-12T07:15:30Z", "")
	if match := isoDate.FindString(stripped); match != "" {
		t.Errorf("trade response contains a calendar date %q\n%s", match, body)
	}
	// Duration as a bar count, which is the blinded way to express it: seventeen bars says how long
	// the trader held without saying what one bar was worth in minutes of 2023.
	if wire := TradeFromDomain(sampleTrade()); wire.BarsHeld == nil || *wire.BarsHeld != 17 {
		t.Errorf("bars_held = %v, want 17", wire.BarsHeld)
	}
}

// The structural guard. The tests above catch a date in a value; this one catches a field that could
// hold one, so adding a bar instant to any of these types means deleting a test on purpose.
func TestNoExecutionResponseTypeCarriesABarInstant(t *testing.T) {
	t.Parallel()
	for _, typ := range []reflect.Type{
		reflect.TypeOf(Order{}), reflect.TypeOf(Trade{}), reflect.TypeOf(Risk{}),
	} {
		walk(t, typ, typ.Name())
	}
}

// banned names any field that would carry a bar's real instant or the daily window. CreatedAt,
// OpenedAt and UpdatedAt are absent on purpose: they are the trader's own clock in their own present,
// and a post-mortem that could not say when the trader acted would be worse than useless.
var banned = []string{
	"placedbarat", "barat", "closedbarat", "bartime", "timestamp", "openedattime",
	"windowstart", "windowend", "symbol", "instrumentid",
	"dayresetat", "dayindex", "resetat", "nextresetat", "barsuntilreset",
}

func walk(t *testing.T, typ reflect.Type, path string) {
	t.Helper()
	for typ.Kind() == reflect.Slice || typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Struct || typ == reflect.TypeOf(time.Time{}) {
		return
	}
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		for _, name := range banned {
			if strings.EqualFold(field.Name, name) {
				t.Errorf("%s.%s carries a bar instant or the daily window", path, field.Name)
			}
		}
		walk(t, field.Type, path+"."+field.Name)
	}
}
