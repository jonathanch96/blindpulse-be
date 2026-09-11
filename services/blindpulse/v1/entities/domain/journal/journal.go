// Package journal holds what the trader was thinking while they could not see the answer.
//
// The rule this package exists to enforce: an entry is anchored to a **bar index**, never to a
// clock. A note written during a paused replay belongs to the moment on the chart, and a session
// walks eight market days in three minutes of wall clock — so a write timestamp would put every
// note in the wrong place in the story, and would also date the window, which is a BR-01 leak.
package journal

import (
	"time"

	"github.com/google/uuid"
)

// Emotion is the trader's own label for their state at the bar. It is a closed set because the
// discipline projector counts them; free text would make "anxious" and "nervous" two feelings.
type Emotion string

const (
	EmotionCalm       Emotion = "calm"
	EmotionConfident  Emotion = "confident"
	EmotionAnxious    Emotion = "anxious"
	EmotionGreedy     Emotion = "greedy"
	EmotionFearful    Emotion = "fearful"
	EmotionFrustrated Emotion = "frustrated"
	EmotionBored      Emotion = "bored"
)

func (e Emotion) Valid() bool {
	switch e {
	case EmotionCalm, EmotionConfident, EmotionAnxious, EmotionGreedy,
		EmotionFearful, EmotionFrustrated, EmotionBored:
		return true
	}
	return false
}

// Entry is one journal note.
//
// TradeID is optional and, until Sprint 04 exists, always nil: there is nothing to attach a note
// to yet. That is not a placeholder — a note about the market rather than about a fill is a normal
// entry and will stay unattached after execution lands.
type Entry struct {
	ID        uuid.UUID
	SessionID uuid.UUID
	TradeID   *uuid.UUID
	UserID    uuid.UUID
	// BarIndex is where on the chart this note belongs. The service refuses an index past the
	// session's cursor: a trader cannot annotate a bar they have not been shown.
	BarIndex   int
	Thesis     *string
	Note       *string
	Emotion    *Emotion
	Conviction *int
	Tags       []string
	MediaKey   *string
	CreatedAt  time.Time
	UpdatedAt  time.Time
	Version    int
}

// Revision is the content an entry had before an edit replaced it. Version is the version that
// content *was*, so revision 1 is what the entry said before it was first changed.
type Revision struct {
	ID           uuid.UUID
	EntryID      uuid.UUID
	Version      int
	BarIndex     int
	Thesis       *string
	Note         *string
	Emotion      *Emotion
	Conviction   *int
	Tags         []string
	SupersededAt time.Time
}

// Supersede snapshots the entry as it stands, before it is changed.
func (e Entry) Supersede(at time.Time) Revision {
	return Revision{
		ID: uuid.New(), EntryID: e.ID, Version: e.Version, BarIndex: e.BarIndex,
		Thesis: e.Thesis, Note: e.Note, Emotion: e.Emotion, Conviction: e.Conviction,
		Tags: e.Tags, SupersededAt: at,
	}
}

// Empty reports an entry with nothing in it. A row carrying only a bar index is not a journal
// entry; it is a click, and storing it would put a blank card on the post-mortem.
func (e Entry) Empty() bool {
	return blank(e.Thesis) && blank(e.Note) && e.Emotion == nil && e.Conviction == nil && len(e.Tags) == 0
}

func blank(value *string) bool { return value == nil || *value == "" }
