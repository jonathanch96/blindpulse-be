package middleware

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jblabs/blindpulse-be/pkg/apperror"
	"github.com/jblabs/blindpulse-be/pkg/response"
)

// RateLimiter is a small in-memory token-bucket limiter for the v1 API. Keeping the key function
// outside the limiter lets one instance count IP addresses and another count email addresses
// without coupling this package to either.
//
// It is per-process, so on N replicas the effective limit is N × the configured one. That is the
// right trade for the front door — a limit that is 3× too generous still turns unlimited credential
// stuffing into a bounded cost — but it is a ceiling, not a guarantee. `pkg/cache.Incr` exists to
// make it distributed and has no caller yet (review finding BE-01-2).
type RateLimiter struct {
	mu     sync.Mutex
	limit  int
	window time.Duration
	now    func() time.Time
	items  map[string]tokenBucket
	// lastSweep bounds the map. Without it, a limiter keyed by client IP grows one entry per
	// attacking address — so the memory cost of being attacked would scale with the attack, which
	// is the opposite of what a rate limiter is for.
	lastSweep time.Time
}

type tokenBucket struct {
	updated time.Time
	tokens  float64
}

func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	return &RateLimiter{limit: limit, window: window, now: time.Now, items: make(map[string]tokenBucket)}
}

// sweepEvery is how often idle buckets are evicted. A bucket is droppable once it has refilled to
// full, because a full bucket and no bucket are the same thing to the next caller.
const sweepEvery = time.Minute

// sweep must be called with the lock held.
func (l *RateLimiter) sweep(now time.Time) {
	if now.Sub(l.lastSweep) < sweepEvery {
		return
	}
	l.lastSweep = now
	for key, bucket := range l.items {
		if now.Sub(bucket.updated) >= l.window {
			delete(l.items, key)
		}
	}
}

func (l *RateLimiter) allow(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.sweep(now)
	bucket, exists := l.items[key]
	if !exists {
		l.items[key] = tokenBucket{updated: now, tokens: float64(l.limit - 1)}
		return true, 0
	}
	rate := float64(l.limit) / float64(l.window)
	bucket.tokens = min(float64(l.limit), bucket.tokens+float64(now.Sub(bucket.updated))*rate)
	bucket.updated = now
	if bucket.tokens < 1 {
		l.items[key] = bucket
		return false, time.Duration((1 - bucket.tokens) / rate)
	}
	bucket.tokens--
	l.items[key] = bucket
	return true, 0
}

// Limiter is what the middleware needs: a decision and, when refused, how long to wait.
//
// Two implementations. The in-memory one above is per-process, so on N replicas the effective limit
// is N × the configured one. The Redis one below counts across every replica, which is what the
// front door actually needs — and it is why `cache.Incr` exists. That primitive was written as
// "the primitive behind the distributed rate limiter" and then had no caller at all (review
// finding BE-01-2).
type Limiter interface {
	// Allow reports whether this key may proceed, and if not, how long until it may.
	Allow(ctx context.Context, key string) (bool, time.Duration)
}

// Allow satisfies Limiter for the in-memory bucket. The context is unused: nothing here can block.
func (l *RateLimiter) Allow(_ context.Context, key string) (bool, time.Duration) {
	return l.allow(key)
}

// counter is the slice of pkg/cache the distributed limiter needs, named here so this package does
// not depend on the whole cache client.
type counter interface {
	Incr(ctx context.Context, key string, window time.Duration) (int64, time.Duration, error)
	Enabled() bool
}

// DistributedRateLimiter counts a fixed window in Redis, so the limit is the limit no matter how
// many API replicas are running.
//
// A fixed window rather than a token bucket: it is one round trip, it is what `cache.Incr` gives,
// and its known weakness — up to 2× the limit across a window boundary — is irrelevant for a
// credential-stuffing budget measured in tens of attempts per quarter hour.
type DistributedRateLimiter struct {
	cache    counter
	fallback *RateLimiter
	limit    int64
	window   time.Duration
	prefix   string
}

func NewDistributedRateLimiter(client counter, prefix string, limit int, window time.Duration) *DistributedRateLimiter {
	return &DistributedRateLimiter{
		cache:    client,
		fallback: NewRateLimiter(limit, window),
		limit:    int64(limit),
		window:   window,
		prefix:   prefix,
	}
}

func (l *DistributedRateLimiter) Allow(ctx context.Context, key string) (bool, time.Duration) {
	// Without Redis this degrades to the in-process limiter rather than to no limiting at all —
	// the same choice the replay frame bus makes. A single instance is exactly the case where
	// per-process counting is correct anyway.
	if l.cache == nil || !l.cache.Enabled() {
		return l.fallback.Allow(ctx, key)
	}
	count, remaining, err := l.cache.Incr(ctx, l.prefix+":"+key, l.window)
	if err != nil {
		// A broken cache must not open the front door. Falling back to the in-process budget keeps
		// a bound in place; failing closed entirely would turn a Redis blip into an outage of the
		// login endpoint.
		return l.fallback.Allow(ctx, key)
	}
	if count > l.limit {
		if remaining <= 0 {
			remaining = l.window
		}
		return false, remaining
	}
	return true, 0
}

// RateLimit rejects a request before the handler runs and gives clients an actionable Retry-After.
func RateLimit(limiter Limiter, key func(*gin.Context) string) gin.HandlerFunc {
	return func(c *gin.Context) {
		allowed, retryAfter := limiter.Allow(c.Request.Context(), key(c))
		if allowed {
			c.Next()
			return
		}
		seconds := int(retryAfter.Round(time.Second).Seconds())
		if seconds < 1 {
			seconds = 1
		}
		c.Header("Retry-After", strconv.Itoa(seconds))
		c.Abort()
		response.Error(c, apperror.Newf("RATE_LIMITED", "rate limit exceeded; retry in %s", fmtDuration(retryAfter)))
	}
}

func fmtDuration(value time.Duration) string {
	if value >= time.Hour {
		return fmt.Sprintf("%dh", int(value.Round(time.Hour).Hours()))
	}
	if value >= time.Minute {
		return fmt.Sprintf("%dm", int(value.Round(time.Minute).Minutes()))
	}
	return fmt.Sprintf("%ds", max(1, int(value.Round(time.Second).Seconds())))
}
