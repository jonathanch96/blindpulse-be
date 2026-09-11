// Package reveal is the unblinding.
//
// It is the one write in the system that cannot be undone and cannot be repeated, so almost all of
// this file is about the order of its three preconditions and about freezing the numbers rather
// than deriving them later.
package reveal

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jblabs/blindpulse-be/pkg/apperror"
	"github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/market"
	domainreveal "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/reveal"
	domainsession "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/session"
	"github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/event"
	"github.com/shopspring/decimal"
)

// EventRecord aliases the outbox event so the domain does not import the persistence package.
type EventRecord = event.OutboxEvent

type Dependencies struct {
	Repo        Repository
	Sessions    SessionReader
	Feeds       FeedReader
	Instruments InstrumentReader
	Bars        BarReader
	Trades      TradeReader
	Accounts    AccountReader
	Outbox      OutboxRepository
	UOW         UnitOfWork
	Topic       func(string) string
	Clock       func() time.Time
}

type service struct{ deps Dependencies }

func NewService(deps Dependencies) Service {
	if deps.Clock == nil {
		deps.Clock = func() time.Time { return time.Now().UTC() }
	}
	if deps.UOW == nil {
		deps.UOW = passthroughUOW{}
	}
	return &service{deps: deps}
}

func (s *service) Unblind(ctx context.Context, userID, sessionID uuid.UUID) (*domainreveal.Reveal, error) {
	replay, err := s.deps.Sessions.OwnedSession(ctx, userID, sessionID)
	if err != nil {
		return nil, err
	}
	// Precondition 2, in order and with its own code. Revealing mid-session would hand the trader
	// the answer with bars still to trade, which is the one failure this product cannot recover
	// from — every remaining decision would be made knowing the ending.
	if replay.Status != domainsession.StatusClosed {
		return nil, apperror.Newf("REVEAL_LOCKED",
			"the reveal unlocks once the session is closed; this one is %s", replay.Status)
	}
	// Precondition 3. Checked before any computation so a second call is cheap, and checked again
	// by the primary key underneath: session_reveals is keyed by session_id, so two concurrent
	// calls cannot both write.
	if existing, err := s.deps.Repo.GetBySessionID(ctx, sessionID); err == nil && existing != nil {
		return nil, apperror.New("ALREADY_REVEALED")
	} else if err != nil && !apperror.Is(err, "REVEAL_LOCKED") {
		return nil, err
	}

	feed, err := s.deps.Feeds.Get(ctx, replay.FeedID)
	if err != nil {
		return nil, err
	}
	instrument, err := s.deps.Instruments.GetByID(ctx, feed.InstrumentID)
	if err != nil {
		return nil, err
	}
	benchmark, err := s.benchmark(ctx, feed.InstrumentID, feed.BaseTimeframe, feed.WindowStart, feed.WindowEnd, feed.WarmupBars)
	if err != nil {
		return nil, err
	}
	strategy, err := s.strategy(ctx, replay)
	if err != nil {
		return nil, err
	}

	now := s.deps.Clock()
	entity := domainreveal.Reveal{
		SessionID: sessionID, InstrumentID: feed.InstrumentID, RevealedAt: now,
		Symbol: instrument.Symbol, Timeframe: string(feed.BaseTimeframe),
		WindowStart: feed.WindowStart, WindowEnd: feed.WindowEnd,
		MacroLabel: feed.MacroLabel, MacroNotes: feed.MacroNotes, MacroTags: macroTags(feed.MacroTags),
		BenchmarkLabel:     domainreveal.BenchmarkBuyAndHold,
		StrategyReturnPct:  strategy,
		BenchmarkReturnPct: benchmark,
		AlphaPct:           domainreveal.Alpha(strategy, benchmark),
	}

	var created *domainreveal.Reveal
	err = s.deps.UOW.Do(ctx, func(txCtx context.Context) error {
		stored, createErr := s.deps.Repo.Create(txCtx, &entity)
		if createErr != nil {
			return createErr
		}
		created = stored
		// The session's own revealed_at. Both writes describe one event, so they share a
		// transaction: a reveal row without the flag would let the trader reveal twice, and a flag
		// without the row would lock them out of their own unblinding with no way back.
		if markErr := s.deps.Sessions.MarkRevealed(txCtx, sessionID, now); markErr != nil {
			return markErr
		}
		return s.emit(txCtx, entity)
	})
	if err != nil {
		return nil, err
	}
	return created, nil
}

func (s *service) Get(ctx context.Context, userID, sessionID uuid.UUID) (*domainreveal.Reveal, error) {
	if _, err := s.deps.Sessions.OwnedSession(ctx, userID, sessionID); err != nil {
		return nil, err
	}
	return s.deps.Repo.GetBySessionID(ctx, sessionID)
}

func (s *service) DisclosedBars(ctx context.Context, userID, sessionID uuid.UUID) ([]market.Bar, *domainreveal.Reveal, error) {
	if _, err := s.deps.Sessions.OwnedSession(ctx, userID, sessionID); err != nil {
		return nil, nil, err
	}
	// The gate is the stored reveal, not the session's status. A closed session is not a revealed
	// one — the trader has to choose to unblind — and reading the reveal row is the only way to
	// know they did.
	stored, err := s.deps.Repo.GetBySessionID(ctx, sessionID)
	if err != nil {
		return nil, nil, err
	}
	// Every coordinate comes from the frozen reveal rather than from the feed. The reveal is what
	// the trader was shown, and a feed rebuilt since then could otherwise hand back a different
	// window than the one they were told about.
	bars, err := s.deps.Bars.ListWindow(ctx, stored.InstrumentID, market.Timeframe(stored.Timeframe),
		stored.WindowStart.Unix(), stored.WindowEnd.Unix())
	if err != nil {
		return nil, nil, err
	}
	return bars, stored, nil
}

// benchmark is buy-and-hold over the identical window: in at the first tradeable bar's open, out at
// the last bar's close, unlevered.
//
// The entry is the bar *after* the warmup, not the first bar of the window. The warmup is lookback
// the trader is shown before the cursor moves — they could not have bought it, so a benchmark that
// did would be comparing a strategy against a position nobody could have taken.
func (s *service) benchmark(
	ctx context.Context, instrumentID uuid.UUID, timeframe market.Timeframe,
	start, end time.Time, warmup int,
) (decimal.Decimal, error) {
	bars, err := s.deps.Bars.ListWindow(ctx, instrumentID, timeframe, start.Unix(), end.Unix())
	if err != nil {
		return decimal.Zero, err
	}
	if warmup < 0 || warmup >= len(bars) {
		// A window with nothing tradeable in it. Zero rather than an error: the rest of the
		// unblinding is still worth having, and the feed builder already refuses windows this
		// short at build time, so reaching here means the reference data changed underneath.
		return decimal.Zero, nil
	}
	return domainreveal.ReturnPct(bars[warmup].Open, bars[len(bars)-1].Close), nil
}

// strategy is the session's realized return as a percentage of the balance the account started the
// iteration with.
//
// The denominator is the iteration's opening balance rather than the equity at the moment the
// session began. Those are the same number for the normal case — a reset opens a new iteration and
// the trader starts trading it — and the alternative needs an equity snapshot at session start,
// which is Sprint 04's to write. When it exists this should read it; until then this is exact for
// the case that actually occurs rather than approximate for one that does not.
func (s *service) strategy(ctx context.Context, replay *domainsession.Session) (decimal.Decimal, error) {
	realized, err := s.deps.Trades.RealizedPnL(ctx, replay.ID)
	if err != nil {
		return decimal.Zero, err
	}
	if realized.IsZero() {
		// No fills: the trader watched and did not act. Returning early also keeps a session from
		// failing its reveal because its account row is unreadable, when the answer is zero either
		// way.
		return decimal.Zero, nil
	}
	account, err := s.deps.Accounts.GetByID(ctx, replay.AccountID)
	if err != nil {
		return decimal.Zero, err
	}
	if !account.InitialBalance.IsPositive() {
		return decimal.Zero, nil
	}
	return realized.Div(account.InitialBalance).Mul(decimal.NewFromInt(100)).Round(domainreveal.PercentScale), nil
}

func (s *service) emit(ctx context.Context, entity domainreveal.Reveal) error {
	if s.deps.Outbox == nil {
		return nil
	}
	// The payload carries the symbol and the window, which no other event in the system does. That
	// is correct here and only here: this event *is* the disclosure, and a consumer reading it is
	// reading a session the trader has already unblinded.
	record, err := event.New("session", entity.SessionID, event.TypeSessionRevealed, s.deps.Topic(event.TopicReveals), map[string]any{
		"session_id": entity.SessionID, "instrument_id": entity.InstrumentID,
		"symbol": entity.Symbol, "timeframe": entity.Timeframe,
		"window_start": entity.WindowStart, "window_end": entity.WindowEnd,
		"strategy_return_pct": entity.StrategyReturnPct, "benchmark_return_pct": entity.BenchmarkReturnPct,
		"alpha_pct": entity.AlphaPct, "benchmark_label": entity.BenchmarkLabel,
		"revealed_at": entity.RevealedAt,
	})
	if err != nil {
		return apperror.Wrap(err, "INTERNAL_ERROR")
	}
	return s.deps.Outbox.Create(ctx, record)
}

func macroTags(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

// passthroughUOW runs the function without a transaction, so a test can construct the service
// without a database. The composition root always wires the real one.
type passthroughUOW struct{}

func (passthroughUOW) Do(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) }
