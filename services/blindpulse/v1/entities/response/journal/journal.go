// Package journalresponse carries the trader-facing view of a journal entry.
//
// This is a blinding boundary like the feed and session response packages, and the field it is
// careful about is the one that looks harmless: an entry anchors to a **bar index**. CreatedAt is
// present because a trader wants to know when they wrote a note, and it is a wall-clock instant in
// *their* session, which says nothing about when the market data is from. What must never appear is
// the bar's own timestamp.
package journalresponse

import (
	"time"

	"github.com/google/uuid"
	domainjournal "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/journal"
)

type Entry struct {
	ID        string  `json:"id"`
	SessionID string  `json:"session_id"`
	TradeID   *string `json:"trade_id"`
	// BarIndex is where on the chart this belongs. There is no bar timestamp beside it, and there
	// is no field from which one could be derived.
	BarIndex   int      `json:"bar_index"`
	Thesis     *string  `json:"thesis"`
	Note       *string  `json:"note"`
	Emotion    *string  `json:"emotion"`
	Conviction *int     `json:"conviction"`
	Tags       []string `json:"tags"`
	MediaURL   *string  `json:"media_url"`
	// Version is shown because it is the trader's own signal that a note has been edited, and the
	// post-mortem says so rather than presenting a revised thesis as the original.
	Version   int       `json:"version"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func FromDomain(entity domainjournal.Entry) Entry {
	return Entry{
		ID: entity.ID.String(), SessionID: entity.SessionID.String(), TradeID: idString(entity.TradeID),
		BarIndex: entity.BarIndex, Thesis: entity.Thesis, Note: entity.Note,
		Emotion: emotionString(entity.Emotion), Conviction: entity.Conviction,
		Tags: tags(entity.Tags), Version: entity.Version,
		CreatedAt: entity.CreatedAt, UpdatedAt: entity.UpdatedAt,
	}
}

func FromDomains(entities []domainjournal.Entry) []Entry {
	list := make([]Entry, 0, len(entities))
	for _, entity := range entities {
		list = append(list, FromDomain(entity))
	}
	return list
}

// Revision is what an entry said before an edit. The trader can read their own history; so can the
// discipline projector, which is the reason it is kept.
type Revision struct {
	Version      int       `json:"version"`
	BarIndex     int       `json:"bar_index"`
	Thesis       *string   `json:"thesis"`
	Note         *string   `json:"note"`
	Emotion      *string   `json:"emotion"`
	Conviction   *int      `json:"conviction"`
	Tags         []string  `json:"tags"`
	SupersededAt time.Time `json:"superseded_at"`
}

func RevisionFromDomain(entity domainjournal.Revision) Revision {
	return Revision{
		Version: entity.Version, BarIndex: entity.BarIndex, Thesis: entity.Thesis, Note: entity.Note,
		Emotion: emotionString(entity.Emotion), Conviction: entity.Conviction,
		Tags: tags(entity.Tags), SupersededAt: entity.SupersededAt,
	}
}

func RevisionsFromDomain(entities []domainjournal.Revision) []Revision {
	list := make([]Revision, 0, len(entities))
	for _, entity := range entities {
		list = append(list, RevisionFromDomain(entity))
	}
	return list
}

func emotionString(value *domainjournal.Emotion) *string {
	if value == nil {
		return nil
	}
	text := string(*value)
	return &text
}

func idString(value *uuid.UUID) *string {
	if value == nil {
		return nil
	}
	text := value.String()
	return &text
}

// tags never serializes as null: the client maps over it.
func tags(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
