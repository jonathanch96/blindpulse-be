package journal

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
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

// --- media (FR-JOURNAL-06) ---

type mediaStub struct {
	objects map[string][]byte
	// failPut simulates a store that refuses, so the rollback path can be exercised.
	failPut bool
}

func newMediaStub() *mediaStub { return &mediaStub{objects: make(map[string][]byte)} }

func (m *mediaStub) Put(_ context.Context, key string, content []byte, _ string) error {
	if m.failPut {
		return errors.New("store unavailable")
	}
	m.objects[key] = content
	return nil
}

func (m *mediaStub) Get(_ context.Context, key string) ([]byte, string, error) {
	content, ok := m.objects[key]
	if !ok {
		return nil, "", errors.New("not found")
	}
	return content, "image/png", nil
}

func (m *mediaStub) Delete(_ context.Context, key string) error {
	delete(m.objects, key)
	return nil
}

func pngFixture(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for x := 0; x < 8; x++ {
		img.Set(x, x, color.RGBA{R: 200, G: 10, B: 10, A: 255})
	}
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		t.Fatalf("encode: %v", err)
	}
	return out.Bytes()
}

func mediaServiceFor(repo *repoStub, session sessionStub, store MediaStore) Service {
	return NewService(Dependencies{
		Repo: repo, Sessions: session, UOW: rollbackUOW{repo: repo}, Media: store,
		Clock: func() time.Time { return time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC) },
		// Pinned so the assertions can name the key. Production mints a fresh suffix per upload, so
		// a replaced image cannot be served from a cache still holding the old one.
		Keys: func(entryID uuid.UUID, extension string) string { return "journal/" + entryID.String() + extension },
	})
}

func TestAttachMediaStoresASanitizedImageAndPointsTheEntryAtIt(t *testing.T) {
	repo, store := newRepoStub(), newMediaStub()
	userID := uuid.New()
	service := mediaServiceFor(repo, sessionStub{userID: userID, cursor: 142}, store)

	entry, err := service.Write(context.Background(), userID, uuid.New(), WriteInput{BarIndex: 88, Note: text("n")})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	updated, err := service.AttachMedia(context.Background(), userID, entry.ID, pngFixture(t))
	if err != nil {
		t.Fatalf("AttachMedia: %v", err)
	}
	if updated.MediaKey == nil {
		t.Fatal("the entry does not point at an image")
	}
	if _, ok := store.objects[*updated.MediaKey]; !ok {
		t.Errorf("nothing was stored under %q", *updated.MediaKey)
	}
	// Swapping the screenshot changes what the entry shows, so it files a revision for the same
	// reason a text edit does.
	if updated.Version != 2 {
		t.Errorf("version = %d, want 2", updated.Version)
	}
	if len(repo.revisions[entry.ID]) != 1 {
		t.Errorf("%d revisions, want 1", len(repo.revisions[entry.ID]))
	}
}

func TestAttachMediaRefusesWhatIsNotAnAcceptedImage(t *testing.T) {
	repo, store := newRepoStub(), newMediaStub()
	userID := uuid.New()
	service := mediaServiceFor(repo, sessionStub{userID: userID, cursor: 142}, store)
	entry, err := service.Write(context.Background(), userID, uuid.New(), WriteInput{BarIndex: 88, Note: text("n")})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	// An SVG is a document that can carry script and fetch remote resources — the worst thing to
	// accept and then hand back under a signed URL.
	svg := []byte(`<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`)
	if _, err := service.AttachMedia(context.Background(), userID, entry.ID, svg); !apperror.Is(err, "UNSUPPORTED_MEDIA_TYPE") {
		t.Errorf("err = %v, want UNSUPPORTED_MEDIA_TYPE", err)
	}
	if len(store.objects) != 0 {
		t.Errorf("%d objects stored for a refused upload", len(store.objects))
	}
}

// The blob is written before the row. A failure after that would leave a file nothing points at,
// which is storage nobody can reach and nobody knows to bill for.
func TestAttachMediaRemovesTheBlobWhenTheRowCannotBeWritten(t *testing.T) {
	repo, store := newRepoStub(), newMediaStub()
	userID := uuid.New()
	service := mediaServiceFor(repo, sessionStub{userID: userID, cursor: 142}, store)
	entry, err := service.Write(context.Background(), userID, uuid.New(), WriteInput{BarIndex: 88, Note: text("n")})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	repo.failUpdate = true
	if _, err := service.AttachMedia(context.Background(), userID, entry.ID, pngFixture(t)); err == nil {
		t.Fatal("the attach succeeded despite a failing update")
	}
	if len(store.objects) != 0 {
		t.Errorf("%d orphaned objects left behind", len(store.objects))
	}
}

// One image per entry. The previous object is removed only after the row points at the new one:
// the other order would, on a failed update, leave the entry referencing a file that is gone.
func TestAttachingASecondImageReplacesAndRemovesTheFirst(t *testing.T) {
	repo, store := newRepoStub(), newMediaStub()
	userID := uuid.New()
	keys := 0
	service := NewService(Dependencies{
		Repo: repo, Sessions: sessionStub{userID: userID, cursor: 142}, UOW: rollbackUOW{repo: repo},
		Media: store, Clock: func() time.Time { return time.Date(2026, 4, 1, 12, 0, 0, 0, time.UTC) },
		Keys: func(entryID uuid.UUID, extension string) string {
			keys++
			return fmt.Sprintf("journal/%s-%d%s", entryID, keys, extension)
		},
	})
	entry, err := service.Write(context.Background(), userID, uuid.New(), WriteInput{BarIndex: 88, Note: text("n")})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	first, err := service.AttachMedia(context.Background(), userID, entry.ID, pngFixture(t))
	if err != nil {
		t.Fatalf("first AttachMedia: %v", err)
	}
	second, err := service.AttachMedia(context.Background(), userID, entry.ID, pngFixture(t))
	if err != nil {
		t.Fatalf("second AttachMedia: %v", err)
	}
	if *first.MediaKey == *second.MediaKey {
		t.Fatal("the replacement reused the key; a cache could still serve the old image")
	}
	if _, ok := store.objects[*first.MediaKey]; ok {
		t.Error("the replaced image was left in the store")
	}
	if _, ok := store.objects[*second.MediaKey]; !ok {
		t.Error("the replacement is not in the store")
	}
}

func TestAttachMediaIsRefusedWhenUploadsAreNotConfigured(t *testing.T) {
	repo := newRepoStub()
	userID := uuid.New()
	// No Media dependency: a deployment with no signing secret has decided not to accept images,
	// which is a configuration rather than a fault, so it answers rather than erroring internally.
	service := serviceFor(repo, sessionStub{userID: userID, cursor: 142})
	entry, err := service.Write(context.Background(), userID, uuid.New(), WriteInput{BarIndex: 88, Note: text("n")})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if _, err := service.AttachMedia(context.Background(), userID, entry.ID, pngFixture(t)); !apperror.Is(err, "MEDIA_LINK_INVALID") {
		t.Errorf("err = %v, want MEDIA_LINK_INVALID", err)
	}
}

func TestSomeoneElsesEntryCannotBeGivenAnImage(t *testing.T) {
	repo, store := newRepoStub(), newMediaStub()
	owner, stranger := uuid.New(), uuid.New()
	service := mediaServiceFor(repo, sessionStub{userID: owner, cursor: 142}, store)
	entry, err := service.Write(context.Background(), owner, uuid.New(), WriteInput{BarIndex: 88, Note: text("mine")})
	if err != nil {
		t.Fatalf("Write: %v", err)
	}
	if _, err := service.AttachMedia(context.Background(), stranger, entry.ID, pngFixture(t)); !apperror.Is(err, "JOURNAL_NOT_FOUND") {
		t.Errorf("err = %v, want JOURNAL_NOT_FOUND", err)
	}
	if len(store.objects) != 0 {
		t.Errorf("%d objects stored for a stranger's upload", len(store.objects))
	}
}
