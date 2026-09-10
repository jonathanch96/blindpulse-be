package session

import (
	"context"

	"github.com/google/uuid"
	domainfeed "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/feed"
	"github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/market"
	domainsession "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/session"
)

type Service interface {
	Start(ctx context.Context, userID uuid.UUID, in StartInput) (*domainsession.Session, error)
	Get(ctx context.Context, userID, sessionID uuid.UUID) (*domainsession.Session, error)
	ListOpen(ctx context.Context, userID uuid.UUID) ([]domainsession.Session, error)
	// Step advances or rewinds the view. Advancing past the revealed edge is what releases new
	// bars; rewinding never does.
	Step(ctx context.Context, userID, sessionID uuid.UUID, count int) (*domainsession.Session, error)
	// Seek moves the view cursor to an already-released bar. Seeking forward past the revealed
	// edge is refused rather than clamped.
	Seek(ctx context.Context, userID, sessionID uuid.UUID, index int) (*domainsession.Session, error)
	SetSpeed(ctx context.Context, userID, sessionID uuid.UUID, speed string) (*domainsession.Session, error)
	// SetTimeframe changes which timeframe the trader is viewing. It never moves the cursor: the
	// session's position is one number in base bars, and looking at it through a coarser lens
	// cannot reveal or un-reveal anything.
	SetTimeframe(ctx context.Context, userID, sessionID uuid.UUID, timeframe market.Timeframe) (*domainsession.Session, error)
	Pause(ctx context.Context, userID, sessionID uuid.UUID) (*domainsession.Session, error)
	Resume(ctx context.Context, userID, sessionID uuid.UUID) (*domainsession.Session, error)
	Close(ctx context.Context, userID, sessionID uuid.UUID) (*domainsession.Session, error)
	// Bars returns blinded candles at the feed's base timeframe, bounded by what the session has
	// released.
	Bars(ctx context.Context, userID, sessionID uuid.UUID, from, to int) ([]domainfeed.Bar, error)
	// ViewBars returns the session rolled up to a timeframe, as of the revealed edge. Pass an
	// empty timeframe to use the session's current one.
	ViewBars(ctx context.Context, userID, sessionID uuid.UUID, timeframe market.Timeframe) ([]domainfeed.Bar, error)
}

type Repository interface {
	Create(context.Context, *domainsession.Session) (*domainsession.Session, error)
	GetByID(context.Context, uuid.UUID) (*domainsession.Session, error)
	ListLiveByUserID(context.Context, uuid.UUID) ([]domainsession.Session, error)
	GetLiveByAccountID(context.Context, uuid.UUID) (*domainsession.Session, error)
	// UpdateCursor persists a cursor move under optimistic locking.
	UpdateCursor(context.Context, *domainsession.Session) error
	UpdateStatus(ctx context.Context, id uuid.UUID, status domainsession.Status, version int) error
}

// StateStore is the Redis-backed hot path. Every method is allowed to be a no-op: the store is a
// cache, so a miss means "read PostgreSQL", never "fail the request".
type StateStore interface {
	Load(ctx context.Context, sessionID uuid.UUID) (*domainsession.State, bool)
	Save(ctx context.Context, state domainsession.State) error
	Clear(ctx context.Context, sessionID uuid.UUID) error
}

// FeedReader is the session's view of Sprint 02's feed service.
type FeedReader interface {
	Get(ctx context.Context, id uuid.UUID) (*domainfeed.Feed, error)
	Bars(ctx context.Context, id uuid.UUID, from, to int) ([]domainfeed.Bar, error)
	ViewBars(ctx context.Context, id uuid.UUID, timeframe market.Timeframe, uptoBaseIndex int) ([]domainfeed.Bar, error)
}

// AccountReader lets the session verify ownership and that the account is tradeable, without the
// session package depending on the account domain's whole surface.
type AccountReader interface {
	OwnedActiveAccount(ctx context.Context, userID, accountID uuid.UUID) error
}

type OutboxRepository interface {
	Create(ctx context.Context, event *EventRecord) error
}
