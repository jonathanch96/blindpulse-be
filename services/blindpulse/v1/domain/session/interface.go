package session

import (
	"context"
	"time"

	"github.com/google/uuid"
	domainfeed "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/feed"
	"github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/market"
	domainsession "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/session"
)

type Service interface {
	Start(ctx context.Context, userID uuid.UUID, in StartInput) (*domainsession.Session, error)
	Get(ctx context.Context, userID, sessionID uuid.UUID) (*domainsession.Session, error)
	ListOpen(ctx context.Context, userID uuid.UUID) ([]domainsession.Session, error)
	// ListFinished returns sessions that have ended, newest first. The journal reads this: a
	// closed session the trader cannot find again is a post-mortem they cannot write.
	ListFinished(ctx context.Context, userID uuid.UUID, limit int) ([]domainsession.Session, error)
	// Step advances the cursor, releasing bars as it goes. Forward only — a negative count is
	// refused, because a trader cannot go back. Once a bar is stepped past it is history, and a
	// trader who wants a different setup randomizes a new feed.
	Step(ctx context.Context, userID, sessionID uuid.UUID, count int) (*domainsession.Session, error)
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

	// IssueStreamTicket trades an authenticated caller's bearer for a single-use websocket ticket.
	IssueStreamTicket(ctx context.Context, userID, sessionID uuid.UUID) (string, time.Duration, error)
	// RedeemStreamTicket consumes a ticket and returns who it belongs to. The websocket handler
	// gets its identity from here and from nowhere else.
	RedeemStreamTicket(ctx context.Context, token string) (*StreamTicket, error)
	// Stream subscribes to a session's frames and makes sure some replica is driving its clock.
	// The returned cancel releases the subscription; the driver stops once the last one goes.
	Stream(ctx context.Context, userID, sessionID uuid.UUID) (<-chan domainsession.Frame, func(), error)
	// Snapshot builds the sync frame: where the server says this session is, right now.
	Snapshot(ctx context.Context, userID, sessionID uuid.UUID) (*domainsession.Frame, error)
	// SweepIdle abandons sessions nobody has touched for idleFor and returns how many. It is the
	// escape hatch from "one live session per account": without it, closing a browser tab leaves
	// an account unable to start another session, forever.
	SweepIdle(ctx context.Context, idleFor time.Duration, limit int) (int, error)
}

type Repository interface {
	Create(context.Context, *domainsession.Session) (*domainsession.Session, error)
	GetByID(context.Context, uuid.UUID) (*domainsession.Session, error)
	ListLiveByUserID(context.Context, uuid.UUID) ([]domainsession.Session, error)
	ListFinishedByUserID(ctx context.Context, userID uuid.UUID, limit int) ([]domainsession.Session, error)
	GetLiveByAccountID(context.Context, uuid.UUID) (*domainsession.Session, error)
	// UpdateCursor persists a cursor move under optimistic locking.
	UpdateCursor(context.Context, *domainsession.Session) error
	UpdateStatus(ctx context.Context, id uuid.UUID, status domainsession.Status, version int) error
	// AbandonIdle flips every live session untouched since the cutoff to abandoned and returns
	// what it flipped. It claims rows in one statement rather than reading then writing, so two
	// worker replicas sweeping at once cannot both abandon the same session.
	AbandonIdle(ctx context.Context, cutoff time.Time, limit int) ([]domainsession.Session, error)
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

// FrameBus fans replay frames out across API replicas.
//
// One replica advances a session's cursor; every replica holding a websocket for that session
// receives the frames. Without this, a reconnect that lands on a different instance — which is
// the normal case behind a load balancer — would go silent.
type FrameBus interface {
	Publish(ctx context.Context, frame domainsession.Frame) error
	// Subscribe returns a channel of frames and a cancel function. The channel is closed when the
	// subscription ends. Implementations must never block the publisher on a slow subscriber.
	Subscribe(ctx context.Context, sessionID uuid.UUID) (<-chan domainsession.Frame, func(), error)
}

// DriverLease elects exactly one replica to advance a given session's cursor.
//
// Two replicas driving the same session would double-step it, which is not a rendering glitch but
// a correctness failure: bars would be released that the trader never saw. The lease is short and
// renewed, so a replica that dies hands the session over within one TTL instead of stalling it.
type DriverLease interface {
	// Acquire reports whether this replica may drive the session. Renewing is Acquire again by
	// the same holder.
	Acquire(ctx context.Context, sessionID uuid.UUID, holder string, ttl time.Duration) (bool, error)
	Release(ctx context.Context, sessionID uuid.UUID, holder string) error
}

// StreamTicket aliases the entity so the domain names it without a second definition. It is what
// a browser presents to open a websocket: a browser cannot set an Authorization header on a
// WebSocket handshake, and putting the access token in the query string would write it into every
// proxy log between here and the client. So the authenticated HTTP caller trades its bearer for a
// single-use ticket worth one connection to one session for a few seconds.
type StreamTicket = domainsession.StreamTicket

type TicketStore interface {
	Issue(ctx context.Context, ticket StreamTicket, ttl time.Duration) (string, error)
	// Redeem consumes the token. A second redemption of the same token must fail, or a leaked
	// ticket is a reusable credential.
	Redeem(ctx context.Context, token string) (*StreamTicket, error)
}

// CursorObserver is the execution domain, seen from here as one call.
//
// The dependency runs this way round deliberately. The session owns the cursor, so it is the only
// thing that knows a move happened; execution owns fills, so it is the only thing that knows what a
// move means. Wiring the two services to each other would be a cycle, and putting the fill logic
// behind the cursor would make the session domain know about margin.
type CursorObserver interface {
	// Advance resolves every bar in (fromBar, toBar]. It must be idempotent over a range: a step
	// that fails after this returns is retried, and the same bars are walked again.
	Advance(ctx context.Context, sessionID uuid.UUID, fromBar, toBar int) error
}
