package reveal

import (
	"context"
	"time"

	"github.com/google/uuid"
	domainaccount "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/account"
	domainfeed "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/feed"
	"github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/market"
	domainreveal "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/reveal"
	domainsession "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/session"
	"github.com/shopspring/decimal"
)

type Service interface {
	// Unblind writes the reveal, once. Its three preconditions are ordered and each has its own
	// code, because "you cannot do that" is useless when the trader cannot tell which rule they
	// hit: not yours, not closed, already done.
	Unblind(ctx context.Context, userID, sessionID uuid.UUID) (*domainreveal.Reveal, error)
	// Get returns an existing reveal, or REVEAL_LOCKED. Locked rather than not-found: the session
	// exists and the trader owns it — what they are being told is that the curtain has not gone up.
	Get(ctx context.Context, userID, sessionID uuid.UUID) (*domainreveal.Reveal, error)
	// DisclosedBars returns the window's *real* candles, with their real timestamps, and only
	// after a reveal. This is 05.4: the disclosure is a separate call returning a separate type
	// rather than the blinded bars endpoint growing a flag, because a field that appears
	// conditionally is one refactor away from appearing unconditionally — and the trader who
	// discovers that mistake has just been handed the answer to a session they were still trading.
	//
	// The whole window is returned, including bars past where the trader stopped. The session is
	// closed and revealed; what happened next is the post-mortem, and the benchmark they are being
	// compared against already runs to the end of it.
	DisclosedBars(ctx context.Context, userID, sessionID uuid.UUID) ([]market.Bar, *domainreveal.Reveal, error)
}

type Repository interface {
	Create(context.Context, *domainreveal.Reveal) (*domainreveal.Reveal, error)
	GetBySessionID(context.Context, uuid.UUID) (*domainreveal.Reveal, error)
}

// SessionReader is this domain's view of a replay session. Unlike the journal's, it also marks the
// session revealed, because the session row carries revealed_at and the two writes describe one
// event.
type SessionReader interface {
	OwnedSession(ctx context.Context, userID, sessionID uuid.UUID) (*domainsession.Session, error)
	MarkRevealed(ctx context.Context, sessionID uuid.UUID, at time.Time) error
}

// FeedReader hands over the parts of a feed the trader has never been allowed to see.
type FeedReader interface {
	Get(ctx context.Context, id uuid.UUID) (*domainfeed.Feed, error)
}

// InstrumentReader resolves the symbol. This is the single most withheld string in the product.
type InstrumentReader interface {
	GetByID(ctx context.Context, id uuid.UUID) (*market.Instrument, error)
}

// BarReader returns the *real* bars of a window, unblinded. It is used for exactly one thing: the
// buy-and-hold benchmark, which cannot be computed on the normalized series because the affine map
// does not preserve percentage returns.
type BarReader interface {
	ListWindow(ctx context.Context, instrumentID uuid.UUID, timeframe market.Timeframe, from, to int64) ([]market.Bar, error)
}

// TradeReader is the session's realized result. It returns zero for a session with no closed
// trades, which is every session until Sprint 04 ships execution — the right answer for a trader
// who watched without acting, and the same call once there are fills to sum.
type TradeReader interface {
	RealizedPnL(ctx context.Context, sessionID uuid.UUID) (decimal.Decimal, error)
}

// AccountReader supplies the denominator of the strategy return.
type AccountReader interface {
	GetByID(ctx context.Context, id uuid.UUID) (*domainaccount.Account, error)
}

type OutboxRepository interface {
	Create(ctx context.Context, event *EventRecord) error
}

// UnitOfWork writes the reveal and marks the session in one transaction. A reveal row without the
// session flag would let the trader reveal twice; a flag without the row would lock them out of
// their own unblinding permanently. Neither is recoverable from the client.
type UnitOfWork interface {
	Do(ctx context.Context, fn func(context.Context) error) error
}
