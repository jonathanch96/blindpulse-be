package sessionresponse

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
	domainsession "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/session"
	"github.com/shopspring/decimal"
)

// The streaming half of the NFR-05 guard.
//
// The frame path is where a date is most likely to slip out, because the internal frame genuinely
// carries one: ReleasedAt, the instant this engine released the bar. It is a server clock and says
// nothing about when the bar traded — but "this timestamp is harmless" is exactly the reasoning
// that ends with a timestamp on the wire, so the wire type carries no date at all and this test
// is what makes that a property rather than a habit.

// No trailing \b: in "2026-09-10T07:15:30Z" the digit and the T are both word
// characters, so there is no boundary between them and a trailing \b would miss the
// single most likely way a date reaches the wire.
var frameISODate = regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}`)

func sampleFrame() domainsession.Frame {
	bar := domainfeed.Normalization{
		Offset:      decimal.RequireFromString("0.4137"),
		Scale:       decimal.RequireFromString("3.72"),
		VolumeScale: decimal.RequireFromString("1.5"),
	}.ApplyBar(market.Bar{
		// The SVB collapse week again: a window a trader would recognize instantly if it reached
		// them, which is the point of using it as the fixture.
		OpenedAt: time.Date(2023, 3, 14, 8, 30, 0, 0, time.UTC),
		Open:     decimal.RequireFromString("1.0850"),
		High:     decimal.RequireFromString("1.0955"),
		Low:      decimal.RequireFromString("1.0788"),
		Close:    decimal.RequireFromString("1.0912"),
		Volume:   decimal.RequireFromString("18420"),
	}, 214)
	return domainsession.Frame{
		SessionID:     uuid.MustParse("2f1c4e10-9a3d-4c77-8b21-6d5e0f3a9c11"),
		Kind:          domainsession.FrameBar,
		Status:        domainsession.StatusOpen,
		Timeframe:     "15m",
		Speed:         "3",
		CursorIndex:   214,
		RevealedIndex: 214,
		TotalBars:     701,
		Bar:           &bar,
		ReleasedAt:    time.Date(2026, 9, 10, 7, 15, 30, 0, time.UTC),
	}
}

func TestStreamFrameCarriesNoDate(t *testing.T) {
	t.Parallel()
	now := sampleFrame().ReleasedAt.Add(7 * time.Millisecond)
	encoded, err := json.Marshal(FrameFromDomain(sampleFrame(), now))
	if err != nil {
		t.Fatalf("marshal frame: %v", err)
	}
	body := string(encoded)
	for _, needle := range []string{"2023", "2026", "08:30", "released_at", "releasedat", "openedat", "opened_at", "timestamp"} {
		if strings.Contains(strings.ToLower(body), strings.ToLower(needle)) {
			t.Errorf("stream frame leaks %q\n%s", needle, body)
		}
	}
	if match := frameISODate.FindString(body); match != "" {
		t.Errorf("stream frame contains a calendar date %q\n%s", match, body)
	}
}

// The latency readout is the reason ReleasedAt exists at all, so prove the conversion actually
// uses it. Without this, a frame type that dropped the date *and* the latency would still pass the
// test above while quietly failing FR-REPLAY-08.
func TestStreamFrameReportsLatencyInsteadOfTheInstant(t *testing.T) {
	t.Parallel()
	frame := sampleFrame()
	wire := FrameFromDomain(frame, frame.ReleasedAt.Add(7*time.Millisecond))
	if wire.LatencyMs != 7 {
		t.Errorf("latency_ms = %d, want 7", wire.LatencyMs)
	}
	if wire.Bar == nil || wire.Bar.Index != 214 {
		t.Fatalf("frame lost its bar: %+v", wire.Bar)
	}
	if wire.BarsScanned != 215 {
		t.Errorf("bars_scanned = %d, want 215 (the revealed edge is zero-based)", wire.BarsScanned)
	}
}

// Clock skew between the replica that released a bar and the one serving the socket can put "now"
// behind ReleasedAt. A negative latency in the readout reads as a broken product, so it floors.
func TestStreamFrameFloorsNegativeLatency(t *testing.T) {
	t.Parallel()
	frame := sampleFrame()
	if got := FrameFromDomain(frame, frame.ReleasedAt.Add(-2*time.Second)).LatencyMs; got != 0 {
		t.Errorf("latency_ms = %d under clock skew, want 0", got)
	}
}

// The structural guard: no field anywhere in the wire frame may carry an absolute time, so adding
// one has to mean deleting this test on purpose.
func TestStreamFrameTypeHasNoTimeFields(t *testing.T) {
	t.Parallel()
	walkFrameFields(t, reflect.TypeOf(Frame{}))
}

func walkFrameFields(t *testing.T, typ reflect.Type) {
	t.Helper()
	for typ.Kind() == reflect.Slice || typ.Kind() == reflect.Ptr {
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Struct {
		return
	}
	if typ == reflect.TypeOf(time.Time{}) {
		t.Errorf("%s carries a time.Time; a streamed frame must not carry an absolute time", typ.Name())
		return
	}
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		for _, banned := range []string{"releasedat", "openedat", "timestamp", "windowstart", "windowend", "symbol"} {
			if strings.EqualFold(field.Name, banned) {
				t.Errorf("Frame.%s is an identity-bearing field on a streamed frame", field.Name)
			}
		}
		walkFrameFields(t, field.Type)
	}
}
