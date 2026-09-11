package drawing

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jblabs/blindpulse-be/pkg/apperror"
	domaindrawing "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/drawing"
	domainsession "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/session"
)

type repoStub struct {
	rows map[uuid.UUID]*domaindrawing.Drawing
}

func newRepoStub() *repoStub {
	return &repoStub{rows: make(map[uuid.UUID]*domaindrawing.Drawing)}
}

func (r *repoStub) Create(_ context.Context, entity *domaindrawing.Drawing) (*domaindrawing.Drawing, error) {
	stored := *entity
	r.rows[entity.ID] = &stored
	created := stored
	return &created, nil
}

func (r *repoStub) GetByID(_ context.Context, id uuid.UUID) (*domaindrawing.Drawing, error) {
	entity, ok := r.rows[id]
	if !ok {
		return nil, apperror.New("DRAWING_NOT_FOUND")
	}
	copied := *entity
	return &copied, nil
}

func (r *repoStub) ListBySessionID(_ context.Context, sessionID uuid.UUID) ([]domaindrawing.Drawing, error) {
	var list []domaindrawing.Drawing
	for _, entity := range r.rows {
		if entity.SessionID == sessionID {
			list = append(list, *entity)
		}
	}
	return list, nil
}

func (r *repoStub) Update(_ context.Context, entity *domaindrawing.Drawing) error {
	stored := *entity
	r.rows[entity.ID] = &stored
	return nil
}

func (r *repoStub) Delete(_ context.Context, id uuid.UUID) error {
	if _, ok := r.rows[id]; !ok {
		return apperror.New("DRAWING_NOT_FOUND")
	}
	delete(r.rows, id)
	return nil
}

type sessionStub struct {
	userID uuid.UUID
	cursor int
}

func (s sessionStub) OwnedSession(_ context.Context, userID, sessionID uuid.UUID) (*domainsession.Session, error) {
	if userID != s.userID {
		return nil, apperror.New("SESSION_NOT_FOUND")
	}
	return &domainsession.Session{ID: sessionID, UserID: userID, CursorIndex: s.cursor}, nil
}

func serviceFor(repo *repoStub, session sessionStub) Service {
	return NewService(Dependencies{
		Repo: repo, Sessions: session,
		Clock: func() time.Time { return time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC) },
	})
}

const trendline = `{"anchors":[{"index":140,"price":"1.07231"},{"index":214,"price":"1.08004"}]}`

func create(index int, payload string) CreateInput {
	return CreateInput{
		Kind: domaindrawing.KindTrendline, Timeframe: "15m",
		Payload: json.RawMessage(payload), CreatedBarIndex: index,
	}
}

func TestCreateRefusesAnAnchorPastTheCursor(t *testing.T) {
	repo := newRepoStub()
	userID := uuid.New()
	service := serviceFor(repo, sessionStub{userID: userID, cursor: 142})

	// Same rule as a journal entry, for the same reason: a line drawn on a candle the trader has
	// not been shown is a line drawn with hindsight.
	_, err := service.Create(context.Background(), userID, uuid.New(), create(500, trendline))
	if !apperror.Is(err, "INVALID_CURSOR") {
		t.Fatalf("err = %v, want INVALID_CURSOR", err)
	}
	if len(repo.rows) != 0 {
		t.Errorf("%d drawings stored for a refused anchor", len(repo.rows))
	}
	if _, err := service.Create(context.Background(), userID, uuid.New(), create(142, trendline)); err != nil {
		t.Fatalf("a drawing on the current bar was refused: %v", err)
	}
}

// AC 05-AC-6. This is the payload a client controls end to end, so it is the one place a date could
// be smuggled into a pre-reveal response.
func TestCreateRefusesAPayloadThatCouldDateTheWindow(t *testing.T) {
	repo := newRepoStub()
	userID := uuid.New()
	service := serviceFor(repo, sessionStub{userID: userID, cursor: 142})

	withDate := `{"anchors":[{"index":140,"price":"1.07","drawn_at":"2023-03-14T08:30:00Z"}]}`
	_, err := service.Create(context.Background(), userID, uuid.New(), create(140, withDate))
	if !apperror.Is(err, "VALIDATION_FAILED") {
		t.Fatalf("err = %v, want VALIDATION_FAILED", err)
	}
	if len(repo.rows) != 0 {
		t.Errorf("%d drawings stored for a payload carrying a date", len(repo.rows))
	}
}

func TestCreateRefusesAnUnknownToolAndAnOversizedPayload(t *testing.T) {
	repo := newRepoStub()
	userID := uuid.New()
	service := serviceFor(repo, sessionStub{userID: userID, cursor: 142})

	unknown := create(10, trendline)
	unknown.Kind = "sentiment_ray"
	if _, err := service.Create(context.Background(), userID, uuid.New(), unknown); !apperror.Is(err, "VALIDATION_FAILED") {
		t.Errorf("err = %v, want VALIDATION_FAILED for an unknown tool", err)
	}

	oversized := create(10, `{"anchors":[],"pad":"`+strings.Repeat("x", domaindrawing.MaxPayloadBytes)+`"}`)
	if _, err := service.Create(context.Background(), userID, uuid.New(), oversized); !apperror.Is(err, "FILE_TOO_LARGE") {
		t.Errorf("err = %v, want FILE_TOO_LARGE", err)
	}

	badTimeframe := create(10, trendline)
	badTimeframe.Timeframe = "7m"
	if _, err := service.Create(context.Background(), userID, uuid.New(), badTimeframe); !apperror.Is(err, "INVALID_TIMEFRAME") {
		t.Errorf("err = %v, want INVALID_TIMEFRAME", err)
	}
}

// A reshape is a dragged handle. What it cannot become is a drawing on a different bar or a
// different timeframe — those are what the drawing is, and letting them move after the fact would
// reintroduce the hindsight the anchor bound refuses on the way in.
func TestUpdateReshapesWithoutMovingTheAnchorOrTheTimeframe(t *testing.T) {
	repo := newRepoStub()
	userID := uuid.New()
	service := serviceFor(repo, sessionStub{userID: userID, cursor: 142})

	entity, err := service.Create(context.Background(), userID, uuid.New(), create(140, trendline))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	reshaped := `{"anchors":[{"index":140,"price":"1.07231"},{"index":142,"price":"1.09"}]}`
	updated, err := service.Update(context.Background(), userID, entity.ID, UpdateInput{Payload: json.RawMessage(reshaped)})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.CreatedBarIndex != 140 || updated.Timeframe != "15m" || updated.Kind != domaindrawing.KindTrendline {
		t.Errorf("update moved the drawing: %+v", updated)
	}
	if updated.Version != 2 {
		t.Errorf("version = %d, want 2", updated.Version)
	}
	// The payload check applies to a reshape too; otherwise the guard is one PATCH away from
	// being bypassed entirely.
	if _, err := service.Update(context.Background(), userID, entity.ID, UpdateInput{
		Payload: json.RawMessage(`{"anchors":[{"index":1,"timestamp":1678780800}]}`),
	}); !apperror.Is(err, "VALIDATION_FAILED") {
		t.Errorf("err = %v, want VALIDATION_FAILED on a reshape carrying a clock", err)
	}
}

func TestSomeoneElsesDrawingIsNotFound(t *testing.T) {
	repo := newRepoStub()
	owner, stranger := uuid.New(), uuid.New()
	service := serviceFor(repo, sessionStub{userID: owner, cursor: 142})

	entity, err := service.Create(context.Background(), owner, uuid.New(), create(140, trendline))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := service.Update(context.Background(), stranger, entity.ID, UpdateInput{Payload: json.RawMessage(trendline)}); !apperror.Is(err, "DRAWING_NOT_FOUND") {
		t.Errorf("err = %v, want DRAWING_NOT_FOUND", err)
	}
	if err := service.Delete(context.Background(), stranger, entity.ID); !apperror.Is(err, "DRAWING_NOT_FOUND") {
		t.Errorf("err = %v, want DRAWING_NOT_FOUND", err)
	}
	if _, ok := repo.rows[entity.ID]; !ok {
		t.Error("a stranger's delete removed the drawing")
	}
}
