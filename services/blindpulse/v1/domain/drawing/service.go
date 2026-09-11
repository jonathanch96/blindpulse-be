// Package drawing is the write side of the trader's chart annotations.
//
// The server stores drawings without understanding them. What it does check is the envelope: a
// known tool, a bar the session has released, a size cap, and — the one that matters — that the
// payload carries no clock. The rest is the terminal's business, which is what lets a new tool ship
// without a migration.
package drawing

import (
	"context"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"github.com/jblabs/blindpulse-be/pkg/apperror"
	domaindrawing "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/drawing"
	"github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/market"
)

func NewService(deps Dependencies) Service {
	if deps.Clock == nil {
		deps.Clock = func() time.Time { return time.Now().UTC() }
	}
	return &service{deps: deps}
}

func (s *service) Create(ctx context.Context, userID, sessionID uuid.UUID, in CreateInput) (*domaindrawing.Drawing, error) {
	replay, err := s.deps.Sessions.OwnedSession(ctx, userID, sessionID)
	if err != nil {
		return nil, err
	}
	if !in.Kind.Valid() {
		return nil, apperror.WithFields("VALIDATION_FAILED", []apperror.FieldError{
			{Field: "kind", Rule: "oneof", Message: "unknown drawing tool"},
		})
	}
	if !market.Timeframe(in.Timeframe).Valid() {
		return nil, apperror.New("INVALID_TIMEFRAME")
	}
	// The anchoring bar is bounded by the cursor for the same reason a journal entry is: a
	// trendline anchored to bar 500 with the cursor at 142 is drawn on a candle the trader has not
	// been shown. The bound is in base bars, which is the only index space the cursor lives in.
	if in.CreatedBarIndex < 0 || in.CreatedBarIndex > replay.CursorIndex {
		return nil, apperror.New("INVALID_CURSOR")
	}
	if err := validatePayload(in.Payload); err != nil {
		return nil, err
	}
	now := s.deps.Clock()
	entity := domaindrawing.Drawing{
		ID: uuid.New(), SessionID: sessionID, Kind: in.Kind, Timeframe: in.Timeframe,
		Payload: in.Payload, CreatedBarIndex: in.CreatedBarIndex,
		CreatedAt: now, UpdatedAt: now, Version: 1,
	}
	return s.deps.Repo.Create(ctx, &entity)
}

func (s *service) List(ctx context.Context, userID, sessionID uuid.UUID) ([]domaindrawing.Drawing, error) {
	if _, err := s.deps.Sessions.OwnedSession(ctx, userID, sessionID); err != nil {
		return nil, err
	}
	return s.deps.Repo.ListBySessionID(ctx, sessionID)
}

// Update replaces the payload — a dragged handle, a re-shaded zone. Neither the kind, the timeframe
// nor the anchoring bar can change: those are what the drawing *is*, and a trendline that could
// move to another bar after the fact is a trendline drawn with hindsight.
func (s *service) Update(ctx context.Context, userID, drawingID uuid.UUID, in UpdateInput) (*domaindrawing.Drawing, error) {
	entity, err := s.owned(ctx, userID, drawingID)
	if err != nil {
		return nil, err
	}
	if err := validatePayload(in.Payload); err != nil {
		return nil, err
	}
	updated := *entity
	updated.Payload = in.Payload
	updated.UpdatedAt = s.deps.Clock()
	updated.Version = entity.Version + 1
	if err := s.deps.Repo.Update(ctx, &updated); err != nil {
		return nil, err
	}
	return &updated, nil
}

func (s *service) Delete(ctx context.Context, userID, drawingID uuid.UUID) error {
	if _, err := s.owned(ctx, userID, drawingID); err != nil {
		return err
	}
	return s.deps.Repo.Delete(ctx, drawingID)
}

// owned loads a drawing and proves the caller owns the session it belongs to. A drawing has no
// user of its own — it belongs to the session, and the session belongs to somebody.
func (s *service) owned(ctx context.Context, userID, drawingID uuid.UUID) (*domaindrawing.Drawing, error) {
	entity, err := s.deps.Repo.GetByID(ctx, drawingID)
	if err != nil {
		return nil, err
	}
	if _, err := s.deps.Sessions.OwnedSession(ctx, userID, entity.SessionID); err != nil {
		// The session check answers SESSION_NOT_FOUND for somebody else's session; from here the
		// useful answer is that this drawing is not the caller's to touch.
		return nil, apperror.New("DRAWING_NOT_FOUND")
	}
	return entity, nil
}

func validatePayload(payload json.RawMessage) error {
	if len(payload) == 0 {
		return apperror.WithFields("VALIDATION_FAILED", []apperror.FieldError{
			{Field: "payload", Rule: "required", Message: "a drawing needs a payload"},
		})
	}
	if len(payload) > domaindrawing.MaxPayloadBytes {
		return apperror.New("FILE_TOO_LARGE")
	}
	if !json.Valid(payload) {
		return apperror.WithFields("VALIDATION_FAILED", []apperror.FieldError{
			{Field: "payload", Rule: "json", Message: "the payload is not valid JSON"},
		})
	}
	// The blinding check. A drawing is the one place a client controls the bytes the server stores
	// and later hands back, so a timestamp smuggled in here would come back out of a pre-reveal
	// endpoint — the exact leak the whole feed pipeline is built to prevent.
	if domaindrawing.DateLeak(payload) {
		return apperror.WithFields("VALIDATION_FAILED", []apperror.FieldError{
			{Field: "payload", Rule: "no_dates", Message: "a drawing anchors to bar indices; it cannot carry a date or a timestamp"},
		})
	}
	return nil
}
