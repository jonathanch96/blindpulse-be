package session

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jblabs/blindpulse-be/pkg/apperror"
	domainfeed "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/feed"
	"github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/market"
	domainsession "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/session"
	"github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/event"
	"github.com/shopspring/decimal"
)

type sessionRepoStub struct {
	rows map[uuid.UUID]*domainsession.Session
}

func newSessionRepoStub() *sessionRepoStub {
	return &sessionRepoStub{rows: make(map[uuid.UUID]*domainsession.Session)}
}

func (r *sessionRepoStub) Create(_ context.Context, entity *domainsession.Session) (*domainsession.Session, error) {
	for _, existing := range r.rows {
		if existing.AccountID == entity.AccountID && existing.Status.Live() {
			return nil, apperror.New("SESSION_ALREADY_OPEN")
		}
	}
	stored := *entity
	r.rows[entity.ID] = &stored
	created := stored
	return &created, nil
}

func (r *sessionRepoStub) GetByID(_ context.Context, id uuid.UUID) (*domainsession.Session, error) {
	entity, ok := r.rows[id]
	if !ok {
		return nil, apperror.New("SESSION_NOT_FOUND")
	}
	copied := *entity
	return &copied, nil
}

func (r *sessionRepoStub) ListLiveByUserID(_ context.Context, userID uuid.UUID) ([]domainsession.Session, error) {
	live := make([]domainsession.Session, 0)
	for _, entity := range r.rows {
		if entity.UserID == userID && entity.Status.Live() {
			live = append(live, *entity)
		}
	}
	return live, nil
}

func (r *sessionRepoStub) GetLiveByAccountID(_ context.Context, accountID uuid.UUID) (*domainsession.Session, error) {
	for _, entity := range r.rows {
		if entity.AccountID == accountID && entity.Status.Live() {
			copied := *entity
			return &copied, nil
		}
	}
	return nil, apperror.New("SESSION_NOT_FOUND")
}

func (r *sessionRepoStub) UpdateCursor(_ context.Context, entity *domainsession.Session) error {
	stored, ok := r.rows[entity.ID]
	if !ok || stored.Version != entity.Version {
		return apperror.New("CONCURRENT_MODIFICATION")
	}
	updated := *entity
	updated.Version++
	r.rows[entity.ID] = &updated
	entity.Version++
	return nil
}

func (r *sessionRepoStub) UpdateStatus(_ context.Context, id uuid.UUID, status domainsession.Status, version int) error {
	stored, ok := r.rows[id]
	if !ok || stored.Version != version {
		return apperror.New("CONCURRENT_MODIFICATION")
	}
	stored.Status = status
	stored.Version++
	return nil
}

type feedReaderStub struct {
	feed domainfeed.Feed
	// served records the widest range ever handed out, so a test can assert what the trader could
	// actually have seen rather than only what the API returned.
	served [][2]int
}

func (f *feedReaderStub) Get(_ context.Context, _ uuid.UUID) (*domainfeed.Feed, error) {
	copied := f.feed
	return &copied, nil
}

func (f *feedReaderStub) Bars(_ context.Context, _ uuid.UUID, from, to int) ([]domainfeed.Bar, error) {
	f.served = append(f.served, [2]int{from, to})
	bars := make([]domainfeed.Bar, 0, to-from+1)
	for index := from; index <= to; index++ {
		bars = append(bars, domainfeed.Bar{Index: index, Close: decimal.NewFromInt(int64(100 + index))})
	}
	return bars, nil
}

type accountReaderStub struct{ err error }

func (a accountReaderStub) OwnedActiveAccount(context.Context, uuid.UUID, uuid.UUID) error {
	return a.err
}

type stateStoreStub struct {
	states map[uuid.UUID]domainsession.State
	// enabled false simulates Redis being absent, which must never change behaviour.
	enabled bool
}

func newStateStoreStub(enabled bool) *stateStoreStub {
	return &stateStoreStub{states: make(map[uuid.UUID]domainsession.State), enabled: enabled}
}

func (s *stateStoreStub) Load(_ context.Context, id uuid.UUID) (*domainsession.State, bool) {
	if !s.enabled {
		return nil, false
	}
	state, ok := s.states[id]
	if !ok {
		return nil, false
	}
	return &state, true
}

func (s *stateStoreStub) Save(_ context.Context, state domainsession.State) error {
	if s.enabled {
		s.states[state.SessionID] = state
	}
	return nil
}

func (s *stateStoreStub) Clear(_ context.Context, id uuid.UUID) error {
	delete(s.states, id)
	return nil
}

type outboxStub struct{ events []event.OutboxEvent }

func (o *outboxStub) Create(_ context.Context, record *event.OutboxEvent) error {
	o.events = append(o.events, *record)
	return nil
}

type harness struct {
	service Service
	repo    *sessionRepoStub
	feeds   *feedReaderStub
	state   *stateStoreStub
	outbox  *outboxStub
	userID  uuid.UUID
	account uuid.UUID
}

func newHarness(t *testing.T, cacheEnabled bool) *harness {
	t.Helper()
	repo := newSessionRepoStub()
	feeds := &feedReaderStub{feed: domainfeed.Feed{
		ID: uuid.New(), BaseTimeframe: market.TF15m, WarmupBars: 200, TotalBars: 500,
	}}
	state := newStateStoreStub(cacheEnabled)
	outbox := &outboxStub{}
	service := NewService(Dependencies{
		Repo: repo, State: state, Feeds: feeds, Accounts: accountReaderStub{}, Outbox: outbox,
		Seeds: func() int64 { return 4242 },
		Clock: func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) },
	})
	return &harness{service: service, repo: repo, feeds: feeds, state: state, outbox: outbox,
		userID: uuid.New(), account: uuid.New()}
}

func (h *harness) start(t *testing.T) *domainsession.Session {
	t.Helper()
	entity, err := h.service.Start(context.Background(), h.userID, StartInput{
		AccountID: h.account, FeedID: h.feeds.feed.ID,
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	return entity
}

func TestStartOpensAtTheEndOfTheWarmupWindow(t *testing.T) {
	t.Parallel()
	h := newHarness(t, true)

	entity := h.start(t)

	// The trader is handed the lookback and nothing beyond it. Bar 199 is the newest free bar;
	// bar 200 is the first they must step to.
	if entity.CursorIndex != 199 || entity.RevealedIndex != 199 {
		t.Fatalf("cursor = %d, revealed = %d; want both at the last warmup bar (199)",
			entity.CursorIndex, entity.RevealedIndex)
	}
	if entity.Seed != 4242 {
		t.Fatalf("seed = %d; a session without a recorded seed is not reproducible", entity.Seed)
	}
	if len(h.outbox.events) != 1 || h.outbox.events[0].EventType != event.TypeSessionStarted {
		t.Fatalf("outbox = %+v", h.outbox.events)
	}
}

func TestOneLiveSessionPerAccount(t *testing.T) {
	t.Parallel()
	h := newHarness(t, true)
	h.start(t)

	_, err := h.service.Start(context.Background(), h.userID, StartInput{
		AccountID: h.account, FeedID: h.feeds.feed.ID,
	})

	// Two cursors over one account's equity would make the drawdown gate meaningless.
	if !apperror.Is(err, "SESSION_ALREADY_OPEN") {
		t.Fatalf("second Start() err = %v, want SESSION_ALREADY_OPEN", err)
	}
}

func TestBarsPastTheRevealedEdgeAreRefusedNotClamped(t *testing.T) {
	t.Parallel()
	h := newHarness(t, true)
	entity := h.start(t)

	_, err := h.service.Bars(context.Background(), h.userID, entity.ID, 0, 300)

	// Clamping would turn an attempt to peek into a plausible-looking success, and would hide a
	// real client bug at the same time.
	if !apperror.Is(err, "INVALID_CURSOR") {
		t.Fatalf("reading past the edge err = %v, want INVALID_CURSOR", err)
	}
	if len(h.feeds.served) != 0 {
		t.Fatalf("the feed was queried for %v despite the request being refused", h.feeds.served)
	}
}

func TestSteppingForwardReleasesExactlyOneBarAtATime(t *testing.T) {
	t.Parallel()
	h := newHarness(t, true)
	entity := h.start(t)

	stepped, err := h.service.Step(context.Background(), h.userID, entity.ID, 1)
	if err != nil {
		t.Fatalf("Step() error = %v", err)
	}

	if stepped.CursorIndex != 200 || stepped.RevealedIndex != 200 {
		t.Fatalf("after one step cursor = %d, revealed = %d; want 200 and 200",
			stepped.CursorIndex, stepped.RevealedIndex)
	}
	if _, err := h.service.Bars(context.Background(), h.userID, entity.ID, 0, 200); err != nil {
		t.Fatalf("bar 200 should be readable after stepping to it: %v", err)
	}
	if _, err := h.service.Bars(context.Background(), h.userID, entity.ID, 0, 201); !apperror.Is(err, "INVALID_CURSOR") {
		t.Fatalf("bar 201 err = %v, want INVALID_CURSOR", err)
	}
}

// The rule that makes candle-by-candle review safe: rewinding moves what the trader is looking at,
// never what they are allowed to know. Without it, stepping back would let somebody act on a bar
// whose outcome they had already seen.
func TestRewindingDoesNotLowerTheRevealedEdge(t *testing.T) {
	t.Parallel()
	h := newHarness(t, true)
	entity := h.start(t)
	if _, err := h.service.Step(context.Background(), h.userID, entity.ID, 20); err != nil {
		t.Fatalf("Step() error = %v", err)
	}

	rewound, err := h.service.Step(context.Background(), h.userID, entity.ID, -15)
	if err != nil {
		t.Fatalf("rewind error = %v", err)
	}

	if rewound.CursorIndex != 204 {
		t.Fatalf("cursor = %d, want 204 after rewinding 15 from 219", rewound.CursorIndex)
	}
	if rewound.RevealedIndex != 219 {
		t.Fatalf("revealed = %d, want it held at 219 — rewinding must not un-reveal", rewound.RevealedIndex)
	}
	// And the bars they already saw are still readable, because they already saw them.
	if _, err := h.service.Bars(context.Background(), h.userID, entity.ID, 0, 219); err != nil {
		t.Fatalf("already-revealed bars should stay readable after a rewind: %v", err)
	}
}

func TestSeekForwardIsRefused(t *testing.T) {
	t.Parallel()
	h := newHarness(t, true)
	entity := h.start(t)

	_, err := h.service.Seek(context.Background(), h.userID, entity.ID, 250)

	if !apperror.Is(err, "INVALID_CURSOR") {
		t.Fatalf("seeking forward err = %v, want INVALID_CURSOR", err)
	}
}

func TestSeekBackwardIsAllowedForReview(t *testing.T) {
	t.Parallel()
	h := newHarness(t, true)
	entity := h.start(t)
	if _, err := h.service.Step(context.Background(), h.userID, entity.ID, 30); err != nil {
		t.Fatalf("Step() error = %v", err)
	}

	sought, err := h.service.Seek(context.Background(), h.userID, entity.ID, 205)
	if err != nil {
		t.Fatalf("Seek() error = %v", err)
	}

	if sought.CursorIndex != 205 || sought.RevealedIndex != 229 {
		t.Fatalf("cursor = %d, revealed = %d; want 205 and 229", sought.CursorIndex, sought.RevealedIndex)
	}
}

func TestSteppingBeyondTheFeedIsExhaustionNotAnError(t *testing.T) {
	t.Parallel()
	h := newHarness(t, true)
	entity := h.start(t)

	_, err := h.service.Step(context.Background(), h.userID, entity.ID, 500)

	if !apperror.Is(err, "BARS_EXHAUSTED") {
		t.Fatalf("stepping past the last bar err = %v, want BARS_EXHAUSTED", err)
	}
}

func TestSpeedIsBounded(t *testing.T) {
	t.Parallel()
	h := newHarness(t, true)
	entity := h.start(t)

	for _, speed := range []string{"0.1", "12", "-1", "nonsense"} {
		if _, err := h.service.SetSpeed(context.Background(), h.userID, entity.ID, speed); !apperror.Is(err, "INVALID_PLAYBACK_SPEED") {
			t.Fatalf("speed %q err = %v, want INVALID_PLAYBACK_SPEED", speed, err)
		}
	}
	if _, err := h.service.SetSpeed(context.Background(), h.userID, entity.ID, "10"); err != nil {
		t.Fatalf("speed 10 should be accepted: %v", err)
	}
}

func TestClosedSessionsRefuseFurtherMovement(t *testing.T) {
	t.Parallel()
	h := newHarness(t, true)
	entity := h.start(t)
	if _, err := h.service.Close(context.Background(), h.userID, entity.ID); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	_, stepErr := h.service.Step(context.Background(), h.userID, entity.ID, 1)
	_, seekErr := h.service.Seek(context.Background(), h.userID, entity.ID, 100)

	if !apperror.Is(stepErr, "SESSION_CLOSED") || !apperror.Is(seekErr, "SESSION_CLOSED") {
		t.Fatalf("step err = %v, seek err = %v; both want SESSION_CLOSED", stepErr, seekErr)
	}
	// Closed sessions stay readable — the post-mortem needs them.
	if _, err := h.service.Bars(context.Background(), h.userID, entity.ID, 0, 199); err != nil {
		t.Fatalf("a closed session's bars should still be readable: %v", err)
	}
}

func TestSessionsAreScopedToTheirOwner(t *testing.T) {
	t.Parallel()
	h := newHarness(t, true)
	entity := h.start(t)

	_, err := h.service.Get(context.Background(), uuid.New(), entity.ID)

	if !apperror.Is(err, "SESSION_NOT_FOUND") {
		t.Fatalf("cross-user Get() err = %v, want SESSION_NOT_FOUND", err)
	}
}

// Redis is a cache. With it absent every rule must still hold, because a cache outage that
// silently loosened the cursor would be a hindsight leak triggered by an infrastructure event.
func TestCursorRulesHoldWithoutTheCache(t *testing.T) {
	t.Parallel()
	h := newHarness(t, false)
	entity := h.start(t)
	if _, err := h.service.Step(context.Background(), h.userID, entity.ID, 10); err != nil {
		t.Fatalf("Step() error = %v", err)
	}

	_, err := h.service.Bars(context.Background(), h.userID, entity.ID, 0, 210)

	if !apperror.Is(err, "INVALID_CURSOR") {
		t.Fatalf("without a cache, reading past the edge err = %v, want INVALID_CURSOR", err)
	}
}

// A stale cache must never be able to un-reveal a bar the trader has already been shown.
func TestAStaleCacheCannotRewindTheRevealedEdge(t *testing.T) {
	t.Parallel()
	h := newHarness(t, true)
	entity := h.start(t)
	if _, err := h.service.Step(context.Background(), h.userID, entity.ID, 50); err != nil {
		t.Fatalf("Step() error = %v", err)
	}
	// Simulate a cache holding an older snapshot than the database.
	h.state.states[entity.ID] = domainsession.State{
		SessionID: entity.ID, Status: domainsession.StatusOpen,
		CursorIndex: 100, RevealedIndex: 100,
	}

	current, err := h.service.Get(context.Background(), h.userID, entity.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	if current.RevealedIndex != 249 {
		t.Fatalf("revealed = %d; a stale cache must not lower it below the stored 249", current.RevealedIndex)
	}
}
