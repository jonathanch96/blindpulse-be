package journal_entries

import (
	domainjournal "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/journal"
	"github.com/lib/pq"
)

func fromDomain(entity domainjournal.Entry) JournalEntry {
	return JournalEntry{
		ID: entity.ID, SessionID: entity.SessionID, TradeID: entity.TradeID, UserID: entity.UserID,
		BarIndex: entity.BarIndex, Thesis: entity.Thesis, Note: entity.Note,
		Emotion: emotionString(entity.Emotion), Conviction: entity.Conviction,
		Tags: pq.StringArray(entity.Tags), MediaKey: entity.MediaKey,
		CreatedAt: entity.CreatedAt, UpdatedAt: entity.UpdatedAt, Version: entity.Version,
	}
}

func toDomain(model JournalEntry) domainjournal.Entry {
	return domainjournal.Entry{
		ID: model.ID, SessionID: model.SessionID, TradeID: model.TradeID, UserID: model.UserID,
		BarIndex: model.BarIndex, Thesis: model.Thesis, Note: model.Note,
		Emotion: emotionValue(model.Emotion), Conviction: model.Conviction,
		Tags: tags(model.Tags), MediaKey: model.MediaKey,
		CreatedAt: model.CreatedAt, UpdatedAt: model.UpdatedAt, Version: model.Version,
	}
}

func toDomains(models []JournalEntry) []domainjournal.Entry {
	entries := make([]domainjournal.Entry, 0, len(models))
	for _, model := range models {
		entries = append(entries, toDomain(model))
	}
	return entries
}

func revisionFromDomain(entity domainjournal.Revision) JournalEntryRevision {
	return JournalEntryRevision{
		ID: entity.ID, EntryID: entity.EntryID, Version: entity.Version, BarIndex: entity.BarIndex,
		Thesis: entity.Thesis, Note: entity.Note, Emotion: emotionString(entity.Emotion),
		Conviction: entity.Conviction, Tags: pq.StringArray(entity.Tags),
		SupersededAt: entity.SupersededAt,
	}
}

func revisionToDomain(model JournalEntryRevision) domainjournal.Revision {
	return domainjournal.Revision{
		ID: model.ID, EntryID: model.EntryID, Version: model.Version, BarIndex: model.BarIndex,
		Thesis: model.Thesis, Note: model.Note, Emotion: emotionValue(model.Emotion),
		Conviction: model.Conviction, Tags: tags(model.Tags), SupersededAt: model.SupersededAt,
	}
}

func revisionsToDomain(models []JournalEntryRevision) []domainjournal.Revision {
	revisions := make([]domainjournal.Revision, 0, len(models))
	for _, model := range models {
		revisions = append(revisions, revisionToDomain(model))
	}
	return revisions
}

func emotionString(value *domainjournal.Emotion) *string {
	if value == nil {
		return nil
	}
	text := string(*value)
	return &text
}

func emotionValue(value *string) *domainjournal.Emotion {
	if value == nil {
		return nil
	}
	emotion := domainjournal.Emotion(*value)
	return &emotion
}

// tags normalizes a NULL array to an empty slice. The column is NOT NULL so this should not arise,
// but a nil slice serializes as `null` where the response type promises a list, and a client that
// maps over it would break on the one row that was written before the default existed.
func tags(values pq.StringArray) []string {
	if values == nil {
		return []string{}
	}
	return []string(values)
}
