package replay_sessions

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jblabs/blindpulse-be/pkg/apperror"
	"github.com/jblabs/blindpulse-be/pkg/cache"
	domainsession "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/session"
)

// RedisDriverLease elects one replica to advance a session's cursor.
//
// This is the one place in the replay path where Redis is not merely a cache. Two replicas driving
// the same session would release bars nobody watched — not a display glitch but a hindsight leak,
// since those bars are then readable. The lease is short and renewed each tick, so a replica that
// dies hands the session on within a TTL instead of freezing it.
type RedisDriverLease struct{ client *cache.Client }

func NewRedisDriverLease(client *cache.Client) *RedisDriverLease {
	return &RedisDriverLease{client: client}
}

func (l *RedisDriverLease) key(sessionID uuid.UUID) string {
	return l.client.Key("session", sessionID.String(), "driver")
}

func (l *RedisDriverLease) Acquire(ctx context.Context, sessionID uuid.UUID, holder string, ttl time.Duration) (bool, error) {
	return l.client.Hold(ctx, l.key(sessionID), holder, ttl)
}

func (l *RedisDriverLease) Release(ctx context.Context, sessionID uuid.UUID, holder string) error {
	return l.client.ReleaseHold(ctx, l.key(sessionID), holder)
}

// RedisTicketStore issues the short-lived, single-use tokens that authenticate a websocket
// handshake. Redemption is a GETDEL, so a token that reaches a proxy log is already spent.
type RedisTicketStore struct{ client *cache.Client }

func NewRedisTicketStore(client *cache.Client) *RedisTicketStore {
	return &RedisTicketStore{client: client}
}

func (s *RedisTicketStore) key(token string) string { return s.client.Key("stream-ticket", token) }

type ticketRecord struct {
	SessionID uuid.UUID `json:"session_id"`
	UserID    uuid.UUID `json:"user_id"`
}

func (s *RedisTicketStore) Issue(ctx context.Context, ticket domainsession.StreamTicket, ttl time.Duration) (string, error) {
	token, err := newToken()
	if err != nil {
		return "", err
	}
	record := ticketRecord{SessionID: ticket.SessionID, UserID: ticket.UserID}
	if err := s.client.SetJSON(ctx, s.key(token), record, ttl); err != nil {
		return "", err
	}
	return token, nil
}

func (s *RedisTicketStore) Redeem(ctx context.Context, token string) (*domainsession.StreamTicket, error) {
	var record ticketRecord
	if err := s.client.Take(ctx, s.key(token), &record); err != nil {
		if errors.Is(err, cache.ErrMiss) {
			return nil, apperror.New("STREAM_TICKET_INVALID")
		}
		return nil, err
	}
	return &domainsession.StreamTicket{SessionID: record.SessionID, UserID: record.UserID}, nil
}

// MemoryTicketStore is the single-instance equivalent. A ticket issued on one process is only
// redeemable on that process, which is exactly right when there is only one.
type MemoryTicketStore struct {
	mu      sync.Mutex
	tickets map[string]memoryTicket
	now     func() time.Time
}

type memoryTicket struct {
	ticket    domainsession.StreamTicket
	expiresAt time.Time
}

func NewMemoryTicketStore() *MemoryTicketStore {
	return &MemoryTicketStore{tickets: make(map[string]memoryTicket), now: func() time.Time { return time.Now().UTC() }}
}

func (s *MemoryTicketStore) Issue(_ context.Context, ticket domainsession.StreamTicket, ttl time.Duration) (string, error) {
	token, err := newToken()
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	// Sweep on write. Without a TTL to expire them, unredeemed tickets would accumulate for the
	// life of the process, and nothing else here ever visits the map.
	for existing, record := range s.tickets {
		if record.expiresAt.Before(now) {
			delete(s.tickets, existing)
		}
	}
	s.tickets[token] = memoryTicket{ticket: ticket, expiresAt: now.Add(ttl)}
	return token, nil
}

func (s *MemoryTicketStore) Redeem(_ context.Context, token string) (*domainsession.StreamTicket, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, ok := s.tickets[token]
	// Deleted whether or not it was still valid: redemption is the only thing a token is for, and
	// an expired one has no second life.
	delete(s.tickets, token)
	if !ok || record.expiresAt.Before(s.now()) {
		return nil, apperror.New("STREAM_TICKET_INVALID")
	}
	ticket := record.ticket
	return &ticket, nil
}

// MemoryDriverLease is the single-instance lease: this process is the only candidate, so it always
// wins. It exists to keep the domain's dependency non-nil rather than to coordinate anything.
type MemoryDriverLease struct{}

func NewMemoryDriverLease() *MemoryDriverLease { return &MemoryDriverLease{} }

func (MemoryDriverLease) Acquire(context.Context, uuid.UUID, string, time.Duration) (bool, error) {
	return true, nil
}

func (MemoryDriverLease) Release(context.Context, uuid.UUID, string) error { return nil }

// newToken draws 32 bytes of CSPRNG entropy. The token is a bearer credential for the length of
// its TTL, so it is drawn the way a credential is drawn, not from a UUID.
func newToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
