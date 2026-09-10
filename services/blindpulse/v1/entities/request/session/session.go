package sessionrequest

type Start struct {
	AccountID string `json:"account_id" binding:"required,uuid"`
	FeedID    string `json:"feed_id" binding:"required,uuid"`
	Timeframe string `json:"timeframe" binding:"omitempty,oneof=1m 5m 15m 30m 1h 4h 1d 1w"`
}

type Step struct {
	// Count is signed: positive advances and may release new bars, negative rewinds and never
	// does. One field rather than a direction plus a magnitude, so "step back 3" cannot be
	// expressed two different ways.
	Count int `json:"count" binding:"required,min=-500,max=500"`
}

type Seek struct {
	BarIndex int `json:"bar_index" binding:"min=0"`
}

type Speed struct {
	// A decimal string, like every other number crossing this boundary: 0.5 through a JSON float
	// is fine, but the rule holds everywhere so nobody has to remember where it does not.
	Speed string `json:"speed" binding:"required,numeric"`
}

type Timeframe struct {
	Timeframe string `json:"timeframe" binding:"required,oneof=1m 5m 15m 30m 1h 4h 1d 1w"`
}
