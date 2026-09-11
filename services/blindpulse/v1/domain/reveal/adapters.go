package reveal

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jblabs/blindpulse-be/pkg/apperror"
	domainsession "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/session"
)

// SessionLookup is the narrow slice of the session repository this domain needs: read one, and
// stamp it revealed.
type SessionLookup interface {
	GetByID(context.Context, uuid.UUID) (*domainsession.Session, error)
	MarkRevealed(ctx context.Context, id uuid.UUID, at time.Time) error
}

type sessionReader struct{ sessions SessionLookup }

// NewSessionReader adapts a session repository to this domain's SessionReader.
func NewSessionReader(sessions SessionLookup) SessionReader {
	return sessionReader{sessions: sessions}
}

func (r sessionReader) OwnedSession(ctx context.Context, userID, sessionID uuid.UUID) (*domainsession.Session, error) {
	entity, err := r.sessions.GetByID(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if entity.UserID != userID {
		// Precondition 1, and not-found rather than forbidden for the usual reason: confirming the
		// id exists leaks that somebody else has a session, which for a reveal also leaks that
		// somebody has traded this feed.
		return nil, apperror.New("SESSION_NOT_FOUND")
	}
	return entity, nil
}

func (r sessionReader) MarkRevealed(ctx context.Context, sessionID uuid.UUID, at time.Time) error {
	return r.sessions.MarkRevealed(ctx, sessionID, at)
}
