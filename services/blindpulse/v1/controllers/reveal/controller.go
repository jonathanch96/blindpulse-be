package reveal

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jblabs/blindpulse-be/pkg/apperror"
	"github.com/jblabs/blindpulse-be/pkg/identity"
	"github.com/jblabs/blindpulse-be/pkg/response"
	revealdomain "github.com/jblabs/blindpulse-be/services/blindpulse/v1/domain/reveal"
	revealresponse "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/response/reveal"
)

type controller struct{ reveals revealdomain.Service }

func NewController(service revealdomain.Service) Controller {
	return &controller{reveals: service}
}

func (c *controller) RegisterRoutes(group *gin.RouterGroup) {
	group.POST("/sessions/:id/reveal", c.unblind)
	group.GET("/sessions/:id/reveal", c.get)
	group.GET("/sessions/:id/reveal/bars", c.disclosedBars)
}

func sessionID(ctx *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		response.Error(ctx, apperror.New("SESSION_NOT_FOUND"))
		return uuid.Nil, false
	}
	return id, true
}

// unblind godoc
// @Summary Unblind a finished session
// @Description Written once. Requires the session to be closed; reveals the real symbol, timeframe, window, macro context and the buy-and-hold comparison. There is no undo.
// @Tags reveal
// @Security BearerAuth
// @Param id path string true "Session ID"
// @Success 201 {object} response.Envelope{data=revealresponse.Reveal}
// @Failure 403 {object} response.Envelope
// @Failure 404 {object} response.Envelope
// @Failure 409 {object} response.Envelope
// @Router /sessions/{id}/reveal [post]
func (c *controller) unblind(ctx *gin.Context) {
	id, ok := sessionID(ctx)
	if !ok {
		return
	}
	entity, err := c.reveals.Unblind(ctx, identity.MustFromContext(ctx.Request.Context()).UserID, id)
	if err != nil {
		response.Error(ctx, err)
		return
	}
	response.Created(ctx, "SESSION_REVEALED", revealresponse.FromDomain(*entity))
}

// get godoc
// @Summary Read a session's reveal
// @Description REVEAL_LOCKED until the session has been unblinded — the session exists and is yours; the curtain has not gone up.
// @Tags reveal
// @Security BearerAuth
// @Param id path string true "Session ID"
// @Success 200 {object} response.Envelope{data=revealresponse.Reveal}
// @Failure 403 {object} response.Envelope
// @Failure 404 {object} response.Envelope
// @Router /sessions/{id}/reveal [get]
func (c *controller) get(ctx *gin.Context) {
	id, ok := sessionID(ctx)
	if !ok {
		return
	}
	entity, err := c.reveals.Get(ctx, identity.MustFromContext(ctx.Request.Context()).UserID, id)
	if err != nil {
		response.Error(ctx, err)
		return
	}
	response.OK(ctx, "REVEAL_FETCHED", revealresponse.FromDomain(*entity))
}

// disclosedBars godoc
// @Summary Read a revealed session's real candles
// @Description Real prices with real timestamps, over the whole window including what happened after the trader stopped. A separate response type from the blinded bars, gated on the reveal existing rather than on the session being closed.
// @Tags reveal
// @Security BearerAuth
// @Param id path string true "Session ID"
// @Success 200 {object} response.Envelope{data=revealresponse.Disclosure}
// @Failure 403 {object} response.Envelope
// @Failure 404 {object} response.Envelope
// @Router /sessions/{id}/reveal/bars [get]
func (c *controller) disclosedBars(ctx *gin.Context) {
	id, ok := sessionID(ctx)
	if !ok {
		return
	}
	bars, entity, err := c.reveals.DisclosedBars(ctx, identity.MustFromContext(ctx.Request.Context()).UserID, id)
	if err != nil {
		response.Error(ctx, err)
		return
	}
	response.OK(ctx, "DISCLOSURE_FETCHED", revealresponse.DisclosureFromDomain(*entity, bars))
}
