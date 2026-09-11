// Package session holds the replay session aggregate: a deterministic walk over one blinded feed.
//
// The rule this package exists to enforce (BR-02): the server decides what time it is. A client
// renders what it is given and asks to move; it never asserts where the cursor is, and it can
// never obtain a bar the session has not released.
package session

import (
	"time"

	"github.com/google/uuid"
	"github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/market"
	"github.com/shopspring/decimal"
)

type Status string

const (
	StatusOpen      Status = "open"
	StatusPaused    Status = "paused"
	StatusClosed    Status = "closed"
	StatusAbandoned Status = "abandoned"
)

func (s Status) Live() bool { return s == StatusOpen || s == StatusPaused }

// Session is the aggregate.
//
// One index, and it only ever moves forward. CursorIndex is both where the trader is looking and
// the furthest bar this session has released, because those cannot differ: a trader cannot go back.
// Once a bar is stepped past it is history, the way it is on a live chart, and a trader who wants a
// different setup randomizes a new feed rather than rewinding this one.
//
// Sprint 03 carried a second index, RevealedIndex, precisely so that a *rewound* trader could not
// act on a bar whose outcome they had already seen. Removing the ability to rewind removes the
// premise, and with it the need to keep two numbers that can never differ.
type Session struct {
	ID          uuid.UUID
	UserID      uuid.UUID
	AccountID   uuid.UUID
	FeedID      uuid.UUID
	Status      Status
	Timeframe   market.Timeframe
	Speed       decimal.Decimal
	CursorIndex int
	CursorAt    *time.Time

	// Seed makes the session reproducible: the same feed and seed produce the same slippage draws,
	// so a disputed fill can be recomputed rather than argued about (BR-10).
	Seed int64

	LastCheckpointIndex int
	StartedAt           time.Time
	LastActiveAt        time.Time
	ClosedAt            *time.Time
	RevealedAt          *time.Time
	RootHash            *string
	CreatedAt           time.Time
	UpdatedAt           time.Time
	Version             int
}

// CanRead reports whether a bar index has been released to this session.
func (s Session) CanRead(index int) bool {
	return index >= 0 && index <= s.CursorIndex
}

// Progress is the "142 / 500 bars scanned" readout.
func (s Session) Progress(totalBars int) (scanned, total int) {
	return s.CursorIndex + 1, totalBars
}

// State is the hot subset kept in Redis between checkpoints. It is a cache: everything here can be
// rebuilt from PostgreSQL, and losing it costs at most the bars since the last checkpoint.
type State struct {
	SessionID   uuid.UUID `json:"session_id"`
	Status      Status    `json:"status"`
	Timeframe   string    `json:"timeframe"`
	Speed       string    `json:"speed"`
	CursorIndex int       `json:"cursor_index"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (s Session) Snapshot() State {
	return State{
		SessionID: s.ID, Status: s.Status, Timeframe: string(s.Timeframe), Speed: s.Speed.String(),
		CursorIndex: s.CursorIndex, UpdatedAt: s.UpdatedAt,
	}
}
