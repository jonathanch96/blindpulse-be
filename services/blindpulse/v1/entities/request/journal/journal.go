package journalrequest

// Write creates an entry. Everything except the bar index is optional individually — the domain
// refuses an entry that is empty overall, which is the rule that actually matters and cannot be
// expressed as a per-field binding.
type Write struct {
	// BarIndex is required and validated against the session's cursor in the domain, not here: a
	// bound this layer could enforce would be a constant, and the real bound is the session's.
	//
	// It uses a pointer so that bar 0 — a note on the very first released candle — is
	// distinguishable from an absent field. With a plain int and `required`, zero is absent.
	BarIndex   *int     `json:"bar_index" binding:"required,min=0"`
	TradeID    string   `json:"trade_id" binding:"omitempty,uuid"`
	Thesis     string   `json:"thesis" binding:"omitempty,max=4000"`
	Note       string   `json:"note" binding:"omitempty,max=4000"`
	Emotion    string   `json:"emotion" binding:"omitempty,oneof=calm confident anxious greedy fearful frustrated bored"`
	Conviction *int     `json:"conviction" binding:"omitempty,min=1,max=5"`
	Tags       []string `json:"tags" binding:"omitempty,max=12,dive,max=40"`
}

// Edit changes an entry. Every field is a pointer because absent and empty mean different things:
// absent leaves the field alone, and an explicit empty string clears it. A plain string could only
// express one of those.
type Edit struct {
	Thesis     *string  `json:"thesis" binding:"omitempty,max=4000"`
	Note       *string  `json:"note" binding:"omitempty,max=4000"`
	Emotion    *string  `json:"emotion" binding:"omitempty,oneof=calm confident anxious greedy fearful frustrated bored"`
	Conviction *int     `json:"conviction" binding:"omitempty,min=1,max=5"`
	Tags       []string `json:"tags" binding:"omitempty,max=12,dive,max=40"`
}
