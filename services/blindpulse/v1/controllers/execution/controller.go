// Package execution is the HTTP surface for order intake and position management.
//
// It parses, it delegates, and it maps. Nothing here decides whether an order is allowed: the gate
// lives in the domain because a gate in the client is advice — a hand-rolled curl would walk straight
// past it — and a gate in the controller would be one route away from being skipped (BR-03, BR-04).
package execution

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jblabs/blindpulse-be/pkg/apperror"
	"github.com/jblabs/blindpulse-be/pkg/identity"
	"github.com/jblabs/blindpulse-be/pkg/response"
	executiondomain "github.com/jblabs/blindpulse-be/services/blindpulse/v1/domain/execution"
	domainexec "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/execution"
	executionrequest "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/request/execution"
	executionresponse "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/response/execution"
	"github.com/shopspring/decimal"
)

type controller struct{ execution executiondomain.Service }

func NewController(service executiondomain.Service) Controller {
	return &controller{execution: service}
}

func (c *controller) RegisterRoutes(group *gin.RouterGroup) {
	group.POST("/sessions/:id/orders", c.place)
	group.GET("/sessions/:id/orders", c.listOrders)
	group.DELETE("/orders/:oid", c.cancel)

	group.GET("/sessions/:id/trades", c.listTrades)
	group.GET("/sessions/:id/positions", c.listPositions)
	group.PATCH("/trades/:tid", c.amend)
	// Breakeven is its own route rather than an Amend with a computed stop, because it is the one
	// adjustment the discipline index recognizes by name: "moved the stop to entry" and "moved the
	// stop to a price that happens to equal entry" are the same arithmetic and different decisions.
	group.POST("/trades/:tid/breakeven", c.breakeven)
	group.POST("/trades/:tid/close", c.close)
	group.POST("/sessions/:id/positions/close-all", c.closeAll)

	group.GET("/sessions/:id/risk", c.risk)
}

func actor(ctx *gin.Context) identity.Identity {
	return identity.MustFromContext(ctx.Request.Context())
}

func bind(ctx *gin.Context, value any) bool {
	if err := ctx.ShouldBindJSON(value); err != nil {
		response.Error(ctx, apperror.WithFields("VALIDATION_FAILED", []apperror.FieldError{
			{Field: "request", Rule: "invalid", Message: err.Error()},
		}))
		return false
	}
	return true
}

func pathID(ctx *gin.Context, param, missing string) (uuid.UUID, bool) {
	id, err := uuid.Parse(ctx.Param(param))
	if err != nil {
		response.Error(ctx, apperror.New(missing))
		return uuid.Nil, false
	}
	return id, true
}

// amount parses a decimal string from the wire. A malformed one is a field error rather than a
// generic 400, so the dock can point at the box the trader typed in.
func amount(raw *string, field string) (*decimal.Decimal, error) {
	if raw == nil {
		return nil, nil
	}
	parsed, err := decimal.NewFromString(*raw)
	if err != nil {
		return nil, apperror.WithFields("VALIDATION_FAILED", []apperror.FieldError{
			{Field: field, Rule: "numeric", Message: "must be a decimal number"},
		})
	}
	return &parsed, nil
}

// place godoc
// @Summary Submit an order
// @Description Runs the account's own risk gate. A refused order is still recorded with the rule that refused it (BR-09), and the response names that rule.
// @Tags execution
// @Security BearerAuth
// @Param id path string true "Session ID"
// @Param body body executionrequest.Place true "Order"
// @Success 201 {object} response.Envelope{data=executionresponse.Order}
// @Failure 400 {object} response.Envelope
// @Failure 404 {object} response.Envelope
// @Failure 409 {object} response.Envelope
// @Failure 422 {object} response.Envelope
// @Router /sessions/{id}/orders [post]
func (c *controller) place(ctx *gin.Context) {
	sessionID, ok := pathID(ctx, "id", "SESSION_NOT_FOUND")
	if !ok {
		return
	}
	var request executionrequest.Place
	if !bind(ctx, &request) {
		return
	}
	stop, err := decimal.NewFromString(request.StopLoss)
	if err != nil {
		response.Error(ctx, apperror.WithFields("VALIDATION_FAILED", []apperror.FieldError{
			{Field: "stop_loss", Rule: "numeric", Message: "must be a decimal number"},
		}))
		return
	}
	quantity, err := amount(request.Quantity, "quantity")
	if err != nil {
		response.Error(ctx, err)
		return
	}
	riskPct, err := amount(request.RiskPct, "risk_pct")
	if err != nil {
		response.Error(ctx, err)
		return
	}
	limit, err := amount(request.LimitPrice, "limit_price")
	if err != nil {
		response.Error(ctx, err)
		return
	}
	target, err := amount(request.TakeProfit, "take_profit")
	if err != nil {
		response.Error(ctx, err)
		return
	}
	entity, err := c.execution.Place(ctx, actor(ctx).UserID, sessionID, executiondomain.PlaceInput{
		ClientKey: request.ClientKey,
		Side:      domainexec.Side(request.Side), Type: domainexec.OrderType(request.Type),
		Quantity: quantity, RiskPct: riskPct, LimitPrice: limit,
		StopLoss: stop, TakeProfit: target,
	})
	if err != nil {
		response.Error(ctx, err)
		return
	}
	response.Created(ctx, "ORDER_PLACED", executionresponse.OrderFromDomain(*entity))
}

// listOrders godoc
// @Summary List a session's orders
// @Description The whole log in the order it was placed, rejections included — what the trader tried is as much of the record as what filled.
// @Tags execution
// @Security BearerAuth
// @Param id path string true "Session ID"
// @Success 200 {object} response.Envelope{data=[]executionresponse.Order}
// @Failure 404 {object} response.Envelope
// @Router /sessions/{id}/orders [get]
func (c *controller) listOrders(ctx *gin.Context) {
	sessionID, ok := pathID(ctx, "id", "SESSION_NOT_FOUND")
	if !ok {
		return
	}
	entities, err := c.execution.ListOrders(ctx, actor(ctx).UserID, sessionID)
	if err != nil {
		response.Error(ctx, err)
		return
	}
	response.OK(ctx, "ORDERS_FETCHED", executionresponse.OrdersFromDomain(entities))
}

// cancel godoc
// @Summary Cancel a resting order
// @Tags execution
// @Security BearerAuth
// @Param oid path string true "Order ID"
// @Success 200 {object} response.Envelope
// @Failure 404 {object} response.Envelope
// @Failure 409 {object} response.Envelope
// @Router /orders/{oid} [delete]
func (c *controller) cancel(ctx *gin.Context) {
	orderID, ok := pathID(ctx, "oid", "ORDER_NOT_FOUND")
	if !ok {
		return
	}
	if err := c.execution.Cancel(ctx, actor(ctx).UserID, orderID); err != nil {
		response.Error(ctx, err)
		return
	}
	response.OK(ctx, "ORDER_CANCELLED", gin.H{"id": orderID})
}

// listTrades godoc
// @Summary List a session's trades
// @Description Open and closed, with excursions and R-multiples where they exist.
// @Tags execution
// @Security BearerAuth
// @Param id path string true "Session ID"
// @Success 200 {object} response.Envelope{data=[]executionresponse.Trade}
// @Failure 404 {object} response.Envelope
// @Router /sessions/{id}/trades [get]
func (c *controller) listTrades(ctx *gin.Context) {
	sessionID, ok := pathID(ctx, "id", "SESSION_NOT_FOUND")
	if !ok {
		return
	}
	entities, err := c.execution.ListTrades(ctx, actor(ctx).UserID, sessionID)
	if err != nil {
		response.Error(ctx, err)
		return
	}
	response.OK(ctx, "TRADES_FETCHED", executionresponse.TradesFromDomain(entities))
}

// listPositions godoc
// @Summary List open positions
// @Tags execution
// @Security BearerAuth
// @Param id path string true "Session ID"
// @Success 200 {object} response.Envelope{data=[]executionresponse.Trade}
// @Failure 404 {object} response.Envelope
// @Router /sessions/{id}/positions [get]
func (c *controller) listPositions(ctx *gin.Context) {
	sessionID, ok := pathID(ctx, "id", "SESSION_NOT_FOUND")
	if !ok {
		return
	}
	entities, err := c.execution.ListPositions(ctx, actor(ctx).UserID, sessionID)
	if err != nil {
		response.Error(ctx, err)
		return
	}
	response.OK(ctx, "POSITIONS_FETCHED", executionresponse.TradesFromDomain(entities))
}

// amend godoc
// @Summary Move an open position's stop or target
// @Description The entry cannot move — that already happened — and the new levels are checked for side just as the original was.
// @Tags execution
// @Security BearerAuth
// @Param tid path string true "Trade ID"
// @Param body body executionrequest.Amend true "New levels"
// @Success 200 {object} response.Envelope{data=executionresponse.Trade}
// @Failure 400 {object} response.Envelope
// @Failure 404 {object} response.Envelope
// @Router /trades/{tid} [patch]
func (c *controller) amend(ctx *gin.Context) {
	tradeID, ok := pathID(ctx, "tid", "TRADE_NOT_FOUND")
	if !ok {
		return
	}
	var request executionrequest.Amend
	if !bind(ctx, &request) {
		return
	}
	stop, err := amount(request.StopLoss, "stop_loss")
	if err != nil {
		response.Error(ctx, err)
		return
	}
	target, err := amount(request.TakeProfit, "take_profit")
	if err != nil {
		response.Error(ctx, err)
		return
	}
	entity, err := c.execution.Amend(ctx, actor(ctx).UserID, tradeID, executiondomain.AmendInput{
		StopLoss: stop, TakeProfit: target,
	})
	if err != nil {
		response.Error(ctx, err)
		return
	}
	response.OK(ctx, "TRADE_AMENDED", executionresponse.TradeFromDomain(*entity))
}

// breakeven godoc
// @Summary Move the stop to the entry price
// @Description A named action rather than an amend with a computed value, because it is the one adjustment the discipline index recognizes.
// @Tags execution
// @Security BearerAuth
// @Param tid path string true "Trade ID"
// @Success 200 {object} response.Envelope{data=executionresponse.Trade}
// @Failure 404 {object} response.Envelope
// @Router /trades/{tid}/breakeven [post]
func (c *controller) breakeven(ctx *gin.Context) {
	tradeID, ok := pathID(ctx, "tid", "TRADE_NOT_FOUND")
	if !ok {
		return
	}
	entity, err := c.execution.Breakeven(ctx, actor(ctx).UserID, tradeID)
	if err != nil {
		response.Error(ctx, err)
		return
	}
	response.OK(ctx, "STOP_MOVED_TO_BREAKEVEN", executionresponse.TradeFromDomain(*entity))
}

// close godoc
// @Summary Exit a position at the market
// @Description Whole, or a fraction of it. A partial close splits the position and the remainder keeps its levels.
// @Tags execution
// @Security BearerAuth
// @Param tid path string true "Trade ID"
// @Param body body executionrequest.Close false "Fraction"
// @Success 200 {object} response.Envelope{data=executionresponse.Trade}
// @Failure 400 {object} response.Envelope
// @Failure 404 {object} response.Envelope
// @Router /trades/{tid}/close [post]
func (c *controller) close(ctx *gin.Context) {
	tradeID, ok := pathID(ctx, "tid", "TRADE_NOT_FOUND")
	if !ok {
		return
	}
	// The body is optional: no body means close all of it, which is the common case and should not
	// require the client to send {"fraction":"1"}.
	var request executionrequest.Close
	if ctx.Request.ContentLength > 0 && !bind(ctx, &request) {
		return
	}
	fraction, err := amount(request.Fraction, "fraction")
	if err != nil {
		response.Error(ctx, err)
		return
	}
	entity, err := c.execution.ClosePosition(ctx, actor(ctx).UserID, tradeID, fraction)
	if err != nil {
		response.Error(ctx, err)
		return
	}
	response.OK(ctx, "POSITION_CLOSED", executionresponse.TradeFromDomain(*entity))
}

// closeAll godoc
// @Summary Flatten every open position in a session
// @Tags execution
// @Security BearerAuth
// @Param id path string true "Session ID"
// @Success 200 {object} response.Envelope
// @Failure 404 {object} response.Envelope
// @Router /sessions/{id}/positions/close-all [post]
func (c *controller) closeAll(ctx *gin.Context) {
	sessionID, ok := pathID(ctx, "id", "SESSION_NOT_FOUND")
	if !ok {
		return
	}
	closed, err := c.execution.CloseAll(ctx, actor(ctx).UserID, sessionID)
	if err != nil {
		response.Error(ctx, err)
		return
	}
	response.OK(ctx, "POSITIONS_CLOSED", gin.H{"closed": closed})
}

// risk godoc
// @Summary Read the session's risk state
// @Description How much of the daily drawdown allowance is left, as a fraction of the allowance. Deliberately silent about when the window turns over: a reset pattern with weekends in it would identify the asset class (SP4-1).
// @Tags execution
// @Security BearerAuth
// @Param id path string true "Session ID"
// @Success 200 {object} response.Envelope{data=executionresponse.Risk}
// @Failure 404 {object} response.Envelope
// @Router /sessions/{id}/risk [get]
func (c *controller) risk(ctx *gin.Context) {
	sessionID, ok := pathID(ctx, "id", "SESSION_NOT_FOUND")
	if !ok {
		return
	}
	state, err := c.execution.Risk(ctx, actor(ctx).UserID, sessionID)
	if err != nil {
		response.Error(ctx, err)
		return
	}
	response.OK(ctx, "RISK_FETCHED", executionresponse.RiskFromDomain(*state))
}
