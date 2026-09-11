// Package drawing holds the trader's chart annotations.
//
// Two rules this package exists to enforce:
//
//  1. **An anchor is a bar index, never a timestamp** (BR-01, NFR-05). A trendline that remembered
//     when it was drawn would date the window, and dating the window identifies the instrument as
//     surely as naming it.
//
//  2. **The server does not model the toolkit.** A fibonacci retracement's levels, a brush stroke's
//     points and a zone's opacity are the client's business. The server validates the envelope —
//     known kind, anchors in range, size cap, no date — so the terminal can add a tool without a
//     migration.
package drawing

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Kind is the tool a drawing came from. The vocabulary lives here rather than in a database CHECK
// precisely so that adding a tool is this list plus the client, not a migration.
type Kind string

const (
	KindTrendline      Kind = "trendline"
	KindHorizontal     Kind = "horizontal"
	KindRay            Kind = "ray"
	KindExtended       Kind = "extended"
	KindVertical       Kind = "vertical"
	KindFibRetracement Kind = "fib_retracement"
	KindFibExtension   Kind = "fib_extension"
	KindZone           Kind = "zone"
	KindPolyline       Kind = "polyline"
	KindBrush          Kind = "brush"
	KindNote           Kind = "note"
)

var kinds = map[Kind]struct{}{
	KindTrendline: {}, KindHorizontal: {}, KindRay: {}, KindExtended: {}, KindVertical: {},
	KindFibRetracement: {}, KindFibExtension: {}, KindZone: {}, KindPolyline: {}, KindBrush: {},
	KindNote: {},
}

func (k Kind) Valid() bool {
	_, ok := kinds[k]
	return ok
}

// MaxPayloadBytes caps one drawing. A brush stroke is the largest honest payload and runs to a few
// kilobytes; this leaves room for a long one while keeping a single row from becoming a file store.
const MaxPayloadBytes = 16 * 1024

// Drawing is one annotation on one timeframe.
//
// Timeframe is part of the identity, not decoration. Bar 214 on 15m and bar 214 on 1h are different
// moments, so an anchor means nothing without the timeframe it was placed on — which is why the
// terminal shows a drawing on its own timeframe and hides it on the others rather than converting.
type Drawing struct {
	ID              uuid.UUID
	SessionID       uuid.UUID
	Kind            Kind
	Timeframe       string
	Payload         json.RawMessage
	CreatedBarIndex int
	CreatedAt       time.Time
	UpdatedAt       time.Time
	Version         int
}

// timeKeys are field names that mean "when". A payload carrying one is carrying a clock, whatever
// it holds — so the key is enough to refuse on, without reading the value.
var timeKeys = map[string]struct{}{
	"timestamp": {}, "time": {}, "date": {}, "datetime": {}, "epoch": {}, "utc": {},
	"at": {}, "when": {}, "created_at": {}, "createdat": {}, "drawn_at": {}, "drawnat": {},
	"start_time": {}, "starttime": {}, "end_time": {}, "endtime": {},
}

// proseKeys are the fields that hold what the trader typed. A trader who writes "SVB week, March
// 2023" in their own note is guessing, not leaking — the server told them nothing — so their prose
// is exempt from the date scan. Everything else in the payload is machine-written and is not.
var proseKeys = map[string]struct{}{
	"text": {}, "label": {}, "note": {}, "title": {}, "comment": {}, "thesis": {},
}

var dateLike = regexp.MustCompile(`\b(19|20)\d{2}[-/](0?[1-9]|1[0-2])[-/](0?[1-9]|[12]\d|3[01])\b`)

// unix second bounds, roughly 2001-09-09 to 2033-05-18. A bar index, a price and an opacity are all
// far outside this; a clock reading in seconds or milliseconds is inside it.
const (
	unixSecondsLow  = 1_000_000_000
	unixSecondsHigh = 2_000_000_000
)

// DateLeak reports whether a payload carries something that could date the window.
//
// It walks the decoded JSON rather than matching the raw bytes, because the server does not know
// the shape of a drawing — that is the design — and a structural check would need updating for
// every new tool, which is exactly the maintenance burden this package avoids. Walking is
// shape-agnostic: a tool added tomorrow is scanned on the day it ships.
//
// It is deliberately blunt about everything except prose. A false positive costs the trader one
// refused drawing and a message saying why; a false negative costs the product its premise.
func DateLeak(payload []byte) bool {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	// Numbers stay as their literal text rather than becoming float64. That is partly the
	// architecture rule — no float arithmetic anywhere under domain — and partly because this check
	// is about integers: routing a clock reading through a float to compare it against a bound is
	// the sort of thing that works until the number is large enough that it does not.
	decoder.UseNumber()
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		// Unparseable payloads are refused by the caller as invalid JSON, not here. Reporting "no
		// leak" for bytes nobody could read would be a lie of the wrong shape.
		return false
	}
	return walk(decoded, false)
}

func walk(node any, prose bool) bool {
	switch value := node.(type) {
	case map[string]any:
		for key, child := range value {
			lower := strings.ToLower(key)
			if _, timey := timeKeys[lower]; timey {
				return true
			}
			_, isProse := proseKeys[lower]
			if walk(child, isProse) {
				return true
			}
		}
	case []any:
		for _, child := range value {
			if walk(child, prose) {
				return true
			}
		}
	case string:
		if !prose && dateLike.MatchString(value) {
			return true
		}
	case json.Number:
		// Prose can hold any number; a machine field holding a clock reading cannot.
		if !prose && isClockReading(value) {
			return true
		}
	}
	return false
}

func isClockReading(value json.Number) bool {
	whole, err := value.Int64()
	if err != nil {
		// A fraction, or a number too large to be an int64. Either way it is a price, an opacity
		// or nonsense — not a clock reading, which is what this looks for.
		return false
	}
	if whole >= unixSecondsLow && whole <= unixSecondsHigh {
		return true
	}
	// The same window in milliseconds.
	return whole >= unixSecondsLow*1000 && whole <= unixSecondsHigh*1000
}
