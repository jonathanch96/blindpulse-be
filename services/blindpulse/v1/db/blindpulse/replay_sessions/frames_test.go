package replay_sessions

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jblabs/blindpulse-be/pkg/apperror"
	domainsession "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/session"
)

func barFrame(sessionID uuid.UUID, index int) domainsession.Frame {
	return domainsession.Frame{SessionID: sessionID, Kind: domainsession.FrameBar, RevealedIndex: index}
}

func TestMemoryBusDeliversToEverySocketOnTheSession(t *testing.T) {
	t.Parallel()
	bus := NewMemoryFrameBus()
	sessionID := uuid.New()
	first, stopFirst, _ := bus.Subscribe(context.Background(), sessionID)
	second, stopSecond, _ := bus.Subscribe(context.Background(), sessionID)
	defer stopFirst()
	defer stopSecond()
	// A socket on a different session must not see it: sessions are other people's trading.
	other, stopOther, _ := bus.Subscribe(context.Background(), uuid.New())
	defer stopOther()

	if err := bus.Publish(context.Background(), barFrame(sessionID, 42)); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	for name, channel := range map[string]<-chan domainsession.Frame{"first": first, "second": second} {
		select {
		case frame := <-channel:
			if frame.RevealedIndex != 42 {
				t.Errorf("%s socket got index %d, want 42", name, frame.RevealedIndex)
			}
		case <-time.After(time.Second):
			t.Errorf("%s socket got no frame", name)
		}
	}
	select {
	case frame := <-other:
		t.Errorf("a socket on another session received %+v", frame)
	default:
	}
}

// The backpressure policy, and the direction of it. A trader acts on what is on screen, so when a
// socket falls behind the newest frame is the one worth keeping. Dropping the newest to preserve a
// backlog would show them a stale market and call it live.
func TestASlowSocketLosesTheOldestFrameAndKeepsTheNewest(t *testing.T) {
	t.Parallel()
	bus := NewMemoryFrameBus()
	sessionID := uuid.New()
	frames, stop, _ := bus.Subscribe(context.Background(), sessionID)
	defer stop()

	// Publish well past the buffer without reading anything.
	total := subscriberBuffer + 50
	for index := 0; index < total; index++ {
		if err := bus.Publish(context.Background(), barFrame(sessionID, index)); err != nil {
			t.Fatalf("Publish(%d) error = %v", index, err)
		}
	}

	received := make([]int, 0, total)
	for {
		select {
		case frame := <-frames:
			received = append(received, frame.RevealedIndex)
			continue
		default:
		}
		break
	}

	if len(received) == 0 {
		t.Fatal("a slow socket received nothing at all")
	}
	if len(received) > subscriberBuffer {
		t.Errorf("buffered %d frames, want at most %d", len(received), subscriberBuffer)
	}
	newest := received[len(received)-1]
	if newest != total-1 {
		t.Errorf("newest buffered frame = %d, want %d — the drop is discarding the wrong end", newest, total-1)
	}
	if received[0] == 0 {
		t.Error("the oldest frame survived; a full buffer must discard from the front")
	}
}

// Publishing must never block on a socket nobody is draining, or one wedged client would freeze
// playback for every other viewer of the session.
func TestPublishDoesNotBlockOnAWedgedSocket(t *testing.T) {
	t.Parallel()
	bus := NewMemoryFrameBus()
	sessionID := uuid.New()
	_, stop, _ := bus.Subscribe(context.Background(), sessionID)
	defer stop()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for index := 0; index < subscriberBuffer*4; index++ {
			_ = bus.Publish(context.Background(), barFrame(sessionID, index))
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Publish blocked on a socket that is never read")
	}
}

func TestUnsubscribeClosesTheChannelAndIsIdempotent(t *testing.T) {
	t.Parallel()
	bus := NewMemoryFrameBus()
	sessionID := uuid.New()
	frames, stop, _ := bus.Subscribe(context.Background(), sessionID)

	stop()
	// Twice: the domain's cancel and a deferred close can both fire, and a double close would
	// panic the request that happened to be second.
	stop()

	select {
	case _, open := <-frames:
		if open {
			t.Error("channel still delivering after unsubscribe")
		}
	case <-time.After(time.Second):
		t.Error("channel was not closed by unsubscribe")
	}
	// A publish after the last socket left must not panic on a closed channel.
	if err := bus.Publish(context.Background(), barFrame(sessionID, 1)); err != nil {
		t.Errorf("Publish() after unsubscribe error = %v", err)
	}
}

func TestMemoryTicketIsSingleUse(t *testing.T) {
	t.Parallel()
	store := NewMemoryTicketStore()
	ticket := domainsession.StreamTicket{SessionID: uuid.New(), UserID: uuid.New()}

	token, err := store.Issue(context.Background(), ticket, time.Minute)
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	redeemed, err := store.Redeem(context.Background(), token)
	if err != nil {
		t.Fatalf("Redeem() error = %v", err)
	}
	if *redeemed != ticket {
		t.Errorf("Redeem() = %+v, want %+v", *redeemed, ticket)
	}
	if _, err := store.Redeem(context.Background(), token); !apperror.Is(err, "STREAM_TICKET_INVALID") {
		t.Errorf("second Redeem() error = %v, want STREAM_TICKET_INVALID", err)
	}
}

func TestAnExpiredTicketIsRefused(t *testing.T) {
	t.Parallel()
	store := NewMemoryTicketStore()
	now := time.Now().UTC()
	store.now = func() time.Time { return now }

	token, err := store.Issue(context.Background(), domainsession.StreamTicket{SessionID: uuid.New()}, 30*time.Second)
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	store.now = func() time.Time { return now.Add(31 * time.Second) }
	if _, err := store.Redeem(context.Background(), token); !apperror.Is(err, "STREAM_TICKET_INVALID") {
		t.Errorf("expired Redeem() error = %v, want STREAM_TICKET_INVALID", err)
	}
}

func TestAnUnknownTicketIsRefused(t *testing.T) {
	t.Parallel()
	store := NewMemoryTicketStore()
	if _, err := store.Redeem(context.Background(), "not-a-ticket"); !apperror.Is(err, "STREAM_TICKET_INVALID") {
		t.Errorf("unknown Redeem() error = %v, want STREAM_TICKET_INVALID", err)
	}
}

// Tokens are bearer credentials for their TTL, so they have to be unguessable and unique.
func TestTicketTokensAreDistinct(t *testing.T) {
	t.Parallel()
	store := NewMemoryTicketStore()
	seen := make(map[string]struct{}, 256)
	for i := 0; i < 256; i++ {
		token, err := store.Issue(context.Background(), domainsession.StreamTicket{SessionID: uuid.New()}, time.Minute)
		if err != nil {
			t.Fatalf("Issue() error = %v", err)
		}
		if len(token) < 32 {
			t.Fatalf("token %q is too short to be a credential", token)
		}
		if _, duplicate := seen[token]; duplicate {
			t.Fatalf("token %q was issued twice", token)
		}
		seen[token] = struct{}{}
	}
}
