package journal

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jblabs/blindpulse-be/pkg/apperror"
	domainjournal "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/journal"
	domainsession "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/session"
)

type repoStub struct {
	entries   map[uuid.UUID]*domainjournal.Entry
	revisions map[uuid.UUID][]domainjournal.Revision
	// failUpdate simulates the optimistic-lock rejection, so the transaction boundary can be
	// exercised without a database.
	failUpdate bool
}

func newRepoStub() *repoStub {
	return &repoStub{
		entries:   make(map[uuid.UUID]*domainjournal.Entry),
		revisions: make(map[uuid.UUID][]domainjournal.Revision),
	}
}

func (r *repoStub) Create(_ context.Context, entity *domainjournal.Entry) (*domainjournal.Entry, error) {
	stored := *entity
	r.entries[entity.ID] = &stored
	created := stored
	return &created, nil
}

func (r *repoStub) GetByID(_ context.Context, id uuid.UUID) (*domainjournal.Entry, error) {
	entry, ok := r.entries[id]
	if !ok {
		return nil, apperror.New("JOURNAL_NOT_FOUND")
	}
	copied := *entry
	return &copied, nil
}

func (r *repoStub) ListBySessionID(_ context.Context, sessionID uuid.UUID) ([]domainjournal.Entry, error) {
	var list []domainjournal.Entry
	for _, entry := range r.entries {
		if entry.SessionID == sessionID {
			list = append(list, *entry)
		}
	}
	return list, nil
}

func (r *repoStub) Update(_ context.Context, entity *domainjournal.Entry) error {
	if r.failUpdate {
		return apperror.New("CONCURRENT_MODIFICATION")
	}
	stored := *entity
	r.entries[entity.ID] = &stored
	return nil
}

func (r *repoStub) Delete(_ context.Context, id uuid.UUID) error {
	if _, ok := r.entries[id]; !ok {
		return apperror.New("JOURNAL_NOT_FOUND")
	}
	delete(r.entries, id)
	delete(r.revisions, id)
	return nil
}

func (r *repoStub) CreateRevision(_ context.Context, revision *domainjournal.Revision) error {
	r.revisions[revision.EntryID] = append(r.revisions[revision.EntryID], *revision)
	return nil
}

func (r *repoStub) ListRevisions(_ context.Context, entryID uuid.UUID) ([]domainjournal.Revision, error) {
	return r.revisions[entryID], nil
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

// rollbackUOW models a transaction: writes made inside a function that returns an error are undone.
// The passthrough default cannot show that, and the whole reason the edit runs in one is that a
// revision filed without its edit is a phantom.
type rollbackUOW struct{ repo *repoStub }

func (u rollbackUOW) Do(ctx context.Context, fn func(context.Context) error) error {
	entries := make(map[uuid.UUID]*domainjournal.Entry, len(u.repo.entries))
	for id, entry := range u.repo.entries {
		copied := *entry
		entries[id] = &copied
	}
	revisions := make(map[uuid.UUID][]domainjournal.Revision, len(u.repo.revisions))
	for id, list := range u.repo.revisions {
		revisions[id] = append([]domainjournal.Revision(nil), list...)
	}
	if err := fn(ctx); err != nil {
		u.repo.entries, u.repo.revisions = entries, revisions
		return err
	}
	return nil
}

func serviceFor(repo *repoStub, session sessionStub) Service {
	return NewService(Dependencies{
		Repo: repo, Sessions: session, UOW: rollbackUOW{repo: repo},
		Clock: func() time.Time { return time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC) },
	})
}

func text(value string) *string { return &value }

func TestWriteRefusesABarTheTraderHasNotBeenShown(t *testing.T) {
	repo := newRepoStub()
	userID := uuid.New()
	service := serviceFor(repo, sessionStub{userID: userID, cursor: 142})

	// The acceptance criterion: a note at bar 500 with the cursor at 142 claims to be an
	// observation of a candle that has not been released. Accepting it would put a prediction in
	// the record as though it were a reading.
	_, err := service.Write(context.Background(), userID, uuid.New(), WriteInput{BarIndex: 500, Note: text("breaks out here")})
	if !apperror.Is(err, "INVALID_CURSOR") {
		t.Fatalf("err = %v, want INVALID_CURSOR", err)
	}
	if len(repo.entries) != 0 {
		t.Errorf("%d entries written for a refused note", len(repo.entries))
	}

	// The cursor itself is inclusive: the bar the trader is looking at right now is annotatable.
	if _, err := service.Write(context.Background(), userID, uuid.New(), WriteInput{BarIndex: 142, Note: text("here")}); err != nil {
		t.Fatalf("a note on the current bar was refused: %v", err)
	}
	// And so is bar zero, which a pointerless request type would have read as a missing field.
	if _, err := service.Write(context.Background(), userID, uuid.New(), WriteInput{BarIndex: 0, Note: text("open")}); err != nil {
		t.Fatalf("a note on bar 0 was refused: %v", err)
	}
}

func TestWriteRefusesAnEntryWithNothingInIt(t *testing.T) {
	repo := newRepoStub()
	userID := uuid.New()
	service := serviceFor(repo, sessionStub{userID: userID, cursor: 10})

	// A row carrying only a bar index is a click, not a journal entry, and it would render as a
	// blank card in the post-mortem.
	_, err := service.Write(context.Background(), userID, uuid.New(), WriteInput{BarIndex: 5, Thesis: text("   ")})
	if !apperror.Is(err, "VALIDATION_FAILED") {
		t.Fatalf("err = %v, want VALIDATION_FAILED", err)
	}
	// An emotion alone is a real entry: "I was anxious here" is worth recording on its own.
	anxious := domainjournal.EmotionAnxious
	if _, err := service.Write(context.Background(), userID, uuid.New(), WriteInput{BarIndex: 5, Emotion: &anxious}); err != nil {
		t.Fatalf("an emotion-only entry was refused: %v", err)
	}
}

func TestWriteNormalizesTags(t *testing.T) {
	repo := newRepoStub()
	userID := uuid.New()
	service := serviceFor(repo, sessionStub{userID: userID, cursor: 10})

	entry, err := service.Write(context.Background(), userID, uuid.New(), WriteInput{
		BarIndex: 5, Note: text("n"), Tags: []string{"Breakout", " breakout ", "BREAKOUT", "", "fomo"},
	})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	// Three spellings of one idea group by nothing, and the post-mortem groups by these.
	if len(entry.Tags) != 2 || entry.Tags[0] != "breakout" || entry.Tags[1] != "fomo" {
		t.Errorf("tags = %v, want [breakout fomo]", entry.Tags)
	}
}

func TestEditFilesWhatTheEntryUsedToSay(t *testing.T) {
	repo := newRepoStub()
	userID := uuid.New()
	service := serviceFor(repo, sessionStub{userID: userID, cursor: 142})

	entry, err := service.Write(context.Background(), userID, uuid.New(), WriteInput{
		BarIndex: 88, Thesis: text("panicking, this is going against me"),
	})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	updated, err := service.Edit(context.Background(), userID, entry.ID, EditInput{
		Thesis: text("calm, following the plan"),
	})
	if err != nil {
		t.Fatalf("Edit: %v", err)
	}
	if updated.Version != 2 {
		t.Errorf("version = %d after one edit, want 2", updated.Version)
	}

	// The point of the whole mechanism: the discipline index reads what the trader thought while
	// the outcome was unknown. Without this, a panicked thesis could be rewritten into a calm one
	// after the fact and score better for it.
	revisions, err := service.Revisions(context.Background(), userID, entry.ID)
	if err != nil {
		t.Fatalf("Revisions: %v", err)
	}
	if len(revisions) != 1 {
		t.Fatalf("%d revisions after one edit, want 1", len(revisions))
	}
	if revisions[0].Version != 1 || revisions[0].Thesis == nil || *revisions[0].Thesis != "panicking, this is going against me" {
		t.Errorf("revision = %+v, want the original thesis at version 1", revisions[0])
	}
}

func TestEditLeavesUnmentionedFieldsAlone(t *testing.T) {
	repo := newRepoStub()
	userID := uuid.New()
	service := serviceFor(repo, sessionStub{userID: userID, cursor: 142})
	calm := domainjournal.EmotionCalm

	entry, err := service.Write(context.Background(), userID, uuid.New(), WriteInput{
		BarIndex: 88, Thesis: text("original"), Emotion: &calm, Tags: []string{"breakout"},
	})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	updated, err := service.Edit(context.Background(), userID, entry.ID, EditInput{Note: text("added a note")})
	if err != nil {
		t.Fatalf("Edit: %v", err)
	}
	// A form submits what it holds. A thesis dropped because the client did not send the field
	// would be a silent loss of exactly the content this feature exists to keep.
	if updated.Thesis == nil || *updated.Thesis != "original" {
		t.Errorf("thesis = %v, want it untouched", updated.Thesis)
	}
	if updated.Emotion == nil || *updated.Emotion != calm {
		t.Errorf("emotion = %v, want it untouched", updated.Emotion)
	}
	if len(updated.Tags) != 1 {
		t.Errorf("tags = %v, want them untouched", updated.Tags)
	}
	// An explicit empty string is the way to clear one.
	cleared, err := service.Edit(context.Background(), userID, entry.ID, EditInput{Thesis: text("")})
	if err != nil {
		t.Fatalf("Edit: %v", err)
	}
	if cleared.Thesis != nil {
		t.Errorf("thesis = %v after an explicit blank, want nil", *cleared.Thesis)
	}
}

func TestEditRollsBackTheRevisionWhenTheUpdateFails(t *testing.T) {
	repo := newRepoStub()
	userID := uuid.New()
	service := serviceFor(repo, sessionStub{userID: userID, cursor: 142})

	entry, err := service.Write(context.Background(), userID, uuid.New(), WriteInput{BarIndex: 88, Thesis: text("original")})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	repo.failUpdate = true
	if _, err := service.Edit(context.Background(), userID, entry.ID, EditInput{Thesis: text("rewritten")}); !apperror.Is(err, "CONCURRENT_MODIFICATION") {
		t.Fatalf("err = %v, want CONCURRENT_MODIFICATION", err)
	}
	// A revision filed for an edit that never landed is a phantom: the history would show the note
	// being changed to something it was never changed to.
	if revisions := repo.revisions[entry.ID]; len(revisions) != 0 {
		t.Errorf("%d revisions survived a failed edit, want 0", len(revisions))
	}
}

func TestSomeoneElsesEntryIsNotFound(t *testing.T) {
	repo := newRepoStub()
	owner, stranger := uuid.New(), uuid.New()
	service := serviceFor(repo, sessionStub{userID: owner, cursor: 142})

	entry, err := service.Write(context.Background(), owner, uuid.New(), WriteInput{BarIndex: 88, Note: text("mine")})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	// Not-found rather than forbidden: confirming the id exists would leak that somebody else
	// wrote a note, and on which session.
	if _, err := service.Edit(context.Background(), stranger, entry.ID, EditInput{Note: text("not yours")}); !apperror.Is(err, "JOURNAL_NOT_FOUND") {
		t.Errorf("err = %v, want JOURNAL_NOT_FOUND", err)
	}
	if err := service.Delete(context.Background(), stranger, entry.ID); !apperror.Is(err, "JOURNAL_NOT_FOUND") {
		t.Errorf("err = %v, want JOURNAL_NOT_FOUND", err)
	}
	if _, err := service.Revisions(context.Background(), stranger, entry.ID); !apperror.Is(err, "JOURNAL_NOT_FOUND") {
		t.Errorf("err = %v, want JOURNAL_NOT_FOUND", err)
	}
}
