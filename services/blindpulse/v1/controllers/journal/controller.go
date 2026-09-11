package journal

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jblabs/blindpulse-be/pkg/apperror"
	"github.com/jblabs/blindpulse-be/pkg/identity"
	"github.com/jblabs/blindpulse-be/pkg/response"
	journaldomain "github.com/jblabs/blindpulse-be/services/blindpulse/v1/domain/journal"
	domainjournal "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/journal"
	journalrequest "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/request/journal"
	journalresponse "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/response/journal"
)

type controller struct{ journal journaldomain.Service }

func NewController(service journaldomain.Service) Controller {
	return &controller{journal: service}
}

func (c *controller) RegisterRoutes(group *gin.RouterGroup) {
	group.POST("/sessions/:id/journal", c.write)
	group.GET("/sessions/:id/journal", c.list)
	group.PATCH("/sessions/:id/journal/:jid", c.edit)
	group.DELETE("/sessions/:id/journal/:jid", c.remove)
	group.GET("/sessions/:id/journal/:jid/revisions", c.revisions)
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

// write godoc
// @Summary Write a journal entry
// @Description Anchors a note to a bar the session has released. A bar past the cursor is refused.
// @Tags journal
// @Security BearerAuth
// @Param id path string true "Session ID"
// @Param body body journalrequest.Write true "Entry"
// @Success 201 {object} response.Envelope{data=journalresponse.Entry}
// @Failure 400 {object} response.Envelope
// @Failure 404 {object} response.Envelope
// @Router /sessions/{id}/journal [post]
func (c *controller) write(ctx *gin.Context) {
	sessionID, ok := pathID(ctx, "id", "SESSION_NOT_FOUND")
	if !ok {
		return
	}
	var request journalrequest.Write
	if !bind(ctx, &request) {
		return
	}
	input := journaldomain.WriteInput{
		BarIndex: *request.BarIndex, Conviction: request.Conviction, Tags: request.Tags,
	}
	if request.TradeID != "" {
		tradeID, err := uuid.Parse(request.TradeID)
		if err != nil {
			response.Error(ctx, apperror.New("TRADE_NOT_FOUND"))
			return
		}
		input.TradeID = &tradeID
	}
	input.Thesis = optional(request.Thesis)
	input.Note = optional(request.Note)
	input.Emotion = emotion(request.Emotion)

	entry, err := c.journal.Write(ctx, actor(ctx).UserID, sessionID, input)
	if err != nil {
		response.Error(ctx, err)
		return
	}
	response.Created(ctx, "JOURNAL_WRITTEN", journalresponse.FromDomain(*entry))
}

// list godoc
// @Summary List a session's journal
// @Tags journal
// @Security BearerAuth
// @Param id path string true "Session ID"
// @Success 200 {object} response.Envelope{data=[]journalresponse.Entry}
// @Failure 404 {object} response.Envelope
// @Router /sessions/{id}/journal [get]
func (c *controller) list(ctx *gin.Context) {
	sessionID, ok := pathID(ctx, "id", "SESSION_NOT_FOUND")
	if !ok {
		return
	}
	entries, err := c.journal.List(ctx, actor(ctx).UserID, sessionID)
	if err != nil {
		response.Error(ctx, err)
		return
	}
	response.OK(ctx, "JOURNAL_FETCHED", journalresponse.FromDomains(entries))
}

// edit godoc
// @Summary Edit a journal entry
// @Description The superseded content is retained as a revision; the bar anchor cannot change.
// @Tags journal
// @Security BearerAuth
// @Param id path string true "Session ID"
// @Param jid path string true "Entry ID"
// @Param body body journalrequest.Edit true "Changes"
// @Success 200 {object} response.Envelope{data=journalresponse.Entry}
// @Failure 404 {object} response.Envelope
// @Failure 409 {object} response.Envelope
// @Router /sessions/{id}/journal/{jid} [patch]
func (c *controller) edit(ctx *gin.Context) {
	entryID, ok := pathID(ctx, "jid", "JOURNAL_NOT_FOUND")
	if !ok {
		return
	}
	var request journalrequest.Edit
	if !bind(ctx, &request) {
		return
	}
	input := journaldomain.EditInput{
		Thesis: request.Thesis, Note: request.Note, Conviction: request.Conviction, Tags: request.Tags,
	}
	if request.Emotion != nil {
		input.Emotion = emotion(*request.Emotion)
	}
	entry, err := c.journal.Edit(ctx, actor(ctx).UserID, entryID, input)
	if err != nil {
		response.Error(ctx, err)
		return
	}
	response.OK(ctx, "JOURNAL_UPDATED", journalresponse.FromDomain(*entry))
}

// remove godoc
// @Summary Delete a journal entry
// @Tags journal
// @Security BearerAuth
// @Param id path string true "Session ID"
// @Param jid path string true "Entry ID"
// @Success 200 {object} response.Envelope
// @Failure 404 {object} response.Envelope
// @Router /sessions/{id}/journal/{jid} [delete]
func (c *controller) remove(ctx *gin.Context) {
	entryID, ok := pathID(ctx, "jid", "JOURNAL_NOT_FOUND")
	if !ok {
		return
	}
	if err := c.journal.Delete(ctx, actor(ctx).UserID, entryID); err != nil {
		response.Error(ctx, err)
		return
	}
	response.NoData(ctx, "JOURNAL_DELETED")
}

// revisions godoc
// @Summary Read what an entry said before each edit
// @Tags journal
// @Security BearerAuth
// @Param id path string true "Session ID"
// @Param jid path string true "Entry ID"
// @Success 200 {object} response.Envelope{data=[]journalresponse.Revision}
// @Failure 404 {object} response.Envelope
// @Router /sessions/{id}/journal/{jid}/revisions [get]
func (c *controller) revisions(ctx *gin.Context) {
	entryID, ok := pathID(ctx, "jid", "JOURNAL_NOT_FOUND")
	if !ok {
		return
	}
	revisions, err := c.journal.Revisions(ctx, actor(ctx).UserID, entryID)
	if err != nil {
		response.Error(ctx, err)
		return
	}
	response.OK(ctx, "JOURNAL_REVISIONS_FETCHED", journalresponse.RevisionsFromDomain(revisions))
}

// optional turns an absent string into an absent field. The write form sends "" for a field the
// trader left blank, and storing an empty thesis would put a blank line in the post-mortem.
func optional(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func emotion(value string) *domainjournal.Emotion {
	if value == "" {
		return nil
	}
	parsed := domainjournal.Emotion(value)
	return &parsed
}
