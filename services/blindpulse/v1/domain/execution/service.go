// Package execution is the simulator's write side: orders in, fills out, and the gate between.
//
// The rule this package exists to enforce (BR-03, BR-04, BR-05): **the account's own rules are what
// stop the trader, and they are enforced here.** A gate that lives in the client is advice; a
// hand-rolled API call would walk straight past it. Every refusal is a specific code so the dock
// can say what failed, and no refusal quietly corrects the order into something acceptable.
package execution

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jblabs/blindpulse-be/pkg/apperror"
	domainaccount "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/account"
	domainexec "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/execution"
	domainfeed "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/feed"
	domainsession "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/session"
	"github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/event"
	"github.com/shopspring/decimal"
)

func NewService(deps Dependencies) Service {
	if deps.Now == nil {
		deps.Now = func() time.Time { return time.Now().UTC() }
	}
	if deps.UOW == nil {
		deps.UOW = passthroughUOW{}
	}
	if !deps.Conditions.TickSize.IsPositive() && deps.Conditions.SpreadTicks == 0 {
		deps.Conditions = domainexec.DefaultConditions
	}
	return &service{deps: deps}
}

func (s *service) Place(ctx context.Context, userID, sessionID uuid.UUID, in PlaceInput) (*domainexec.Order, error) {
	replay, err := s.deps.Sessions.OwnedSession(ctx, userID, sessionID)
	if err != nil {
		return nil, err
	}
	if in.ClientKey == "" {
		return nil, apperror.WithFields("VALIDATION_FAILED", []apperror.FieldError{
			{Field: "client_key", Rule: "required", Message: "a client key is required so a retry cannot double-fill"},
		})
	}
	// Idempotency's fast path. The unique index is the real guarantee — two concurrent submits race
	// past this and exactly one wins there — but answering a retry without touching the gate keeps
	// a flaky network from filling the rejection log with duplicates.
	if existing, err := s.deps.Orders.GetByClientKey(ctx, sessionID, in.ClientKey); err == nil && existing != nil {
		return nil, apperror.New("DUPLICATE_ORDER")
	}
	if !in.Side.Valid() || !in.Type.Valid() {
		return nil, apperror.WithFields("VALIDATION_FAILED", []apperror.FieldError{
			{Field: "side", Rule: "oneof", Message: "side is buy or sell; type is market, limit or stop"},
		})
	}
	if in.Type.Resting() && in.LimitPrice == nil {
		return nil, apperror.WithFields("VALIDATION_FAILED", []apperror.FieldError{
			{Field: "limit_price", Rule: "required", Message: "a resting order needs the price it waits at"},
		})
	}
	if (in.Quantity == nil) == (in.RiskPct == nil) {
		// Refused rather than resolved by precedence: either precedence would silently ignore
		// something the trader typed, and sizing is the thing this product is teaching.
		return nil, apperror.WithFields("VALIDATION_FAILED", []apperror.FieldError{
			{Field: "quantity", Rule: "exclusive", Message: "give either a quantity or a risk percentage, not both"},
		})
	}

	context_, err := s.contextFor(ctx, replay)
	if err != nil {
		return nil, err
	}
	account, err := s.deps.Accounts.GetByID(ctx, replay.AccountID)
	if err != nil {
		return nil, err
	}
	// The reference price the gate reasons about: where a resting order waits, or the last price
	// the trader has been shown for a market order. It is not the fill price — that is the next
	// bar's open, which does not exist yet.
	reference, err := s.referencePrice(ctx, replay, in)
	if err != nil {
		return nil, err
	}
	open, err := s.deps.Trades.ListOpenBySessionID(ctx, replay.ID)
	if err != nil {
		return nil, err
	}
	equity, committed := s.markToMarket(account, open, reference, context_.contractSize)

	quantity := decimal.Zero
	if in.Quantity != nil {
		quantity = *in.Quantity
	} else {
		quantity = domainexec.SizeFromRisk(equity, *in.RiskPct, reference, in.StopLoss,
			context_.contractSize, context_.tickSize)
	}

	halted, _, err := s.drawdown(ctx, replay, account, equity, replay.CursorIndex, context_)
	if err != nil {
		return nil, err
	}

	code := domainexec.Evaluate(domainexec.GateInput{
		SessionLive: replay.Status.Live(), Halted: halted,
		Side: in.Side, Type: in.Type, Quantity: quantity, EntryPrice: reference,
		StopLoss: in.StopLoss, TakeProfit: in.TakeProfit,
		TickSize: context_.tickSize, ContractSize: context_.contractSize,
		RiskPerTradePct: account.Risk.RiskPerTradePct, MinRiskReward: account.Risk.MinRiskReward,
		MaxOpenPositions: account.Risk.MaxOpenPositions, Leverage: account.Risk.Leverage,
		OpenPositions: len(open), AccountEquity: equity, CommittedMargin: committed,
	})

	now := s.deps.Now()
	order := domainexec.Order{
		ID: uuid.New(), SessionID: replay.ID, AccountID: replay.AccountID, ClientKey: in.ClientKey,
		Side: in.Side, Type: in.Type, Quantity: quantity, LimitPrice: in.LimitPrice,
		StopLoss: in.StopLoss, TakeProfit: in.TakeProfit,
		PlacedBarIndex: replay.CursorIndex, PlacedBarAt: context_.barTime(replay.CursorIndex),
		CreatedAt: now, UpdatedAt: now, Version: 1,
	}
	if quantity.IsPositive() {
		risk := domainexec.RiskAmount(quantity, reference, in.StopLoss, context_.contractSize)
		order.RiskAmount = &risk
		if in.TakeProfit != nil {
			distance := reference.Sub(in.StopLoss).Abs()
			if distance.IsPositive() {
				ratio := in.TakeProfit.Sub(reference).Abs().Div(distance).Round(3)
				order.RiskReward = &ratio
			}
		}
	}
	if code != "" {
		// BR-09. The refusal is a row, and the trader is told which rule they hit.
		order.Status = domainexec.StatusRejected
		order.RejectionCode = &code
		stored, err := s.deps.Orders.Create(ctx, &order)
		if err != nil {
			return nil, err
		}
		if err := s.emit(ctx, stored.ID, event.TypeOrderRejected, event.TopicOrders, orderPayload(*stored)); err != nil {
			return nil, err
		}
		return nil, apperror.Newf(code, "%s", gateMessage(code))
	}

	order.Status = domainexec.StatusPending
	stored, err := s.deps.Orders.Create(ctx, &order)
	if err != nil {
		return nil, err
	}
	if err := s.emit(ctx, stored.ID, event.TypeOrderPlaced, event.TopicOrders, orderPayload(*stored)); err != nil {
		return nil, err
	}
	return stored, nil
}

// Advance walks the bars a cursor move released.
//
// The order of operations inside one bar is the whole correctness question:
//
//  1. Open positions resolve first. A stop set before this bar existed takes priority over anything
//     that happens next, because that is the order in which it would have happened.
//  2. Resting orders fill, and a newly filled position is immediately checked against *the same
//     bar* — that is SP4-5: a bar whose range covers both a limit and its stop fills and then stops
//     out, the same adverse assumption the stop-versus-target rule makes one level up.
//  3. Excursions extend for everything still open.
//  4. An equity snapshot is written.
//  5. The drawdown gate is checked last, on the equity this bar produced.
func (s *service) Advance(ctx context.Context, sessionID uuid.UUID, fromBar, toBar int) error {
	replay, err := s.deps.Sessions.Session(ctx, sessionID)
	if err != nil {
		return err
	}
	context_, err := s.contextFor(ctx, replay)
	if err != nil {
		return err
	}
	if toBar <= fromBar {
		return nil
	}
	bars, err := s.deps.Feeds.Bars(ctx, replay.FeedID, fromBar+1, toBar)
	if err != nil {
		return err
	}
	for _, bar := range bars {
		if err := s.advanceOne(ctx, replay, context_, bar); err != nil {
			return err
		}
	}
	return nil
}

func (s *service) advanceOne(ctx context.Context, replay *domainsession.Session, context_ fillContext, bar domainfeed.Bar) error {
	open, err := s.deps.Trades.ListOpenBySessionID(ctx, replay.ID)
	if err != nil {
		return err
	}
	for _, trade := range open {
		if resolution := domainexec.ResolveOpen(trade, bar); resolution.Closed {
			// The bar that ends a position is part of its life, so its extremes count toward the
			// excursions. Folded in before the close rather than written separately: two updates to
			// one row in one bar would collide on the version, and the close is the write that has
			// to land.
			ended := domainexec.ExtendExcursions(trade, bar)
			if _, err := s.closeTrade(ctx, ended, resolution.Price, resolution.Exit, bar.Index, context_); err != nil {
				return err
			}
			continue
		}
		if err := s.extendExcursions(ctx, trade, bar); err != nil {
			return err
		}
	}

	pending, err := s.deps.Orders.ListRestingBySessionID(ctx, replay.ID)
	if err != nil {
		return err
	}
	for sequence, order := range pending {
		// The order sequence is the draw's third coordinate. It is the order's position in this
		// bar's pending list rather than a global counter, so the same bar replayed in isolation
		// draws the same slippage — which is what makes a single disputed fill recomputable
		// without replaying the whole session (NFR-03).
		draw := domainexec.SlippageDraw(context_.seed, bar.Index, sequence, context_.conditions.MaxSlippageTicks)

		var price decimal.Decimal
		var filled bool
		if order.Type == domainexec.OrderMarket {
			// A market order placed while the trader was on the previous bar. This bar is the
			// "next" one — the first price that exists after the decision — so it fills at this
			// open, crossing the spread and wearing the slippage.
			price, filled = domainexec.MarketFill(bar, order.Side, context_.conditions, draw), true
		} else {
			price, filled = domainexec.RestingFill(order, bar)
			if filled {
				// Slippage on a resting fill too: a limit that only just traded is not guaranteed
				// a complete fill at its price.
				adverse := decimal.NewFromInt(int64(draw)).Mul(context_.tickSize)
				if order.Side.Long() {
					price = price.Add(adverse)
				} else {
					price = price.Sub(adverse)
				}
			}
		}
		if !filled {
			continue
		}
		trade, err := s.fill(ctx, order, price.Round(domainexec.PriceScale), bar.Index, context_)
		if err != nil {
			return err
		}
		// SP4-5. The bar that filled it may also contain its stop: fill, then stop — the same
		// adverse assumption the stop-versus-target rule makes one level up.
		if resolution := domainexec.ResolveOpen(*trade, bar); resolution.Closed {
			ended := domainexec.ExtendExcursions(*trade, bar)
			if _, err := s.closeTrade(ctx, ended, resolution.Price, resolution.Exit, bar.Index, context_); err != nil {
				return err
			}
		}
	}

	return s.settleBar(ctx, replay, context_, bar)
}

// settleBar marks the session to market, writes the snapshot, and checks the drawdown gate.
func (s *service) settleBar(ctx context.Context, replay *domainsession.Session, context_ fillContext, bar domainfeed.Bar) error {
	account, err := s.deps.Accounts.GetByID(ctx, replay.AccountID)
	if err != nil {
		return err
	}
	open, err := s.deps.Trades.ListOpenBySessionID(ctx, replay.ID)
	if err != nil {
		return err
	}
	equity, _ := s.markToMarket(account, open, bar.Close, context_.contractSize)
	halted, drawdownPct, err := s.drawdown(ctx, replay, account, equity, bar.Index, context_)
	if err != nil {
		return err
	}
	if err := s.deps.Snapshots.Upsert(ctx, domainexec.EquitySnapshot{
		SessionID: replay.ID, BarIndex: bar.Index, BarAt: context_.barTime(bar.Index),
		Balance: account.CurrentBalance, Equity: equity,
		DrawdownPct: drawdownPct, OpenPositions: len(open),
	}); err != nil {
		return err
	}
	if !halted || len(open) == 0 {
		return nil
	}
	// BR-05. The positions go at market on this bar's close; the session is not deleted, because
	// the post-mortem needs it, and a halted session refuses new orders through the gate.
	for _, trade := range open {
		ended := domainexec.ExtendExcursions(trade, bar)
		if _, err := s.closeTrade(ctx, ended, bar.Close, domainexec.ExitDrawdownHalt, bar.Index, context_); err != nil {
			return err
		}
	}
	return s.emit(ctx, replay.ID, event.TypeGateBreached, event.TopicTrades, map[string]any{
		"session_id": replay.ID, "account_id": replay.AccountID,
		"bar_index": bar.Index, "drawdown_pct": drawdownPct, "positions_closed": len(open),
	})
}

// drawdown reports whether the daily gate binds, and how far down the account currently is.
//
// "Daily" is a market day derived from the bars' real timestamps (SP4-1) — not a wall-clock day, not
// a count of bars. A session replaying three months of history crosses sixty market days, and the
// allowance resets at each one, because that is what "daily drawdown" means to the prop firm whose
// rules this is imitating.
//
// The reference is a high-water mark rather than the day's opening equity: measuring from the open
// would let an account that ran up 5% in the morning give the 5% back before the gate noticed
// anything, and giving back a morning's gain is exactly the pattern the rule exists to stop. The
// mark is the day's own peak — the highest equity snapshotted since the day began, or the current
// equity when this is the day's first bar — so it is both daily and a high-water mark.
//
// None of this reaches the client. RiskState reports the fraction of the allowance left and says
// nothing about when the window turns over.
func (s *service) drawdown(
	ctx context.Context, replay *domainsession.Session, account *domainaccount.Account,
	equity decimal.Decimal, barIndex int, context_ fillContext,
) (bool, decimal.Decimal, error) {
	dayStart := domainexec.MarketDay(context_.barTime(barIndex))
	opening, peak, err := s.deps.Snapshots.DayEquity(ctx, replay.ID, dayStart)
	if err != nil {
		return false, decimal.Zero, err
	}
	// A session that has never been marked before this day — it just opened, or it opened mid-day —
	// gets its reference from the balance, what the account has actually banked. Without this the
	// first bar's own mark would be the only number the day had to compare against, so a position
	// that filled and lost on a session's opening bar would spend none of the allowance and the gate
	// would report an untouched account that was already down.
	if opening.IsZero() {
		opening = account.CurrentBalance
	}
	if peak.LessThan(opening) {
		peak = opening
	}
	if peak.LessThan(equity) {
		peak = equity
	}
	if !peak.IsPositive() {
		return false, decimal.Zero, nil
	}
	drawdownPct := peak.Sub(equity).Div(peak).Mul(decimal.NewFromInt(100)).Round(4)
	if drawdownPct.IsNegative() {
		drawdownPct = decimal.Zero
	}
	return drawdownPct.GreaterThanOrEqual(account.Risk.MaxDailyDrawdownPct), drawdownPct, nil
}

func (s *service) Risk(ctx context.Context, userID, sessionID uuid.UUID) (*RiskState, error) {
	replay, err := s.deps.Sessions.OwnedSession(ctx, userID, sessionID)
	if err != nil {
		return nil, err
	}
	context_, err := s.contextFor(ctx, replay)
	if err != nil {
		return nil, err
	}
	account, err := s.deps.Accounts.GetByID(ctx, replay.AccountID)
	if err != nil {
		return nil, err
	}
	open, err := s.deps.Trades.ListOpenBySessionID(ctx, replay.ID)
	if err != nil {
		return nil, err
	}
	mark, err := s.lastPrice(ctx, replay)
	if err != nil {
		return nil, err
	}
	equity, committed := s.markToMarket(account, open, mark, context_.contractSize)
	halted, drawdownPct, err := s.drawdown(ctx, replay, account, equity, replay.CursorIndex, context_)
	if err != nil {
		return nil, err
	}
	// A fraction of the allowance, not a distance in currency and not a time. 100 means untouched;
	// 0 means the gate binds.
	room := decimal.NewFromInt(100)
	if account.Risk.MaxDailyDrawdownPct.IsPositive() {
		used := drawdownPct.Div(account.Risk.MaxDailyDrawdownPct).Mul(decimal.NewFromInt(100))
		room = decimal.NewFromInt(100).Sub(used).Round(2)
		if room.IsNegative() {
			room = decimal.Zero
		}
	}
	return &RiskState{
		Halted: halted, RoomRemainingPct: room, OpenPositions: len(open),
		Balance: account.CurrentBalance, Equity: equity, CommittedMargin: committed,
	}, nil
}

// markToMarket values the account at a price: balance plus unrealized PnL, and the margin the open
// positions tie up. Both are what the gate reads (SP4-3).
func (s *service) markToMarket(
	account *domainaccount.Account, open []domainexec.Trade, at, contractSize decimal.Decimal,
) (equity, committed decimal.Decimal) {
	equity = account.CurrentBalance
	committed = decimal.Zero
	for _, trade := range open {
		equity = equity.Add(trade.UnrealizedPnL(at).Mul(contractSize))
		committed = committed.Add(domainexec.RequiredMargin(
			trade.Quantity, trade.EntryPrice, contractSize, account.Risk.Leverage))
	}
	return equity.Round(domainexec.MoneyScale), committed
}

type passthroughUOW struct{}

func (passthroughUOW) Do(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) }
