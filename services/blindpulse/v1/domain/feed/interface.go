package feed

import (
	"context"

	"github.com/google/uuid"
	domainfeed "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/feed"
	"github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/market"
)

type Service interface {
	// Build creates a blinded feed over a window of an instrument's bars.
	Build(ctx context.Context, in BuildInput) (*domainfeed.Feed, error)
	// List returns published feeds for the catalogue.
	List(ctx context.Context, userID uuid.UUID, filter ListFilter) ([]domainfeed.Feed, error)
	// Get returns one published feed.
	Get(ctx context.Context, id uuid.UUID) (*domainfeed.Feed, error)
	// Random picks a published feed the user has not traded, so "randomize" cannot hand back a
	// window whose ending they already know — which would reintroduce exactly the hindsight the
	// product removes.
	Random(ctx context.Context, userID uuid.UUID, filter ListFilter) (*domainfeed.Feed, error)
	// Bars returns the blinded candles of a feed between two indices, inclusive, at the feed's
	// base timeframe.
	Bars(ctx context.Context, id uuid.UUID, from, to int) ([]domainfeed.Bar, error)
	// ViewBars returns the feed rolled up to a viewing timeframe, covering only base bars at or
	// before uptoBaseIndex. The last bar may be flagged as still forming.
	//
	// The cursor is expressed in *base* bars and stays that way: a session's position is one
	// number regardless of which timeframe the trader happens to be looking at, so switching
	// timeframes can never move it.
	ViewBars(ctx context.Context, id uuid.UUID, timeframe market.Timeframe, uptoBaseIndex int) ([]domainfeed.Bar, error)
}

type Repository interface {
	Create(context.Context, *domainfeed.Feed) (*domainfeed.Feed, error)
	GetByID(context.Context, uuid.UUID) (*domainfeed.Feed, error)
	List(context.Context, ListFilter) ([]domainfeed.Feed, error)
	// ListExcludingTraded backs Random: feeds this user has never opened a session against.
	ListExcludingTraded(ctx context.Context, userID uuid.UUID, filter ListFilter) ([]domainfeed.Feed, error)
	ExistsByAlias(context.Context, string) (bool, error)
	// NextAliasNumber draws from a sequence, so two builders running at once cannot mint the same
	// alias and no alias is ever derived from the symbol it hides.
	NextAliasNumber(context.Context) (int64, error)
}

type BarRepository interface {
	// ListWindow returns real bars in ascending time order.
	ListWindow(ctx context.Context, instrumentID uuid.UUID, timeframe market.Timeframe, from, to int64) ([]market.Bar, error)
	CountWindow(ctx context.Context, instrumentID uuid.UUID, timeframe market.Timeframe, from, to int64) (int64, error)
	Insert(ctx context.Context, bars []market.Bar) (int64, error)
	// Bounds reports the first and last bar times held for a series, so the builder can pick a
	// window without scanning.
	Bounds(ctx context.Context, instrumentID uuid.UUID, timeframe market.Timeframe) (first, last int64, err error)
}

type InstrumentRepository interface {
	Upsert(context.Context, *market.Instrument) (*market.Instrument, error)
	GetByID(context.Context, uuid.UUID) (*market.Instrument, error)
	GetBySymbol(context.Context, string) (*market.Instrument, error)
	List(context.Context) ([]market.Instrument, error)
}

// WindowCache holds a feed's bar window between reads.
//
// Every released bar used to cost a full-window read from PostgreSQL: building one stream frame
// calls ViewBars, which fetches the whole window — up to 800 rows — and then normalizes and
// aggregates the revealed prefix. At 10x playback with a 250ms tick that is roughly forty
// full-window reads per second, per session (review finding BE-03-2).
//
// A feed's window is immutable once built, which makes this the easiest possible cache: there is no
// invalidation question, only a TTL. `REDIS_BAR_WINDOW_TTL` was configured for exactly this and had
// no caller.
//
// Every method may be a no-op. This is a cache, so a miss means "read PostgreSQL" and a write
// failure costs a round trip, never a request.
type WindowCache interface {
	Get(ctx context.Context, feedID uuid.UUID) ([]market.Bar, bool)
	Put(ctx context.Context, feedID uuid.UUID, bars []market.Bar)
}
