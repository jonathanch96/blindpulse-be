package execution

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jblabs/blindpulse-be/pkg/apperror"
	accountdomain "github.com/jblabs/blindpulse-be/services/blindpulse/v1/domain/account"
	domainaccount "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/account"
	domainfeed "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/feed"
	"github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/market"
	domainsession "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/session"
	"github.com/shopspring/decimal"
)

// accountRepo is the slice of the accounts repository execution needs. Named here rather than
// imported, so the composition root can hand over the same adapter every other domain uses without
// this package depending on that one.
type accountRepo interface {
	GetByID(ctx context.Context, id uuid.UUID) (*domainaccount.Account, error)
	UpdateBalances(ctx context.Context, entity *domainaccount.Account) error
}

type accountWriter struct{ repo accountRepo }

// NewAccountWriter gives execution the balance side of an account.
func NewAccountWriter(repo accountRepo) AccountWriter { return &accountWriter{repo: repo} }

func (w *accountWriter) GetByID(ctx context.Context, id uuid.UUID) (*domainaccount.Account, error) {
	return w.repo.GetByID(ctx, id)
}

// ApplyRealizedPnL moves the balance by a closed trade's result.
//
// Equity is set equal to the new balance, and the peak is raised if the balance exceeded it. That is
// right precisely because this runs when a position closes: at that instant the account holds no
// unrealized PnL from *this* trade, and an account's peak should be a mark it actually banked rather
// than one a paper gain touched on the way past. The per-bar unrealized mark lives in
// equity_snapshots, which is what the curve and the daily gate read.
func (w *accountWriter) ApplyRealizedPnL(ctx context.Context, accountID uuid.UUID, amount decimal.Decimal) error {
	entity, err := w.repo.GetByID(ctx, accountID)
	if err != nil {
		return err
	}
	entity.CurrentBalance = entity.CurrentBalance.Add(amount)
	entity.CurrentEquity = entity.CurrentBalance
	if entity.CurrentBalance.GreaterThan(entity.PeakEquity) {
		entity.PeakEquity = entity.CurrentBalance
	}
	return w.repo.UpdateBalances(ctx, entity)
}

// ledgerRepo is the append-only side of the account ledger.
type ledgerRepo interface {
	Append(ctx context.Context, entry *domainaccount.LedgerEntry) error
	Last(ctx context.Context, accountID uuid.UUID) (*domainaccount.LedgerEntry, error)
}

type ledgerWriter struct {
	accounts accountRepo
	ledger   ledgerRepo
	now      func() time.Time
}

// NewLedgerWriter appends a trade's settlement to the account's hash chain.
func NewLedgerWriter(accounts accountRepo, ledger ledgerRepo, now func() time.Time) LedgerWriter {
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &ledgerWriter{accounts: accounts, ledger: ledger, now: now}
}

// AppendTrade links one settlement into the chain.
//
// It must run *after* the balance moved and inside the same transaction, because BalanceAfter is
// hashed: an entry recording a balance the account never held would verify against itself and
// contradict the account, which is the one thing NFR-07 promises cannot happen.
//
// The hash comes from accountdomain.HashLedgerEntry rather than a copy of the rule. A second
// implementation that drifted by one field would make the integrity badge read "tampered" for every
// account that had ever traded.
func (w *ledgerWriter) AppendTrade(ctx context.Context, accountID, tradeID uuid.UUID, amount decimal.Decimal) error {
	entity, err := w.accounts.GetByID(ctx, accountID)
	if err != nil {
		return err
	}
	last, err := w.ledger.Last(ctx, accountID)
	if err != nil && !apperror.Is(err, "ACCOUNT_NOT_FOUND") {
		return err
	}
	var previousHash *string
	sequence := int64(1)
	if last != nil {
		hash := last.EntryHash
		previousHash = &hash
		sequence = last.Sequence + 1
	}
	encoded, err := json.Marshal(map[string]any{"trade_id": tradeID, "realized_pnl": amount.String()})
	if err != nil {
		return apperror.Wrap(err, "INTERNAL_ERROR")
	}
	referenceType := "trade"
	entry := &domainaccount.LedgerEntry{
		ID: uuid.New(), AccountID: accountID, Sequence: sequence,
		Kind: domainaccount.LedgerTrade, ReferenceType: &referenceType, ReferenceID: &tradeID,
		Amount: amount, BalanceAfter: entity.CurrentBalance, EquityAfter: entity.CurrentEquity,
		Payload: encoded, PreviousHash: previousHash,
		// Truncated to what PostgreSQL stores. A nanosecond in the hash is a nanosecond the
		// database rounds away, and the chain then fails to verify on read-back.
		RecordedAt: w.now().Truncate(time.Microsecond),
	}
	entry.EntryHash = accountdomain.HashLedgerEntry(*entry, previousHash)
	return w.ledger.Append(ctx, entry)
}

// feedReader is the feed service's shape, and barReader the real-bar repository's.
type feedReader interface {
	Get(ctx context.Context, id uuid.UUID) (*domainfeed.Feed, error)
}

type barReader interface {
	ListWindow(ctx context.Context, instrumentID uuid.UUID, timeframe market.Timeframe, from, to int64) ([]market.Bar, error)
}

type marketClock struct {
	feeds feedReader
	bars  barReader
}

// NewMarketClock maps a feed's bar indices to the real instants behind them.
//
// It returns times and only times. The bars it reads are the unblinded ones — they have to be, since
// a blinded bar carries an index and no timestamp at all — and an interface that could also hand back
// their prices would be one careless refactor away from a fill computed on the real series. There is
// nothing in this one but a clock, so there is nothing to misuse.
func NewMarketClock(feeds feedReader, bars barReader) MarketClock {
	return &marketClock{feeds: feeds, bars: bars}
}

func (c *marketClock) BarTimes(ctx context.Context, feedID uuid.UUID) ([]time.Time, error) {
	entity, err := c.feeds.Get(ctx, feedID)
	if err != nil {
		return nil, err
	}
	// The same window, in the same order, that the feed's own Bars indexes into — so element i here
	// is the instant of blinded bar i. Any divergence would put the day boundary on the wrong bar.
	real, err := c.bars.ListWindow(ctx, entity.InstrumentID, entity.BaseTimeframe,
		entity.WindowStart.Unix(), entity.WindowEnd.Unix())
	if err != nil {
		return nil, err
	}
	times := make([]time.Time, 0, len(real))
	for _, bar := range real {
		times = append(times, bar.OpenedAt)
	}
	return times, nil
}

// SessionLookup is the session repository's read side.
type SessionLookup interface {
	GetByID(ctx context.Context, id uuid.UUID) (*domainsession.Session, error)
}

type sessionReader struct{ sessions SessionLookup }

// NewSessionReader adapts a session repository to this domain's SessionReader.
//
// Execution takes a repository rather than the session service because the dependency runs the other
// way: the session service calls Advance when its cursor moves, and wiring the two services to each
// other would be a cycle. What execution needs of a session — who owns it, which account and feed it
// is on, where the cursor is — is all on the row.
func NewSessionReader(sessions SessionLookup) SessionReader {
	return sessionReader{sessions: sessions}
}

func (r sessionReader) OwnedSession(ctx context.Context, userID, sessionID uuid.UUID) (*domainsession.Session, error) {
	entity, err := r.sessions.GetByID(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if entity.UserID != userID {
		// Not-found rather than forbidden: confirming the id exists leaks somebody else's session.
		return nil, apperror.New("SESSION_NOT_FOUND")
	}
	return entity, nil
}

// Session is the unowned lookup, used by Advance — which the session service calls after it has
// already checked ownership, on a cursor move it authorized.
func (r sessionReader) Session(ctx context.Context, sessionID uuid.UUID) (*domainsession.Session, error) {
	return r.sessions.GetByID(ctx, sessionID)
}
