package session

import (
	"context"
	"testing"
	"time"

	"github.com/jblabs/blindpulse-be/pkg/apperror"
	domainsession "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/session"
	"github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/event"
)

// The sweeper exists because of review finding BE-03-1, and the finding was not "a background job
// is missing". It was that `replay_sessions_single_open` makes an abandoned session a **lockout**:
// a trader who closes the browser mid-replay has an account that cannot start another session.
//
// So the test that matters is not "does it flip a status" but "can the trader start again".

func newSweepHarness(t *testing.T, now *time.Time) *harness {
	t.Helper()
	base := newHarness(t, true)
	base.service = NewService(Dependencies{
		Repo: base.repo, State: base.state, Feeds: base.feeds, Accounts: accountReaderStub{},
		Outbox: base.outbox,
		Seeds:  func() int64 { return 4242 },
		Clock:  func() time.Time { return *now },
	})
	return base
}

func TestSweepingAnIdleSessionReleasesTheAccount(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	h := newSweepHarness(t, &now)
	ctx := context.Background()

	first := h.start(t)

	// The trader closes the browser. Nothing marks the session; it simply stops being touched.
	// Before the sweeper existed, this account was finished.
	if _, err := h.service.Start(ctx, h.userID, StartInput{AccountID: h.account, FeedID: h.feeds.feed.ID}); !apperror.Is(err, "SESSION_ALREADY_OPEN") {
		t.Fatalf("a second session while one is live = %v, want SESSION_ALREADY_OPEN", err)
	}

	now = now.Add(45 * time.Minute)
	swept, err := h.service.SweepIdle(ctx, 30*time.Minute, 100)
	if err != nil {
		t.Fatalf("SweepIdle() error = %v", err)
	}
	if swept != 1 {
		t.Fatalf("swept %d sessions, want 1", swept)
	}

	// The point of the whole exercise.
	second, err := h.service.Start(ctx, h.userID, StartInput{AccountID: h.account, FeedID: h.feeds.feed.ID})
	if err != nil {
		t.Fatalf("the account is still locked out after the sweep: %v", err)
	}
	if second.ID == first.ID {
		t.Error("Start returned the abandoned session rather than a new one")
	}
}

func TestSweepLeavesAnActiveSessionAlone(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	h := newSweepHarness(t, &now)
	ctx := context.Background()
	entity := h.start(t)

	// Ten minutes on a thirty-minute timeout: this trader is reading the chart, not gone.
	now = now.Add(10 * time.Minute)
	swept, err := h.service.SweepIdle(ctx, 30*time.Minute, 100)
	if err != nil {
		t.Fatalf("SweepIdle() error = %v", err)
	}
	if swept != 0 {
		t.Errorf("swept %d sessions, want 0 — a session in use was abandoned", swept)
	}
	current, err := h.service.Get(ctx, h.userID, entity.ID)
	if err != nil || current.Status != domainsession.StatusOpen {
		t.Errorf("session status = %v (err %v), want it still open", current.Status, err)
	}
}

// A paused session is still live for the purposes of the single-open constraint, so it can lock an
// account out exactly as an open one can — and being paused is not evidence of being present.
func TestSweepAbandonsAPausedSessionToo(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	h := newSweepHarness(t, &now)
	ctx := context.Background()
	entity := h.start(t)
	if _, err := h.service.Pause(ctx, h.userID, entity.ID); err != nil {
		t.Fatalf("Pause() error = %v", err)
	}

	now = now.Add(45 * time.Minute)
	swept, err := h.service.SweepIdle(ctx, 30*time.Minute, 100)
	if err != nil {
		t.Fatalf("SweepIdle() error = %v", err)
	}
	if swept != 1 {
		t.Errorf("swept %d, want 1 — a paused session locks the account just as an open one does", swept)
	}
}

// Closing is a decision the trader made; abandoning is one made for them. A closed session must not
// be swept, because it would emit an abandonment event for a session that ended properly and would
// muddy the post-mortem the reveal is built on.
func TestSweepIgnoresAClosedSession(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	h := newSweepHarness(t, &now)
	ctx := context.Background()
	entity := h.start(t)
	if _, err := h.service.Close(ctx, h.userID, entity.ID); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	now = now.Add(10 * time.Hour)
	swept, err := h.service.SweepIdle(ctx, 30*time.Minute, 100)
	if err != nil {
		t.Fatalf("SweepIdle() error = %v", err)
	}
	if swept != 0 {
		t.Errorf("swept %d, want 0 — a closed session was re-ended as abandoned", swept)
	}
}

func TestSweepEmitsAnAbandonedEventPerSession(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	h := newSweepHarness(t, &now)
	ctx := context.Background()
	h.start(t)

	now = now.Add(45 * time.Minute)
	if _, err := h.service.SweepIdle(ctx, 30*time.Minute, 100); err != nil {
		t.Fatalf("SweepIdle() error = %v", err)
	}

	h.outbox.mu.Lock()
	defer h.outbox.mu.Unlock()
	var abandoned int
	for _, record := range h.outbox.events {
		if record.EventType == event.TypeSessionAbandoned {
			abandoned++
		}
	}
	if abandoned != 1 {
		t.Errorf("%d abandonment events, want 1", abandoned)
	}
}

// The sweep is idempotent: running it twice must not abandon the same session twice, or the event
// stream gains a duplicate for every tick after the first.
func TestSweepingTwiceAbandonsNothingTheSecondTime(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	h := newSweepHarness(t, &now)
	ctx := context.Background()
	h.start(t)
	now = now.Add(45 * time.Minute)

	if swept, _ := h.service.SweepIdle(ctx, 30*time.Minute, 100); swept != 1 {
		t.Fatalf("first sweep = %d, want 1", swept)
	}
	if swept, _ := h.service.SweepIdle(ctx, 30*time.Minute, 100); swept != 0 {
		t.Errorf("second sweep = %d, want 0", swept)
	}
}

func TestSweepRefusesANonPositiveTimeout(t *testing.T) {
	t.Parallel()
	now := time.Now()
	h := newSweepHarness(t, &now)
	// A zero timeout would abandon every live session on the first tick, which is worse than not
	// sweeping at all.
	if _, err := h.service.SweepIdle(context.Background(), 0, 100); err == nil {
		t.Error("a zero idle timeout was accepted")
	}
}
