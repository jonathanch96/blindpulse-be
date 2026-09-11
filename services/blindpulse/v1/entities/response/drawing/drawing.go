// Package drawingresponse carries the trader-facing view of a chart annotation.
//
// The payload goes back out exactly as it came in, which is why the write path checks it: this is
// the one response in the system whose bytes a client chose. Everything else here is an index or a
// duration — no instant, and nothing a date could be derived from.
package drawingresponse

import (
	"encoding/json"
	"time"

	domaindrawing "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/drawing"
)

type Drawing struct {
	ID        string `json:"id"`
	SessionID string `json:"session_id"`
	Kind      string `json:"kind"`
	// Timeframe is a duration like "15m", never a date. It is part of the drawing's identity: an
	// anchor only means something against the timeframe it was placed on.
	Timeframe       string          `json:"timeframe"`
	CreatedBarIndex int             `json:"created_bar_index"`
	Payload         json.RawMessage `json:"payload" swaggertype:"object"`
	Version         int             `json:"version"`
	// When the trader drew it, in their own session. Not when the bar is from.
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func FromDomain(entity domaindrawing.Drawing) Drawing {
	return Drawing{
		ID: entity.ID.String(), SessionID: entity.SessionID.String(), Kind: string(entity.Kind),
		Timeframe: entity.Timeframe, CreatedBarIndex: entity.CreatedBarIndex,
		Payload: entity.Payload, Version: entity.Version,
		CreatedAt: entity.CreatedAt, UpdatedAt: entity.UpdatedAt,
	}
}

func FromDomains(entities []domaindrawing.Drawing) []Drawing {
	list := make([]Drawing, 0, len(entities))
	for _, entity := range entities {
		list = append(list, FromDomain(entity))
	}
	return list
}
