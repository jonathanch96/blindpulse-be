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
	// Media is optional. Without it, uploads are refused with a code that says the feature is off
	// rather than failing as an internal error — a deployment with no signing secret has decided
	// not to accept images, and that is a configuration, not a fault.
	Media MediaStore
	// MaxUploadBytes caps one image. Zero uses the media package's default.
	MaxUploadBytes int64
	// Keys mints the storage key for an entry's image. Injected so a test can pin it and so the
	// key never derives from anything the uploader controls.
	Keys func(entryID uuid.UUID, extension string) string
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
