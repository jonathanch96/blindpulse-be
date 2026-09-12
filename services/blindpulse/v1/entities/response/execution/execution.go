// Package executionresponse is the trader-facing view of orders, positions and risk.
//
// What these types omit is load-bearing (NFR-05, SP4-1):
//
//   - No bar timestamps. An order row stores placed_bar_at, because the drawdown's day boundary is a
//     market day derived from real instants; no response here has a field for it.
//   - No day boundary in the risk readout. Not the window, not its ordinal, not a countdown to the
//     reset. A trader who could see where the resets fall would see a two-day gap every five days,
//     and that is a weekend — which rules out crypto outright and narrows the rest.
//
// The only times present are CreatedAt and the like: when the *trader* acted, in their own wall
// clock, which says nothing about when the bar traded. leak_test.go is what keeps that true.
package executionresponse

import (
	"time"

	executiondomain "github.com/jblabs/blindpulse-be/services/blindpulse/v1/domain/execution"
	domainexec "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/execution"
	"github.com/shopspring/decimal"
)

type Order struct {
	ID        string `json:"id"`
	SessionID string `json:"session_id"`
	ClientKey string `json:"client_key"`
	Side      string `json:"side"`
	Type      string `json:"type"`
	Quantity  string `json:"quantity"`
	// Decimal strings out as well as in, for the same reason: a float in the response would hand the
	// client a different number than the server holds.
	LimitPrice *string `json:"limit_price,omitempty"`
	StopLoss   string  `json:"stop_loss"`
	TakeProfit *string `json:"take_profit,omitempty"`
	RiskReward *string `json:"risk_reward,omitempty"`
	// RiskAmount is what this order puts at risk in account currency — the number the dock shows
	// before the trader commits, and the one the gate measured.
	RiskAmount *string `json:"risk_amount,omitempty"`
	Status     string  `json:"status"`
	// RejectionCode names the rule that refused the order (BR-09). Present on a rejected row,
	// which is stored rather than discarded: what the trader *tried* to do is the discipline index's
	// richest input.
	RejectionCode *string `json:"rejection_code,omitempty"`
	// PlacedBarIndex is where the trader was when they pressed the button. An index, never a time.
	PlacedBarIndex int     `json:"placed_bar_index"`
	FilledBarIndex *int    `json:"filled_bar_index,omitempty"`
	FilledPrice    *string `json:"filled_price,omitempty"`
	Slippage       string  `json:"slippage"`
	// CreatedAt is when the trader acted, in their own clock. Not when the bar is from.
	CreatedAt time.Time `json:"created_at"`
	Version   int       `json:"version"`
}

type Trade struct {
	ID           string  `json:"id"`
	SessionID    string  `json:"session_id"`
	EntryOrderID string  `json:"entry_order_id"`
	Side         string  `json:"side"`
	Quantity     string  `json:"quantity"`
	EntryPrice   string  `json:"entry_price"`
	ExitPrice    *string `json:"exit_price,omitempty"`
	StopLoss     string  `json:"stop_loss"`
	// InitialStopLoss is what 1R was measured against, and it does not move when the stop does. The
	// dock needs both to show "stop at breakeven, risking 0 of the original 1R".
	InitialStopLoss string  `json:"initial_stop_loss"`
	TakeProfit      *string `json:"take_profit,omitempty"`
	Status          string  `json:"status"`
	ExitReason      *string `json:"exit_reason,omitempty"`
	BehaviorTag     *string `json:"behavior_tag,omitempty"`
	RealizedPnL     *string `json:"realized_pnl,omitempty"`
	RMultiple       *string `json:"r_multiple,omitempty"`
	// The excursions separate "the idea was wrong" from "the idea was right and the stop was in the
	// wrong place", which is the most useful thing a post-mortem can tell a trader.
	MaxAdverseExcursion   *string `json:"max_adverse_excursion,omitempty"`
	MaxFavorableExcursion *string `json:"max_favorable_excursion,omitempty"`
	OpenedBarIndex        int     `json:"opened_bar_index"`
	ClosedBarIndex        *int    `json:"closed_bar_index,omitempty"`
	// BarsHeld is a count, which is the blinded way to express duration: it is how long the trader
	// held in bars they saw, and carries no clue what one bar was worth in minutes of 2023.
	BarsHeld *int      `json:"bars_held,omitempty"`
	OpenedAt time.Time `json:"opened_at"`
	Version  int       `json:"version"`
}

// Risk is the compliance panel.
//
// RoomRemainingPct is a fraction of the allowance rather than a distance in currency and rather than
// a time: it says how close the trader is to the gate without saying anything about the window it is
// measured over.
type Risk struct {
	Halted           bool   `json:"halted"`
	RoomRemainingPct string `json:"room_remaining_pct"`
	OpenPositions    int    `json:"open_positions"`
	Balance          string `json:"balance"`
	Equity           string `json:"equity"`
	CommittedMargin  string `json:"committed_margin"`
}

func OrderFromDomain(entity domainexec.Order) Order {
	return Order{
		ID: entity.ID.String(), SessionID: entity.SessionID.String(), ClientKey: entity.ClientKey,
		Side: string(entity.Side), Type: string(entity.Type), Quantity: entity.Quantity.String(),
		LimitPrice: money(entity.LimitPrice), StopLoss: entity.StopLoss.String(),
		TakeProfit: money(entity.TakeProfit), RiskReward: money(entity.RiskReward),
		RiskAmount: money(entity.RiskAmount), Status: string(entity.Status),
		RejectionCode: entity.RejectionCode, PlacedBarIndex: entity.PlacedBarIndex,
		FilledBarIndex: entity.FilledBarIndex, FilledPrice: money(entity.FilledPrice),
		Slippage: entity.Slippage.String(), CreatedAt: entity.CreatedAt, Version: entity.Version,
	}
}

func OrdersFromDomain(entities []domainexec.Order) []Order {
	list := make([]Order, 0, len(entities))
	for _, entity := range entities {
		list = append(list, OrderFromDomain(entity))
	}
	return list
}

func TradeFromDomain(entity domainexec.Trade) Trade {
	var reason *string
	if entity.ExitReason != nil {
		value := string(*entity.ExitReason)
		reason = &value
	}
	return Trade{
		ID: entity.ID.String(), SessionID: entity.SessionID.String(),
		EntryOrderID: entity.EntryOrderID.String(), Side: string(entity.Side),
		Quantity: entity.Quantity.String(), EntryPrice: entity.EntryPrice.String(),
		ExitPrice: money(entity.ExitPrice), StopLoss: entity.StopLoss.String(),
		InitialStopLoss: initialStop(entity).String(),
		TakeProfit:      money(entity.TakeProfit), Status: entity.Status,
		ExitReason: reason, BehaviorTag: entity.BehaviorTag,
		RealizedPnL: money(entity.RealizedPnL), RMultiple: money(entity.RMultiple),
		MaxAdverseExcursion:   money(entity.MaxAdverseExcursion),
		MaxFavorableExcursion: money(entity.MaxFavorableExcursion),
		OpenedBarIndex:        entity.OpenedBarIndex, ClosedBarIndex: entity.ClosedBarIndex,
		BarsHeld: entity.BarsHeld, OpenedAt: entity.OpenedAt, Version: entity.Version,
	}
}

func TradesFromDomain(entities []domainexec.Trade) []Trade {
	list := make([]Trade, 0, len(entities))
	for _, entity := range entities {
		list = append(list, TradeFromDomain(entity))
	}
	return list
}

// RiskFromDomain converts the risk readout.
//
// Field by field, and deliberately not by embedding the domain state: embedding would mean a field
// added there — a day ordinal, a bars-to-reset — appeared on the wire without anyone choosing it,
// which is precisely the SP4-1 leak. Adding one here is a decision somebody has to make on purpose,
// and leak_test.go argues with them about it.
func RiskFromDomain(state executiondomain.RiskState) Risk {
	return Risk{
		Halted: state.Halted, RoomRemainingPct: state.RoomRemainingPct.String(),
		OpenPositions: state.OpenPositions, Balance: state.Balance.String(),
		Equity: state.Equity.String(), CommittedMargin: state.CommittedMargin.String(),
	}
}

// money renders an optional decimal as an optional string, so a nil stays absent from the JSON rather
// than becoming "0" — an unset take-profit and a take-profit at zero are different orders.
func money(value *decimal.Decimal) *string {
	if value == nil {
		return nil
	}
	rendered := value.String()
	return &rendered
}

// initialStop is the stop the position was sized on, falling back to the live one for a row written
// before the column existed — the same fallback Trade.Risk makes, so the level the client sees and
// the denominator of the R-multiple it is shown can never disagree.
func initialStop(entity domainexec.Trade) decimal.Decimal {
	if entity.InitialStopLoss.IsZero() {
		return entity.StopLoss
	}
	return entity.InitialStopLoss
}
