package middleware

import (
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

// RateLimit rejects a request before the handler runs and gives clients an actionable Retry-After.
func RateLimit(limiter *RateLimiter, key func(*gin.Context) string) gin.HandlerFunc {
	return func(c *gin.Context) {
		allowed, retryAfter := limiter.allow(key(c))
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
