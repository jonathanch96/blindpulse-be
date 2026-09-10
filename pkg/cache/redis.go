// Package cache holds the replay hot path's shared state. Everything here is a cache or a lease:
// losing Redis must never lose a trade, only speed. Durable truth lives in PostgreSQL, and every
// helper in this file is written so a nil client (Redis not configured) is a valid, working
// no-op rather than a startup failure.
package cache

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// ErrMiss is returned by GetJSON when the key is absent, so callers can tell "nothing cached"
// apart from "the cache is broken" and fall back to PostgreSQL only in the first case.
var ErrMiss = errors.New("cache miss")

type Config struct {
	Addr        string
	Password    string
	DB          int
	Namespace   string
	DialTimeout time.Duration
	ReadTimeout time.Duration
	PoolSize    int
}

type Client struct {
	rdb       *redis.Client
	namespace string
}

// New dials Redis and verifies the connection. A blank Addr returns (nil, nil): the caller runs
// without a shared cache, which is supported for single-instance local development.
func New(ctx context.Context, cfg Config) (*Client, error) {
	if cfg.Addr == "" {
		return nil, nil
	}
	rdb := redis.NewClient(&redis.Options{
		Addr:         cfg.Addr,
		Password:     cfg.Password,
		DB:           cfg.DB,
		DialTimeout:  cfg.DialTimeout,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.ReadTimeout,
		PoolSize:     cfg.PoolSize,
	})
	if err := rdb.Ping(ctx).Err(); err != nil {
		_ = rdb.Close()
		return nil, fmt.Errorf("connect redis: %w", err)
	}
	return &Client{rdb: rdb, namespace: cfg.Namespace}, nil
}

func (c *Client) Enabled() bool { return c != nil && c.rdb != nil }

func (c *Client) Close() error {
	if !c.Enabled() {
		return nil
	}
	return c.rdb.Close()
}

func (c *Client) Ping(ctx context.Context) error {
	if !c.Enabled() {
		return nil
	}
	return c.rdb.Ping(ctx).Err()
}

// Key builds a namespaced key. Callers pass logical parts ("session", id, "state") rather than a
// pre-joined string so the namespace can never be forgotten on one call site.
func (c *Client) Key(parts ...string) string {
	key := c.namespace
	for _, part := range parts {
		key += ":" + part
	}
	return key
}

func (c *Client) GetJSON(ctx context.Context, key string, target any) error {
	if !c.Enabled() {
		return ErrMiss
	}
	raw, err := c.rdb.Get(ctx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		return ErrMiss
	}
	if err != nil {
		return fmt.Errorf("cache get %s: %w", key, err)
	}
	return json.Unmarshal(raw, target)
}

func (c *Client) SetJSON(ctx context.Context, key string, value any, ttl time.Duration) error {
	if !c.Enabled() {
		return nil
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("cache marshal %s: %w", key, err)
	}
	return c.rdb.Set(ctx, key, raw, ttl).Err()
}

func (c *Client) Delete(ctx context.Context, keys ...string) error {
	if !c.Enabled() || len(keys) == 0 {
		return nil
	}
	return c.rdb.Del(ctx, keys...).Err()
}

// Reserve claims a key for the given TTL and reports whether this caller won it. It backs order
// idempotency: the same client retry carries the same key, and only the first attempt executes.
func (c *Client) Reserve(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	if !c.Enabled() {
		return true, nil
	}
	won, err := c.rdb.SetNX(ctx, key, "1", ttl).Result()
	if err != nil {
		return false, fmt.Errorf("cache reserve %s: %w", key, err)
	}
	return won, nil
}

// Take reads a key and deletes it in the same round trip, reporting ErrMiss if it was not there.
// It is what makes a single-use token single-use: two concurrent redemptions of the same token
// cannot both succeed, which a read-then-delete pair could not promise.
func (c *Client) Take(ctx context.Context, key string, target any) error {
	if !c.Enabled() {
		return ErrMiss
	}
	raw, err := c.rdb.GetDel(ctx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		return ErrMiss
	}
	if err != nil {
		return fmt.Errorf("cache take %s: %w", key, err)
	}
	return json.Unmarshal(raw, target)
}

// holdScript takes or renews a lease. Renewal has to be conditional on still being the holder:
// an unconditional SET would let a replica that lost its lease during a pause quietly take it
// back while another replica is already driving the session.
var holdScript = redis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] or redis.call("EXISTS", KEYS[1]) == 0 then
  redis.call("SET", KEYS[1], ARGV[1], "PX", ARGV[2])
  return 1
end
return 0
`)

// releaseScript deletes the lease only if we still hold it, so a slow release cannot stomp the
// lease its successor has already taken.
var releaseScript = redis.NewScript(`
if redis.call("GET", KEYS[1]) == ARGV[1] then
  return redis.call("DEL", KEYS[1])
end
return 0
`)

// Hold takes or renews a named lease and reports whether this holder owns it. With Redis
// disabled it always succeeds: a single instance is trivially the only candidate.
func (c *Client) Hold(ctx context.Context, key, holder string, ttl time.Duration) (bool, error) {
	if !c.Enabled() {
		return true, nil
	}
	result, err := holdScript.Run(ctx, c.rdb, []string{key}, holder, ttl.Milliseconds()).Int()
	if err != nil {
		return false, fmt.Errorf("cache hold %s: %w", key, err)
	}
	return result == 1, nil
}

// ReleaseHold gives a lease up early, so a successor does not have to wait out the TTL.
func (c *Client) ReleaseHold(ctx context.Context, key, holder string) error {
	if !c.Enabled() {
		return nil
	}
	return releaseScript.Run(ctx, c.rdb, []string{key}, holder).Err()
}

// Incr counts an event inside a fixed window and returns the running total plus the time left in
// the window. It is the primitive behind the distributed rate limiter.
func (c *Client) Incr(ctx context.Context, key string, window time.Duration) (int64, time.Duration, error) {
	if !c.Enabled() {
		return 0, 0, nil
	}
	pipe := c.rdb.TxPipeline()
	count := pipe.Incr(ctx, key)
	pipe.ExpireNX(ctx, key, window)
	ttl := pipe.TTL(ctx, key)
	if _, err := pipe.Exec(ctx); err != nil {
		return 0, 0, fmt.Errorf("cache incr %s: %w", key, err)
	}
	remaining := ttl.Val()
	if remaining < 0 {
		remaining = window
	}
	return count.Val(), remaining, nil
}

// Publish fans a replay frame out to every API instance holding a websocket for the session.
func (c *Client) Publish(ctx context.Context, channel string, payload any) error {
	if !c.Enabled() {
		return nil
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("cache publish marshal %s: %w", channel, err)
	}
	return c.rdb.Publish(ctx, channel, raw).Err()
}

// Subscribe returns a channel of raw payloads and a function that closes the subscription. The
// go-redis handle stays inside this package: callers get a plain string channel, so the pub/sub
// library is not something the rest of the codebase has to know about or mock.
//
// The returned channel is closed when the subscription is closed or ctx is cancelled. With Redis
// disabled it is nil, which selects forever — a caller with a local fallback should check
// Enabled() first rather than subscribing and waiting for nothing.
func (c *Client) Subscribe(ctx context.Context, channel string) (<-chan string, func()) {
	if !c.Enabled() {
		return nil, func() {}
	}
	pubsub := c.rdb.Subscribe(ctx, channel)
	out := make(chan string, 64)
	go func() {
		defer close(out)
		defer func() { _ = pubsub.Close() }()
		incoming := pubsub.Channel()
		for {
			select {
			case <-ctx.Done():
				return
			case message, ok := <-incoming:
				if !ok {
					return
				}
				select {
				case out <- message.Payload:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out, func() { _ = pubsub.Close() }
}
