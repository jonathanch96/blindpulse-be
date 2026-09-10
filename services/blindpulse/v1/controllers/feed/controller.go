package feed

import (
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jblabs/blindpulse-be/pkg/apperror"
	"github.com/jblabs/blindpulse-be/pkg/identity"
	"github.com/jblabs/blindpulse-be/pkg/response"
	feeddomain "github.com/jblabs/blindpulse-be/services/blindpulse/v1/domain/feed"
	domainfeed "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/feed"
	"github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/market"
	feedresponse "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/response/feed"
)

func NewController(feeds feeddomain.Service, instruments feeddomain.InstrumentRepository) Controller {
	return &controller{feeds: feeds, instruments: instruments}
}

func (c *controller) RegisterRoutes(group *gin.RouterGroup) {
	group.GET("/feeds", c.list)
	group.POST("/feeds/random", c.random)
	group.GET("/feeds/:id", c.get)
	group.GET("/feeds/:id/bars", c.bars)
}

func actor(ctx *gin.Context) identity.Identity {
	return identity.MustFromContext(ctx.Request.Context())
}

func filterFrom(ctx *gin.Context) (feeddomain.ListFilter, bool) {
	filter := feeddomain.ListFilter{}
	if raw := ctx.Query("difficulty"); raw != "" {
		difficulty := domainfeed.Difficulty(raw)
		if !difficulty.Valid() {
			response.Error(ctx, apperror.WithFields("VALIDATION_FAILED", []apperror.FieldError{
				{Field: "difficulty", Rule: "oneof", Message: "difficulty must be calm, standard, volatile or crisis"},
			}))
			return filter, false
		}
		filter.Difficulty = difficulty
	}
	if raw := ctx.Query("timeframe"); raw != "" {
		timeframe := market.Timeframe(raw)
		if !timeframe.Valid() {
			response.Error(ctx, apperror.New("INVALID_TIMEFRAME"))
			return filter, false
		}
		filter.BaseTimeframe = timeframe
	}
	if raw := ctx.Query("limit"); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil || limit < 1 {
			response.Error(ctx, apperror.WithFields("VALIDATION_FAILED", []apperror.FieldError{
				{Field: "limit", Rule: "numeric", Message: "limit must be a positive integer"},
			}))
			return filter, false
		}
		filter.Limit = limit
	}
	return filter, true
}

// project builds the blinded view. It looks the instrument up only to derive the coarse asset
// class hint, and the hint is all that survives into the response.
func (c *controller) project(ctx *gin.Context, entity domainfeed.Feed) (feedresponse.Feed, bool) {
	instrument, err := c.instruments.GetByID(ctx, entity.InstrumentID)
	if err != nil {
		response.Error(ctx, err)
		return feedresponse.Feed{}, false
	}
	return feedresponse.FromDomain(entity, instrument.AssetClass), true
}

// list godoc
// @Summary List blinded feeds
// @Description Returns the published catalogue. No response field identifies the instrument, the calendar window, or the venue.
// @Tags feeds
// @Security BearerAuth
// @Param difficulty query string false "calm, standard, volatile or crisis"
// @Param timeframe query string false "Base timeframe"
// @Param limit query int false "Maximum feeds to return"
// @Success 200 {object} response.Envelope{data=[]feedresponse.Feed}
// @Failure 400 {object} response.Envelope
// @Router /feeds [get]
func (c *controller) list(ctx *gin.Context) {
	filter, ok := filterFrom(ctx)
	if !ok {
		return
	}
	entities, err := c.feeds.List(ctx, actor(ctx).UserID, filter)
	if err != nil {
		response.Error(ctx, err)
		return
	}
	projected := make([]feedresponse.Feed, 0, len(entities))
	for _, entity := range entities {
		view, ok := c.project(ctx, entity)
		if !ok {
			return
		}
		projected = append(projected, view)
	}
	response.OK(ctx, "FEEDS_LISTED", projected)
}

// get godoc
// @Summary Get one blinded feed
// @Tags feeds
// @Security BearerAuth
// @Param id path string true "Feed ID"
// @Success 200 {object} response.Envelope{data=feedresponse.Detail}
// @Failure 404 {object} response.Envelope
// @Router /feeds/{id} [get]
func (c *controller) get(ctx *gin.Context) {
	id, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		response.Error(ctx, apperror.New("FEED_NOT_FOUND"))
		return
	}
	entity, err := c.feeds.Get(ctx, id)
	if err != nil {
		response.Error(ctx, err)
		return
	}
	instrument, err := c.instruments.GetByID(ctx, entity.InstrumentID)
	if err != nil {
		response.Error(ctx, err)
		return
	}
	response.OK(ctx, "FEED_FETCHED", feedresponse.FromDomainDetail(*entity, instrument.AssetClass))
}

// random godoc
// @Summary Pick a random unseen feed
// @Description Chooses uniformly from published feeds the caller has never opened a session against.
// @Tags feeds
// @Security BearerAuth
// @Success 200 {object} response.Envelope{data=feedresponse.Detail}
// @Failure 404 {object} response.Envelope
// @Router /feeds/random [post]
func (c *controller) random(ctx *gin.Context) {
	filter, ok := filterFrom(ctx)
	if !ok {
		return
	}
	entity, err := c.feeds.Random(ctx, actor(ctx).UserID, filter)
	if err != nil {
		response.Error(ctx, err)
		return
	}
	instrument, err := c.instruments.GetByID(ctx, entity.InstrumentID)
	if err != nil {
		response.Error(ctx, err)
		return
	}
	response.OK(ctx, "FEED_SELECTED", feedresponse.FromDomainDetail(*entity, instrument.AssetClass))
}

// bars godoc
// @Summary Read blinded candles
// @Description Bars carry an index, never a timestamp. Sprint 03's session cursor bounds how far a trader may read.
// @Tags feeds
// @Security BearerAuth
// @Param id path string true "Feed ID"
// @Param from query int false "First bar index"
// @Param to query int false "Last bar index"
// @Success 200 {object} response.Envelope{data=feedresponse.Bars}
// @Failure 400 {object} response.Envelope
// @Router /feeds/{id}/bars [get]
func (c *controller) bars(ctx *gin.Context) {
	id, err := uuid.Parse(ctx.Param("id"))
	if err != nil {
		response.Error(ctx, apperror.New("FEED_NOT_FOUND"))
		return
	}
	from, to := 0, 0
	if raw := ctx.Query("from"); raw != "" {
		if from, err = strconv.Atoi(raw); err != nil {
			response.Error(ctx, apperror.New("INVALID_CURSOR"))
			return
		}
	}
	if raw := ctx.Query("to"); raw != "" {
		if to, err = strconv.Atoi(raw); err != nil {
			response.Error(ctx, apperror.New("INVALID_CURSOR"))
			return
		}
	} else {
		// Without an explicit end, serve the warmup lookback only. Defaulting to the whole feed
		// would hand a trader every future bar in one request, which is the one thing this
		// endpoint must never do.
		entity, err := c.feeds.Get(ctx, id)
		if err != nil {
			response.Error(ctx, err)
			return
		}
		to = entity.WarmupBars - 1
		if to < from {
			to = from
		}
	}
	blinded, err := c.feeds.Bars(ctx, id, from, to)
	if err != nil {
		response.Error(ctx, err)
		return
	}
	entity, err := c.feeds.Get(ctx, id)
	if err != nil {
		response.Error(ctx, err)
		return
	}
	response.OK(ctx, "FEED_BARS_FETCHED", feedresponse.Bars{
		FeedID: id.String(), Timeframe: string(entity.BaseTimeframe), From: from, To: to,
		Bars: feedresponse.FromDomainBars(blinded),
	})
}
