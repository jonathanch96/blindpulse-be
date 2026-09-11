package reveal

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jblabs/blindpulse-be/pkg/apperror"
	domainaccount "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/account"
	domainfeed "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/feed"
	"github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/market"
	domainreveal "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/reveal"
	domainsession "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/session"
	"github.com/shopspring/decimal"
)

var (
	windowStart = time.Date(2023, 3, 14, 8, 30, 0, 0, time.UTC)
	windowEnd   = time.Date(2023, 3, 17, 16, 0, 0, 0, time.UTC)
	revealedAt  = time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC)
)

type repoStub struct {
	rows map[uuid.UUID]*domainreveal.Reveal
}

func newRepoStub() *repoStub { return &repoStub{rows: make(map[uuid.UUID]*domainreveal.Reveal)} }

func (r *repoStub) Create(_ context.Context, entity *domainreveal.Reveal) (*domainreveal.Reveal, error) {
	if _, exists := r.rows[entity.SessionID]; exists {
		return nil, apperror.New("ALREADY_REVEALED")
	}
	stored := *entity
	r.rows[entity.SessionID] = &stored
	created := stored
	return &created, nil
}

func (r *repoStub) GetBySessionID(_ context.Context, sessionID uuid.UUID) (*domainreveal.Reveal, error) {
	entity, ok := r.rows[sessionID]
	if !ok {
		return nil, apperror.New("REVEAL_LOCKED")
	}
	copied := *entity
	return &copied, nil
}

type sessionStub struct {
	session  domainsession.Session
	revealed *time.Time
}

func (s *sessionStub) OwnedSession(_ context.Context, userID, sessionID uuid.UUID) (*domainsession.Session, error) {
	if userID != s.session.UserID {
		return nil, apperror.New("SESSION_NOT_FOUND")
	}
	copied := s.session
	copied.ID = sessionID
	return &copied, nil
}

func (s *sessionStub) MarkRevealed(_ context.Context, _ uuid.UUID, at time.Time) error {
	if s.revealed != nil {
		return apperror.New("ALREADY_REVEALED")
	}
	s.revealed = &at
	return nil
}

type feedStub struct{ feed domainfeed.Feed }

func (f feedStub) Get(context.Context, uuid.UUID) (*domainfeed.Feed, error) {
	copied := f.feed
	return &copied, nil
}

type instrumentStub struct{ symbol string }

func (i instrumentStub) GetByID(_ context.Context, id uuid.UUID) (*market.Instrument, error) {
	return &market.Instrument{ID: id, Symbol: i.symbol, AssetClass: market.AssetClassFX}, nil
}

type barStub struct{ bars []market.Bar }

func (b barStub) ListWindow(context.Context, uuid.UUID, market.Timeframe, int64, int64) ([]market.Bar, error) {
	return b.bars, nil
}

type tradeStub struct{ realized decimal.Decimal }

func (t tradeStub) RealizedPnL(context.Context, uuid.UUID) (decimal.Decimal, error) {
	return t.realized, nil
}

type accountStub struct{ initial decimal.Decimal }

func (a accountStub) GetByID(_ context.Context, id uuid.UUID) (*domainaccount.Account, error) {
	return &domainaccount.Account{ID: id, InitialBalance: a.initial}, nil
}

func price(value string) decimal.Decimal { return decimal.RequireFromString(value) }

// bars builds a window whose first tradeable bar opens at 1.0000 and whose last closes at 1.1000 —
// a clean 10% so a hand calculation and the code can be compared without argument.
func bars(warmup int) []market.Bar {
	window := make([]market.Bar, 0, warmup+3)
	for i := 0; i < warmup; i++ {
		// The lookback the trader is shown before the cursor moves. Deliberately priced far from
		// the tradeable range: a benchmark that entered here would be a position nobody could have
		// taken, and this makes that mistake visible as a wrong number rather than a near-miss.
		window = append(window, market.Bar{Open: price("5.0"), High: price("5.0"), Low: price("5.0"), Close: price("5.0")})
	}
	window = append(window,
		market.Bar{Open: price("1.0"), High: price("1.2"), Low: price("0.9"), Close: price("1.05")},
		market.Bar{Open: price("1.05"), High: price("1.2"), Low: price("1.0"), Close: price("1.08")},
		market.Bar{Open: price("1.08"), High: price("1.2"), Low: price("1.0"), Close: price("1.1")},
	)
	return window
}

type fixture struct {
	service  Service
	repo     *repoStub
	sessions *sessionStub
	userID   uuid.UUID
}

func newFixture(status domainsession.Status, realized decimal.Decimal, warmup int) fixture {
	userID := uuid.New()
	notes := "Regional banking stress; the Fed backstopped deposits on the 12th."
	repo := newRepoStub()
	sessions := &sessionStub{session: domainsession.Session{
		UserID: userID, AccountID: uuid.New(), FeedID: uuid.New(), Status: status, CursorIndex: 500,
	}}
	label := "SVB Contagion"
	service := NewService(Dependencies{
		Repo:     repo,
		Sessions: sessions,
		Feeds: feedStub{feed: domainfeed.Feed{
			InstrumentID: uuid.New(), BaseTimeframe: market.TF15m,
			WindowStart: windowStart, WindowEnd: windowEnd, WarmupBars: warmup,
			MacroLabel: &label, MacroNotes: &notes, MacroTags: []string{"#SVB_COLLAPSE"},
		}},
		Instruments: instrumentStub{symbol: "EURUSD"},
		Bars:        barStub{bars: bars(warmup)},
		Trades:      tradeStub{realized: realized},
		Accounts:    accountStub{initial: price("10000")},
		Clock:       func() time.Time { return revealedAt },
	})
	return fixture{service: service, repo: repo, sessions: sessions, userID: userID}
}

// 05-AC-1. The one failure this product cannot recover from: handing a trader the answer while they
// still have bars to trade.
func TestRevealIsLockedWhileTheSessionIsStillOpen(t *testing.T) {
	for _, status := range []domainsession.Status{domainsession.StatusOpen, domainsession.StatusPaused} {
		f := newFixture(status, decimal.Zero, 2)
		_, err := f.service.Unblind(context.Background(), f.userID, uuid.New())
		if !apperror.Is(err, "REVEAL_LOCKED") {
			t.Errorf("status %s: err = %v, want REVEAL_LOCKED", status, err)
		}
		if len(f.repo.rows) != 0 {
			t.Errorf("status %s: a reveal was written anyway", status)
		}
		if f.sessions.revealed != nil {
			t.Errorf("status %s: the session was stamped revealed anyway", status)
		}
	}
}

// 05-AC-2 and 05-AC-8. The benchmark is checked against a window whose numbers were chosen so the
// answer can be worked out by hand: in at 1.0, out at 1.1, so 10%.
func TestRevealFreezesTheUnblindingAndTheBenchmark(t *testing.T) {
	f := newFixture(domainsession.StatusClosed, decimal.Zero, 2)
	entity, err := f.service.Unblind(context.Background(), f.userID, uuid.New())
	if err != nil {
		t.Fatalf("Unblind: %v", err)
	}
	if entity.Symbol != "EURUSD" || entity.Timeframe != string(market.TF15m) {
		t.Errorf("identity = %s %s, want EURUSD 15m", entity.Symbol, entity.Timeframe)
	}
	if !entity.WindowStart.Equal(windowStart) || !entity.WindowEnd.Equal(windowEnd) {
		t.Errorf("window = %s..%s, want the feed's", entity.WindowStart, entity.WindowEnd)
	}
	if entity.MacroLabel == nil || *entity.MacroLabel != "SVB Contagion" || len(entity.MacroTags) != 1 {
		t.Errorf("macro = %+v, want the feed's label and tags", entity)
	}
	if got := entity.BenchmarkReturnPct.String(); got != "10" {
		t.Errorf("benchmark = %s, want 10 (1.0 open to 1.1 close)", got)
	}
	// No fills, so the strategy returned nothing and alpha is the negative of the benchmark. That
	// is the right answer for a trader who watched without acting, not a placeholder.
	if got := entity.StrategyReturnPct.String(); got != "0" {
		t.Errorf("strategy = %s, want 0 with no closed trades", got)
	}
	if got := entity.AlphaPct.String(); got != "-10" {
		t.Errorf("alpha = %s, want -10", got)
	}
	if entity.BenchmarkLabel != domainreveal.BenchmarkBuyAndHold {
		t.Errorf("benchmark label = %q, want it stated on the record", entity.BenchmarkLabel)
	}
	if f.sessions.revealed == nil {
		t.Error("the session was not stamped revealed")
	}
}

// The warmup is lookback shown before the cursor moves, so the trader could not have bought it. A
// benchmark that entered there would compare the strategy against a position nobody could take.
func TestBenchmarkEntersAtTheFirstTradeableBarNotTheFirstBar(t *testing.T) {
	f := newFixture(domainsession.StatusClosed, decimal.Zero, 2)
	entity, err := f.service.Unblind(context.Background(), f.userID, uuid.New())
	if err != nil {
		t.Fatalf("Unblind: %v", err)
	}
	// Entering at the warmup's 5.0 instead of the first tradeable 1.0 would give -78%, so the
	// difference between right and wrong here is not subtle.
	if got := entity.BenchmarkReturnPct.String(); got != "10" {
		t.Errorf("benchmark = %s; the warmup bars were included in the hold", got)
	}
}

func TestStrategyReturnIsRealizedPnLOverTheOpeningBalance(t *testing.T) {
	f := newFixture(domainsession.StatusClosed, price("250"), 2)
	entity, err := f.service.Unblind(context.Background(), f.userID, uuid.New())
	if err != nil {
		t.Fatalf("Unblind: %v", err)
	}
	// 250 on 10,000 is 2.5%, against a benchmark of 10%: alpha is -7.5.
	if got := entity.StrategyReturnPct.String(); got != "2.5" {
		t.Errorf("strategy = %s, want 2.5", got)
	}
	if got := entity.AlphaPct.String(); got != "-7.5" {
		t.Errorf("alpha = %s, want -7.5", got)
	}
}

// 05-AC-3. The reveal is the one write with no undo, so a second attempt must not be able to change
// what the first one said.
func TestASecondRevealChangesNothing(t *testing.T) {
	f := newFixture(domainsession.StatusClosed, decimal.Zero, 2)
	sessionID := uuid.New()
	first, err := f.service.Unblind(context.Background(), f.userID, sessionID)
	if err != nil {
		t.Fatalf("Unblind: %v", err)
	}
	if _, err := f.service.Unblind(context.Background(), f.userID, sessionID); !apperror.Is(err, "ALREADY_REVEALED") {
		t.Fatalf("err = %v, want ALREADY_REVEALED", err)
	}
	stored, err := f.service.Get(context.Background(), f.userID, sessionID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !stored.RevealedAt.Equal(first.RevealedAt) || stored.Symbol != first.Symbol {
		t.Errorf("the stored reveal changed: %+v", stored)
	}
}

// 05-AC-4. Locked, not missing: the session exists and the caller owns it.
func TestGetIsLockedUntilTheSessionIsRevealed(t *testing.T) {
	f := newFixture(domainsession.StatusClosed, decimal.Zero, 2)
	if _, err := f.service.Get(context.Background(), f.userID, uuid.New()); !apperror.Is(err, "REVEAL_LOCKED") {
		t.Errorf("err = %v, want REVEAL_LOCKED", err)
	}
}

func TestSomeoneElsesSessionIsNotFound(t *testing.T) {
	f := newFixture(domainsession.StatusClosed, decimal.Zero, 2)
	stranger := uuid.New()
	if _, err := f.service.Unblind(context.Background(), stranger, uuid.New()); !apperror.Is(err, "SESSION_NOT_FOUND") {
		t.Errorf("err = %v, want SESSION_NOT_FOUND", err)
	}
	if _, err := f.service.Get(context.Background(), stranger, uuid.New()); !apperror.Is(err, "SESSION_NOT_FOUND") {
		t.Errorf("err = %v, want SESSION_NOT_FOUND", err)
	}
}

func TestReturnPctHandlesAnUnusableEntryPrice(t *testing.T) {
	// A zero entry would divide by zero. Zero out rather than an error: a reveal that failed over
	// a bad reference price would deny the trader the whole unblinding for one number.
	if got := domainreveal.ReturnPct(decimal.Zero, price("1.1")); !got.IsZero() {
		t.Errorf("ReturnPct(0, 1.1) = %s, want 0", got)
	}
	if got := domainreveal.ReturnPct(price("1.0"), price("0.9")); got.String() != "-10" {
		t.Errorf("ReturnPct(1.0, 0.9) = %s, want -10", got)
	}
}
