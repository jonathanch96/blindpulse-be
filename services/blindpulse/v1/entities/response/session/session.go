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

	// Where the trader is looking.
	CursorIndex int `json:"cursor_index"`
	// The furthest bar released. Never less than the cursor, and it only ever grows — the client
	// needs it to know which bars it is allowed to have.
	RevealedIndex int `json:"revealed_index"`
	// "142 / 500 bars scanned" comes from these two.
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
		CursorIndex: entity.CursorIndex, RevealedIndex: entity.RevealedIndex,
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
