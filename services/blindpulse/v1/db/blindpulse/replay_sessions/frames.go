package replay_sessions

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"

	"github.com/google/uuid"
	"github.com/jblabs/blindpulse-be/pkg/cache"
	domainsession "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/session"
)

// subscriberBuffer is how many frames one socket may fall behind before frames start being
// dropped. It is generous on purpose: dropping a bar frame is recoverable — the client sees a gap
// in bar indices and backfills over HTTP — but it costs a round trip, so it should take a genuinely
// stuck consumer to trigger it, not an ordinary GC pause.
const subscriberBuffer = 256

// fanout delivers frames to the sockets on this process. Both buses share it, because "how a slow
// socket is handled" is a policy that should not have two implementations that can disagree.
//
// The policy: never block the publisher. When a subscriber's buffer is full, the *oldest* frame is
// dropped to make room for the newest. That direction matters. A trader acts on what is on screen,
// so the newest frame is the one worth keeping; dropping the newest to preserve a backlog would
// show them a stale market and call it live.
type fanout struct {
	mu      sync.Mutex
	targets map[uuid.UUID]map[chan domainsession.Frame]struct{}
}

func newFanout() *fanout {
	return &fanout{targets: make(map[uuid.UUID]map[chan domainsession.Frame]struct{})}
}

func (f *fanout) add(sessionID uuid.UUID) chan domainsession.Frame {
	f.mu.Lock()
	defer f.mu.Unlock()
	channel := make(chan domainsession.Frame, subscriberBuffer)
	if f.targets[sessionID] == nil {
		f.targets[sessionID] = make(map[chan domainsession.Frame]struct{})
	}
	f.targets[sessionID][channel] = struct{}{}
	return channel
}

// remove detaches a subscriber and closes its channel. Closing under the same lock the publisher
// takes is what makes "send on closed channel" impossible here.
func (f *fanout) remove(sessionID uuid.UUID, channel chan domainsession.Frame) (last bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	channels, ok := f.targets[sessionID]
	if !ok {
		return false
	}
	if _, ok := channels[channel]; !ok {
		return false
	}
	delete(channels, channel)
	close(channel)
	if len(channels) == 0 {
		delete(f.targets, sessionID)
		return true
	}
	return false
}

func (f *fanout) deliver(frame domainsession.Frame) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for channel := range f.targets[frame.SessionID] {
		select {
		case channel <- frame:
		default:
			// Full: discard the oldest to make room, then take the newest. If the drain loses the
			// race the send is skipped rather than blocked — a publisher stuck behind one wedged
			// socket would stall every other socket on the session.
			select {
			case <-channel:
			default:
			}
			select {
			case channel <- frame:
			default:
			}
		}
	}
}

func (f *fanout) empty(sessionID uuid.UUID) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.targets[sessionID]) == 0
}

// MemoryFrameBus is the single-instance bus: publish goes straight to the local sockets.
//
// It exists so that "no Redis" degrades to "streaming works, on one instance" rather than to
// "streaming is broken". Local development is the obvious beneficiary; so is any deployment small
// enough to run one API process.
type MemoryFrameBus struct{ out *fanout }

func NewMemoryFrameBus() *MemoryFrameBus { return &MemoryFrameBus{out: newFanout()} }

func (b *MemoryFrameBus) Publish(_ context.Context, frame domainsession.Frame) error {
	b.out.deliver(frame)
	return nil
}

func (b *MemoryFrameBus) Subscribe(_ context.Context, sessionID uuid.UUID) (<-chan domainsession.Frame, func(), error) {
	channel := b.out.add(sessionID)
	var once sync.Once
	return channel, func() { once.Do(func() { b.out.remove(sessionID, channel) }) }, nil
}

// RedisFrameBus fans frames out across API replicas over pub/sub.
//
// Without it, a session driven on replica A would be silent for a socket that reconnected onto
// replica B — which behind a load balancer is not an edge case but the expected outcome of any
// deployment or scale event.
type RedisFrameBus struct {
	client *cache.Client
	out    *fanout

	mu     sync.Mutex
	topics map[uuid.UUID]*topic
}

// topic is one Redis subscription, shared by every socket on this replica watching that session.
// One subscription per socket would multiply connections by concurrent viewers for no benefit:
// the frames are identical.
type topic struct {
	messages <-chan string
	close    func()
	cancel   context.CancelFunc
}

func NewRedisFrameBus(client *cache.Client) *RedisFrameBus {
	return &RedisFrameBus{client: client, out: newFanout(), topics: make(map[uuid.UUID]*topic)}
}

func (b *RedisFrameBus) channel(sessionID uuid.UUID) string {
	return b.client.Key("session", sessionID.String(), "frames")
}

func (b *RedisFrameBus) Publish(ctx context.Context, frame domainsession.Frame) error {
	return b.client.Publish(ctx, b.channel(frame.SessionID), frame)
}

func (b *RedisFrameBus) Subscribe(_ context.Context, sessionID uuid.UUID) (<-chan domainsession.Frame, func(), error) {
	out := b.out.add(sessionID)
	b.mu.Lock()
	if _, running := b.topics[sessionID]; !running {
		// The pump outlives the request that started it: it serves every socket on this replica,
		// not just the one that happened to be first.
		ctx, cancel := context.WithCancel(context.Background())
		messages, closeSubscription := b.client.Subscribe(ctx, b.channel(sessionID))
		b.topics[sessionID] = &topic{messages: messages, close: closeSubscription, cancel: cancel}
		go b.pump(ctx, sessionID, messages)
	}
	b.mu.Unlock()

	var once sync.Once
	return out, func() {
		once.Do(func() {
			if last := b.out.remove(sessionID, out); last {
				b.closeTopic(sessionID)
			}
		})
	}, nil
}

func (b *RedisFrameBus) closeTopic(sessionID uuid.UUID) {
	b.mu.Lock()
	defer b.mu.Unlock()
	// Re-check under the lock: a new subscriber may have arrived between the last remove and here,
	// and tearing the subscription down under it would silence a live socket.
	if !b.out.empty(sessionID) {
		return
	}
	existing, ok := b.topics[sessionID]
	if !ok {
		return
	}
	delete(b.topics, sessionID)
	existing.cancel()
	existing.close()
}

func (b *RedisFrameBus) pump(ctx context.Context, sessionID uuid.UUID, messages <-chan string) {
	for {
		select {
		case <-ctx.Done():
			return
		case message, ok := <-messages:
			if !ok {
				return
			}
			var frame domainsession.Frame
			if err := json.Unmarshal([]byte(message), &frame); err != nil {
				slog.Debug("undecodable replay frame", "session_id", sessionID, "error", err)
				continue
			}
			b.out.deliver(frame)
		}
	}
}
