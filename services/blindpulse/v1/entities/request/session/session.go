package sessionrequest

type Start struct {
	AccountID string `json:"account_id" binding:"required,uuid"`
	FeedID    string `json:"feed_id" binding:"required,uuid"`
	Timeframe string `json:"timeframe" binding:"omitempty,oneof=1m 5m 15m 30m 1h 4h 1d 1w"`
}

type Step struct {
	// Count advances the cursor. It stays signed, and negatives stay inside the accepted range,
	// even though the cursor is forward-only: a negative count reaches the domain and comes back
	// as CURSOR_IS_FORWARD_ONLY, which tells the caller what the rule is. Rejecting it here would
	// answer a client that tried to rewind with a generic validation failure instead.
	Count int `json:"count" binding:"required,min=-500,max=500"`
}

type Speed struct {
	// A decimal string, like every other number crossing this boundary: 0.5 through a JSON float
	// is fine, but the rule holds everywhere so nobody has to remember where it does not.
	Speed string `json:"speed" binding:"required,numeric"`
}

type Timeframe struct {
	Timeframe string `json:"timeframe" binding:"required,oneof=1m 5m 15m 30m 1h 4h 1d 1w"`
}
