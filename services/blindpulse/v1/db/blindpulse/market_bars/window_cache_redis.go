package market_bars

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jblabs/blindpulse-be/pkg/cache"
	"github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/market"
)

// RedisWindowCache holds a feed's bar window between reads.
//
// A feed's window is immutable once the feed is built — the instrument, timeframe and both window
// bounds are fixed on the row — so this is a cache with no invalidation question. The feed id is
// the whole key, and the TTL is the whole eviction policy.
//
// It exists because the streaming path fetched the entire window from PostgreSQL for every bar it
// released: at 10x with a 250ms tick, roughly forty full-window reads a second per session.
type RedisWindowCache struct {
	client *cache.Client
	ttl    time.Duration
}

func NewRedisWindowCache(client *cache.Client, ttl time.Duration) *RedisWindowCache {
	if ttl <= 0 {
		ttl = time.Hour
	}
	return &RedisWindowCache{client: client, ttl: ttl}
}

func (c *RedisWindowCache) key(feedID uuid.UUID) string {
	return c.client.Key("feed", feedID.String(), "window")
}

func (c *RedisWindowCache) Get(ctx context.Context, feedID uuid.UUID) ([]market.Bar, bool) {
	if !c.client.Enabled() {
		return nil, false
	}
	var bars []market.Bar
	if err := c.client.GetJSON(ctx, c.key(feedID), &bars); err != nil {
		// A miss and a broken cache are the same thing to the caller: read PostgreSQL.
		return nil, false
	}
	// A cached empty window would be indistinguishable from a miss on the next read anyway, and
	// treating it as a hit would pin an empty result for the whole TTL if it were ever written.
	if len(bars) == 0 {
		return nil, false
	}
	return bars, true
}

func (c *RedisWindowCache) Put(ctx context.Context, feedID uuid.UUID, bars []market.Bar) {
	if !c.client.Enabled() || len(bars) == 0 {
		return
	}
	if err := c.client.SetJSON(ctx, c.key(feedID), bars, c.ttl); err != nil {
		// Failing to cache costs a round trip next time and nothing else, so it must never fail
		// the read that produced the data.
		slog.DebugContext(ctx, "bar window not cached", "feed_id", feedID, "error", err)
	}
}
