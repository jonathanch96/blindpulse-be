package journal

import (
	"context"

	"github.com/google/uuid"
	"github.com/jblabs/blindpulse-be/pkg/apperror"
	domainsession "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/session"
)

// SessionLookup is the narrow slice of the session repository this domain needs.
type SessionLookup interface {
	GetByID(context.Context, uuid.UUID) (*domainsession.Session, error)
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
		// Not-found rather than forbidden: confirming the id exists leaks somebody else's session.
		return nil, apperror.New("SESSION_NOT_FOUND")
	}
	// A closed session is deliberately *not* refused. Journalling after the fact is the point of
	// the post-mortem, and the cursor bound still applies — a closed session's cursor is wherever
	// the trader stopped, so nothing beyond what they saw becomes annotatable by closing it.
	return entity, nil
}
