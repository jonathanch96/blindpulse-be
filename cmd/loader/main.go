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
	instrument, err := instruments.Upsert(ctx, &market.Instrument{
		Symbol: strings.ToUpper(*symbol), DisplayName: displayName,
		AssetClass: market.AssetClass(*class), QuoteCurrency: "USD",
		TickSize: decimal.RequireFromString("0.00001"), ContractSize: decimal.NewFromInt(1),
	})
	if err != nil {
		fatal(err)
	}
	log.Info("instrument ready", "symbol", instrument.Symbol, "id", instrument.ID)

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

func parseTime(raw string) (time.Time, error) {
	trimmed := strings.TrimSpace(raw)
	if seconds, err := strconv.ParseInt(trimmed, 10, 64); err == nil {
		return time.Unix(seconds, 0).UTC(), nil
	}
	parsed, err := time.Parse(time.RFC3339, trimmed)
	if err != nil {
		return time.Time{}, fmt.Errorf("timestamp %q is neither unix seconds nor RFC3339", trimmed)
	}
	return parsed.UTC(), nil
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
