package middleware

import (
	"context"
	"errors"
	"testing"
	"time"
)

// Review finding BE-01-2. `cache.Incr` was documented as "the primitive behind the distributed rate
// limiter" and had no caller, so the auth throttle counted per process: on N replicas the effective
// limit was N × the configured one.

type fakeCounter struct {
	enabled bool
	counts  map[string]int64
	err     error
	calls   int
}

func newFakeCounter() *fakeCounter {
	return &fakeCounter{enabled: true, counts: make(map[string]int64)}
}

func (f *fakeCounter) Enabled() bool { return f.enabled }

func (f *fakeCounter) Incr(_ context.Context, key string, window time.Duration) (int64, time.Duration, error) {
	f.calls++
	if f.err != nil {
		return 0, 0, f.err
	}
	f.counts[key]++
	return f.counts[key], window, nil
}

func TestDistributedLimiterRefusesPastTheLimit(t *testing.T) {
	t.Parallel()
	counter := newFakeCounter()
	limiter := NewDistributedRateLimiter(counter, "test", 3, time.Minute)
	ctx := context.Background()

	for attempt := 1; attempt <= 3; attempt++ {
		if allowed, _ := limiter.Allow(ctx, "ip:1.2.3.4"); !allowed {
			t.Fatalf("attempt %d was refused inside the limit", attempt)
		}
	}
	allowed, retryAfter := limiter.Allow(ctx, "ip:1.2.3.4")
	if allowed {
		t.Error("the fourth attempt was allowed past a limit of 3")
	}
	if retryAfter <= 0 {
		t.Errorf("retryAfter = %v, want the time left in the window", retryAfter)
	}
}

func TestDistributedLimiterCountsKeysSeparately(t *testing.T) {
	t.Parallel()
	limiter := NewDistributedRateLimiter(newFakeCounter(), "test", 1, time.Minute)
	ctx := context.Background()
	if allowed, _ := limiter.Allow(ctx, "ip:1.1.1.1"); !allowed {
		t.Fatal("first key refused")
	}
	if allowed, _ := limiter.Allow(ctx, "ip:2.2.2.2"); !allowed {
		t.Error("a different key was refused; budgets must be per key")
	}
}

// Without Redis this must fall back to counting in process, not to counting nothing. A single
// instance is exactly the case where per-process counting is correct anyway.
func TestDistributedLimiterFallsBackWhenRedisIsOff(t *testing.T) {
	t.Parallel()
	counter := newFakeCounter()
	counter.enabled = false
	limiter := NewDistributedRateLimiter(counter, "test", 2, time.Minute)
	ctx := context.Background()

	limiter.Allow(ctx, "ip:9.9.9.9")
	limiter.Allow(ctx, "ip:9.9.9.9")
	if allowed, _ := limiter.Allow(ctx, "ip:9.9.9.9"); allowed {
		t.Error("with Redis off the limiter stopped limiting rather than falling back")
	}
	if counter.calls != 0 {
		t.Errorf("a disabled cache was called %d times", counter.calls)
	}
}

// A broken cache must not open the front door — and must not close it either. Failing to the
// in-process budget keeps a bound in place without turning a Redis blip into a login outage.
func TestDistributedLimiterFallsBackWhenRedisErrors(t *testing.T) {
	t.Parallel()
	counter := newFakeCounter()
	counter.err = errors.New("connection refused")
	limiter := NewDistributedRateLimiter(counter, "test", 2, time.Minute)
	ctx := context.Background()

	if allowed, _ := limiter.Allow(ctx, "ip:8.8.8.8"); !allowed {
		t.Error("a Redis error closed the front door entirely")
	}
	limiter.Allow(ctx, "ip:8.8.8.8")
	if allowed, _ := limiter.Allow(ctx, "ip:8.8.8.8"); allowed {
		t.Error("a Redis error removed the limit rather than falling back to the local one")
	}
}

func TestDistributedLimiterToleratesNoCacheAtAll(t *testing.T) {
	t.Parallel()
	limiter := NewDistributedRateLimiter(nil, "test", 1, time.Minute)
	ctx := context.Background()
	if allowed, _ := limiter.Allow(ctx, "k"); !allowed {
		t.Fatal("first attempt refused")
	}
	if allowed, _ := limiter.Allow(ctx, "k"); allowed {
		t.Error("a nil cache removed the limit")
	}
}
