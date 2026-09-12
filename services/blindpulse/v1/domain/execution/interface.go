package execution

import (
	"context"
	"time"

	"github.com/google/uuid"
	domainaccount "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/account"
	domainexec "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/execution"
	domainfeed "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/feed"
	domainsession "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/session"
	"github.com/shopspring/decimal"
)

type Service interface {
	// Place runs the gate and records the order either way. A refused order is stored with its
	// rejection code (BR-09): Sprint 06's discipline index is built largely from what the trader
	// *tried* to do, so discarding rejections would erase the most interesting data here.
	Place(ctx context.Context, userID, sessionID uuid.UUID, in PlaceInput) (*domainexec.Order, error)
	// Cancel withdraws a resting order that has not filled.
	Cancel(ctx context.Context, userID, orderID uuid.UUID) error
	// Amend moves an open position's stop or target. It cannot move the entry — that already
	// happened — and the new levels are checked for side just as the original was.
	Amend(ctx context.Context, userID, tradeID uuid.UUID, in AmendInput) (*domainexec.Trade, error)
	// Breakeven moves the stop to the entry price. A named action rather than an Amend with a
	// computed value, because it is the one adjustment the discipline index wants to recognize.
	Breakeven(ctx context.Context, userID, tradeID uuid.UUID) (*domainexec.Trade, error)
	// ClosePosition exits at the market, in whole or in part.
	ClosePosition(ctx context.Context, userID, tradeID uuid.UUID, fraction *decimal.Decimal) (*domainexec.Trade, error)
	CloseAll(ctx context.Context, userID, sessionID uuid.UUID) (int, error)

	ListOrders(ctx context.Context, userID, sessionID uuid.UUID) ([]domainexec.Order, error)
	ListTrades(ctx context.Context, userID, sessionID uuid.UUID) ([]domainexec.Trade, error)
	ListPositions(ctx context.Context, userID, sessionID uuid.UUID) ([]domainexec.Trade, error)

	// Risk is what the compliance panel renders. It reports how much room is left and whether the
	// gate binds — and nothing about *when* the daily window turns over, because a reset pattern
	// with weekends in it identifies the asset class (SP4-1).
	Risk(ctx context.Context, userID, sessionID uuid.UUID) (*RiskState, error)

	// Advance walks the bars a cursor move released, resolving resting orders and open positions
	// over each one. It is called by the session after the cursor moves, synchronously, because a
	// client that reads its positions immediately after stepping must see the new bar's effect.
	Advance(ctx context.Context, sessionID uuid.UUID, fromBar, toBar int) error
}

// RiskState is the trader-facing risk readout.
//
// What is absent is the point. There is no timestamp, no day ordinal, no bar count to the boundary
// and no countdown: a trader who can see where the daily resets fall sees a two-day gap every five
// days, and that is a weekend, which rules out crypto outright and narrows everything else.
type RiskState struct {
	Halted bool
	// RoomRemainingPct is how much of the daily drawdown allowance is left, 0–100. A fraction of
	// the allowance rather than a distance in currency, so it says how close the trader is without
	// saying what the window is.
	RoomRemainingPct decimal.Decimal
	OpenPositions    int
	Balance          decimal.Decimal
	Equity           decimal.Decimal
	CommittedMargin  decimal.Decimal
}

type OrderRepository interface {
	Create(context.Context, *domainexec.Order) (*domainexec.Order, error)
	GetByID(context.Context, uuid.UUID) (*domainexec.Order, error)
	GetByClientKey(ctx context.Context, sessionID uuid.UUID, clientKey string) (*domainexec.Order, error)
	ListBySessionID(context.Context, uuid.UUID) ([]domainexec.Order, error)
	// ListRestingBySessionID returns every order still awaiting a fill — market orders placed on
	// the previous bar as well as resting limits and stops. A market order is pending too: it
	// fills at the *next* bar's open, which has not happened when it is accepted.
	ListRestingBySessionID(context.Context, uuid.UUID) ([]domainexec.Order, error)
	Update(context.Context, *domainexec.Order) error
}

type TradeRepository interface {
	Create(context.Context, *domainexec.Trade) (*domainexec.Trade, error)
	GetByID(context.Context, uuid.UUID) (*domainexec.Trade, error)
	ListBySessionID(context.Context, uuid.UUID) ([]domainexec.Trade, error)
	ListOpenBySessionID(context.Context, uuid.UUID) ([]domainexec.Trade, error)
	Update(context.Context, *domainexec.Trade) error
}

type SnapshotRepository interface {
	Upsert(context.Context, domainexec.EquitySnapshot) error
	// DayEquity is everything the *daily* drawdown gate needs about one market day, in one query.
	//
	// opening is the equity the day began at: the last snapshot strictly before the boundary — the
	// previous market day's close. It is what makes the allowance reset. A floating loss carried
	// across the boundary is already inside this number, so it does not consume the new day's
	// allowance, which is exactly how a prop firm's daily rule behaves.
	//
	// peak is the highest equity marked since the boundary, so a day that runs up and gives it back
	// is measured from the run-up rather than from the open. Measuring from the open would let a
	// profitable day hand back an unlimited amount unnoticed.
	//
	// Both are zero when no snapshot exists on that side. opening zero means the session has never
	// been marked before this day at all, and the caller seeds the day from the account's balance
	// instead — otherwise a loss on a session's opening bar would become its own reference and spend
	// none of the allowance.
	//
	// One call rather than two because they are one question, and asking it twice invites a caller
	// that asks only half of it.
	DayEquity(ctx context.Context, sessionID uuid.UUID, dayStart time.Time) (opening, peak decimal.Decimal, err error)
}

// SessionReader is this domain's view of a replay session.
type SessionReader interface {
	OwnedSession(ctx context.Context, userID, sessionID uuid.UUID) (*domainsession.Session, error)
	Session(ctx context.Context, sessionID uuid.UUID) (*domainsession.Session, error)
}

// AccountWriter is the balance side. Execution moves money, so it needs more than the read-only
// view the session domain takes.
type AccountWriter interface {
	GetByID(ctx context.Context, id uuid.UUID) (*domainaccount.Account, error)
	ApplyRealizedPnL(ctx context.Context, accountID uuid.UUID, amount decimal.Decimal) error
}

// FeedReader supplies the blinded bars the fills are computed on.
type FeedReader interface {
	Get(ctx context.Context, id uuid.UUID) (*domainfeed.Feed, error)
	Bars(ctx context.Context, id uuid.UUID, from, to int) ([]domainfeed.Bar, error)
}

// MarketClock maps a feed's bar indices to their real instants.
//
// It returns *only times*, and that shape is deliberate: the drawdown's day boundary is a market day
// derived from real timestamps (SP4-1), and an interface that could also return real prices would
// be one refactor away from a fill computed on the unblinded series. There is no way to misuse this
// one — there is nothing in it but a clock.
type MarketClock interface {
	BarTimes(ctx context.Context, feedID uuid.UUID) ([]time.Time, error)
}

// LedgerWriter appends the trade entries that extend Sprint 01's hash chain.
type LedgerWriter interface {
	AppendTrade(ctx context.Context, accountID uuid.UUID, tradeID uuid.UUID, amount decimal.Decimal) error
}

type OutboxRepository interface {
	Create(ctx context.Context, event *EventRecord) error
}

type UnitOfWork interface {
	Do(ctx context.Context, fn func(context.Context) error) error
}
