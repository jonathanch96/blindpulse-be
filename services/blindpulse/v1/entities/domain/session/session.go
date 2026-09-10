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

// Session is the aggregate. Two indices matter and they are not the same thing:
//
//   - CursorIndex is where the trader is looking. Stepping backward moves it.
//   - RevealedIndex is the high-water mark: the furthest bar this session has ever released.
//
// The distinction is what makes candle-by-candle review safe. Without it, a trader could step
// back and act on a bar whose outcome they had already seen, which is exactly the hindsight the
// product removes. Reads are bounded by RevealedIndex, and order fills (Sprint 04) resolve at
// RevealedIndex — never at a rewound cursor.
type Session struct {
	ID            uuid.UUID
	UserID        uuid.UUID
	AccountID     uuid.UUID
	FeedID        uuid.UUID
	Status        Status
	Timeframe     market.Timeframe
	Speed         decimal.Decimal
	CursorIndex   int
	RevealedIndex int
	CursorAt      *time.Time

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
	return index >= 0 && index <= s.RevealedIndex
}

// Progress is the "142 / 500 bars scanned" readout. It reports the revealed edge rather than the
// view cursor, because that is what the trader has actually consumed of the feed.
func (s Session) Progress(totalBars int) (scanned, total int) {
	return s.RevealedIndex + 1, totalBars
}

// State is the hot subset kept in Redis between checkpoints. It is a cache: everything here can be
// rebuilt from PostgreSQL, and losing it costs at most the bars since the last checkpoint.
type State struct {
	SessionID     uuid.UUID `json:"session_id"`
	Status        Status    `json:"status"`
	Timeframe     string    `json:"timeframe"`
	Speed         string    `json:"speed"`
	CursorIndex   int       `json:"cursor_index"`
	RevealedIndex int       `json:"revealed_index"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func (s Session) Snapshot() State {
	return State{
		SessionID: s.ID, Status: s.Status, Timeframe: string(s.Timeframe), Speed: s.Speed.String(),
		CursorIndex: s.CursorIndex, RevealedIndex: s.RevealedIndex, UpdatedAt: s.UpdatedAt,
	}
}
