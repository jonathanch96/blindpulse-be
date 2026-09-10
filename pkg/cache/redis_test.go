package cache

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"
)

// Review finding BE-03-4. Hold and ReleaseHold are Lua scripts implementing the driver lease that
// stops two API replicas double-stepping one replay session — which the code correctly identifies
// as a hindsight leak rather than a display glitch, because a bar released is a bar readable.
//
// The domain's use of the lease was tested against a stub; the scripts themselves were not, and
// their correctness is entirely in a conditional ("GET == holder or not exists") that a stub cannot
// exercise. These run against a real Redis and skip when there is not one.

func testClient(t *testing.T) *Client {
	t.Helper()
	addr := os.Getenv("REDIS_ADDR")
	if addr == "" {
		addr = "localhost:6379"
	}
	ctx := context.Background()
	client, err := New(ctx, Config{Addr: addr, Namespace: "blindpulse-test", DialTimeout: time.Second, ReadTimeout: time.Second, PoolSize: 4})
	if err != nil || client == nil || !client.Enabled() {
		t.Skipf("no Redis at %s; skipping the script tests", addr)
	}
	if err := client.Ping(ctx); err != nil {
		t.Skipf("Redis at %s is not answering: %v", addr, err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func uniqueKey(t *testing.T, client *Client) string {
	t.Helper()
	key := client.Key("test", t.Name(), time.Now().Format("150405.000000000"))
	t.Cleanup(func() { _ = client.Delete(context.Background(), key) })
	return key
}

func TestHoldGrantsToTheFirstCallerAndRefusesTheSecond(t *testing.T) {
	client := testClient(t)
	ctx := context.Background()
	key := uniqueKey(t, client)

	won, err := client.Hold(ctx, key, "replica-a", time.Minute)
	if err != nil || !won {
		t.Fatalf("Hold(a) = %v, %v; want the lease", won, err)
	}
	won, err = client.Hold(ctx, key, "replica-b", time.Minute)
	if err != nil {
		t.Fatalf("Hold(b) error = %v", err)
	}
	if won {
		t.Error("two replicas hold the same lease; they would double-step the session")
	}
}

// Renewal has to be conditional on still being the holder. An unconditional SET would let a replica
// that lost the lease take it back while another is already driving.
func TestHoldRenewsForTheSameHolder(t *testing.T) {
	client := testClient(t)
	ctx := context.Background()
	key := uniqueKey(t, client)

	if won, _ := client.Hold(ctx, key, "replica-a", time.Minute); !won {
		t.Fatal("initial Hold failed")
	}
	for renewal := 1; renewal <= 3; renewal++ {
		won, err := client.Hold(ctx, key, "replica-a", time.Minute)
		if err != nil || !won {
			t.Fatalf("renewal %d = %v, %v; the holder cannot renew its own lease", renewal, won, err)
		}
	}
	// And the other replica still cannot take it.
	if won, _ := client.Hold(ctx, key, "replica-b", time.Minute); won {
		t.Error("renewing let a second replica in")
	}
}

func TestHoldPassesToTheNextReplicaAfterTheTtl(t *testing.T) {
	client := testClient(t)
	ctx := context.Background()
	key := uniqueKey(t, client)

	if won, _ := client.Hold(ctx, key, "replica-a", 150*time.Millisecond); !won {
		t.Fatal("initial Hold failed")
	}
	// This is the property that makes a dead replica recoverable: nobody has to notice it died.
	time.Sleep(250 * time.Millisecond)
	won, err := client.Hold(ctx, key, "replica-b", time.Minute)
	if err != nil || !won {
		t.Errorf("Hold(b) after expiry = %v, %v; a dead replica would strand the session", won, err)
	}
}

func TestReleaseHoldOnlyReleasesYourOwn(t *testing.T) {
	client := testClient(t)
	ctx := context.Background()
	key := uniqueKey(t, client)

	if won, _ := client.Hold(ctx, key, "replica-a", time.Minute); !won {
		t.Fatal("initial Hold failed")
	}
	// A slow release from a replica that already lost the lease must not evict its successor.
	if err := client.ReleaseHold(ctx, key, "replica-b"); err != nil {
		t.Fatalf("ReleaseHold(b) error = %v", err)
	}
	if won, _ := client.Hold(ctx, key, "replica-c", time.Minute); won {
		t.Error("a non-holder released somebody else's lease")
	}

	if err := client.ReleaseHold(ctx, key, "replica-a"); err != nil {
		t.Fatalf("ReleaseHold(a) error = %v", err)
	}
	if won, _ := client.Hold(ctx, key, "replica-c", time.Minute); !won {
		t.Error("the lease was not released by its holder")
	}
}

// GETDEL is what makes a websocket ticket single-use: two concurrent redemptions cannot both
// succeed, which a read-then-delete pair could not promise.
func TestTakeReturnsTheValueOnceAndThenMisses(t *testing.T) {
	client := testClient(t)
	ctx := context.Background()
	key := uniqueKey(t, client)

	type ticket struct {
		Session string `json:"session"`
	}
	if err := client.SetJSON(ctx, key, ticket{Session: "abc"}, time.Minute); err != nil {
		t.Fatalf("SetJSON error = %v", err)
	}

	var first ticket
	if err := client.Take(ctx, key, &first); err != nil {
		t.Fatalf("first Take error = %v", err)
	}
	if first.Session != "abc" {
		t.Errorf("Take returned %+v", first)
	}
	var second ticket
	if err := client.Take(ctx, key, &second); !errors.Is(err, ErrMiss) {
		t.Errorf("second Take error = %v, want ErrMiss — the token is reusable", err)
	}
}

func TestTakeOnAnUnknownKeyMisses(t *testing.T) {
	client := testClient(t)
	var target struct{}
	if err := client.Take(context.Background(), client.Key("test", "never-written"), &target); !errors.Is(err, ErrMiss) {
		t.Errorf("Take(unknown) error = %v, want ErrMiss", err)
	}
}

// Incr backs the distributed auth throttle. The window has to be set on the first increment and
// then left alone, or a steady stream of attempts would push the expiry out forever and the
// counter would never reset.
func TestIncrCountsInAFixedWindow(t *testing.T) {
	client := testClient(t)
	ctx := context.Background()
	key := uniqueKey(t, client)

	var lastRemaining time.Duration
	for expected := int64(1); expected <= 3; expected++ {
		count, remaining, err := client.Incr(ctx, key, time.Minute)
		if err != nil {
			t.Fatalf("Incr error = %v", err)
		}
		if count != expected {
			t.Errorf("count = %d, want %d", count, expected)
		}
		if remaining <= 0 || remaining > time.Minute {
			t.Errorf("remaining = %v, want a value inside the window", remaining)
		}
		if lastRemaining > 0 && remaining > lastRemaining {
			t.Errorf("the window grew from %v to %v; a steady stream would never reset", lastRemaining, remaining)
		}
		lastRemaining = remaining
	}
}

func TestADisabledClientIsSafeToCall(t *testing.T) {
	t.Parallel()
	var client *Client
	ctx := context.Background()
	if client.Enabled() {
		t.Fatal("a nil client reports itself enabled")
	}
	// Every one of these is called on a path that must survive Redis being switched off.
	if won, err := client.Hold(ctx, "k", "holder", time.Minute); !won || err != nil {
		t.Errorf("Hold on a disabled client = %v, %v; want it to degrade open", won, err)
	}
	if err := client.ReleaseHold(ctx, "k", "holder"); err != nil {
		t.Errorf("ReleaseHold on a disabled client = %v", err)
	}
	var target struct{}
	if err := client.Take(ctx, "k", &target); !errors.Is(err, ErrMiss) {
		t.Errorf("Take on a disabled client = %v, want ErrMiss", err)
	}
}
