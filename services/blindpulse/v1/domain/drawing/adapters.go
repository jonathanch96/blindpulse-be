package drawing

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
		return nil, apperror.New("SESSION_NOT_FOUND")
	}
	return entity, nil
}
