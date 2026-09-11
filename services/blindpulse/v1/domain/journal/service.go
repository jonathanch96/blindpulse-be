// Package journal is the write side of what the trader was thinking.
//
// Two rules live here rather than in a handler, because both are about what a trader is allowed to
// claim rather than about request shape:
//
//   - A note belongs to a bar the session has released. Anything past the cursor is refused.
//   - An edit cannot erase what the note used to say. The superseded content is filed in the same
//     transaction, so a panicked thesis cannot be rewritten into a calm one after the outcome is
//     known — which is precisely what the discipline index would otherwise reward.
package journal

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jblabs/blindpulse-be/pkg/apperror"
	"github.com/jblabs/blindpulse-be/pkg/media"
	domainjournal "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/journal"
	"github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/event"
)

// MaxTags caps the tag list. Tags are for grouping in the post-mortem; a note with fifty of them is
// not grouped by anything.
const MaxTags = 12

func NewService(deps Dependencies) Service {
	if deps.Clock == nil {
		deps.Clock = func() time.Time { return time.Now().UTC() }
	}
	if deps.UOW == nil {
		deps.UOW = passthroughUOW{}
	}
	return &service{deps: deps}
}

func (s *service) Write(ctx context.Context, userID, sessionID uuid.UUID, in WriteInput) (*domainjournal.Entry, error) {
	replay, err := s.deps.Sessions.OwnedSession(ctx, userID, sessionID)
	if err != nil {
		return nil, err
	}
	// The cursor is the whole rule. A note at bar 500 with the cursor at 142 is a note about a
	// candle the trader has not seen, and accepting it would put a prediction in the record as
	// though it were an observation.
	if in.BarIndex < 0 || in.BarIndex > replay.CursorIndex {
		return nil, apperror.New("INVALID_CURSOR")
	}
	tags, err := normalizeTags(in.Tags)
	if err != nil {
		return nil, err
	}
	if err := validate(in.Emotion, in.Conviction); err != nil {
		return nil, err
	}
	now := s.deps.Clock()
	entry := domainjournal.Entry{
		ID: uuid.New(), SessionID: sessionID, TradeID: in.TradeID, UserID: userID,
		BarIndex: in.BarIndex, Thesis: trimmed(in.Thesis), Note: trimmed(in.Note),
		Emotion: in.Emotion, Conviction: in.Conviction, Tags: tags,
		CreatedAt: now, UpdatedAt: now, Version: 1,
	}
	if entry.Empty() {
		return nil, apperror.WithFields("VALIDATION_FAILED", []apperror.FieldError{
			{Field: "entry", Rule: "empty", Message: "a journal entry needs a thesis, a note, an emotion, a conviction or a tag"},
		})
	}
	created, err := s.deps.Repo.Create(ctx, &entry)
	if err != nil {
		return nil, err
	}
	if err := s.emit(ctx, created.ID, sessionID, "created", *created); err != nil {
		return nil, err
	}
	return created, nil
}

func (s *service) List(ctx context.Context, userID, sessionID uuid.UUID) ([]domainjournal.Entry, error) {
	if _, err := s.deps.Sessions.OwnedSession(ctx, userID, sessionID); err != nil {
		return nil, err
	}
	return s.deps.Repo.ListBySessionID(ctx, sessionID)
}

func (s *service) Edit(ctx context.Context, userID, entryID uuid.UUID, in EditInput) (*domainjournal.Entry, error) {
	entry, err := s.owned(ctx, userID, entryID)
	if err != nil {
		return nil, err
	}
	if err := validate(in.Emotion, in.Conviction); err != nil {
		return nil, err
	}
	// The bar index is not editable. Moving a note to a different candle after the fact would let
	// a trader claim they called a move they actually watched, which is the same hindsight the
	// cursor check refuses on the way in.
	revision := entry.Supersede(s.deps.Clock())
	updated := *entry
	if in.Thesis != nil {
		updated.Thesis = trimmed(in.Thesis)
	}
	if in.Note != nil {
		updated.Note = trimmed(in.Note)
	}
	if in.Emotion != nil {
		updated.Emotion = in.Emotion
	}
	if in.Conviction != nil {
		updated.Conviction = in.Conviction
	}
	if in.Tags != nil {
		tags, tagErr := normalizeTags(in.Tags)
		if tagErr != nil {
			return nil, tagErr
		}
		updated.Tags = tags
	}
	if updated.Empty() {
		return nil, apperror.WithFields("VALIDATION_FAILED", []apperror.FieldError{
			{Field: "entry", Rule: "empty", Message: "an edit cannot empty the entry; delete it instead"},
		})
	}
	updated.UpdatedAt = s.deps.Clock()
	updated.Version = entry.Version + 1

	err = s.deps.UOW.Do(ctx, func(txCtx context.Context) error {
		// The revision first: if the update fails on a version conflict, the transaction rolls
		// back and no orphan history is left behind. If they were separate, a crash between them
		// would file a revision for an edit that never happened.
		if revisionErr := s.deps.Repo.CreateRevision(txCtx, &revision); revisionErr != nil {
			return revisionErr
		}
		if updateErr := s.deps.Repo.Update(txCtx, &updated); updateErr != nil {
			return updateErr
		}
		return s.emit(txCtx, updated.ID, updated.SessionID, "edited", updated)
	})
	if err != nil {
		return nil, err
	}
	return &updated, nil
}

func (s *service) Delete(ctx context.Context, userID, entryID uuid.UUID) error {
	entry, err := s.owned(ctx, userID, entryID)
	if err != nil {
		return err
	}
	if err := s.deps.Repo.Delete(ctx, entryID); err != nil {
		return err
	}
	// The revisions go with the row, so the event is the only remaining trace that this entry
	// existed at all. That is what makes a deletion distinguishable from a note never written.
	return s.emit(ctx, entryID, entry.SessionID, "deleted", *entry)
}

// AttachMedia is FR-JOURNAL-06, and its EXIF rule is a blinding guard rather than hygiene.
//
// A trader's screenshot carries a capture timestamp, and often GPS. The session withheld the date
// from every payload for eight hundred bars; one screenshot saying "taken 14 March 2023, 08:31"
// undoes that, and the leak arrives from inside the trader's own upload — the only vector in this
// system where the client supplies bytes the server later hands back.
func (s *service) AttachMedia(ctx context.Context, userID, entryID uuid.UUID, upload []byte) (*domainjournal.Entry, error) {
	if s.deps.Media == nil {
		return nil, apperror.Newf("MEDIA_LINK_INVALID", "image uploads are not enabled on this deployment")
	}
	entry, err := s.owned(ctx, userID, entryID)
	if err != nil {
		return nil, err
	}
	limits := media.DefaultLimits
	if s.deps.MaxUploadBytes > 0 {
		limits.MaxBytes = s.deps.MaxUploadBytes
	}
	sanitized, err := media.Sanitize(upload, limits)
	switch {
	case errors.Is(err, media.ErrTooLarge):
		return nil, apperror.New("FILE_TOO_LARGE")
	case errors.Is(err, media.ErrTooManyPixels):
		return nil, apperror.Newf("FILE_TOO_LARGE", "image dimensions exceed the limit")
	case errors.Is(err, media.ErrUnsupportedType), errors.Is(err, media.ErrUndecodable):
		return nil, apperror.New("UNSUPPORTED_MEDIA_TYPE")
	case err != nil:
		return nil, apperror.Wrap(err, "INTERNAL_ERROR")
	}

	key := s.mediaKey(entryID, media.Extension(sanitized.ContentType))
	if err := s.deps.Media.Put(ctx, key, sanitized.Bytes, sanitized.ContentType); err != nil {
		return nil, apperror.Wrap(err, "INTERNAL_ERROR")
	}
	previous := entry.MediaKey

	updated := *entry
	updated.MediaKey = &key
	updated.UpdatedAt = s.deps.Clock()
	updated.Version = entry.Version + 1
	// The revision is filed for the same reason an edit files one: swapping the screenshot changes
	// what the entry shows, and the projector reads what the trader recorded at the time.
	revision := entry.Supersede(s.deps.Clock())
	err = s.deps.UOW.Do(ctx, func(txCtx context.Context) error {
		if revisionErr := s.deps.Repo.CreateRevision(txCtx, &revision); revisionErr != nil {
			return revisionErr
		}
		return s.deps.Repo.Update(txCtx, &updated)
	})
	if err != nil {
		// The blob is already written. Remove it rather than leave a file nothing points at: the
		// row is the index, so an unreferenced object is storage nobody can ever reach or bill for
		// knowingly.
		_ = s.deps.Media.Delete(ctx, key)
		return nil, err
	}
	// Only once the row points at the new image. Deleting the old one first would, on a failed
	// update, leave the entry referencing a file that no longer exists.
	if previous != nil && *previous != key {
		_ = s.deps.Media.Delete(ctx, *previous)
	}
	return &updated, nil
}

func (s *service) Media(ctx context.Context, key string) ([]byte, string, error) {
	if s.deps.Media == nil {
		return nil, "", apperror.New("MEDIA_LINK_INVALID")
	}
	content, contentType, err := s.deps.Media.Get(ctx, key)
	if err != nil {
		// Not-found and malformed collapse into one answer. A signed link that resolves to nothing
		// is indistinguishable from a forged one from the client's side, and distinguishing them
		// would confirm which keys exist.
		return nil, "", apperror.New("MEDIA_LINK_INVALID")
	}
	return content, contentType, nil
}

// mediaKey names the stored object. It derives from the entry id and the sniffed format and from
// nothing the uploader sent: a client-supplied filename is a path traversal waiting to happen, and
// it is also somewhere a trader could leak the window by calling their file eurusd-2023-03-14.png.
func (s *service) mediaKey(entryID uuid.UUID, extension string) string {
	if s.deps.Keys != nil {
		return s.deps.Keys(entryID, extension)
	}
	// A fresh suffix per upload rather than a stable name, so a replaced image cannot be served
	// from a cache or a CDN that still holds the old one under the same URL.
	return fmt.Sprintf("journal/%s-%s%s", entryID, uuid.NewString()[:8], extension)
}

func (s *service) Revisions(ctx context.Context, userID, entryID uuid.UUID) ([]domainjournal.Revision, error) {
	if _, err := s.owned(ctx, userID, entryID); err != nil {
		return nil, err
	}
	return s.deps.Repo.ListRevisions(ctx, entryID)
}

// owned loads an entry and proves it is the caller's. It checks the entry's own user_id rather
// than re-reading the session: the entry records who wrote it, and a session that changed hands
// (it cannot, today) must not carry somebody else's notes with it.
func (s *service) owned(ctx context.Context, userID, entryID uuid.UUID) (*domainjournal.Entry, error) {
	entry, err := s.deps.Repo.GetByID(ctx, entryID)
	if err != nil {
		return nil, err
	}
	if entry.UserID != userID {
		// Not-found rather than forbidden: confirming the id exists leaks that somebody else
		// wrote a note, and on which session.
		return nil, apperror.New("JOURNAL_NOT_FOUND")
	}
	return entry, nil
}

func validate(emotion *domainjournal.Emotion, conviction *int) error {
	var fields []apperror.FieldError
	if emotion != nil && !emotion.Valid() {
		fields = append(fields, apperror.FieldError{
			Field: "emotion", Rule: "oneof", Message: "emotion is not one of the recorded states",
		})
	}
	if conviction != nil && (*conviction < 1 || *conviction > 5) {
		fields = append(fields, apperror.FieldError{
			Field: "conviction", Rule: "range", Message: "conviction runs from 1 to 5",
		})
	}
	if len(fields) > 0 {
		return apperror.WithFields("VALIDATION_FAILED", fields)
	}
	return nil
}

// normalizeTags trims, lower-cases and de-duplicates. "Breakout", "breakout" and " breakout " are
// one tag: the post-mortem groups by them, and three spellings of one idea group by nothing.
func normalizeTags(values []string) ([]string, error) {
	if values == nil {
		return []string{}, nil
	}
	seen := make(map[string]struct{}, len(values))
	tags := make([]string, 0, len(values))
	for _, value := range values {
		tag := strings.ToLower(strings.TrimSpace(value))
		if tag == "" {
			continue
		}
		if len(tag) > 40 {
			return nil, apperror.WithFields("VALIDATION_FAILED", []apperror.FieldError{
				{Field: "tags", Rule: "max", Message: "a tag is at most 40 characters"},
			})
		}
		if _, duplicate := seen[tag]; duplicate {
			continue
		}
		seen[tag] = struct{}{}
		tags = append(tags, tag)
	}
	if len(tags) > MaxTags {
		return nil, apperror.WithFields("VALIDATION_FAILED", []apperror.FieldError{
			{Field: "tags", Rule: "max", Message: "at most 12 tags"},
		})
	}
	return tags, nil
}

func trimmed(value *string) *string {
	if value == nil {
		return nil
	}
	text := strings.TrimSpace(*value)
	if text == "" {
		return nil
	}
	return &text
}

// emit records the fact. The payload carries the bar index and the content, never a wall-clock
// position in the replay: a consumer that could date the session from an event stream would be the
// same leak as a bar carrying a timestamp.
func (s *service) emit(ctx context.Context, entryID, sessionID uuid.UUID, action string, entry domainjournal.Entry) error {
	if s.deps.Outbox == nil {
		return nil
	}
	record, err := event.New("journal", entryID, event.TypeJournalWritten, s.deps.Topic(event.TopicJournal), map[string]any{
		"entry_id": entryID, "session_id": sessionID, "user_id": entry.UserID,
		"action": action, "bar_index": entry.BarIndex, "version": entry.Version,
		"emotion": entry.Emotion, "conviction": entry.Conviction, "tags": entry.Tags,
		"has_thesis": entry.Thesis != nil, "has_note": entry.Note != nil,
	})
	if err != nil {
		return apperror.Wrap(err, "INTERNAL_ERROR")
	}
	return s.deps.Outbox.Create(ctx, record)
}

// passthroughUOW runs the function without a transaction. It exists so a test can construct the
// service without a database; the composition root always wires the real one.
type passthroughUOW struct{}

func (passthroughUOW) Do(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) }
