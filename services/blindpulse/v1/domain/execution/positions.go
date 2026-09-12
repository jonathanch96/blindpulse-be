package execution

import (
	"context"

	"github.com/google/uuid"
	"github.com/jblabs/blindpulse-be/pkg/apperror"
	domainexec "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/execution"
	domainfeed "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/feed"
	domainsession "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/session"
	"github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/event"
	"github.com/shopspring/decimal"
)

// fill turns a filled order into an open position.
func (s *service) fill(
	ctx context.Context, order domainexec.Order, price decimal.Decimal, barIndex int, context_ fillContext,
) (*domainexec.Trade, error) {
	now := s.deps.Now()
	filled := order
	filled.Status = domainexec.StatusFilled
	filled.FilledBarIndex = &barIndex
	filled.FilledPrice = &price
	if order.LimitPrice != nil {
		filled.Slippage = price.Sub(*order.LimitPrice).Abs()
	}
	filled.UpdatedAt = now
	if err := s.deps.Orders.Update(ctx, &filled); err != nil {
		return nil, err
	}

	trade := domainexec.Trade{
		ID: uuid.New(), SessionID: order.SessionID, AccountID: order.AccountID,
		EntryOrderID: order.ID, Side: order.Side, Quantity: order.Quantity,
		EntryPrice: price, StopLoss: order.StopLoss,
		// The stop as it was at the fill, frozen. Every R-multiple this position ever reports is
		// measured against it, however many times the live stop moves afterwards.
		InitialStopLoss: order.StopLoss,
		TakeProfit:      order.TakeProfit,
		Status:          domainexec.TradeOpen, OpenedBarIndex: barIndex,
		OpenedAt: now, CreatedAt: now, UpdatedAt: now, Version: 1,
	}
	stored, err := s.deps.Trades.Create(ctx, &trade)
	if err != nil {
		return nil, err
	}
	if err := s.emit(ctx, order.ID, event.TypeOrderFilled, event.TopicOrders, map[string]any{
		"order_id": order.ID, "session_id": order.SessionID, "trade_id": stored.ID,
		"bar_index": barIndex, "price": price, "quantity": order.Quantity,
	}); err != nil {
		return nil, err
	}
	if err := s.emit(ctx, stored.ID, event.TypeTradeOpened, event.TopicTrades, tradePayload(*stored)); err != nil {
		return nil, err
	}
	return stored, nil
}

// closeTrade realizes a position: the money moves, the ledger records it, and the event says so.
//
// The three happen in one transaction. A balance moved without a ledger entry breaks the hash chain
// the accounts screen verifies, and a ledger entry without the balance move is a claim about money
// that did not arrive — NFR-07's whole point is that the chain and the balance cannot disagree.
func (s *service) closeTrade(
	ctx context.Context, trade domainexec.Trade, exit decimal.Decimal,
	reason domainexec.ExitReason, barIndex int, context_ fillContext,
) (*domainexec.Trade, error) {
	now := s.deps.Now()
	realized := domainexec.RealizedPnL(trade, exit, context_.contractSize)
	rMultiple := domainexec.RMultiple(trade, exit)
	held := barIndex - trade.OpenedBarIndex

	closed := trade
	closed.Status = domainexec.TradeClosed
	closed.ExitPrice = &exit
	closed.ExitReason = &reason
	closed.RealizedPnL = &realized
	closed.RMultiple = &rMultiple
	closed.ClosedBarIndex = &barIndex
	closed.BarsHeld = &held
	closed.ClosedAt = &now
	closed.UpdatedAt = now

	if err := s.deps.UOW.Do(ctx, func(txCtx context.Context) error {
		if err := s.deps.Trades.Update(txCtx, &closed); err != nil {
			return err
		}
		if err := s.deps.Accounts.ApplyRealizedPnL(txCtx, trade.AccountID, realized); err != nil {
			return err
		}
		if s.deps.Ledger != nil {
			if err := s.deps.Ledger.AppendTrade(txCtx, trade.AccountID, trade.ID, realized); err != nil {
				return err
			}
		}
		return s.emit(txCtx, closed.ID, event.TypeTradeClosed, event.TopicTrades, tradePayload(closed))
	}); err != nil {
		return nil, err
	}
	// Returned rather than left for the caller to reconstruct. ClosePosition used to rebuild its own
	// answer from the pre-close trade plus a status and an exit price, which silently dropped the
	// realized PnL, the R-multiple, the bars held and the closing index — so a client that closed a
	// position was told it had closed for nothing, while the row in the database had the real number.
	return &closed, nil
}

func (s *service) Cancel(ctx context.Context, userID, orderID uuid.UUID) error {
	order, err := s.ownedOrder(ctx, userID, orderID)
	if err != nil {
		return err
	}
	if order.Status != domainexec.StatusPending {
		// A filled order is history and a rejected one never existed as an instruction. Neither is
		// cancellable, and saying so beats pretending the cancel worked.
		return apperror.Newf("POSITION_ALREADY_CLOSED", "this order is %s and cannot be cancelled", order.Status)
	}
	cancelled := *order
	cancelled.Status = domainexec.StatusCancelled
	cancelled.UpdatedAt = s.deps.Now()
	return s.deps.Orders.Update(ctx, &cancelled)
}

func (s *service) Amend(ctx context.Context, userID, tradeID uuid.UUID, in AmendInput) (*domainexec.Trade, error) {
	trade, err := s.ownedTrade(ctx, userID, tradeID)
	if err != nil {
		return nil, err
	}
	if !trade.Open() {
		return nil, apperror.New("POSITION_ALREADY_CLOSED")
	}
	amended := *trade
	if in.StopLoss != nil {
		amended.StopLoss = *in.StopLoss
	}
	if in.TakeProfit != nil {
		amended.TakeProfit = in.TakeProfit
	}
	// The same side checks the entry passed, applied only to the level this call is actually moving.
	//
	// Checking both every time made Breakeven a one-way door: it puts the stop *at* the entry on
	// purpose, outside the rule that a stop must sit beyond it, and the next amend of the target then
	// re-validated that stop and refused with ORDER_STOP_INVALID. So using the feature the discipline
	// index most wants to reward permanently froze the position's target. A level the caller did not
	// name is an existing fact, not something this request is asserting.
	if in.StopLoss != nil {
		if trade.Side.Long() && !amended.StopLoss.LessThan(trade.EntryPrice) {
			return nil, apperror.New("ORDER_STOP_INVALID")
		}
		if !trade.Side.Long() && !amended.StopLoss.GreaterThan(trade.EntryPrice) {
			return nil, apperror.New("ORDER_STOP_INVALID")
		}
	}
	if in.TakeProfit != nil && amended.TakeProfit != nil {
		if trade.Side.Long() && !amended.TakeProfit.GreaterThan(trade.EntryPrice) {
			return nil, apperror.New("ORDER_TARGET_INVALID")
		}
		if !trade.Side.Long() && !amended.TakeProfit.LessThan(trade.EntryPrice) {
			return nil, apperror.New("ORDER_TARGET_INVALID")
		}
	}
	amended.UpdatedAt = s.deps.Now()
	if err := s.deps.Trades.Update(ctx, &amended); err != nil {
		return nil, err
	}
	return &amended, nil
}

// Breakeven moves the stop to the entry.
//
// A named action rather than an Amend the client computes, for two reasons: the discipline index
// wants to recognize it as a behaviour, and "breakeven" means the entry price exactly — a client
// computing it would eventually send a value a tick off and the trade would stop out for a cent.
//
// It deliberately bypasses the side check that Amend applies, because a stop *at* the entry is
// exactly what is being asked for and that check refuses it.
func (s *service) Breakeven(ctx context.Context, userID, tradeID uuid.UUID) (*domainexec.Trade, error) {
	trade, err := s.ownedTrade(ctx, userID, tradeID)
	if err != nil {
		return nil, err
	}
	if !trade.Open() {
		return nil, apperror.New("POSITION_ALREADY_CLOSED")
	}
	moved := *trade
	moved.StopLoss = trade.EntryPrice
	moved.UpdatedAt = s.deps.Now()
	if err := s.deps.Trades.Update(ctx, &moved); err != nil {
		return nil, err
	}
	return &moved, nil
}

// ClosePosition exits at the market, in whole or in part.
//
// A partial close splits the position: the closed portion realizes now and the remainder keeps the
// original entry, stop and target. Reducing the quantity in place would be simpler and would lose
// the record of the scale-out, which is one of the behaviours the journal is built to show.
func (s *service) ClosePosition(
	ctx context.Context, userID, tradeID uuid.UUID, fraction *decimal.Decimal,
) (*domainexec.Trade, error) {
	trade, err := s.ownedTrade(ctx, userID, tradeID)
	if err != nil {
		return nil, err
	}
	if !trade.Open() {
		return nil, apperror.New("POSITION_ALREADY_CLOSED")
	}
	replay, err := s.deps.Sessions.OwnedSession(ctx, userID, trade.SessionID)
	if err != nil {
		return nil, err
	}
	context_, err := s.contextFor(ctx, replay)
	if err != nil {
		return nil, err
	}
	price, err := s.lastPrice(ctx, replay)
	if err != nil {
		return nil, err
	}
	// Crossing the spread to get out, the same as getting in. A frictionless exit would make
	// scratching a trade free, which is the single cheapest way to flatter a strategy.
	half := decimal.NewFromInt(int64(context_.conditions.SpreadTicks)).Div(decimal.NewFromInt(2)).Ceil().Mul(context_.tickSize)
	if trade.Side.Long() {
		price = price.Sub(half)
	} else {
		price = price.Add(half)
	}
	price = price.Round(domainexec.PriceScale)

	// A partial close splits the position, and *which half keeps the id* is the whole of the API
	// contract here. The closed portion becomes a new row and the caller's trade keeps its id and
	// stays open with the reduced quantity — the other way round, taking 40% off a position silently
	// invalidated the id the client was holding, so the next amend on it failed with
	// POSITION_ALREADY_CLOSED and the open 60% had an id the client had never seen.
	portion := *trade
	if fraction != nil && fraction.IsPositive() && fraction.LessThan(decimal.NewFromInt(1)) {
		closing := domainexec.FloorToTick(trade.Quantity.Mul(*fraction), context_.tickSize)
		remaining := trade.Quantity.Sub(closing)
		if !closing.IsPositive() || !remaining.IsPositive() {
			// A fraction that rounds to nothing, or to everything, is not a partial close. Refused
			// rather than silently promoted to a full one: the trader asked for a specific size.
			return nil, apperror.New("ORDER_QUANTITY_INVALID")
		}
		portion.ID = uuid.New()
		portion.Quantity = closing
		portion.Version = 1
		portion.CreatedAt = s.deps.Now()
		portion.UpdatedAt = portion.CreatedAt
		if _, err := s.deps.Trades.Create(ctx, &portion); err != nil {
			return nil, err
		}
		shrunk := *trade
		shrunk.Quantity = remaining
		shrunk.UpdatedAt = s.deps.Now()
		if err := s.deps.Trades.Update(ctx, &shrunk); err != nil {
			return nil, err
		}
	}
	return s.closeTrade(ctx, portion, price, domainexec.ExitManual, replay.CursorIndex, context_)
}

func (s *service) CloseAll(ctx context.Context, userID, sessionID uuid.UUID) (int, error) {
	replay, err := s.deps.Sessions.OwnedSession(ctx, userID, sessionID)
	if err != nil {
		return 0, err
	}
	open, err := s.deps.Trades.ListOpenBySessionID(ctx, replay.ID)
	if err != nil {
		return 0, err
	}
	for _, trade := range open {
		if _, err := s.ClosePosition(ctx, userID, trade.ID, nil); err != nil {
			return 0, err
		}
	}
	return len(open), nil
}

func (s *service) ListOrders(ctx context.Context, userID, sessionID uuid.UUID) ([]domainexec.Order, error) {
	if _, err := s.deps.Sessions.OwnedSession(ctx, userID, sessionID); err != nil {
		return nil, err
	}
	return s.deps.Orders.ListBySessionID(ctx, sessionID)
}

func (s *service) ListTrades(ctx context.Context, userID, sessionID uuid.UUID) ([]domainexec.Trade, error) {
	if _, err := s.deps.Sessions.OwnedSession(ctx, userID, sessionID); err != nil {
		return nil, err
	}
	return s.deps.Trades.ListBySessionID(ctx, sessionID)
}

func (s *service) ListPositions(ctx context.Context, userID, sessionID uuid.UUID) ([]domainexec.Trade, error) {
	if _, err := s.deps.Sessions.OwnedSession(ctx, userID, sessionID); err != nil {
		return nil, err
	}
	return s.deps.Trades.ListOpenBySessionID(ctx, sessionID)
}

func (s *service) extendExcursions(ctx context.Context, trade domainexec.Trade, bar domainfeed.Bar) error {
	adverse, favorable := domainexec.ExcursionsFor(trade, bar)
	updated := trade
	// Both are non-negative magnitudes, so both extend upward. Adverse used to be a negative money
	// figure and took the minimum; flipping the units without flipping this comparison would have
	// frozen MAE at the first bar's value forever.
	if trade.MaxAdverseExcursion == nil || adverse.GreaterThan(*trade.MaxAdverseExcursion) {
		updated.MaxAdverseExcursion = &adverse
	}
	if trade.MaxFavorableExcursion == nil || favorable.GreaterThan(*trade.MaxFavorableExcursion) {
		updated.MaxFavorableExcursion = &favorable
	}
	if updated.MaxAdverseExcursion == trade.MaxAdverseExcursion &&
		updated.MaxFavorableExcursion == trade.MaxFavorableExcursion {
		// Nothing moved past a previous extreme. Skipping the write keeps a quiet bar from costing
		// one UPDATE per open position per bar, which at 10x playback is the hot path.
		return nil
	}
	updated.UpdatedAt = s.deps.Now()
	return s.deps.Trades.Update(ctx, &updated)
}

// ownedOrder and ownedTrade answer not-found rather than forbidden for somebody else's row, for the
// usual reason: confirming the id exists leaks that another trader has one.
func (s *service) ownedOrder(ctx context.Context, userID, orderID uuid.UUID) (*domainexec.Order, error) {
	order, err := s.deps.Orders.GetByID(ctx, orderID)
	if err != nil {
		return nil, err
	}
	if _, err := s.deps.Sessions.OwnedSession(ctx, userID, order.SessionID); err != nil {
		return nil, apperror.New("ORDER_NOT_FOUND")
	}
	return order, nil
}

func (s *service) ownedTrade(ctx context.Context, userID, tradeID uuid.UUID) (*domainexec.Trade, error) {
	trade, err := s.deps.Trades.GetByID(ctx, tradeID)
	if err != nil {
		return nil, err
	}
	if _, err := s.deps.Sessions.OwnedSession(ctx, userID, trade.SessionID); err != nil {
		return nil, apperror.New("TRADE_NOT_FOUND")
	}
	return trade, nil
}

// contextFor gathers the per-session constants one bar's resolution needs.
func (s *service) contextFor(ctx context.Context, replay *domainsession.Session) (fillContext, error) {
	feed, err := s.deps.Feeds.Get(ctx, replay.FeedID)
	if err != nil {
		return fillContext{}, err
	}
	times, err := s.deps.Clock.BarTimes(ctx, replay.FeedID)
	if err != nil {
		return fillContext{}, err
	}
	// One tick is one unit of the display scale, for every feed, and that uniformity is the point.
	// The instrument's real tick size is an identifier — 0.00001 says a currency pair, 0.25 says the
	// E-mini — and the blinding map would scale it into something else anyway, so the blinded series
	// is quantized to PriceScale and a tick is what the trader can actually see move.
	//
	// Contract size is 1 for the same reason: 100,000 names an FX lot and 50 names an index future.
	// A trader sizing from dollar risk over a stop distance needs neither, because the blinding scale
	// cancels out of that calculation entirely.
	tick := s.deps.Conditions.TickSize
	if !tick.IsPositive() {
		tick = decimal.New(1, -domainexec.PriceScale)
	}
	conditions := s.deps.Conditions
	conditions.TickSize = tick
	if feed.TotalBars > 0 && len(times) > feed.TotalBars {
		// The clock reads the same window the feed indexes into, so a longer list means the window
		// query drifted from the feed's own. Trim rather than fail: every index the feed can serve
		// still maps to the right instant, and the drawdown boundary stays on the right bar.
		times = times[:feed.TotalBars]
	}
	return fillContext{
		sessionID: replay.ID, accountID: replay.AccountID, seed: replay.Seed,
		conditions: conditions, tickSize: tick,
		contractSize: decimal.NewFromInt(1), barTimes: times,
	}, nil
}

// lastPrice is the close of the bar the trader is on: the most recent price they have been shown.
func (s *service) lastPrice(ctx context.Context, replay *domainsession.Session) (decimal.Decimal, error) {
	bars, err := s.deps.Feeds.Bars(ctx, replay.FeedID, replay.CursorIndex, replay.CursorIndex)
	if err != nil {
		return decimal.Zero, err
	}
	if len(bars) == 0 {
		return decimal.Zero, apperror.New("BARS_EXHAUSTED")
	}
	return bars[len(bars)-1].Close, nil
}

// referencePrice is what the gate reasons about: where a resting order waits, or the last price
// shown for a market order.
func (s *service) referencePrice(
	ctx context.Context, replay *domainsession.Session, in PlaceInput,
) (decimal.Decimal, error) {
	if in.Type.Resting() && in.LimitPrice != nil {
		return *in.LimitPrice, nil
	}
	return s.lastPrice(ctx, replay)
}

func (s *service) emit(ctx context.Context, aggregateID uuid.UUID, eventType, topic string, payload any) error {
	if s.deps.Outbox == nil {
		return nil
	}
	record, err := event.New("execution", aggregateID, eventType, s.deps.Topic(topic), payload)
	if err != nil {
		return apperror.Wrap(err, "INTERNAL_ERROR")
	}
	return s.deps.Outbox.Create(ctx, record)
}

// The event payloads carry indices and prices in blinded space, never a symbol or an instant. A
// projector reading this stream learns what the trader did and still cannot tell what they traded.
func orderPayload(order domainexec.Order) map[string]any {
	return map[string]any{
		"order_id": order.ID, "session_id": order.SessionID, "account_id": order.AccountID,
		"side": order.Side, "type": order.Type, "quantity": order.Quantity,
		"status": order.Status, "rejection_code": order.RejectionCode,
		"bar_index": order.PlacedBarIndex, "risk_amount": order.RiskAmount, "risk_reward": order.RiskReward,
	}
}

func tradePayload(trade domainexec.Trade) map[string]any {
	return map[string]any{
		"trade_id": trade.ID, "session_id": trade.SessionID, "account_id": trade.AccountID,
		"side": trade.Side, "quantity": trade.Quantity, "entry_price": trade.EntryPrice,
		"exit_price": trade.ExitPrice, "exit_reason": trade.ExitReason,
		"realized_pnl": trade.RealizedPnL, "r_multiple": trade.RMultiple,
		"opened_bar_index": trade.OpenedBarIndex, "closed_bar_index": trade.ClosedBarIndex,
	}
}

// gateMessage turns a rejection code into the sentence the trader reads. The catalog carries a
// generic message per code; these say what to do about it, which is the difference between a gate
// that teaches and one that merely refuses.
func gateMessage(code string) string {
	switch code {
	case domainexec.CodeStopRequired:
		return "every entry needs a hard stop loss before it can be submitted"
	case domainexec.CodeStopInvalid:
		return "the stop is on the wrong side of the entry"
	case domainexec.CodeTargetInvalid:
		return "the target is on the wrong side of the entry"
	case domainexec.CodeQuantityInvalid:
		return "the quantity must be positive and on the instrument's tick grid"
	case domainexec.CodeStopTooWide:
		return "this order risks more than the account's risk-per-trade limit; it is refused rather than resized"
	case domainexec.CodeRiskRewardTooLow:
		return "the risk-to-reward is below the account's minimum"
	case domainexec.CodeMaxPositions:
		return "the account is already at its open-position limit"
	case domainexec.CodeInsufficientMargin:
		return "account equity cannot support this position; unrealized losses count against it"
	case domainexec.CodeDrawdownBreached:
		return "the daily drawdown gate is breached and trading is halted for this session"
	default:
		return "the order was refused"
	}
}
