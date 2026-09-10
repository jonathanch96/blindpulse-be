package replay_sessions

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jblabs/blindpulse-be/pkg/cache"
	domainsession "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/session"
)

// RedisStateStore keeps the live cursor in Redis between PostgreSQL checkpoints.
//
// Every method here is allowed to fail quietly. This is a cache, not a record: a miss means "read
// PostgreSQL", and a write failure costs at most the bars since the last checkpoint. Making a
// replay step fail because Redis hiccuped would trade a durability property the system does not
// need for an availability one it very much does.
type RedisStateStore struct {
	client *cache.Client
	ttl    time.Duration
}

func NewRedisStateStore(client *cache.Client, ttl time.Duration) *RedisStateStore {
	if ttl <= 0 {
		ttl = 12 * time.Hour
	}
	return &RedisStateStore{client: client, ttl: ttl}
}

func (s *RedisStateStore) key(sessionID uuid.UUID) string {
	return s.client.Key("session", sessionID.String(), "state")
}

func (s *RedisStateStore) Load(ctx context.Context, sessionID uuid.UUID) (*domainsession.State, bool) {
	if !s.client.Enabled() {
		return nil, false
	}
	var state domainsession.State
	if err := s.client.GetJSON(ctx, s.key(sessionID), &state); err != nil {
		// A miss and a broken cache are the same thing to the caller: fall back to PostgreSQL.
		if !errors.Is(err, cache.ErrMiss) {
			return nil, false
		}
		return nil, false
	}
	return &state, true
}

func (s *RedisStateStore) Save(ctx context.Context, state domainsession.State) error {
	if !s.client.Enabled() {
		return nil
	}
	return s.client.SetJSON(ctx, s.key(state.SessionID), state, s.ttl)
}

func (s *RedisStateStore) Clear(ctx context.Context, sessionID uuid.UUID) error {
	if !s.client.Enabled() {
		return nil
	}
	return s.client.Delete(ctx, s.key(sessionID))
}
