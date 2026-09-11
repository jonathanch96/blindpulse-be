// Package sessionresponse carries the trader-facing view of a replay session.
//
// Like the feed response package, this is a blinding boundary: no absolute timestamp, no feed
// window, no instrument. A session's position is always an index.
package sessionresponse

import (
	"time"

	domainsession "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/session"
)

type Session struct {
	ID        string `json:"id"`
	AccountID string `json:"account_id"`
	FeedID    string `json:"feed_id"`
	Status    string `json:"status"`
	Timeframe string `json:"timeframe"`
	Speed     string `json:"speed"`

	// Where the trader is, which is also the furthest bar released: the cursor only moves forward,
	// so those two can never differ. The client may read bars up to here and no further.
	CursorIndex int `json:"cursor_index"`
	// The same position counted from one rather than zero, for the PRD's "142 / 500 bars scanned"
	// readout. Derived, not stored — it is sent so the client never has to know the off-by-one.
	BarsScanned int `json:"bars_scanned"`
	TotalBars   int `json:"total_bars"`

	// Wall-clock timestamps about the *session*, not about the market window. When the trader sat
	// down says nothing about when the data is from.
	StartedAt time.Time  `json:"started_at"`
	ClosedAt  *time.Time `json:"closed_at"`
}

func FromDomain(entity domainsession.Session, totalBars int) Session {
	scanned, total := entity.Progress(totalBars)
	return Session{
		ID: entity.ID.String(), AccountID: entity.AccountID.String(), FeedID: entity.FeedID.String(),
		Status: string(entity.Status), Timeframe: string(entity.Timeframe), Speed: entity.Speed.String(),
		CursorIndex: entity.CursorIndex,
		BarsScanned: scanned, TotalBars: total,
		StartedAt: entity.StartedAt, ClosedAt: entity.ClosedAt,
	}
}

func FromDomains(entities []domainsession.Session, totalBars int) []Session {
	sessions := make([]Session, 0, len(entities))
	for _, entity := range entities {
		sessions = append(sessions, FromDomain(entity, totalBars))
	}
	return sessions
}
