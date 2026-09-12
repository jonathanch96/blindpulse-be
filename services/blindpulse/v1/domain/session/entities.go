package session

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/market"
	"github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/event"
	"github.com/shopspring/decimal"
)

// EventRecord aliases the outbox event so the domain does not import the persistence package.
type EventRecord = event.OutboxEvent

type Dependencies struct {
	Repo     Repository
	State    StateStore
	Feeds    FeedReader
	Accounts AccountReader
	Outbox   OutboxRepository
	// Execution resolves the bars a cursor move releases: resting orders fill, stops and targets
	// trigger, equity is marked. Optional — a deployment without it still steps, it just never
	// fills anything — which is what let the replay half ship a sprint before execution did.
	Execution CursorObserver
	Topic     func(string) string
	Clock     func() time.Time
	// Seeds draws the determinism seed. Injected so a test can pin it.
	Seeds func() int64

	// Bounds mirror the configured replay limits. They live here rather than being read from
	// config inside the domain, so the rules are testable without an environment.
	MinSpeed          decimal.Decimal
	MaxSpeed          decimal.Decimal
	CheckpointEvery   int
	MaxBarsPerRequest int

	// Streaming. All three are optional: with none of them wired the session still steps over
	// HTTP, it just does not stream. Bus and Lease have in-process implementations for a
	// single-instance deployment, so "no Redis" degrades rather than fails.
	Bus    FrameBus
	Lease  DriverLease
	Ticket TicketStore
	// BaseTick is how long one bar takes at 1x. Playback speed divides it, so 10x is ten bars in
	// the same wall-clock second. This is replay time, not market time: a 15m feed at 1x would
	// otherwise take a working week to walk.
	BaseTick  time.Duration
	TicketTTL time.Duration
	// Holder identifies this replica when it takes a driver lease.
	Holder string
}

type service struct {
	deps Dependencies
	// drivers tracks the sessions this replica is currently driving, so a second subscriber joins
	// the running clock instead of starting a second one.
	driversMu sync.Mutex
	drivers   map[uuid.UUID]*driver
}

// driver is one session's playback clock on this replica, and the count of sockets keeping it
// alive. The last socket to leave stops the clock — a session nobody is watching should not be
// burning through its feed.
type driver struct {
	cancel context.CancelFunc
	refs   int
}

type StartInput struct {
	AccountID uuid.UUID
	FeedID    uuid.UUID
	Timeframe market.Timeframe
}
