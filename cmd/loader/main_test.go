package main

import (
	"testing"
	"time"

	"github.com/google/uuid"
	feeddomain "github.com/jblabs/blindpulse-be/services/blindpulse/v1/domain/feed"
	"github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/market"
	"github.com/shopspring/decimal"
)

// 2023-11-14T22:13:20Z, expressed four ways. Every real provider picks one of these and none of
// them says which.
const (
	epochSeconds = int64(1700000000)
	epochMillis  = int64(1700000000000)
	epochMicros  = int64(1700000000000000)
	epochNanos   = int64(1700000000000000000)
)

var expected = time.Date(2023, 11, 14, 22, 13, 20, 0, time.UTC)

// The regression behind BE-02-1. Treating every integer as seconds turned a millisecond epoch —
// which is what Binance emits everywhere, in both the REST API and the CSV dumps — into the year
// 55,840, with no error and nothing downstream to notice.
func TestParseTimeDetectsTheEpochUnit(t *testing.T) {
	t.Parallel()
	cases := map[string]int64{
		"seconds":      epochSeconds,
		"milliseconds": epochMillis,
		"microseconds": epochMicros,
		"nanoseconds":  epochNanos,
	}
	for unit, epoch := range cases {
		t.Run(unit, func(t *testing.T) {
			t.Parallel()
			parsed, err := parseTime(decimal.NewFromInt(epoch).String())
			if err != nil {
				t.Fatalf("parseTime(%d) error = %v", epoch, err)
			}
			if !parsed.Equal(expected) {
				t.Errorf("parseTime(%d) = %s, want %s", epoch, parsed, expected)
			}
		})
	}
}

// The specific failure, named: a millisecond epoch must never land in the far future.
func TestParseTimeDoesNotDateMillisecondsToTheYear55840(t *testing.T) {
	t.Parallel()
	parsed, err := parseTime("1700000000000")
	if err != nil {
		t.Fatalf("parseTime error = %v", err)
	}
	if parsed.Year() != 2023 {
		t.Errorf("a millisecond epoch parsed to year %d, want 2023", parsed.Year())
	}
}

// Refusing rather than guessing: a value that is neither sensible seconds nor sensible
// milliseconds is a mistake elsewhere in the file, and picking an interpretation would bury it.
func TestParseTimeRefusesImplausibleValues(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{"0", "-1", "1", "99999999999999999999", "not-a-time", ""} {
		if parsed, err := parseTime(raw); err == nil {
			t.Errorf("parseTime(%q) = %s, want an error", raw, parsed)
		}
	}
}

func TestParseTimeAcceptsRFC3339AndNormalizesToUTC(t *testing.T) {
	t.Parallel()
	parsed, err := parseTime("2023-11-14T23:13:20+01:00")
	if err != nil {
		t.Fatalf("parseTime error = %v", err)
	}
	if !parsed.Equal(expected) || parsed.Location() != time.UTC {
		t.Errorf("parseTime = %s (%s), want %s in UTC", parsed, parsed.Location(), expected)
	}
}

func TestParseTimeRefusesAnRFC3339DateOutsideThePlausibleWindow(t *testing.T) {
	t.Parallel()
	if _, err := parseTime("1789-07-14T00:00:00Z"); err == nil {
		t.Error("an implausible RFC3339 date was accepted")
	}
}

func bar(at time.Time, open, high, low, close string) market.Bar {
	return market.Bar{
		InstrumentID: uuid.New(), Timeframe: market.TF1m, OpenedAt: at,
		Open:   decimal.RequireFromString(open),
		High:   decimal.RequireFromString(high),
		Low:    decimal.RequireFromString(low),
		Close:  decimal.RequireFromString(close),
		Volume: decimal.NewFromInt(10),
	}
}

// BE-02-2: ValidateSeries was correct, tested, and called by nothing. These assert the properties
// the loader now depends on, so a change that stops calling it fails here rather than silently.
func TestValidateSeriesRejectsWhatTheLoaderMustRefuse(t *testing.T) {
	t.Parallel()
	base := time.Date(2023, 11, 14, 22, 0, 0, 0, time.UTC)

	t.Run("out of order", func(t *testing.T) {
		t.Parallel()
		// Aggregate assumes sorted input, so this is not cosmetic: an unsorted file produces a 1h
		// candle whose open came from the wrong minute.
		series := []market.Bar{
			bar(base.Add(2*time.Minute), "1", "2", "0.5", "1.5"),
			bar(base.Add(time.Minute), "1", "2", "0.5", "1.5"),
		}
		if problems := feeddomain.ValidateSeries(series); len(problems) == 0 {
			t.Error("an out-of-order series was accepted")
		}
	})

	t.Run("duplicate timestamps", func(t *testing.T) {
		t.Parallel()
		series := []market.Bar{
			bar(base, "1", "2", "0.5", "1.5"),
			bar(base, "1", "2", "0.5", "1.5"),
		}
		if problems := feeddomain.ValidateSeries(series); len(problems) == 0 {
			t.Error("a duplicated timestamp was accepted")
		}
	})

	t.Run("inconsistent OHLC", func(t *testing.T) {
		t.Parallel()
		// High below the close: not a bar that ever traded.
		series := []market.Bar{bar(base, "1", "1.2", "0.5", "1.5")}
		if problems := feeddomain.ValidateSeries(series); len(problems) == 0 {
			t.Error("an inconsistent bar was accepted")
		}
	})

	t.Run("a sound series passes", func(t *testing.T) {
		t.Parallel()
		series := []market.Bar{
			bar(base, "1", "2", "0.5", "1.5"),
			bar(base.Add(time.Minute), "1.5", "2.5", "1.4", "2"),
		}
		if problems := feeddomain.ValidateSeries(series); len(problems) != 0 {
			t.Errorf("a sound series was rejected: %v", problems)
		}
	})
}
