package drawing

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jblabs/blindpulse-be/pkg/apperror"
	"github.com/jblabs/blindpulse-be/pkg/identity"
	"github.com/jblabs/blindpulse-be/pkg/response"
	drawingdomain "github.com/jblabs/blindpulse-be/services/blindpulse/v1/domain/drawing"
	domaindrawing "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/drawing"
	drawingrequest "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/request/drawing"
	drawingresponse "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/response/drawing"
)

type controller struct{ drawings drawingdomain.Service }

func NewController(service drawingdomain.Service) Controller {
	return &controller{drawings: service}
}

func (c *controller) RegisterRoutes(group *gin.RouterGroup) {
	group.POST("/sessions/:id/drawings", c.create)
	group.GET("/sessions/:id/drawings", c.list)
	group.PATCH("/sessions/:id/drawings/:did", c.update)
	group.DELETE("/sessions/:id/drawings/:did", c.remove)
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

// create godoc
// @Summary Store a chart drawing
// @Description The payload is opaque per kind; the server checks the tool, the anchoring bar, the size, and that nothing in it could date the window.
// @Tags drawings
// @Security BearerAuth
// @Param id path string true "Session ID"
// @Param body body drawingrequest.Create true "Drawing"
// @Success 201 {object} response.Envelope{data=drawingresponse.Drawing}
// @Failure 400 {object} response.Envelope
// @Failure 404 {object} response.Envelope
// @Router /sessions/{id}/drawings [post]
func (c *controller) create(ctx *gin.Context) {
	sessionID, ok := pathID(ctx, "id", "SESSION_NOT_FOUND")
	if !ok {
		return
	}
	var request drawingrequest.Create
	if !bind(ctx, &request) {
		return
	}
	entity, err := c.drawings.Create(ctx, actor(ctx).UserID, sessionID, drawingdomain.CreateInput{
		Kind: domaindrawing.Kind(request.Kind), Timeframe: request.Timeframe,
		Payload: request.Payload, CreatedBarIndex: *request.CreatedBarIndex,
	})
	if err != nil {
		response.Error(ctx, err)
		return
	}
	response.Created(ctx, "DRAWING_CREATED", drawingresponse.FromDomain(*entity))
}

// list godoc
// @Summary List a session's drawings
// @Description Every timeframe's drawings, because the client filters to the one it is showing and switching lenses must not refetch.
// @Tags drawings
// @Security BearerAuth
// @Param id path string true "Session ID"
// @Success 200 {object} response.Envelope{data=[]drawingresponse.Drawing}
// @Failure 404 {object} response.Envelope
// @Router /sessions/{id}/drawings [get]
func (c *controller) list(ctx *gin.Context) {
	sessionID, ok := pathID(ctx, "id", "SESSION_NOT_FOUND")
	if !ok {
		return
	}
	entities, err := c.drawings.List(ctx, actor(ctx).UserID, sessionID)
	if err != nil {
		response.Error(ctx, err)
		return
	}
	response.OK(ctx, "DRAWINGS_FETCHED", drawingresponse.FromDomains(entities))
}

// update godoc
// @Summary Reshape a drawing
// @Description Replaces the payload. The kind, timeframe and anchoring bar are fixed — a line that could move to another bar is a line drawn with hindsight.
// @Tags drawings
// @Security BearerAuth
// @Param id path string true "Session ID"
// @Param did path string true "Drawing ID"
// @Param body body drawingrequest.Update true "Payload"
// @Success 200 {object} response.Envelope{data=drawingresponse.Drawing}
// @Failure 404 {object} response.Envelope
// @Router /sessions/{id}/drawings/{did} [patch]
func (c *controller) update(ctx *gin.Context) {
	drawingID, ok := pathID(ctx, "did", "DRAWING_NOT_FOUND")
	if !ok {
		return
	}
	var request drawingrequest.Update
	if !bind(ctx, &request) {
		return
	}
	entity, err := c.drawings.Update(ctx, actor(ctx).UserID, drawingID, drawingdomain.UpdateInput{Payload: request.Payload})
	if err != nil {
		response.Error(ctx, err)
		return
	}
	response.OK(ctx, "DRAWING_UPDATED", drawingresponse.FromDomain(*entity))
}

// remove godoc
// @Summary Delete a drawing
// @Tags drawings
// @Security BearerAuth
// @Param id path string true "Session ID"
// @Param did path string true "Drawing ID"
// @Success 200 {object} response.Envelope
// @Failure 404 {object} response.Envelope
// @Router /sessions/{id}/drawings/{did} [delete]
func (c *controller) remove(ctx *gin.Context) {
	drawingID, ok := pathID(ctx, "did", "DRAWING_NOT_FOUND")
	if !ok {
		return
	}
	if err := c.drawings.Delete(ctx, actor(ctx).UserID, drawingID); err != nil {
		response.Error(ctx, err)
		return
	}
	response.NoData(ctx, "DRAWING_DELETED")
}
