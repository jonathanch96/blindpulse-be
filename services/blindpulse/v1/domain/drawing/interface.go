package drawing

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	domaindrawing "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/drawing"
	domainsession "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/session"
)

type Service interface {
	// Create stores one annotation. The payload is opaque except for three checks: the kind is
	// known, the anchoring bar has been released, and nothing in it could date the window.
	Create(ctx context.Context, userID, sessionID uuid.UUID, in CreateInput) (*domaindrawing.Drawing, error)
	List(ctx context.Context, userID, sessionID uuid.UUID) ([]domaindrawing.Drawing, error)
	Update(ctx context.Context, userID, drawingID uuid.UUID, in UpdateInput) (*domaindrawing.Drawing, error)
	Delete(ctx context.Context, userID, drawingID uuid.UUID) error
}

type Repository interface {
	Create(context.Context, *domaindrawing.Drawing) (*domaindrawing.Drawing, error)
	GetByID(context.Context, uuid.UUID) (*domaindrawing.Drawing, error)
	ListBySessionID(context.Context, uuid.UUID) ([]domaindrawing.Drawing, error)
	Update(context.Context, *domaindrawing.Drawing) error
	Delete(context.Context, uuid.UUID) error
}

// SessionReader is this domain's view of a replay session. Declared here rather than imported from
// the session domain so the two aggregates stay independent.
type SessionReader interface {
	OwnedSession(ctx context.Context, userID, sessionID uuid.UUID) (*domainsession.Session, error)
}

type Dependencies struct {
	Repo     Repository
	Sessions SessionReader
	Clock    func() time.Time
}

type service struct{ deps Dependencies }

type CreateInput struct {
	Kind            domaindrawing.Kind
	Timeframe       string
	Payload         json.RawMessage
	CreatedBarIndex int
}

type UpdateInput struct {
	Payload json.RawMessage
}
