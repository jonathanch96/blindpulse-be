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

// RateLimiter is a small in-memory token-bucket limiter for the v1 API. Redis replaces this in
// Sprint 6; keeping the key function outside the limiter lets one instance count users and another
// count trips without coupling this package to either domain.
type RateLimiter struct {
	mu     sync.Mutex
	limit  int
	window time.Duration
	now    func() time.Time
	items  map[string]tokenBucket
}

type tokenBucket struct {
	updated time.Time
	tokens  float64
}

func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	return &RateLimiter{limit: limit, window: window, now: time.Now, items: make(map[string]tokenBucket)}
}

func (l *RateLimiter) allow(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
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
