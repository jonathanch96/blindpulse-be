package feed

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jblabs/blindpulse-be/pkg/apperror"
	domainfeed "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/feed"
	"github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/market"
	"github.com/shopspring/decimal"
)

// Review finding BE-03-2. Building one stream frame calls ViewBars, which fetched the whole feed
// window from PostgreSQL — up to 800 rows — and then normalized and aggregated the revealed prefix.
// At 10x with a 250ms tick that is roughly forty full-window reads per second, per session.
//
// The window is immutable once the feed is built, so the fix is a cache with no invalidation
// question. These tests are about the read *count*, because that is the thing that was wrong.

type countingBarRepo struct {
	mu    sync.Mutex
	bars  []market.Bar
	reads int
}

func (r *countingBarRepo) ListWindow(_ context.Context, _ uuid.UUID, _ market.Timeframe, _, _ int64) ([]market.Bar, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reads++
	return r.bars, nil
}

func (r *countingBarRepo) CountWindow(_ context.Context, _ uuid.UUID, _ market.Timeframe, _, _ int64) (int64, error) {
	return int64(len(r.bars)), nil
}
func (r *countingBarRepo) Insert(context.Context, []market.Bar) (int64, error) { return 0, nil }
func (r *countingBarRepo) Bounds(_ context.Context, _ uuid.UUID, _ market.Timeframe) (int64, int64, error) {
	if len(r.bars) == 0 {
		return 0, 0, nil
	}
	return r.bars[0].OpenedAt.Unix(), r.bars[len(r.bars)-1].OpenedAt.Unix(), nil
}

func (r *countingBarRepo) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.reads
}

type feedRepoStub struct{ feed domainfeed.Feed }

func (r *feedRepoStub) GetByID(_ context.Context, id uuid.UUID) (*domainfeed.Feed, error) {
	if id != r.feed.ID {
		return nil, apperror.New("FEED_NOT_FOUND")
	}
	copied := r.feed
	return &copied, nil
}
func (r *feedRepoStub) Create(_ context.Context, entity *domainfeed.Feed) (*domainfeed.Feed, error) {
	return entity, nil
}
func (r *feedRepoStub) List(context.Context, ListFilter) ([]domainfeed.Feed, error) { return nil, nil }
func (r *feedRepoStub) ListExcludingTraded(context.Context, uuid.UUID, ListFilter) ([]domainfeed.Feed, error) {
	return nil, nil
}
func (r *feedRepoStub) ExistsByAlias(context.Context, string) (bool, error) { return false, nil }
func (r *feedRepoStub) NextAliasNumber(context.Context) (int64, error)      { return 1, nil }

type instrumentRepoStub struct{}

func (instrumentRepoStub) GetByID(_ context.Context, id uuid.UUID) (*market.Instrument, error) {
	return &market.Instrument{ID: id, Symbol: "TEST", AssetClass: market.AssetClassFX}, nil
}
func (instrumentRepoStub) Upsert(_ context.Context, entity *market.Instrument) (*market.Instrument, error) {
	return entity, nil
}
func (instrumentRepoStub) GetBySymbol(_ context.Context, symbol string) (*market.Instrument, error) {
	return &market.Instrument{ID: uuid.New(), Symbol: symbol, AssetClass: market.AssetClassFX}, nil
}
func (instrumentRepoStub) List(context.Context) ([]market.Instrument, error) { return nil, nil }

// memoryWindowCache stands in for the Redis adapter. The adapter's own no-op-when-disabled
// behaviour is its concern; this is about whether the domain consults a cache at all.
type memoryWindowCache struct {
	mu   sync.Mutex
	rows map[uuid.UUID][]market.Bar
}

func newMemoryWindowCache() *memoryWindowCache {
	return &memoryWindowCache{rows: make(map[uuid.UUID][]market.Bar)}
}

func (c *memoryWindowCache) Get(_ context.Context, feedID uuid.UUID) ([]market.Bar, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	bars, ok := c.rows[feedID]
	return bars, ok
}

func (c *memoryWindowCache) Put(_ context.Context, feedID uuid.UUID, bars []market.Bar) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.rows[feedID] = bars
}

func windowFeed() domainfeed.Feed {
	start := time.Date(2023, 3, 14, 0, 0, 0, 0, time.UTC)
	return domainfeed.Feed{
		ID: uuid.New(), InstrumentID: uuid.New(), AliasLabel: "Asset #1",
		BaseTimeframe: market.TF15m, WindowStart: start, WindowEnd: start.Add(400 * 15 * time.Minute),
		WarmupBars: 200, TotalBars: 400, IsPublished: true,
		Normalization: domainfeed.Normalization{
			Offset: decimal.NewFromInt(1), Scale: decimal.NewFromInt(2), VolumeScale: decimal.NewFromInt(1),
		},
	}
}

// Both services in a comparison must serve the *same* feed, or the uncached one reports
// FEED_NOT_FOUND and the test passes for the wrong reason.
func windowFixture(t *testing.T, entity domainfeed.Feed, windows WindowCache) (Service, *countingBarRepo) {
	t.Helper()
	repo := &countingBarRepo{bars: viewBars(400, entity.WindowStart)}
	service := NewService(Dependencies{
		Repo: &feedRepoStub{feed: entity}, Bars: repo, Instruments: instrumentRepoStub{}, Windows: windows,
	})
	return service, repo
}

// The behaviour the finding is about: replaying a session must not re-read the window per bar.
func TestReplayingAWindowReadsTheDatabaseOnce(t *testing.T) {
	t.Parallel()
	entity := windowFeed()
	service, repo := windowFixture(t, entity, newMemoryWindowCache())
	ctx := context.Background()

	// Forty released bars, the way the streaming path walks a session.
	for cursor := entity.WarmupBars; cursor < entity.WarmupBars+40; cursor++ {
		if _, err := service.ViewBars(ctx, entity.ID, market.TF1h, cursor); err != nil {
			t.Fatalf("ViewBars(%d) error = %v", cursor, err)
		}
	}
	if got := repo.count(); got != 1 {
		t.Errorf("%d full-window database reads for 40 released bars, want 1", got)
	}
}

// Without a cache the behaviour must be identical, only slower. A cache that changes answers is
// worse than no cache.
func TestTheCacheDoesNotChangeWhatIsReturned(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	entity := windowFeed()
	cached, _ := windowFixture(t, entity, newMemoryWindowCache())
	uncached, _ := windowFixture(t, entity, nil)

	for _, timeframe := range []market.Timeframe{market.TF15m, market.TF1h, market.TF4h} {
		withCache, err := cached.ViewBars(ctx, entity.ID, timeframe, 250)
		if err != nil {
			t.Fatalf("cached ViewBars error = %v", err)
		}
		withoutCache, err := uncached.ViewBars(ctx, entity.ID, timeframe, 250)
		if err != nil {
			t.Fatalf("uncached ViewBars error = %v", err)
		}
		if len(withCache) != len(withoutCache) {
			t.Fatalf("%s: %d bars cached vs %d uncached", timeframe, len(withCache), len(withoutCache))
		}
		for i := range withCache {
			if !sameBar(withCache[i], withoutCache[i]) {
				t.Fatalf("%s: bar %d differs with the cache: %s vs %s",
					timeframe, i, describe(withCache[i]), describe(withoutCache[i]))
			}
		}
	}
}

// A nil cache is the deployment without Redis. It must read PostgreSQL every time rather than fail.
func TestWithoutACacheEveryReadHitsTheDatabase(t *testing.T) {
	t.Parallel()
	entity := windowFeed()
	service, repo := windowFixture(t, entity, nil)
	ctx := context.Background()
	for cursor := 250; cursor < 253; cursor++ {
		if _, err := service.ViewBars(ctx, entity.ID, market.TF1h, cursor); err != nil {
			t.Fatalf("ViewBars error = %v", err)
		}
	}
	if got := repo.count(); got != 3 {
		t.Errorf("%d reads without a cache, want 3 — the fallback must not be silently skipped", got)
	}
}

// The cursor bound is the safety property (BR-02), and it is enforced by slicing the window before
// aggregating. Serving that window from a cache must not move where the slice happens.
func TestTheCacheDoesNotLetAViewSeePastTheCursor(t *testing.T) {
	t.Parallel()
	entity := windowFeed()
	service, _ := windowFixture(t, entity, newMemoryWindowCache())
	ctx := context.Background()

	// Warm the cache with a read far along the window, then ask for an earlier cursor.
	if _, err := service.ViewBars(ctx, entity.ID, market.TF15m, 399); err != nil {
		t.Fatalf("ViewBars error = %v", err)
	}
	early, err := service.ViewBars(ctx, entity.ID, market.TF15m, 210)
	if err != nil {
		t.Fatalf("ViewBars error = %v", err)
	}
	if len(early) != 211 {
		t.Errorf("a cursor at 210 returned %d bars, want 211 — the cache widened the window", len(early))
	}
}

// decimal.Decimal holds a *big.Int, so struct equality compares pointers rather than values — two
// bars with identical numbers compare unequal. Comparisons here go through Equal.
func sameBar(a, b domainfeed.Bar) bool {
	return a.Index == b.Index && a.Forming == b.Forming &&
		a.Open.Equal(b.Open) && a.High.Equal(b.High) &&
		a.Low.Equal(b.Low) && a.Close.Equal(b.Close) && a.Volume.Equal(b.Volume)
}

func describe(bar domainfeed.Bar) string {
	return fmt.Sprintf("#%d O%s H%s L%s C%s V%s forming=%t",
		bar.Index, bar.Open, bar.High, bar.Low, bar.Close, bar.Volume, bar.Forming)
}
