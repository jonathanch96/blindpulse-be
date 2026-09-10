package session

import (
	"context"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jblabs/blindpulse-be/pkg/apperror"
	"github.com/jblabs/blindpulse-be/pkg/identity"
	"github.com/jblabs/blindpulse-be/pkg/response"
	feeddomain "github.com/jblabs/blindpulse-be/services/blindpulse/v1/domain/feed"
	sessiondomain "github.com/jblabs/blindpulse-be/services/blindpulse/v1/domain/session"
	"github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/market"
	domainsession "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/session"
	sessionrequest "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/request/session"
	feedresponse "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/response/feed"
	sessionresponse "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/response/session"
)

func NewController(sessions sessiondomain.Service, feeds feeddomain.Service) Controller {
	return &controller{sessions: sessions, feeds: feeds}
}

func (c *controller) RegisterRoutes(group *gin.RouterGroup) {
	group.POST("/sessions", c.start)
	group.GET("/sessions", c.list)
	group.GET("/sessions/:id", c.get)
	group.GET("/sessions/:id/bars", c.bars)
	group.POST("/sessions/:id/step", c.step)
	group.POST("/sessions/:id/seek", c.seek)
	group.POST("/sessions/:id/speed", c.speed)
	group.POST("/sessions/:id/pause", c.pause)
	group.POST("/sessions/:id/resume", c.resume)
	group.POST("/sessions/:id/close", c.close)
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

func sessionID(ctx *gin.Context) (uuid.UUID, bool) {
	id, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		response.Error(ctx, apperror.New("SESSION_NOT_FOUND"))
		return uuid.Nil, false
	}
	return id, true
}

// render looks the feed up for its bar count so the progress readout is accurate. Only the count
// is used; nothing identifying crosses into the session response.
func (c *controller) render(ctx *gin.Context, entity *domainsession.Session, code string) {
	total := 0
	if feed, err := c.feeds.Get(ctx, entity.FeedID); err == nil {
		total = feed.TotalBars
	}
	response.OK(ctx, code, sessionresponse.FromDomain(*entity, total))
}

// start godoc
// @Summary Start a replay session
// @Description Opens a session at the end of the feed's warmup window. Everything past it must be stepped to.
// @Tags sessions
// @Security BearerAuth
// @Param body body sessionrequest.Start true "Session"
// @Success 201 {object} response.Envelope{data=sessionresponse.Session}
// @Failure 409 {object} response.Envelope
// @Router /sessions [post]
func (c *controller) start(ctx *gin.Context) {
	var request sessionrequest.Start
	if !bind(ctx, &request) {
		return
	}
	accountID, err := uuid.Parse(request.AccountID)
	if err != nil {
		response.Error(ctx, apperror.New("ACCOUNT_NOT_FOUND"))
		return
	}
	feedID, err := uuid.Parse(request.FeedID)
	if err != nil {
		response.Error(ctx, apperror.New("FEED_NOT_FOUND"))
		return
	}
	entity, err := c.sessions.Start(ctx, actor(ctx).UserID, sessiondomain.StartInput{
		AccountID: accountID, FeedID: feedID, Timeframe: market.Timeframe(request.Timeframe),
	})
	if err != nil {
		response.Error(ctx, err)
		return
	}
	total := 0
	if feed, err := c.feeds.Get(ctx, entity.FeedID); err == nil {
		total = feed.TotalBars
	}
	response.Created(ctx, "SESSION_STARTED", sessionresponse.FromDomain(*entity, total))
}

// list godoc
// @Summary List live sessions
// @Tags sessions
// @Security BearerAuth
// @Success 200 {object} response.Envelope{data=[]sessionresponse.Session}
// @Router /sessions [get]
func (c *controller) list(ctx *gin.Context) {
	entities, err := c.sessions.ListOpen(ctx, actor(ctx).UserID)
	if err != nil {
		response.Error(ctx, err)
		return
	}
	rendered := make([]sessionresponse.Session, 0, len(entities))
	for _, entity := range entities {
		total := 0
		if feed, err := c.feeds.Get(ctx, entity.FeedID); err == nil {
			total = feed.TotalBars
		}
		rendered = append(rendered, sessionresponse.FromDomain(entity, total))
	}
	response.OK(ctx, "SESSIONS_LISTED", rendered)
}

// get godoc
// @Summary Get a replay session
// @Tags sessions
// @Security BearerAuth
// @Param id path string true "Session ID"
// @Success 200 {object} response.Envelope{data=sessionresponse.Session}
// @Failure 404 {object} response.Envelope
// @Router /sessions/{id} [get]
func (c *controller) get(ctx *gin.Context) {
	id, ok := sessionID(ctx)
	if !ok {
		return
	}
	entity, err := c.sessions.Get(ctx, actor(ctx).UserID, id)
	if err != nil {
		response.Error(ctx, err)
		return
	}
	c.render(ctx, entity, "SESSION_FETCHED")
}

// bars godoc
// @Summary Read released bars
// @Description Returns candles up to the session's revealed edge. Requesting past it is refused, never clamped.
// @Tags sessions
// @Security BearerAuth
// @Param id path string true "Session ID"
// @Param from query int false "First bar index"
// @Param to query int false "Last bar index; defaults to the revealed edge"
// @Success 200 {object} response.Envelope{data=feedresponse.Bars}
// @Failure 400 {object} response.Envelope
// @Router /sessions/{id}/bars [get]
func (c *controller) bars(ctx *gin.Context) {
	id, ok := sessionID(ctx)
	if !ok {
		return
	}
	from, to := 0, -1
	if raw := ctx.Query("from"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			response.Error(ctx, apperror.New("INVALID_CURSOR"))
			return
		}
		from = parsed
	}
	if raw := ctx.Query("to"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil {
			response.Error(ctx, apperror.New("INVALID_CURSOR"))
			return
		}
		to = parsed
	}
	blinded, err := c.sessions.Bars(ctx, actor(ctx).UserID, id, from, to)
	if err != nil {
		response.Error(ctx, err)
		return
	}
	resolvedTo := from + len(blinded) - 1
	if len(blinded) == 0 {
		resolvedTo = from
	}
	response.OK(ctx, "SESSION_BARS_FETCHED", feedresponse.Bars{
		FeedID: id.String(), From: from, To: resolvedTo, Bars: feedresponse.FromDomainBars(blinded),
	})
}

// step godoc
// @Summary Step the cursor
// @Description A positive count advances and may release new bars; a negative count rewinds and never does.
// @Tags sessions
// @Security BearerAuth
// @Param id path string true "Session ID"
// @Param body body sessionrequest.Step true "Step"
// @Success 200 {object} response.Envelope{data=sessionresponse.Session}
// @Failure 400 {object} response.Envelope
// @Router /sessions/{id}/step [post]
func (c *controller) step(ctx *gin.Context) {
	id, ok := sessionID(ctx)
	if !ok {
		return
	}
	var request sessionrequest.Step
	if !bind(ctx, &request) {
		return
	}
	entity, err := c.sessions.Step(ctx, actor(ctx).UserID, id, request.Count)
	if err != nil {
		response.Error(ctx, err)
		return
	}
	c.render(ctx, entity, "SESSION_STEPPED")
}

// seek godoc
// @Summary Move the view to an already-released bar
// @Tags sessions
// @Security BearerAuth
// @Param id path string true "Session ID"
// @Param body body sessionrequest.Seek true "Seek"
// @Success 200 {object} response.Envelope{data=sessionresponse.Session}
// @Failure 400 {object} response.Envelope
// @Router /sessions/{id}/seek [post]
func (c *controller) seek(ctx *gin.Context) {
	id, ok := sessionID(ctx)
	if !ok {
		return
	}
	var request sessionrequest.Seek
	if !bind(ctx, &request) {
		return
	}
	entity, err := c.sessions.Seek(ctx, actor(ctx).UserID, id, request.BarIndex)
	if err != nil {
		response.Error(ctx, err)
		return
	}
	c.render(ctx, entity, "SESSION_SEEKED")
}

// speed godoc
// @Summary Set playback speed
// @Tags sessions
// @Security BearerAuth
// @Param id path string true "Session ID"
// @Param body body sessionrequest.Speed true "Speed"
// @Success 200 {object} response.Envelope{data=sessionresponse.Session}
// @Failure 400 {object} response.Envelope
// @Router /sessions/{id}/speed [post]
func (c *controller) speed(ctx *gin.Context) {
	id, ok := sessionID(ctx)
	if !ok {
		return
	}
	var request sessionrequest.Speed
	if !bind(ctx, &request) {
		return
	}
	entity, err := c.sessions.SetSpeed(ctx, actor(ctx).UserID, id, request.Speed)
	if err != nil {
		response.Error(ctx, err)
		return
	}
	c.render(ctx, entity, "SESSION_SPEED_SET")
}

// pause godoc
// @Summary Pause playback
// @Tags sessions
// @Security BearerAuth
// @Param id path string true "Session ID"
// @Success 200 {object} response.Envelope{data=sessionresponse.Session}
// @Router /sessions/{id}/pause [post]
func (c *controller) pause(ctx *gin.Context) {
	c.transition(ctx, c.sessions.Pause, "SESSION_PAUSED")
}

// resume godoc
// @Summary Resume playback
// @Tags sessions
// @Security BearerAuth
// @Param id path string true "Session ID"
// @Success 200 {object} response.Envelope{data=sessionresponse.Session}
// @Router /sessions/{id}/resume [post]
func (c *controller) resume(ctx *gin.Context) {
	c.transition(ctx, c.sessions.Resume, "SESSION_RESUMED")
}

// close godoc
// @Summary Close a session
// @Description One-way. Closing is the precondition for the post-session reveal.
// @Tags sessions
// @Security BearerAuth
// @Param id path string true "Session ID"
// @Success 200 {object} response.Envelope{data=sessionresponse.Session}
// @Router /sessions/{id}/close [post]
func (c *controller) close(ctx *gin.Context) {
	c.transition(ctx, c.sessions.Close, "SESSION_CLOSED")
}

type transitionFunc func(ctx context.Context, userID, sessionID uuid.UUID) (*domainsession.Session, error)

func (c *controller) transition(ctx *gin.Context, run transitionFunc, code string) {
	id, ok := sessionID(ctx)
	if !ok {
		return
	}
	entity, err := run(ctx, actor(ctx).UserID, id)
	if err != nil {
		response.Error(ctx, err)
		return
	}
	c.render(ctx, entity, code)
}
