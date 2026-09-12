// Package executionrequest is the order intake surface.
package executionrequest

// Place is one order as the trader described it.
//
// Quantity and RiskPct are alternatives and the server refuses both rather than picking one: either
// precedence would silently ignore something the trader typed, and sizing is the thing this product
// exists to teach. The validator cannot express "exactly one of these", so it is checked in the
// domain where the refusal can say why.
type Place struct {
	// ClientKey makes a retry a no-op rather than a second position. The client generates it per
	// submission, not per session.
	ClientKey string `json:"client_key" binding:"required,max=100"`
	Side      string `json:"side" binding:"required,oneof=buy sell"`
	Type      string `json:"type" binding:"required,oneof=market limit stop"`
	// Decimal strings, not numbers. A price or a size through a JSON float is a price or a size
	// rounded by the parser, and the whole account is computed from these.
	Quantity *string `json:"quantity,omitempty" binding:"omitempty,numeric"`
	RiskPct  *string `json:"risk_pct,omitempty" binding:"omitempty,numeric"`
	// LimitPrice is where a resting order waits. Required for limit and stop, refused for market.
	LimitPrice *string `json:"limit_price,omitempty" binding:"omitempty,numeric"`
	// StopLoss is required and not a pointer: BR-03 is that no entry exists without a hard stop, so
	// an order without one cannot even be expressed here.
	StopLoss   string  `json:"stop_loss" binding:"required,numeric"`
	TakeProfit *string `json:"take_profit,omitempty" binding:"omitempty,numeric"`
}

// Amend moves an open position's levels. It cannot move the entry — that already happened.
type Amend struct {
	StopLoss   *string `json:"stop_loss,omitempty" binding:"omitempty,numeric"`
	TakeProfit *string `json:"take_profit,omitempty" binding:"omitempty,numeric"`
}

// Close exits a position. Fraction absent means all of it.
type Close struct {
	Fraction *string `json:"fraction,omitempty" binding:"omitempty,numeric"`
}
