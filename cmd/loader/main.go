// Command loader ingests historical OHLCV into blindpulse.market_bars and, optionally, builds
// blinded feeds over what it loaded.
//
// It is idempotent: bars key on (instrument, timeframe, opened_at) and conflicts do nothing, so a
// file that half-loaded is fixed by running it again rather than by cleaning up first.
//
//	loader -symbol EURUSD -class fx -file eurusd_1m.csv           # ingest + derive timeframes
//	loader -symbol EURUSD -build -timeframe 15m -bars 800         # build a blinded feed
//
// CSV columns: timestamp,open,high,low,close,volume  (RFC3339 or unix seconds)
package main

import (
	"context"
	"encoding/csv"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jblabs/blindpulse-be/adapters/rest/config"
	appLogger "github.com/jblabs/blindpulse-be/pkg/logger"
	feedsdb "github.com/jblabs/blindpulse-be/services/blindpulse/v1/db/blindpulse/blinded_feeds"
	instrumentsdb "github.com/jblabs/blindpulse-be/services/blindpulse/v1/db/blindpulse/instruments"
	barsdb "github.com/jblabs/blindpulse-be/services/blindpulse/v1/db/blindpulse/market_bars"
	feeddomain "github.com/jblabs/blindpulse-be/services/blindpulse/v1/domain/feed"
	"github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/market"
	"github.com/shopspring/decimal"
)

// maxReportedProblems bounds the error message. A file with every row out of order would otherwise
// print one line per row, and the first few are enough to see what is wrong.
const maxReportedProblems = 10

func main() {
	var (
		symbol     = flag.String("symbol", "", "instrument symbol, e.g. EURUSD")
		name       = flag.String("name", "", "display name (defaults to the symbol)")
		class      = flag.String("class", "fx", "asset class: fx, equity, crypto, futures, index, commodity")
		file       = flag.String("file", "", "CSV of 1m bars to ingest")
		build      = flag.Bool("build", false, "build a blinded feed after ingesting")
		timeframe  = flag.String("timeframe", "15m", "feed base timeframe")
		barCount   = flag.Int("bars", 800, "bars in the feed window")
		warmup     = flag.Int("warmup", 200, "lookback bars shown before the cursor moves")
		macroLabel = flag.String("macro", "", "macro label, withheld until the reveal")
		tickSize   = flag.String("tick", "", "tick size; derived from the asset class and symbol when empty")
		quote      = flag.String("quote", "", "quote currency; derived from the symbol when empty")
		publish    = flag.Bool("publish", true, "publish the feed to the catalogue")
	)
	flag.Parse()

	if strings.TrimSpace(*symbol) == "" {
		fatal(fmt.Errorf("-symbol is required"))
	}

	cfg, err := config.Load()
	if err != nil {
		fatal(err)
	}
	log := appLogger.New(cfg.App.Env)
	db, err := config.NewDatabase(cfg, log)
	if err != nil {
		fatal(err)
	}

	ctx := context.Background()
	instruments := instrumentsdb.New(db)
	bars := barsdb.New(db)

	displayName := *name
	if displayName == "" {
		displayName = *symbol
	}
	// Derived rather than hardcoded. Every instrument used to be stamped with an FX tick and a USD
	// quote whatever it was, which is right for EURUSD and wrong for USDJPY, an equity and BTC.
	// Nothing reads tick size yet; Sprint 04's order gate is the first consumer, and there a wrong
	// tick produces a plausible-looking size that is off by a factor of a hundred.
	assetClass := market.AssetClass(*class)
	conventions := market.DeriveConventions(*symbol, assetClass)
	if *quote != "" {
		conventions.QuoteCurrency = strings.ToUpper(*quote)
	}
	if *tickSize != "" {
		parsed, err := decimal.NewFromString(*tickSize)
		if err != nil || !parsed.IsPositive() {
			fatal(fmt.Errorf("-tick %q is not a positive decimal", *tickSize))
		}
		conventions.TickSize = parsed
	}
	instrument, err := instruments.Upsert(ctx, &market.Instrument{
		Symbol: strings.ToUpper(*symbol), DisplayName: displayName,
		AssetClass: assetClass, QuoteCurrency: conventions.QuoteCurrency,
		TickSize: conventions.TickSize, ContractSize: conventions.ContractSize,
	})
	if err != nil {
		fatal(err)
	}
	log.Info("instrument ready", "symbol", instrument.Symbol, "id", instrument.ID,
		"quote", instrument.QuoteCurrency, "tick", instrument.TickSize.String())

	if *file != "" {
		if err := ingest(ctx, bars, instrument, *file, log.Info); err != nil {
			fatal(err)
		}
	}

	if *build {
		service := feeddomain.NewService(feeddomain.Dependencies{
			Repo: feedsdb.New(db), Bars: bars, Instruments: instruments,
		})
		target := market.Timeframe(*timeframe)
		first, last, err := bars.Bounds(ctx, instrument.ID, target)
		if err != nil {
			fatal(fmt.Errorf("no %s bars for %s — ingest first: %w", target, instrument.Symbol, err))
		}
		interval, ok := target.Duration()
		if !ok {
			fatal(fmt.Errorf("unsupported timeframe %q", target))
		}
		// Take the most recent `barCount` bars available, bounded by what actually exists.
		start := last - int64(*barCount)*int64(interval/time.Second)
		if start < first {
			start = first
		}
		feed, err := service.Build(ctx, feeddomain.BuildInput{
			InstrumentID: instrument.ID, Timeframe: target,
			WindowStart: time.Unix(start, 0).UTC(), WindowEnd: time.Unix(last, 0).UTC(),
			WarmupBars: *warmup, MacroLabel: *macroLabel, Publish: *publish,
		})
		if err != nil {
			fatal(err)
		}
		// The alias is safe to print; the symbol it hides is not printed beside it anywhere a
		// trader could see, and this is an operator tool.
		log.Info("feed built", "alias", feed.AliasLabel, "id", feed.ID,
			"difficulty", feed.Difficulty, "bars", feed.TotalBars, "tradeable", feed.TradeableBars())
	}
}

func ingest(ctx context.Context, repo feeddomain.BarRepository, instrument *market.Instrument, path string,
	logf func(string, ...any)) error {
	handle, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer func() { _ = handle.Close() }()

	reader := csv.NewReader(handle)
	reader.FieldsPerRecord = -1
	parsed := make([]market.Bar, 0, 4096)
	rejected := 0
	row := 0

	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		row++
		if row == 1 && !looksNumeric(record[0]) {
			continue // header
		}
		bar, err := parseRow(instrument.ID, record)
		if err != nil {
			// Reject the row and keep going, naming it. A whole file failing because of one bad
			// line means nobody ever loads the other 99,999.
			logf("rejected row", "row", row, "err", err)
			rejected++
			continue
		}
		if !bar.Valid() {
			logf("rejected row", "row", row, "err", "OHLC values are inconsistent")
			rejected++
			continue
		}
		parsed = append(parsed, bar)
	}

	if len(parsed) == 0 {
		return fmt.Errorf("%s produced no valid bars (%d rejected)", path, rejected)
	}

	// Per-row validation catches a malformed bar; this catches a malformed *series*. Duplicates and
	// out-of-order rows used to load silently, and Aggregate assumes sorted input — so an unsorted
	// CSV produced quietly wrong higher timeframes, a 1h candle whose open came from the wrong
	// minute (review finding BE-02-2).
	//
	// The whole file is refused rather than the offending rows dropped: a series that is out of
	// order is not a series with a few bad rows in it, it is a file we do not understand.
	if problems := feeddomain.ValidateSeries(parsed); len(problems) > 0 {
		shown := problems
		if len(shown) > maxReportedProblems {
			shown = shown[:maxReportedProblems]
		}
		return fmt.Errorf("%s is not a valid series (%d problems, first %d shown):\n  %s",
			path, len(problems), len(shown), strings.Join(shown, "\n  "))
	}

	inserted, err := repo.Insert(ctx, parsed)
	if err != nil {
		return err
	}
	logf("ingested base series", "timeframe", market.TF1m, "parsed", len(parsed),
		"inserted", inserted, "rejected", rejected)

	// Derive the higher timeframes now, so the replay path never aggregates on read.
	for _, target := range market.DerivedTimeframes() {
		rolled := feeddomain.Aggregate(parsed, target)
		if len(rolled) == 0 {
			continue
		}
		count, err := repo.Insert(ctx, rolled)
		if err != nil {
			return err
		}
		logf("derived timeframe", "timeframe", target, "bars", len(rolled), "inserted", count)
	}
	return nil
}

func parseRow(instrumentID uuid.UUID, record []string) (market.Bar, error) {
	if len(record) < 6 {
		return market.Bar{}, fmt.Errorf("expected 6 columns, got %d", len(record))
	}
	openedAt, err := parseTime(record[0])
	if err != nil {
		return market.Bar{}, err
	}
	values := make([]decimal.Decimal, 5)
	for i := 0; i < 5; i++ {
		value, err := decimal.NewFromString(strings.TrimSpace(record[i+1]))
		if err != nil {
			return market.Bar{}, fmt.Errorf("column %d: %w", i+1, err)
		}
		values[i] = value
	}
	return market.Bar{
		InstrumentID: instrumentID, Timeframe: market.TF1m, OpenedAt: openedAt,
		Open: values[0], High: values[1], Low: values[2], Close: values[3], Volume: values[4],
	}, nil
}

// The range a market bar's timestamp may plausibly fall in. Anything outside it is a unit mistake
// or a column-order mistake, and both are better refused than stored.
var (
	earliestPlausible = time.Date(1990, 1, 1, 0, 0, 0, 0, time.UTC)
	latestPlausible   = time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC)
)

// parseTime reads a bar timestamp, detecting the unit of a bare integer rather than assuming one.
//
// The previous version treated every integer as unix *seconds*. A millisecond epoch —
// 1700000000000, which is November 2023 and what Binance emits everywhere — parsed successfully as
// the year 55,840. There was no error and nothing downstream noticed, so an entire load of the best
// free crypto data available would have looked like it worked (review finding BE-02-1).
//
// Detection is by magnitude, and anything that lands outside a plausible window is **refused**
// rather than guessed at: a value that is neither sensible seconds nor sensible milliseconds is a
// mistake somewhere else in the file, and picking an interpretation would bury it.
func parseTime(raw string) (time.Time, error) {
	trimmed := strings.TrimSpace(raw)
	if epoch, err := strconv.ParseInt(trimmed, 10, 64); err == nil {
		parsed, err := fromEpoch(epoch)
		if err != nil {
			return time.Time{}, err
		}
		return parsed, nil
	}
	parsed, err := time.Parse(time.RFC3339, trimmed)
	if err != nil {
		return time.Time{}, fmt.Errorf("timestamp %q is neither a unix epoch nor RFC3339", trimmed)
	}
	if !plausible(parsed) {
		return time.Time{}, fmt.Errorf("timestamp %q is outside %d-%d", trimmed, earliestPlausible.Year(), latestPlausible.Year())
	}
	return parsed.UTC(), nil
}

// fromEpoch tries each unit in turn and takes the one that lands in a plausible year. The units are
// orders of magnitude apart, so at most one can ever match — there is no ambiguity to resolve, only
// a unit to identify.
func fromEpoch(epoch int64) (time.Time, error) {
	for _, candidate := range []struct {
		unit string
		at   time.Time
	}{
		{"seconds", time.Unix(epoch, 0).UTC()},
		{"milliseconds", time.UnixMilli(epoch).UTC()},
		{"microseconds", time.UnixMicro(epoch).UTC()},
		{"nanoseconds", time.Unix(0, epoch).UTC()},
	} {
		if plausible(candidate.at) {
			return candidate.at, nil
		}
	}
	return time.Time{}, fmt.Errorf(
		"epoch %d is not a plausible timestamp in seconds, milliseconds, microseconds or nanoseconds", epoch)
}

func plausible(at time.Time) bool {
	return at.After(earliestPlausible) && at.Before(latestPlausible)
}

func looksNumeric(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return false
	}
	if _, err := strconv.ParseInt(trimmed, 10, 64); err == nil {
		return true
	}
	_, err := time.Parse(time.RFC3339, trimmed)
	return err == nil
}

func fatal(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
