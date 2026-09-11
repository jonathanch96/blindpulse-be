package journal

import (
	"time"

	"github.com/google/uuid"
	domainjournal "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/journal"
	"github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/event"
)

// EventRecord aliases the outbox event so the domain does not import the persistence package.
type EventRecord = event.OutboxEvent

type Dependencies struct {
	Repo     Repository
	Sessions SessionReader
	Outbox   OutboxRepository
	UOW      UnitOfWork
	Topic    func(string) string
	Clock    func() time.Time
}

type service struct{ deps Dependencies }

type WriteInput struct {
	BarIndex   int
	TradeID    *uuid.UUID
	Thesis     *string
	Note       *string
	Emotion    *domainjournal.Emotion
	Conviction *int
	Tags       []string
}

// EditInput carries only what the caller sent. A nil field means "leave it", not "clear it" — the
// form submits what it holds, and a thesis dropped because the client omitted the field would be a
// silent loss. Clearing is an explicit empty string.
type EditInput struct {
	Thesis     *string
	Note       *string
	Emotion    *domainjournal.Emotion
	Conviction *int
	Tags       []string
}
