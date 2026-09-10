package session

import (
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
	Topic    func(string) string
	Clock    func() time.Time
	// Seeds draws the determinism seed. Injected so a test can pin it.
	Seeds func() int64

	// Bounds mirror the configured replay limits. They live here rather than being read from
	// config inside the domain, so the rules are testable without an environment.
	MinSpeed          decimal.Decimal
	MaxSpeed          decimal.Decimal
	CheckpointEvery   int
	MaxBarsPerRequest int
}

type service struct{ deps Dependencies }

type StartInput struct {
	AccountID uuid.UUID
	FeedID    uuid.UUID
	Timeframe market.Timeframe
}
