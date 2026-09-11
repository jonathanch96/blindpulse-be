package journal

import (
	"context"

	"github.com/google/uuid"
	domainjournal "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/journal"
	domainsession "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/session"
)

type Service interface {
	// Write records a note against a bar the session has already released. It refuses a bar past
	// the cursor: a trader cannot annotate a candle they have not been shown, and an entry that
	// claims to be from bar 500 while the cursor is at 142 is either a bug or a client trying to
	// backdate a thesis.
	Write(ctx context.Context, userID, sessionID uuid.UUID, in WriteInput) (*domainjournal.Entry, error)
	List(ctx context.Context, userID, sessionID uuid.UUID) ([]domainjournal.Entry, error)
	// Edit replaces an entry's content, filing what it said before. Entries stay editable after
	// the session closes because reflection is the point — but the discipline projector reads what
	// the trader thought at the time, so the superseded content has to survive the edit.
	Edit(ctx context.Context, userID, entryID uuid.UUID, in EditInput) (*domainjournal.Entry, error)
	Delete(ctx context.Context, userID, entryID uuid.UUID) error
	// Revisions returns what an entry said before each edit, oldest first.
	Revisions(ctx context.Context, userID, entryID uuid.UUID) ([]domainjournal.Revision, error)
}

type Repository interface {
	Create(context.Context, *domainjournal.Entry) (*domainjournal.Entry, error)
	GetByID(context.Context, uuid.UUID) (*domainjournal.Entry, error)
	ListBySessionID(context.Context, uuid.UUID) ([]domainjournal.Entry, error)
	Update(context.Context, *domainjournal.Entry) error
	Delete(context.Context, uuid.UUID) error
	CreateRevision(context.Context, *domainjournal.Revision) error
	ListRevisions(context.Context, uuid.UUID) ([]domainjournal.Revision, error)
}

// SessionReader is this domain's view of a replay session: who owns it and where its cursor is.
// Declared here rather than imported from the session domain so the two aggregates stay
// independent — a journal needs three facts about a session, not its whole surface.
type SessionReader interface {
	OwnedSession(ctx context.Context, userID, sessionID uuid.UUID) (*domainsession.Session, error)
}

type OutboxRepository interface {
	Create(ctx context.Context, event *EventRecord) error
}

// UnitOfWork runs the edit and its revision in one transaction. A revision written without the
// edit is a phantom; an edit written without the revision is the lost history this exists to keep.
type UnitOfWork interface {
	Do(ctx context.Context, fn func(context.Context) error) error
}
