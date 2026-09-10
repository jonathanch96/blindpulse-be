package session

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jblabs/blindpulse-be/pkg/apperror"
	domainfeed "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/feed"
	"github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/market"
	domainsession "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/session"
)

// recordingBus is an unbounded local bus. The interesting drop-oldest behaviour lives in the real
// adapter and is tested there; here the bus must never lose a frame, so that a missing frame in
// these tests means the domain never published it.
type recordingBus struct {
	mu          sync.Mutex
	published   []domainsession.Frame
	subscribers map[uuid.UUID][]chan domainsession.Frame
}

func newRecordingBus() *recordingBus {
	return &recordingBus{subscribers: make(map[uuid.UUID][]chan domainsession.Frame)}
}

func (b *recordingBus) Publish(_ context.Context, frame domainsession.Frame) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.published = append(b.published, frame)
	for _, channel := range b.subscribers[frame.SessionID] {
		select {
		case channel <- frame:
		default:
		}
	}
	return nil
}

func (b *recordingBus) Subscribe(_ context.Context, sessionID uuid.UUID) (<-chan domainsession.Frame, func(), error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	channel := make(chan domainsession.Frame, 512)
	b.subscribers[sessionID] = append(b.subscribers[sessionID], channel)
	return channel, func() {}, nil
}

func (b *recordingBus) kinds() []domainsession.FrameKind {
	b.mu.Lock()
	defer b.mu.Unlock()
	kinds := make([]domainsession.FrameKind, 0, len(b.published))
	for _, frame := range b.published {
		kinds = append(kinds, frame.Kind)
	}
	return kinds
}

func (b *recordingBus) count() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.published)
}

// countingLease grants the lease a fixed number of times and then refuses, which is how a test
// makes "another replica holds it" happen on demand.
type countingLease struct {
	mu       sync.Mutex
	granted  bool
	holder   string
	released int
	refuse   bool
}

func (l *countingLease) Acquire(_ context.Context, _ uuid.UUID, holder string, _ time.Duration) (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.refuse {
		return false, nil
	}
	if l.granted && l.holder != holder {
		return false, nil
	}
	l.granted, l.holder = true, holder
	return true, nil
}

func (l *countingLease) Release(_ context.Context, _ uuid.UUID, holder string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.holder == holder {
		l.granted = false
		l.released++
	}
	return nil
}

type streamHarness struct {
	*harness
	bus   *recordingBus
	lease *countingLease
}

// driverCount reaches into the concrete service because the number of running clocks is exactly
// what one of these tests is about, and there is no way to observe it from the interface.
func (h *streamHarness) driverCount() int {
	concrete, ok := h.service.(*service)
	if !ok {
		return -1
	}
	concrete.driversMu.Lock()
	defer concrete.driversMu.Unlock()
	return len(concrete.drivers)
}

// newStreamHarness runs the clock fast: a 2ms bar keeps these tests to milliseconds while still
// exercising the real timer path rather than a fake clock that could hide an ordering bug.
func newStreamHarness(t *testing.T) *streamHarness {
	t.Helper()
	base := newHarness(t, true)
	bus, lease := newRecordingBus(), &countingLease{}
	base.service = NewService(Dependencies{
		Repo: base.repo, State: base.state, Feeds: base.feeds, Accounts: accountReaderStub{},
		Outbox: base.outbox,
		Seeds:  func() int64 { return 4242 },
		Clock:  func() time.Time { return time.Now().UTC() },
		Bus:    bus, Lease: lease, Ticket: newMemoryTickets(),
		BaseTick: 2 * time.Millisecond, Holder: "replica-a",
	})
	return &streamHarness{harness: base, bus: bus, lease: lease}
}

type memoryTickets struct {
	mu   sync.Mutex
	rows map[string]StreamTicket
	next int
}

func newMemoryTickets() *memoryTickets { return &memoryTickets{rows: make(map[string]StreamTicket)} }

func (m *memoryTickets) Issue(_ context.Context, ticket StreamTicket, _ time.Duration) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.next++
	token := uuid.NewString()
	m.rows[token] = ticket
	return token, nil
}

func (m *memoryTickets) Redeem(_ context.Context, token string) (*StreamTicket, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	ticket, ok := m.rows[token]
	delete(m.rows, token)
	if !ok {
		return nil, apperror.New("STREAM_TICKET_INVALID")
	}
	return &ticket, nil
}

// waitFor polls until the condition holds, so a test asserts on an outcome rather than on a sleep
// long enough to be flaky on a loaded machine.
func waitFor(t *testing.T, why string, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", why)
}

// The core of FR-REPLAY-05: the server advances the cursor on its own clock. The client asks for
// nothing; bars simply arrive.
func TestDriverAdvancesTheCursorWithoutTheClientAskingForAnything(t *testing.T) {
	h := newStreamHarness(t)
	entity := h.start(t)
	opened := entity.RevealedIndex

	frames, stop, err := h.service.Stream(context.Background(), h.userID, entity.ID)
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}
	defer stop()

	var bars int
	deadline := time.After(2 * time.Second)
	for bars < 3 {
		select {
		case frame := <-frames:
			if frame.Kind == domainsession.FrameBar {
				bars++
				if frame.Bar == nil {
					t.Errorf("bar frame %d arrived with no bar", bars)
				}
			}
		case <-deadline:
			t.Fatalf("only %d bar frames arrived; the clock is not driving", bars)
		}
	}

	current, err := h.service.Get(context.Background(), h.userID, entity.ID)
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if current.RevealedIndex < opened+3 {
		t.Errorf("revealed index = %d after 3 bar frames, want at least %d", current.RevealedIndex, opened+3)
	}
}

// A paused session must not walk itself forward. This is not a cosmetic bug: every bar the clock
// releases is a bar the trader can then read, so a driver that ignores pause hands out hindsight.
func TestDriverDoesNotAdvanceAPausedSession(t *testing.T) {
	h := newStreamHarness(t)
	entity := h.start(t)
	if _, err := h.service.Pause(context.Background(), h.userID, entity.ID); err != nil {
		t.Fatalf("Pause() error = %v", err)
	}
	paused, _ := h.service.Get(context.Background(), h.userID, entity.ID)

	_, stop, err := h.service.Stream(context.Background(), h.userID, entity.ID)
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}
	defer stop()

	// Twenty bar-times at the harness's 2ms tick: long enough that a driver ignoring pause would
	// have released bars by now.
	time.Sleep(40 * time.Millisecond)

	current, _ := h.service.Get(context.Background(), h.userID, entity.ID)
	if current.RevealedIndex != paused.RevealedIndex {
		t.Errorf("paused session advanced from %d to %d", paused.RevealedIndex, current.RevealedIndex)
	}
}

// The lease is what stops two replicas double-stepping one session. A replica that does not hold
// it must sit still, not fall back to driving anyway.
func TestAReplicaWithoutTheLeaseDoesNotDrive(t *testing.T) {
	h := newStreamHarness(t)
	entity := h.start(t)
	h.lease.mu.Lock()
	h.lease.refuse = true
	h.lease.mu.Unlock()

	before, _ := h.service.Get(context.Background(), h.userID, entity.ID)
	_, stop, err := h.service.Stream(context.Background(), h.userID, entity.ID)
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}
	defer stop()
	time.Sleep(40 * time.Millisecond)

	after, _ := h.service.Get(context.Background(), h.userID, entity.ID)
	if after.RevealedIndex != before.RevealedIndex {
		t.Errorf("a replica without the lease advanced the session from %d to %d",
			before.RevealedIndex, after.RevealedIndex)
	}
}

// A session nobody is watching should not burn through its feed, and the lease should go back so
// the next replica does not wait out a TTL.
func TestTheLastSocketLeavingStopsTheClock(t *testing.T) {
	h := newStreamHarness(t)
	entity := h.start(t)

	_, stop, err := h.service.Stream(context.Background(), h.userID, entity.ID)
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}
	waitFor(t, "the clock to release a bar", func() bool { return h.bus.count() > 0 })
	stop()

	waitFor(t, "the driver to release its lease", func() bool {
		h.lease.mu.Lock()
		defer h.lease.mu.Unlock()
		return h.lease.released > 0
	})

	settled, _ := h.service.Get(context.Background(), h.userID, entity.ID)
	time.Sleep(40 * time.Millisecond)
	after, _ := h.service.Get(context.Background(), h.userID, entity.ID)
	if after.RevealedIndex != settled.RevealedIndex {
		t.Errorf("the clock kept running after the last socket left: %d then %d",
			settled.RevealedIndex, after.RevealedIndex)
	}
}

// Two sockets on one session share one clock. Two clocks would release two bars per tick, which
// is the same hindsight failure as two replicas driving.
func TestASecondSocketJoinsTheRunningClockRatherThanStartingASecond(t *testing.T) {
	h := newStreamHarness(t)
	entity := h.start(t)

	_, stopA, err := h.service.Stream(context.Background(), h.userID, entity.ID)
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}
	defer stopA()
	_, stopB, err := h.service.Stream(context.Background(), h.userID, entity.ID)
	if err != nil {
		t.Fatalf("second Stream() error = %v", err)
	}
	defer stopB()

	if got := h.driverCount(); got != 1 {
		t.Errorf("driver count = %d with two sockets on one session, want 1", got)
	}
}

// Rewinding must never carry a bar. A rewind moves where the trader is looking and releases
// nothing, so a bar frame there would be the stream quietly re-revealing what the cursor just left.
func TestRewindPublishesStateAndNeverABar(t *testing.T) {
	h := newStreamHarness(t)
	entity := h.start(t)
	ctx := context.Background()

	if _, err := h.service.Step(ctx, h.userID, entity.ID, 5); err != nil {
		t.Fatalf("Step(+5) error = %v", err)
	}
	before := h.bus.count()
	if _, err := h.service.Step(ctx, h.userID, entity.ID, -3); err != nil {
		t.Fatalf("Step(-3) error = %v", err)
	}

	kinds := h.bus.kinds()[before:]
	if len(kinds) != 1 {
		t.Fatalf("a rewind published %d frames, want 1: %v", len(kinds), kinds)
	}
	if kinds[0] != domainsession.FrameState {
		t.Errorf("a rewind published a %s frame, want %s", kinds[0], domainsession.FrameState)
	}
}

// A trader pressing step and the playback clock must reach other screens the same way, or a second
// viewer of the same session drifts out of sync with the one being driven.
func TestAManualStepPublishesABarFrameToOtherSockets(t *testing.T) {
	h := newStreamHarness(t)
	entity := h.start(t)
	ctx := context.Background()

	frames, stop, err := h.service.Stream(ctx, h.userID, entity.ID)
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}
	// The clock would produce bar frames of its own, so take the lease away and leave the manual
	// step as the only thing that can publish one.
	h.lease.mu.Lock()
	h.lease.refuse = true
	h.lease.mu.Unlock()
	defer stop()
	drain(frames)

	if _, err := h.service.Step(ctx, h.userID, entity.ID, 1); err != nil {
		t.Fatalf("Step() error = %v", err)
	}
	select {
	case frame := <-frames:
		if frame.Kind != domainsession.FrameBar {
			t.Errorf("manual step published %s, want %s", frame.Kind, domainsession.FrameBar)
		}
		if frame.Bar == nil {
			t.Error("manual step published a bar frame with no bar")
		}
	case <-time.After(time.Second):
		t.Fatal("a manual step reached no socket")
	}
}

// The sync frame answers "where am I?" from the session, not from the client. A client that could
// move the cursor by asserting a position could walk itself forward through the feed (BR-02).
func TestSnapshotReportsTheServersCursor(t *testing.T) {
	h := newStreamHarness(t)
	entity := h.start(t)
	ctx := context.Background()
	if _, err := h.service.Step(ctx, h.userID, entity.ID, 4); err != nil {
		t.Fatalf("Step() error = %v", err)
	}

	frame, err := h.service.Snapshot(ctx, h.userID, entity.ID)
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if frame.Kind != domainsession.FrameSync {
		t.Errorf("Snapshot kind = %s, want %s", frame.Kind, domainsession.FrameSync)
	}
	if frame.RevealedIndex != entity.RevealedIndex+4 {
		t.Errorf("Snapshot revealed index = %d, want %d", frame.RevealedIndex, entity.RevealedIndex+4)
	}
	if frame.TotalBars != h.feeds.feed.TotalBars {
		t.Errorf("Snapshot total bars = %d, want %d", frame.TotalBars, h.feeds.feed.TotalBars)
	}
}

// A ticket is a bearer credential for a few seconds. Spending it twice would make a ticket that
// reaches a proxy log a reusable key to somebody's session.
func TestAStreamTicketIsSpentOnFirstUse(t *testing.T) {
	h := newStreamHarness(t)
	entity := h.start(t)
	ctx := context.Background()

	token, ttl, err := h.service.IssueStreamTicket(ctx, h.userID, entity.ID)
	if err != nil {
		t.Fatalf("IssueStreamTicket() error = %v", err)
	}
	if ttl <= 0 {
		t.Errorf("ticket TTL = %v, want a positive lifetime", ttl)
	}
	first, err := h.service.RedeemStreamTicket(ctx, token)
	if err != nil {
		t.Fatalf("first redemption error = %v", err)
	}
	if first.SessionID != entity.ID || first.UserID != h.userID {
		t.Errorf("ticket redeemed to the wrong owner: %+v", first)
	}
	if _, err := h.service.RedeemStreamTicket(ctx, token); !apperror.Is(err, "STREAM_TICKET_INVALID") {
		t.Errorf("second redemption error = %v, want STREAM_TICKET_INVALID", err)
	}
}

// Streaming somebody else's session must fail at the ticket counter, before a socket exists.
func TestAnotherTradersSessionCannotBeStreamed(t *testing.T) {
	h := newStreamHarness(t)
	entity := h.start(t)
	stranger := uuid.New()

	if _, _, err := h.service.IssueStreamTicket(context.Background(), stranger, entity.ID); !apperror.Is(err, "SESSION_NOT_FOUND") {
		t.Errorf("IssueStreamTicket for a stranger error = %v, want SESSION_NOT_FOUND", err)
	}
	if _, _, err := h.service.Stream(context.Background(), stranger, entity.ID); !apperror.Is(err, "SESSION_NOT_FOUND") {
		t.Errorf("Stream for a stranger error = %v, want SESSION_NOT_FOUND", err)
	}
}

// The frame carries the tail bar of the *current* timeframe, which on a higher timeframe is the
// forming bucket. A client that replaces its last bar by index then gets the forming bar animating
// and the closed bar landing from one rule.
func TestFramesCarryTheViewingTimeframesTailBar(t *testing.T) {
	h := newStreamHarness(t)
	h.feeds.viewBars = func(timeframe market.Timeframe, upto int) []domainfeed.Bar {
		return []domainfeed.Bar{{Index: 6, Forming: true}, {Index: 7, Forming: true}}
	}
	entity := h.start(t)
	ctx := context.Background()

	if _, err := h.service.SetTimeframe(ctx, h.userID, entity.ID, market.TF1h); err != nil {
		t.Fatalf("SetTimeframe() error = %v", err)
	}
	frame, err := h.service.Snapshot(ctx, h.userID, entity.ID)
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if frame.Timeframe != string(market.TF1h) {
		t.Errorf("frame timeframe = %q, want %q", frame.Timeframe, market.TF1h)
	}
	if frame.Bar == nil || frame.Bar.Index != 7 || !frame.Bar.Forming {
		t.Errorf("frame bar = %+v, want the forming tail bar at index 7", frame.Bar)
	}
}

func drain[T any](channel <-chan T) {
	for {
		select {
		case <-channel:
		default:
			return
		}
	}
}
