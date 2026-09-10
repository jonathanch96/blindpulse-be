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
	// Bars returns the blinded candles of a feed between two indices, inclusive.
	Bars(ctx context.Context, id uuid.UUID, from, to int) ([]domainfeed.Bar, error)
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
