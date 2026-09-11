package drawingrequest

import "encoding/json"

// Create stores one annotation.
//
// Payload is raw on purpose: the server does not model the toolkit, so binding it to a struct would
// make every new tool a change here. It is checked in the domain for size, validity and — the one
// that matters — that it carries no date.
type Create struct {
	Kind      string `json:"kind" binding:"required,max=40"`
	Timeframe string `json:"timeframe" binding:"required,oneof=1m 5m 15m 30m 1h 4h 1d 1w"`
	// Pointer for the same reason as a journal entry's: a drawing anchored to bar 0 is legitimate,
	// and with a plain int `required` would read it as missing.
	CreatedBarIndex *int `json:"created_bar_index" binding:"required,min=0"`
	// swaggertype tells the doc generator this is an arbitrary JSON object. It cannot infer that
	// from json.RawMessage, and leaving it out fails the build rather than producing vague docs.
	Payload json.RawMessage `json:"payload" binding:"required" swaggertype:"object"`
}

type Update struct {
	Payload json.RawMessage `json:"payload" binding:"required" swaggertype:"object"`
}
