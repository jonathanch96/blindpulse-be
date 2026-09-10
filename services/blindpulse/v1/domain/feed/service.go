package feed

import (
	"context"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jblabs/blindpulse-be/pkg/apperror"
	"github.com/jblabs/blindpulse-be/pkg/stats"
	domainfeed "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/feed"
	"github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/market"
	"github.com/shopspring/decimal"
)

// BuilderVersion identifies the normalization scheme. Bump it when the map changes, so a feed
// built under an older scheme is identifiable and can be rebuilt deliberately instead of silently
// serving two different maps of the same window.
const BuilderVersion = 1

// targetPriceMagnitude is the band every feed's prices are mapped into, regardless of what the
// real instrument trades at. EUR/USD near 1.08 and BTC near 60,000 must come out looking alike, or
// the magnitude alone tells the trader the asset class and the blinding is decorative.
var (
	targetMagnitudeLow  = decimal.NewFromInt(80)
	targetMagnitudeHigh = decimal.NewFromInt(320)

	// Difficulty thresholds on realized volatility and trend persistence. Deliberately coarse:
	// this is a hint for choosing a session, not a risk model.
	crisisVolatility     = decimal.RequireFromString("0.012")
	elevatedVolatility   = decimal.RequireFromString("0.006")
	compressedVolatility = decimal.RequireFromString("0.002")
	directionlessTrend   = decimal.RequireFromString("0.15")
)

func NewService(deps Dependencies) Service {
	if deps.Clock == nil {
		deps.Clock = func() time.Time { return time.Now().UTC() }
	}
	if deps.Rand == nil {
		source := rand.New(rand.NewSource(time.Now().UnixNano()))
		deps.Rand = source.Float64
	}
	return &service{deps: deps}
}

func (s *service) Build(ctx context.Context, in BuildInput) (*domainfeed.Feed, error) {
	if !in.Timeframe.Valid() {
		return nil, apperror.New("INVALID_TIMEFRAME")
	}
	if !in.WindowEnd.After(in.WindowStart) {
		return nil, apperror.WithFields("VALIDATION_FAILED", []apperror.FieldError{
			{Field: "window_end", Rule: "after", Message: "window_end must be after window_start"},
		})
	}
	if in.WarmupBars < 0 {
		return nil, apperror.WithFields("VALIDATION_FAILED", []apperror.FieldError{
			{Field: "warmup_bars", Rule: "min", Message: "warmup_bars cannot be negative"},
		})
	}
	if _, err := s.deps.Instruments.GetByID(ctx, in.InstrumentID); err != nil {
		return nil, err
	}

	bars, err := s.deps.Bars.ListWindow(ctx, in.InstrumentID, in.Timeframe, in.WindowStart.Unix(), in.WindowEnd.Unix())
	if err != nil {
		return nil, err
	}
	if len(bars)-in.WarmupBars < MinTradeableBars {
		return nil, apperror.Newf("FEED_WINDOW_TOO_SHORT",
			"window yields %d tradeable bars; at least %d are required", max(0, len(bars)-in.WarmupBars), MinTradeableBars)
	}

	normalization := s.drawNormalization(bars)
	alias, err := s.mintAlias(ctx)
	if err != nil {
		return nil, err
	}
	closes := make([]decimal.Decimal, 0, len(bars))
	for _, bar := range bars {
		closes = append(closes, bar.Close)
	}
	volatility := stats.Volatility(closes)
	persistence := stats.TrendPersistence(closes)

	entity := &domainfeed.Feed{
		ID: uuid.New(), InstrumentID: in.InstrumentID, AliasLabel: alias,
		BaseTimeframe: in.Timeframe,
		WindowStart:   bars[0].OpenedAt, WindowEnd: bars[len(bars)-1].OpenedAt,
		WarmupBars: in.WarmupBars, TotalBars: len(bars),
		Normalization:      normalization,
		Difficulty:         difficultyFor(volatility, persistence),
		MacroLabel:         trimmedOrNil(in.MacroLabel),
		IsPublished:        in.Publish,
		RealizedVolatility: &volatility, TrendPersistence: &persistence,
		BuilderVersion: BuilderVersion, BuiltAt: s.deps.Clock(),
	}
	return s.deps.Repo.Create(ctx, entity)
}

func (s *service) List(ctx context.Context, _ uuid.UUID, filter ListFilter) ([]domainfeed.Feed, error) {
	return s.deps.Repo.List(ctx, normalizeFilter(filter))
}

func (s *service) Get(ctx context.Context, id uuid.UUID) (*domainfeed.Feed, error) {
	entity, err := s.deps.Repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	// An unpublished feed is a draft. Reporting it as not-found rather than forbidden keeps the
	// catalogue from confirming that a given id exists at all.
	if !entity.IsPublished {
		return nil, apperror.New("FEED_NOT_FOUND")
	}
	return entity, nil
}

func (s *service) Random(ctx context.Context, userID uuid.UUID, filter ListFilter) (*domainfeed.Feed, error) {
	candidates, err := s.deps.Repo.ListExcludingTraded(ctx, userID, normalizeFilter(filter))
	if err != nil {
		return nil, err
	}
	if len(candidates) == 0 {
		return nil, apperror.Newf("FEED_NOT_FOUND", "no unseen feed matches this filter")
	}
	// Uniform over the untraded set. Weighting by difficulty here would quietly curate the
	// trader's experience, which is the curve-fitting the product exists to prevent.
	return &candidates[stats.Index(s.deps.Rand, len(candidates))], nil
}

// windowBars is the one place a feed's real bars are fetched.
//
// A feed's window is immutable once built, so this is a cache with no invalidation question — only
// a TTL. Both read paths go through here rather than calling ListWindow directly, because the
// streaming path calls it once per released bar and a second call site would be a second chance to
// forget the cache.
func (s *service) windowBars(ctx context.Context, entity *domainfeed.Feed) ([]market.Bar, error) {
	if s.deps.Windows != nil {
		if cached, ok := s.deps.Windows.Get(ctx, entity.ID); ok {
			return cached, nil
		}
	}
	real, err := s.deps.Bars.ListWindow(ctx, entity.InstrumentID, entity.BaseTimeframe,
		entity.WindowStart.Unix(), entity.WindowEnd.Unix())
	if err != nil {
		return nil, err
	}
	if s.deps.Windows != nil {
		s.deps.Windows.Put(ctx, entity.ID, real)
	}
	return real, nil
}

func (s *service) Bars(ctx context.Context, id uuid.UUID, from, to int) ([]domainfeed.Bar, error) {
	entity, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if from < 0 || to < from || to >= entity.TotalBars {
		return nil, apperror.New("INVALID_CURSOR")
	}
	real, err := s.windowBars(ctx, entity)
	if err != nil {
		return nil, err
	}
	if to >= len(real) {
		return nil, apperror.New("BARS_EXHAUSTED")
	}
	blinded := make([]domainfeed.Bar, 0, to-from+1)
	for index := from; index <= to; index++ {
		blinded = append(blinded, entity.Normalization.ApplyBar(real[index], index))
	}
	return blinded, nil
}

// drawNormalization picks the affine map for a feed. The scale is chosen so the window's median
// price lands inside a fixed band shared by every feed, and the offset is a fraction of the
// window's own range — large enough to break the percentage-return fingerprint, small enough that
// prices stay positive.
func (s *service) drawNormalization(bars []market.Bar) domainfeed.Normalization {
	low, high := bars[0].Low, bars[0].High
	var sum decimal.Decimal
	for _, bar := range bars {
		if bar.Low.LessThan(low) {
			low = bar.Low
		}
		if bar.High.GreaterThan(high) {
			high = bar.High
		}
		sum = sum.Add(bar.Close)
	}
	mean := sum.Div(decimal.NewFromInt(int64(len(bars))))
	span := high.Sub(low)
	if span.IsZero() {
		span = mean.Abs()
	}

	// Offset is drawn from the window's own range, so the distortion of percentage returns is
	// meaningful relative to the series rather than a fixed number that barely moves an
	// instrument priced at 60,000 and overwhelms one priced at 1.08.
	offset := span.Mul(stats.Between(s.deps.Rand, decimal.NewFromFloat(0.25), decimal.NewFromInt(2)))
	shifted := mean.Add(offset)
	if shifted.LessThanOrEqual(decimal.Zero) {
		// A window whose mean sits at or below the negated offset would map to non-positive
		// prices. Shift clear of zero rather than emitting a chart with negative candles.
		offset = mean.Abs().Add(span)
		shifted = mean.Add(offset)
	}

	target := stats.Between(s.deps.Rand, targetMagnitudeLow, targetMagnitudeHigh)
	scale := target.Div(shifted)
	if scale.LessThanOrEqual(decimal.Zero) {
		scale = decimal.NewFromInt(1)
	}

	// Volume is rebased independently: absolute share or contract counts identify a venue on
	// their own, and the trader only ever reads volume relative to its own history.
	volumeScale := stats.Between(s.deps.Rand, decimal.NewFromFloat(0.5), decimal.NewFromInt(3))

	return domainfeed.Normalization{
		Offset:      offset.Round(10),
		Scale:       scale.Round(10),
		VolumeScale: volumeScale.Round(10),
	}
}

func (s *service) mintAlias(ctx context.Context) (string, error) {
	for attempt := 0; attempt < 5; attempt++ {
		number, err := s.deps.Repo.NextAliasNumber(ctx)
		if err != nil {
			return "", err
		}
		alias := fmt.Sprintf("Asset #%d", number)
		taken, err := s.deps.Repo.ExistsByAlias(ctx, alias)
		if err != nil {
			return "", err
		}
		if !taken {
			return alias, nil
		}
	}
	return "", apperror.New("INTERNAL_ERROR")
}

// difficultyFor turns the window statistics into the label the catalogue shows. The thresholds are
// deliberately coarse: this is a hint for choosing a session, not a risk model.
func difficultyFor(volatility, persistence decimal.Decimal) domainfeed.Difficulty {
	switch {
	case volatility.GreaterThanOrEqual(crisisVolatility):
		return domainfeed.DifficultyCrisis
	case volatility.GreaterThanOrEqual(elevatedVolatility):
		return domainfeed.DifficultyVolatile
	case volatility.LessThanOrEqual(compressedVolatility) && persistence.LessThan(directionlessTrend):
		// Low volatility and no net direction: the hardest window to trade badly, and the
		// easiest to be bored by.
		return domainfeed.DifficultyCalm
	default:
		return domainfeed.DifficultyStandard
	}
}

func normalizeFilter(filter ListFilter) ListFilter {
	if filter.Limit <= 0 || filter.Limit > 100 {
		filter.Limit = 50
	}
	return filter
}

func trimmedOrNil(value string) *string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}

// ViewBars rolls the feed up to a viewing timeframe as of a cursor position.
//
// The blinding property here is structural rather than checked: the aggregator is handed only base
// bars at or before the cursor, so a higher-timeframe bar it produces can only ever summarize data
// the session has already released. A completed 1h bar in the result covers an hour that has fully
// elapsed for this trader; the trailing bar, if any, is flagged as forming.
func (s *service) ViewBars(ctx context.Context, id uuid.UUID, timeframe market.Timeframe, uptoBaseIndex int) ([]domainfeed.Bar, error) {
	entity, err := s.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	if !timeframe.Valid() {
		return nil, apperror.New("INVALID_TIMEFRAME")
	}
	if uptoBaseIndex < 0 || uptoBaseIndex >= entity.TotalBars {
		return nil, apperror.New("INVALID_CURSOR")
	}
	viewInterval, _ := timeframe.Duration()
	baseInterval, _ := entity.BaseTimeframe.Duration()
	if viewInterval < baseInterval {
		// A feed built on 15m bars cannot be shown at 1m: the finer data was never loaded for this
		// window, and inventing it would be fabricating price action.
		return nil, apperror.Newf("INVALID_TIMEFRAME",
			"this feed's finest available timeframe is %s", entity.BaseTimeframe)
	}

	real, err := s.windowBars(ctx, entity)
	if err != nil {
		return nil, err
	}
	if uptoBaseIndex >= len(real) {
		return nil, apperror.New("BARS_EXHAUSTED")
	}
	// The slice is the enforcement: nothing past the cursor is even visible to the aggregator.
	revealed := real[:uptoBaseIndex+1]

	if timeframe == entity.BaseTimeframe {
		blinded := make([]domainfeed.Bar, 0, len(revealed))
		for index, bar := range revealed {
			blinded = append(blinded, entity.Normalization.ApplyBar(bar, index))
		}
		return blinded, nil
	}

	rolled, lastIsForming := AggregateView(revealed, timeframe)
	blinded := make([]domainfeed.Bar, 0, len(rolled))
	for index, bar := range rolled {
		view := entity.Normalization.ApplyBar(bar, index)
		if lastIsForming && index == len(rolled)-1 {
			view.Forming = true
		}
		blinded = append(blinded, view)
	}
	return blinded, nil
}
