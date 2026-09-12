package execution

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jblabs/blindpulse-be/pkg/apperror"
	domainaccount "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/account"
	domainexec "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/execution"
	domainfeed "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/feed"
	domainsession "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/session"
	"github.com/shopspring/decimal"
)

func dec(value string) decimal.Decimal { return decimal.RequireFromString(value) }

func ptr(value string) *decimal.Decimal {
	parsed := dec(value)
	return &parsed
}

// --- stubs ---

type orderStub struct {
	rows map[uuid.UUID]*domainexec.Order
}

func (r *orderStub) Create(_ context.Context, entity *domainexec.Order) (*domainexec.Order, error) {
	stored := *entity
	r.rows[entity.ID] = &stored
	created := stored
	return &created, nil
}
func (r *orderStub) GetByID(_ context.Context, id uuid.UUID) (*domainexec.Order, error) {
	row, ok := r.rows[id]
	if !ok {
		return nil, apperror.New("ORDER_NOT_FOUND")
	}
	copied := *row
	return &copied, nil
}
func (r *orderStub) GetByClientKey(_ context.Context, sessionID uuid.UUID, key string) (*domainexec.Order, error) {
	for _, row := range r.rows {
		if row.SessionID == sessionID && row.ClientKey == key {
			copied := *row
			return &copied, nil
		}
	}
	return nil, apperror.New("ORDER_NOT_FOUND")
}
func (r *orderStub) ListBySessionID(_ context.Context, sessionID uuid.UUID) ([]domainexec.Order, error) {
	return r.filter(sessionID, func(domainexec.Order) bool { return true }), nil
}
func (r *orderStub) ListRestingBySessionID(_ context.Context, sessionID uuid.UUID) ([]domainexec.Order, error) {
	return r.filter(sessionID, func(o domainexec.Order) bool { return o.Status == domainexec.StatusPending }), nil
}

// Update enforces the optimistic lock, as the SQL adapter's WHERE ... AND version = ? does, and
// bumps the version the same way. A stub that ignored the version would let the domain pre-increment
// it and every test would still pass — while every real close failed with CONCURRENT_MODIFICATION.
func (r *orderStub) Update(_ context.Context, entity *domainexec.Order) error {
	current, ok := r.rows[entity.ID]
	if !ok || current.Version != entity.Version {
		return apperror.New("CONCURRENT_MODIFICATION")
	}
	stored := *entity
	stored.Version++
	r.rows[entity.ID] = &stored
	entity.Version = stored.Version
	return nil
}
func (r *orderStub) filter(sessionID uuid.UUID, keep func(domainexec.Order) bool) []domainexec.Order {
	list := make([]domainexec.Order, 0, len(r.rows))
	for _, row := range r.rows {
		if row.SessionID == sessionID && keep(*row) {
			list = append(list, *row)
		}
	}
	return list
}

type tradeStub struct {
	rows map[uuid.UUID]*domainexec.Trade
}

func (r *tradeStub) Create(_ context.Context, entity *domainexec.Trade) (*domainexec.Trade, error) {
	stored := *entity
	r.rows[entity.ID] = &stored
	created := stored
	return &created, nil
}
func (r *tradeStub) GetByID(_ context.Context, id uuid.UUID) (*domainexec.Trade, error) {
	row, ok := r.rows[id]
	if !ok {
		return nil, apperror.New("TRADE_NOT_FOUND")
	}
	copied := *row
	return &copied, nil
}
func (r *tradeStub) ListBySessionID(_ context.Context, sessionID uuid.UUID) ([]domainexec.Trade, error) {
	return r.filter(sessionID, func(domainexec.Trade) bool { return true }), nil
}
func (r *tradeStub) ListOpenBySessionID(_ context.Context, sessionID uuid.UUID) ([]domainexec.Trade, error) {
	return r.filter(sessionID, func(t domainexec.Trade) bool { return t.Open() }), nil
}
func (r *tradeStub) Update(_ context.Context, entity *domainexec.Trade) error {
	current, ok := r.rows[entity.ID]
	if !ok || current.Version != entity.Version {
		return apperror.New("CONCURRENT_MODIFICATION")
	}
	stored := *entity
	stored.Version++
	r.rows[entity.ID] = &stored
	entity.Version = stored.Version
	return nil
}
func (r *tradeStub) filter(sessionID uuid.UUID, keep func(domainexec.Trade) bool) []domainexec.Trade {
	list := make([]domainexec.Trade, 0, len(r.rows))
	for _, row := range r.rows {
		if row.SessionID == sessionID && keep(*row) {
			list = append(list, *row)
		}
	}
	return list
}

type snapshotStub struct{ rows []domainexec.EquitySnapshot }

func (r *snapshotStub) Upsert(_ context.Context, snapshot domainexec.EquitySnapshot) error {
	r.rows = append(r.rows, snapshot)
	return nil
}

// DayEquity is the in-memory twin of the adapter's one statement: the last mark before the boundary,
// and the highest since it. The stub filters on BarAt exactly as the SQL does — a stub that ignored
// the window would make every daily-reset assertion pass for the wrong reason.
func (r *snapshotStub) DayEquity(_ context.Context, _ uuid.UUID, dayStart time.Time) (decimal.Decimal, decimal.Decimal, error) {
	opening, peak := decimal.Zero, decimal.Zero
	var openingAt time.Time
	for _, row := range r.rows {
		if row.BarAt.Before(dayStart) {
			if openingAt.IsZero() || !row.BarAt.Before(openingAt) {
				opening, openingAt = row.Equity, row.BarAt
			}
			continue
		}
		if row.Equity.GreaterThan(peak) {
			peak = row.Equity
		}
	}
	return opening, peak, nil
}

type sessionStub struct{ session domainsession.Session }

func (s *sessionStub) OwnedSession(_ context.Context, userID, sessionID uuid.UUID) (*domainsession.Session, error) {
	if userID != s.session.UserID {
		return nil, apperror.New("SESSION_NOT_FOUND")
	}
	copied := s.session
	return &copied, nil
}
func (s *sessionStub) Session(_ context.Context, _ uuid.UUID) (*domainsession.Session, error) {
	copied := s.session
	return &copied, nil
}

type accountStub struct{ account domainaccount.Account }

func (a *accountStub) GetByID(_ context.Context, _ uuid.UUID) (*domainaccount.Account, error) {
	copied := a.account
	return &copied, nil
}
func (a *accountStub) ApplyRealizedPnL(_ context.Context, _ uuid.UUID, amount decimal.Decimal) error {
	a.account.CurrentBalance = a.account.CurrentBalance.Add(amount)
	a.account.CurrentEquity = a.account.CurrentBalance
	if a.account.CurrentBalance.GreaterThan(a.account.PeakEquity) {
		a.account.PeakEquity = a.account.CurrentBalance
	}
	return nil
}

type feedStub struct{ bars []domainfeed.Bar }

func (f *feedStub) Get(_ context.Context, id uuid.UUID) (*domainfeed.Feed, error) {
	return &domainfeed.Feed{ID: id, TotalBars: len(f.bars)}, nil
}
func (f *feedStub) Bars(_ context.Context, _ uuid.UUID, from, to int) ([]domainfeed.Bar, error) {
	list := make([]domainfeed.Bar, 0)
	for index := from; index <= to && index < len(f.bars); index++ {
		if index < 0 {
			continue
		}
		list = append(list, f.bars[index])
	}
	return list, nil
}

type clockStub struct{ times []time.Time }

func (c *clockStub) BarTimes(context.Context, uuid.UUID) ([]time.Time, error) { return c.times, nil }

type ledgerStub struct{ entries int }

func (l *ledgerStub) AppendTrade(context.Context, uuid.UUID, uuid.UUID, decimal.Decimal) error {
	l.entries++
	return nil
}

// --- fixture ---

type fixture struct {
	service  Service
	orders   *orderStub
	trades   *tradeStub
	accounts *accountStub
	snaps    *snapshotStub
	ledger   *ledgerStub
	sessions *sessionStub
	clock    *clockStub
	feeds    *feedStub
	userID   uuid.UUID
	session  uuid.UUID
}

// rebuild swaps the venue's frictions, keeping every stub and its accumulated state. It exists so a
// test can assert against the conditions the *composition root* actually produces rather than the
// convenient ones the fixture picks.
func (f *fixture) rebuild(conditions domainexec.Conditions) {
	f.service = NewService(Dependencies{
		Orders: f.orders, Trades: f.trades, Snapshots: f.snaps, Sessions: f.sessions,
		Accounts: f.accounts, Feeds: f.feeds, Clock: f.clock, Ledger: f.ledger,
		Now:        func() time.Time { return time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC) },
		Conditions: conditions,
	})
}

// flat builds a feed of bars all at the same price, so a test that wants nothing to happen gets
// nothing. Individual tests overwrite the bars they care about.
func flat(count int, price string) []domainfeed.Bar {
	bars := make([]domainfeed.Bar, count)
	value := dec(price)
	base := time.Date(2023, 3, 14, 8, 0, 0, 0, time.UTC)
	for i := range bars {
		bars[i] = domainfeed.Bar{Index: i, Open: value, High: value, Low: value, Close: value}
		_ = base
	}
	return bars
}

func newFixture(t *testing.T, bars []domainfeed.Bar) *fixture {
	t.Helper()
	userID, sessionID, accountID := uuid.New(), uuid.New(), uuid.New()
	times := make([]time.Time, len(bars))
	start := time.Date(2023, 3, 14, 8, 0, 0, 0, time.UTC)
	for i := range times {
		times[i] = start.Add(time.Duration(i) * 15 * time.Minute)
	}
	clock := &clockStub{times: times}
	orders := &orderStub{rows: map[uuid.UUID]*domainexec.Order{}}
	trades := &tradeStub{rows: map[uuid.UUID]*domainexec.Trade{}}
	snaps := &snapshotStub{}
	ledger := &ledgerStub{}
	sessions := &sessionStub{session: domainsession.Session{
		ID: sessionID, UserID: userID, AccountID: accountID, FeedID: uuid.New(),
		Status: domainsession.StatusOpen, CursorIndex: 10, Seed: 4242,
	}}
	accounts := &accountStub{account: domainaccount.Account{
		ID: accountID, InitialBalance: dec("10000"), CurrentBalance: dec("10000"),
		CurrentEquity: dec("10000"), PeakEquity: dec("10000"),
		Risk: domainaccount.RiskPolicy{
			RiskPerTradePct: dec("1"), MaxDailyDrawdownPct: dec("5"),
			MinRiskReward: dec("2"), MaxOpenPositions: 5, Leverage: dec("100"),
		},
	}}
	feeds := &feedStub{bars: bars}
	service := NewService(Dependencies{
		Orders: orders, Trades: trades, Snapshots: snaps, Sessions: sessions,
		Accounts: accounts, Feeds: feeds, Clock: clock,
		Ledger:     ledger,
		Now:        func() time.Time { return time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC) },
		Conditions: domainexec.Conditions{SpreadTicks: 1, MaxSlippageTicks: 0, TickSize: dec("0.00001")},
	})
	return &fixture{service: service, orders: orders, trades: trades, accounts: accounts,
		snaps: snaps, ledger: ledger, sessions: sessions, clock: clock, feeds: feeds,
		userID: userID, session: sessionID}
}

func marketBuy(key string) PlaceInput {
	return PlaceInput{
		ClientKey: key, Side: domainexec.SideBuy, Type: domainexec.OrderMarket,
		Quantity: ptr("10000"), StopLoss: dec("1.09000"), TakeProfit: ptr("1.12000"),
	}
}

// --- tests ---

// 04-AC-1 and BR-09 together: the rule is refused *and* the attempt is kept, because Sprint 06's
// discipline index is built from what the trader tried to do.
func TestARejectedOrderIsRefusedAndStillRecorded(t *testing.T) {
	f := newFixture(t, flat(40, "1.10000"))
	input := marketBuy("k1")
	input.StopLoss = decimal.Zero

	if _, err := f.service.Place(context.Background(), f.userID, f.session, input); !apperror.Is(err, domainexec.CodeStopRequired) {
		t.Fatalf("err = %v, want ORDER_STOP_REQUIRED", err)
	}
	stored, _ := f.orders.ListBySessionID(context.Background(), f.session)
	if len(stored) != 1 {
		t.Fatalf("%d orders stored, want the rejection kept", len(stored))
	}
	if stored[0].Status != domainexec.StatusRejected || stored[0].RejectionCode == nil ||
		*stored[0].RejectionCode != domainexec.CodeStopRequired {
		t.Errorf("stored order = %+v, want a rejection carrying its code", stored[0])
	}
	// And nothing was opened.
	if open, _ := f.trades.ListOpenBySessionID(context.Background(), f.session); len(open) != 0 {
		t.Errorf("%d positions opened by a refused order", len(open))
	}
}

// 04-AC-4. No check clamps a quantity to make an order acceptable: a trader who learns that
// oversized orders quietly shrink learns nothing about sizing.
func TestAnOversizedOrderIsRefusedRatherThanResized(t *testing.T) {
	f := newFixture(t, flat(40, "1.10000"))
	input := marketBuy("k1")
	input.Quantity = ptr("500000") // risks 5,000 of a 10,000 account against a 1% limit

	if _, err := f.service.Place(context.Background(), f.userID, f.session, input); !apperror.Is(err, domainexec.CodeStopTooWide) {
		t.Fatalf("err = %v, want RISK_STOP_TOO_WIDE", err)
	}
	stored, _ := f.orders.ListBySessionID(context.Background(), f.session)
	if got := stored[0].Quantity.String(); got != "500000" {
		t.Errorf("stored quantity = %s, want the size the trader actually asked for", got)
	}
}

// 04-AC-5. A retried submit carries the same client key and must not fill twice.
func TestTheSameClientKeyTwiceIsRefused(t *testing.T) {
	f := newFixture(t, flat(40, "1.10000"))
	if _, err := f.service.Place(context.Background(), f.userID, f.session, marketBuy("retry-me")); err != nil {
		t.Fatalf("Place: %v", err)
	}
	if _, err := f.service.Place(context.Background(), f.userID, f.session, marketBuy("retry-me")); !apperror.Is(err, "DUPLICATE_ORDER") {
		t.Fatalf("err = %v, want DUPLICATE_ORDER", err)
	}
	stored, _ := f.orders.ListBySessionID(context.Background(), f.session)
	if len(stored) != 1 {
		t.Errorf("%d orders exist, want exactly one", len(stored))
	}
}

// A market order fills at the *next* bar's open, not at the close the trader has already seen.
func TestAMarketOrderFillsOnTheNextBarAndOpensAPosition(t *testing.T) {
	bars := flat(40, "1.10000")
	bars[11] = domainfeed.Bar{Index: 11, Open: dec("1.10500"), High: dec("1.10600"), Low: dec("1.10400"), Close: dec("1.10500")}
	f := newFixture(t, bars)

	if _, err := f.service.Place(context.Background(), f.userID, f.session, marketBuy("k1")); err != nil {
		t.Fatalf("Place: %v", err)
	}
	if open, _ := f.trades.ListOpenBySessionID(context.Background(), f.session); len(open) != 0 {
		t.Fatal("a market order opened a position before the next bar existed")
	}
	if err := f.service.Advance(context.Background(), f.session, 10, 11); err != nil {
		t.Fatalf("Advance: %v", err)
	}
	open, _ := f.trades.ListOpenBySessionID(context.Background(), f.session)
	if len(open) != 1 {
		t.Fatalf("%d positions open, want 1", len(open))
	}
	// Bar 11's open plus one tick of half-spread. Filling at bar 10's close (1.10000) would be a
	// fill at a price the trader had already seen.
	if got := open[0].EntryPrice.String(); got != "1.10501" {
		t.Errorf("entry = %s, want 1.10501 (bar 11's open plus the spread)", got)
	}
}

// 04-AC-6 through the service: a bar covering both levels closes at the stop.
func TestABarCoveringBothLevelsClosesAtTheStopAndPaysTheLedger(t *testing.T) {
	bars := flat(40, "1.10000")
	bars[11] = domainfeed.Bar{Index: 11, Open: dec("1.10000"), High: dec("1.10100"), Low: dec("1.09900"), Close: dec("1.10000")}
	bars[12] = domainfeed.Bar{Index: 12, Open: dec("1.10000"), High: dec("1.12500"), Low: dec("1.08500"), Close: dec("1.10000")}
	f := newFixture(t, bars)

	if _, err := f.service.Place(context.Background(), f.userID, f.session, marketBuy("k1")); err != nil {
		t.Fatalf("Place: %v", err)
	}
	if err := f.service.Advance(context.Background(), f.session, 10, 12); err != nil {
		t.Fatalf("Advance: %v", err)
	}
	trades, _ := f.trades.ListBySessionID(context.Background(), f.session)
	if len(trades) != 1 {
		t.Fatalf("%d trades, want 1", len(trades))
	}
	closed := trades[0]
	if closed.Open() || closed.ExitReason == nil || *closed.ExitReason != domainexec.ExitStop {
		t.Fatalf("trade = %+v, want closed at the stop", closed)
	}
	if closed.RealizedPnL == nil || !closed.RealizedPnL.IsNegative() {
		t.Errorf("realized = %v, want a loss", closed.RealizedPnL)
	}
	// 04-AC-10: the money moved and the ledger recorded it.
	if f.ledger.entries != 1 {
		t.Errorf("%d ledger entries, want 1", f.ledger.entries)
	}
	if f.accounts.account.CurrentBalance.GreaterThanOrEqual(dec("10000")) {
		t.Errorf("balance = %s, want it reduced by the loss", f.accounts.account.CurrentBalance)
	}
}

// SP4-5. A resting order whose bar covers both its trigger and its stop fills and then stops out —
// one closed trade at roughly -1R, not an unfilled order.
func TestARestingOrderFilledAndStoppedOnOneBarProducesAClosedTrade(t *testing.T) {
	bars := flat(40, "1.10000")
	// A bar that trades down through the limit at 1.09500 and on through the stop at 1.09000.
	bars[11] = domainfeed.Bar{Index: 11, Open: dec("1.09900"), High: dec("1.10000"), Low: dec("1.08800"), Close: dec("1.08900")}
	f := newFixture(t, bars)

	input := PlaceInput{
		ClientKey: "limit", Side: domainexec.SideBuy, Type: domainexec.OrderLimit,
		LimitPrice: ptr("1.09500"), Quantity: ptr("10000"),
		StopLoss: dec("1.09000"), TakeProfit: ptr("1.10500"),
	}
	if _, err := f.service.Place(context.Background(), f.userID, f.session, input); err != nil {
		t.Fatalf("Place: %v", err)
	}
	if err := f.service.Advance(context.Background(), f.session, 10, 11); err != nil {
		t.Fatalf("Advance: %v", err)
	}
	trades, _ := f.trades.ListBySessionID(context.Background(), f.session)
	if len(trades) != 1 {
		t.Fatalf("%d trades, want 1 — it filled and then stopped", len(trades))
	}
	if trades[0].Open() {
		t.Fatal("the position is still open; the same bar's stop was not applied")
	}
	if got := trades[0].RMultiple; got == nil || got.GreaterThan(dec("-0.9")) {
		t.Errorf("R = %v, want about -1", got)
	}
}

// 04-AC-7 and 04-AC-8. The halt closes what is open and then refuses new orders.
func TestBreachingTheDrawdownHaltsTheSessionAndRefusesNewOrders(t *testing.T) {
	bars := flat(40, "1.10000")
	f := newFixture(t, bars)
	// A position big enough that the next bar's move breaches 5%. Risk-per-trade is widened so the
	// gate lets the size through — the point of this test is the halt, not the sizing limit.
	f.accounts.account.Risk.RiskPerTradePct = dec("90")
	bars[11] = domainfeed.Bar{Index: 11, Open: dec("1.10000"), High: dec("1.10000"), Low: dec("1.10000"), Close: dec("1.10000")}
	bars[12] = domainfeed.Bar{Index: 12, Open: dec("1.09000"), High: dec("1.09000"), Low: dec("1.09000"), Close: dec("1.09000")}

	input := marketBuy("big")
	input.Quantity = ptr("100000") // 0.01 against 100,000 units is 1,000: 10% of the account
	input.StopLoss = dec("1.05000")
	input.TakeProfit = ptr("1.20000")
	if _, err := f.service.Place(context.Background(), f.userID, f.session, input); err != nil {
		t.Fatalf("Place: %v", err)
	}
	if err := f.service.Advance(context.Background(), f.session, 10, 12); err != nil {
		t.Fatalf("Advance: %v", err)
	}
	if open, _ := f.trades.ListOpenBySessionID(context.Background(), f.session); len(open) != 0 {
		t.Fatalf("%d positions still open after the halt", len(open))
	}
	trades, _ := f.trades.ListBySessionID(context.Background(), f.session)
	if trades[0].ExitReason == nil || *trades[0].ExitReason != domainexec.ExitDrawdownHalt {
		t.Errorf("exit = %v, want the drawdown halt", trades[0].ExitReason)
	}
	// 04-AC-8: the halted session refuses the next order with the reason.
	next := marketBuy("after")
	if _, err := f.service.Place(context.Background(), f.userID, f.session, next); !apperror.Is(err, domainexec.CodeDrawdownBreached) {
		t.Errorf("err = %v, want DAILY_DRAWDOWN_BREACHED", err)
	}
}

// SP4-1's client-facing half. The readout says how close the trader is and nothing about when the
// window turns over — a reset pattern with weekends in it identifies the asset class.
func TestTheRiskReadoutCarriesNoDayBoundary(t *testing.T) {
	f := newFixture(t, flat(40, "1.10000"))
	state, err := f.service.Risk(context.Background(), f.userID, f.session)
	if err != nil {
		t.Fatalf("Risk: %v", err)
	}
	if state.Halted {
		t.Error("a fresh session reports halted")
	}
	if got := state.RoomRemainingPct.String(); got != "100" {
		t.Errorf("room = %s, want 100 on an untouched account", got)
	}
	// The type itself is the guard: a day ordinal or a countdown would have to be a field, and
	// there is nowhere to put one.
	if state.Equity.String() != "10000" {
		t.Errorf("equity = %s, want 10000", state.Equity)
	}
}

func TestAnEquitySnapshotIsWrittenPerBar(t *testing.T) {
	f := newFixture(t, flat(40, "1.10000"))
	if err := f.service.Advance(context.Background(), f.session, 10, 15); err != nil {
		t.Fatalf("Advance: %v", err)
	}
	if len(f.snaps.rows) != 5 {
		t.Errorf("%d snapshots for 5 bars", len(f.snaps.rows))
	}
	for _, snapshot := range f.snaps.rows {
		if snapshot.BarAt.IsZero() {
			t.Error("a snapshot has no bar time; the drawdown's day boundary reads it")
		}
	}
}

func TestSomeoneElsesSessionIsNotFound(t *testing.T) {
	f := newFixture(t, flat(40, "1.10000"))
	if _, err := f.service.Place(context.Background(), uuid.New(), f.session, marketBuy("k1")); !apperror.Is(err, "SESSION_NOT_FOUND") {
		t.Errorf("err = %v, want SESSION_NOT_FOUND", err)
	}
}

func TestQuantityAndRiskPercentAreExclusive(t *testing.T) {
	f := newFixture(t, flat(40, "1.10000"))
	both := marketBuy("k1")
	both.RiskPct = ptr("1")
	if _, err := f.service.Place(context.Background(), f.userID, f.session, both); !apperror.Is(err, "VALIDATION_FAILED") {
		t.Errorf("err = %v, want VALIDATION_FAILED for both", err)
	}
	neither := marketBuy("k2")
	neither.Quantity = nil
	if _, err := f.service.Place(context.Background(), f.userID, f.session, neither); !apperror.Is(err, "VALIDATION_FAILED") {
		t.Errorf("err = %v, want VALIDATION_FAILED for neither", err)
	}
	// And sizing from a risk percentage works on its own.
	sized := marketBuy("k3")
	sized.Quantity = nil
	sized.RiskPct = ptr("1")
	order, err := f.service.Place(context.Background(), f.userID, f.session, sized)
	if err != nil {
		t.Fatalf("Place with a risk percentage: %v", err)
	}
	if got := order.Quantity.String(); got != "10000" {
		t.Errorf("quantity = %s, want 10000 (1%% of 10,000 over a 0.01 stop)", got)
	}
}

// TestTheDrawdownAllowanceResetsAtTheMarketDay is SP4-1's substance: the *daily* in
// MaxDailyDrawdownPct.
//
// A position goes 4% under on the first market day and is still 4% under when the day turns over;
// on the second day it falls another 3.1%. Measured from the iteration's peak — or from the day-one
// equity — that is 7% and the gate should have tripped. Measured from where day two *started*, which
// already had the 4% inside it, day two lost 3.1% of a 5% allowance and the trader keeps trading.
// That is what a prop firm's daily rule does at rollover, and it is why the day's opening reference
// is the last mark before the boundary rather than a lifetime high.
func TestTheDrawdownAllowanceResetsAtTheMarketDay(t *testing.T) {
	bars := flat(40, "1.10000")
	// Day one falls to 1.09600 and *stays* there through the boundary: the 4% is still open when the
	// day turns over, which is the case the reset is actually about. An earlier version of this test
	// let bars 13 and 14 recover to 1.10000, so nothing was ever carried across and it passed against
	// a gate that ignored the previous day entirely.
	for index := 12; index <= 15; index++ {
		bars[index] = domainfeed.Bar{Index: index, Open: dec("1.09600"), High: dec("1.09600"), Low: dec("1.09600"), Close: dec("1.09600")}
	}
	bars[16] = domainfeed.Bar{Index: 16, Open: dec("1.09300"), High: dec("1.09300"), Low: dec("1.09300"), Close: dec("1.09300")}
	f := newFixture(t, bars)
	// Bars 0-14 are one market day; 15 onward are the next. The boundary is a real-timestamp fact
	// the trader never sees — the bars themselves carry only indices.
	dayTwo := time.Date(2023, 3, 15, 8, 0, 0, 0, time.UTC)
	for i := 15; i < len(f.clock.times); i++ {
		f.clock.times[i] = dayTwo.Add(time.Duration(i-15) * 15 * time.Minute)
	}
	f.accounts.account.Risk.RiskPerTradePct = dec("90")

	input := marketBuy("held")
	input.Quantity = ptr("100000")
	input.StopLoss = dec("1.05000")
	input.TakeProfit = ptr("1.20000")
	if _, err := f.service.Place(context.Background(), f.userID, f.session, input); err != nil {
		t.Fatalf("Place: %v", err)
	}
	// Walk the bars one step at a time, as a cursor move does, so each day accumulates the snapshots
	// its own high-water mark is read from.
	for bar := 11; bar <= 16; bar++ {
		if err := f.service.Advance(context.Background(), f.session, bar-1, bar); err != nil {
			t.Fatalf("Advance to %d: %v", bar, err)
		}
		f.sessions.session.CursorIndex = bar
	}

	open, _ := f.trades.ListOpenBySessionID(context.Background(), f.session)
	if len(open) != 1 {
		t.Fatalf("%d open positions, want the one still running: the day reset the allowance", len(open))
	}
	// Day two's own drawdown, not the 7% since the iteration's peak.
	state, err := f.service.Risk(context.Background(), f.userID, f.session)
	if err != nil {
		t.Fatalf("Risk: %v", err)
	}
	if state.Halted {
		t.Error("halted, but neither market day breached its own 5%")
	}
	if room := state.RoomRemainingPct; room.LessThan(dec("35")) || room.GreaterThan(dec("40")) {
		t.Errorf("room = %s, want ~37.5: 3.125%% used of a 5%% allowance", room)
	}
	next := marketBuy("day-two")
	next.Quantity = ptr("1000")
	next.TakeProfit = ptr("1.10000")
	if _, err := f.service.Place(context.Background(), f.userID, f.session, next); err != nil {
		t.Errorf("Place on day two: %v", err)
	}
}

// TestFrictionSurvivesConfigurationThatNamesNoTickSize runs the production shape.
//
// config.ExecutionConfig names a spread and a slippage bound and cannot name a tick size, because a
// tick is one unit of the blinded display scale and is resolved per session. Two places currently
// supply that default — the fill context, and MarketFill itself — so removing either alone changes
// nothing and this test passes. Removing both makes every market order fill at the bare open: a
// frictionless simulator, the most flattering bug this product could have, and invisible, because
// every number it produces stays entirely plausible.
//
// So this is a guard on the *behaviour* rather than on one of its two implementations: whatever
// happens to those defaults, a market buy under the configured conditions has to cost something.
func TestFrictionSurvivesConfigurationThatNamesNoTickSize(t *testing.T) {
	bars := flat(40, "1.10000")
	f := newFixture(t, bars)
	// Exactly what config.ExecutionConfig produces: ticks named, tick size absent.
	f.rebuild(domainexec.Conditions{SpreadTicks: 1, MaxSlippageTicks: 2})

	if _, err := f.service.Place(context.Background(), f.userID, f.session, marketBuy("friction")); err != nil {
		t.Fatalf("Place: %v", err)
	}
	if err := f.service.Advance(context.Background(), f.session, 10, 11); err != nil {
		t.Fatalf("Advance: %v", err)
	}
	open, _ := f.trades.ListOpenBySessionID(context.Background(), f.session)
	if len(open) != 1 {
		t.Fatalf("%d positions, want 1", len(open))
	}
	// A buy crosses half a one-tick spread and wears the adverse slippage draw, so it must pay
	// *more* than the bar's open. The exact figure depends on the seeded draw; the direction and the
	// fact that it moved at all are what this test is about.
	if entry := open[0].EntryPrice; !entry.GreaterThan(dec("1.10000")) {
		t.Errorf("entry = %s at an open of 1.10000: the fill cost nothing, so the tick size was zero", entry)
	}
}

// TestALossOnTheSessionsOpeningBarSpendsTheAllowance closes the hole at the start of a session.
//
// The daily reference is the day's own high-water mark, read from the equity snapshots. On a session's
// very first bar there are none — nothing has been marked yet — so a reference taken from the
// snapshots alone would be whatever that first bar left the account at, and a position that filled and
// lost on it would have spent none of the day's allowance. The gate would read 100% room on an account
// already down.
//
// Seeding an unmarked day from the balance fixes it, and this is the test that says so: an opening bar
// that takes the account 2% under must show up as 2% of a 5% allowance spent.
func TestALossOnTheSessionsOpeningBarSpendsTheAllowance(t *testing.T) {
	bars := flat(40, "1.10000")
	// The bar the order fills on opens flat and closes 2% of the account lower.
	bars[11] = domainfeed.Bar{Index: 11, Open: dec("1.10000"), High: dec("1.10000"), Low: dec("1.09800"), Close: dec("1.09800")}
	f := newFixture(t, bars)
	f.accounts.account.Risk.RiskPerTradePct = dec("90")

	input := marketBuy("opener")
	input.Quantity = ptr("100000") // 0.002 × 100,000 = 200, which is 2% of 10,000
	input.StopLoss = dec("1.05000")
	input.TakeProfit = ptr("1.20000")
	if _, err := f.service.Place(context.Background(), f.userID, f.session, input); err != nil {
		t.Fatalf("Place: %v", err)
	}
	if err := f.service.Advance(context.Background(), f.session, 10, 11); err != nil {
		t.Fatalf("Advance: %v", err)
	}
	f.sessions.session.CursorIndex = 11

	state, err := f.service.Risk(context.Background(), f.userID, f.session)
	if err != nil {
		t.Fatalf("Risk: %v", err)
	}
	// 2% used of a 5% allowance leaves 60%. A reference read from the opening bar's own snapshot
	// would report 100 — the account down 2% and the gate reporting untouched.
	if room := state.RoomRemainingPct; room.GreaterThan(dec("62")) {
		t.Errorf("room = %s after a 2%% opening-bar loss, want ~60: the day was never seeded", room)
	}
	if state.Halted {
		t.Error("halted at 2% of a 5% allowance")
	}
}

// TestAPartialCloseLeavesTheCallersPositionOpenUnderItsOwnID is an API contract, not an accounting
// detail.
//
// Closing 40% splits the position, and which half keeps the id decides whether the client's handle
// still works afterwards. It used to be the wrong way round: the original row was closed and the
// remaining 60% became a row with a fresh id, so the trade id the dock was holding came back
// POSITION_ALREADY_CLOSED on the very next amend, and the position the trader could still see had an
// id they had never been told.
func TestAPartialCloseLeavesTheCallersPositionOpenUnderItsOwnID(t *testing.T) {
	bars := flat(40, "1.10000")
	f := newFixture(t, bars)

	if _, err := f.service.Place(context.Background(), f.userID, f.session, marketBuy("splitter")); err != nil {
		t.Fatalf("Place: %v", err)
	}
	if err := f.service.Advance(context.Background(), f.session, 10, 11); err != nil {
		t.Fatalf("Advance: %v", err)
	}
	f.sessions.session.CursorIndex = 11
	open, _ := f.trades.ListOpenBySessionID(context.Background(), f.session)
	if len(open) != 1 {
		t.Fatalf("%d positions, want 1", len(open))
	}
	original := open[0].ID
	full := open[0].Quantity

	fraction := dec("0.4")
	closed, err := f.service.ClosePosition(context.Background(), f.userID, original, &fraction)
	if err != nil {
		t.Fatalf("ClosePosition: %v", err)
	}
	// The closed portion is the new row, and it carries the realized result.
	if closed.ID == original {
		t.Error("the closed portion took the caller's id; the open remainder is now unreachable")
	}
	if closed.RealizedPnL == nil {
		t.Error("the closed portion has no realized PnL")
	}

	// The caller's id is still open, and holds the other 60%.
	remaining, err := f.trades.GetByID(context.Background(), original)
	if err != nil {
		t.Fatalf("the caller's trade id no longer resolves: %v", err)
	}
	if !remaining.Open() {
		t.Error("the caller's position was closed by a partial close")
	}
	if want := full.Sub(closed.Quantity); !remaining.Quantity.Equal(want) {
		t.Errorf("remaining quantity = %s, want %s", remaining.Quantity, want)
	}
	// And it is still amendable, which is the failure the old split produced.
	target := dec("1.15000")
	if _, err := f.service.Amend(context.Background(), f.userID, original, AmendInput{TakeProfit: &target}); err != nil {
		t.Errorf("amending the position after a partial close: %v", err)
	}
}

// A fraction that rounds to nothing, or to the whole position, is refused rather than quietly
// promoted to a full close: the trader asked for a specific size.
func TestAPartialCloseThatRoundsAwayIsRefused(t *testing.T) {
	bars := flat(40, "1.10000")
	f := newFixture(t, bars)
	if _, err := f.service.Place(context.Background(), f.userID, f.session, marketBuy("tiny")); err != nil {
		t.Fatalf("Place: %v", err)
	}
	if err := f.service.Advance(context.Background(), f.session, 10, 11); err != nil {
		t.Fatalf("Advance: %v", err)
	}
	f.sessions.session.CursorIndex = 11
	open, _ := f.trades.ListOpenBySessionID(context.Background(), f.session)

	sliver := dec("0.0000000001")
	if _, err := f.service.ClosePosition(context.Background(), f.userID, open[0].ID, &sliver); !apperror.Is(err, domainexec.CodeQuantityInvalid) {
		t.Errorf("err = %v, want ORDER_QUANTITY_INVALID for a fraction that rounds to nothing", err)
	}
	if still, _ := f.trades.ListOpenBySessionID(context.Background(), f.session); len(still) != 1 {
		t.Errorf("%d positions after a refused partial close, want the original still open", len(still))
	}
}

// TestTheTargetCanStillMoveAfterTheStopWentToBreakeven keeps Breakeven from being a one-way door.
//
// Breakeven puts the stop *at* the entry deliberately, which is outside the rule that a stop must sit
// beyond the entry — that is the whole point of it being its own action rather than an amend. Amend
// used to re-check both levels on every call regardless of which one was named, so the next attempt to
// move the target re-validated the breakeven stop and refused with ORDER_STOP_INVALID. Using the one
// action the discipline index most wants to reward permanently froze the position's target.
func TestTheTargetCanStillMoveAfterTheStopWentToBreakeven(t *testing.T) {
	bars := flat(40, "1.10000")
	f := newFixture(t, bars)
	if _, err := f.service.Place(context.Background(), f.userID, f.session, marketBuy("be-then-target")); err != nil {
		t.Fatalf("Place: %v", err)
	}
	if err := f.service.Advance(context.Background(), f.session, 10, 11); err != nil {
		t.Fatalf("Advance: %v", err)
	}
	f.sessions.session.CursorIndex = 11
	open, _ := f.trades.ListOpenBySessionID(context.Background(), f.session)
	tradeID := open[0].ID

	moved, err := f.service.Breakeven(context.Background(), f.userID, tradeID)
	if err != nil {
		t.Fatalf("Breakeven: %v", err)
	}
	if !moved.StopLoss.Equal(moved.EntryPrice) {
		t.Fatalf("stop = %s, want the entry %s", moved.StopLoss, moved.EntryPrice)
	}
	// 1R is unchanged by the move, which is what makes the R-multiple still mean something.
	if !moved.InitialStopLoss.Equal(dec("1.09000")) {
		t.Errorf("initial stop = %s, want the original 1.09000", moved.InitialStopLoss)
	}

	target := dec("1.15000")
	amended, err := f.service.Amend(context.Background(), f.userID, tradeID, AmendInput{TakeProfit: &target})
	if err != nil {
		t.Fatalf("moving the target after breakeven: %v", err)
	}
	if amended.TakeProfit == nil || !amended.TakeProfit.Equal(target) {
		t.Errorf("take profit = %v, want %s", amended.TakeProfit, target)
	}
	// And naming a bad stop is still refused: the check was narrowed, not removed.
	bad := dec("1.11000")
	if _, err := f.service.Amend(context.Background(), f.userID, tradeID, AmendInput{StopLoss: &bad}); !apperror.Is(err, domainexec.CodeStopInvalid) {
		t.Errorf("err = %v, want ORDER_STOP_INVALID for a stop above a long's entry", err)
	}
}

// TestAStoppedTradeRecordsTravellingAllTheWayToItsStop makes MAE comparable between winners and
// losers.
//
// Excursions were extended only for positions that *survived* a bar, so the bar that stopped a trade
// never contributed its extremes. A stopped trade therefore recorded a maximum adverse excursion
// smaller than the stop distance it had demonstrably travelled, while a winner's covered its whole
// life — and MAE exists precisely to be compared across trades, so two numbers measured over
// different spans made it useless.
func TestAStoppedTradeRecordsTravellingAllTheWayToItsStop(t *testing.T) {
	bars := flat(40, "1.10000")
	// Bar 11 fills it at the open; bar 12 reaches down through the stop at 1.09000.
	bars[12] = domainfeed.Bar{Index: 12, Open: dec("1.10000"), High: dec("1.10000"), Low: dec("1.08900"), Close: dec("1.09500")}
	f := newFixture(t, bars)

	if _, err := f.service.Place(context.Background(), f.userID, f.session, marketBuy("stopped")); err != nil {
		t.Fatalf("Place: %v", err)
	}
	if err := f.service.Advance(context.Background(), f.session, 10, 12); err != nil {
		t.Fatalf("Advance: %v", err)
	}

	trades, _ := f.trades.ListBySessionID(context.Background(), f.session)
	if len(trades) != 1 || trades[0].Open() {
		t.Fatalf("want one closed trade, got %d", len(trades))
	}
	closed := trades[0]
	if closed.MaxAdverseExcursion == nil {
		t.Fatal("a stopped trade recorded no adverse excursion at all")
	}
	// It travelled at least the full distance from entry to stop — that is what being stopped means.
	// Reporting less would say the trade was never seriously threatened by the move that ended it.
	stopDistance := closed.EntryPrice.Sub(closed.StopLoss).Abs()
	if closed.MaxAdverseExcursion.LessThan(stopDistance) {
		t.Errorf("MAE %s is less than the %s stop distance it was stopped at", closed.MaxAdverseExcursion, stopDistance)
	}
}
