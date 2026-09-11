package journal

import (
	"errors"
	"io"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jblabs/blindpulse-be/pkg/apperror"
	"github.com/jblabs/blindpulse-be/pkg/identity"
	"github.com/jblabs/blindpulse-be/pkg/media"
	"github.com/jblabs/blindpulse-be/pkg/response"
	journaldomain "github.com/jblabs/blindpulse-be/services/blindpulse/v1/domain/journal"
	domainjournal "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/journal"
	journalrequest "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/request/journal"
	journalresponse "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/response/journal"
)

type controller struct {
	journal journaldomain.Service
	signer  *media.Signer
	baseURL string
	maxBody int64
}

func NewController(service journaldomain.Service, signer *media.Signer, baseURL string, maxBody int64) Controller {
	if maxBody <= 0 {
		maxBody = 5 << 20
	}
	return &controller{journal: service, signer: signer, baseURL: baseURL, maxBody: maxBody}
}

func (c *controller) RegisterRoutes(group *gin.RouterGroup) {
	group.POST("/sessions/:id/journal", c.write)
	group.GET("/sessions/:id/journal", c.list)
	group.PATCH("/sessions/:id/journal/:jid", c.edit)
	group.DELETE("/sessions/:id/journal/:jid", c.remove)
	group.GET("/sessions/:id/journal/:jid/revisions", c.revisions)
	group.POST("/sessions/:id/journal/:jid/media", c.attachMedia)
}

// RegisterMediaRoutes mounts the image itself, outside the bearer middleware.
//
// Same constraint that produced the websocket's ticket: an <img src> cannot carry an Authorization
// header. So the link carries the authority — an HMAC over the key and an expiry, minted when the
// owner read the entry — and it expires in minutes.
func (c *controller) RegisterMediaRoutes(group *gin.RouterGroup) {
	// A wildcard, not a named segment. A storage key contains slashes ("journal/<id>-<n>.png"), and
	// Go normalizes %2F back to a literal "/" before routing — so a :key parameter simply never
	// matched and every signed link 404'd. The wildcard captures the rest of the path, leading slash
	// included, which is stripped below.
	group.GET("/journal-media/*key", c.serveMedia)
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
	response.Created(ctx, "JOURNAL_WRITTEN", c.render(*entry))
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
	response.OK(ctx, "JOURNAL_FETCHED", c.renderAll(entries))
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
	response.OK(ctx, "JOURNAL_UPDATED", c.render(*entry))
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

// render signs the entry's media link on the way out. The URL is minted per response rather than
// stored, because it expires: a signed link in the database would be a link that was valid once.
func (c *controller) render(entry domainjournal.Entry) journalresponse.Entry {
	wire := journalresponse.FromDomain(entry)
	if entry.MediaKey != nil && c.signer.Enabled() {
		url := c.signer.URL(c.baseURL, *entry.MediaKey)
		wire.MediaURL = &url
	}
	return wire
}

func (c *controller) renderAll(entries []domainjournal.Entry) []journalresponse.Entry {
	list := make([]journalresponse.Entry, 0, len(entries))
	for _, entry := range entries {
		list = append(list, c.render(entry))
	}
	return list
}

// attachMedia godoc
// @Summary Attach an image to a journal entry
// @Description JPEG or PNG, re-encoded from its pixels so no EXIF survives. One image per entry; a second replaces the first.
// @Tags journal
// @Security BearerAuth
// @Accept mpfd
// @Param id path string true "Session ID"
// @Param jid path string true "Entry ID"
// @Param file formData file true "Image"
// @Success 200 {object} response.Envelope{data=journalresponse.Entry}
// @Failure 413 {object} response.Envelope
// @Failure 415 {object} response.Envelope
// @Router /sessions/{id}/journal/{jid}/media [post]
func (c *controller) attachMedia(ctx *gin.Context) {
	entryID, ok := pathID(ctx, "jid", "JOURNAL_NOT_FOUND")
	if !ok {
		return
	}
	// The body is capped before it is read, not after. Reading an unbounded multipart into memory
	// to discover it is too large is the denial of service the limit exists to prevent.
	ctx.Request.Body = http.MaxBytesReader(ctx.Writer, ctx.Request.Body, c.maxBody+multipartOverhead)
	file, err := ctx.FormFile("file")
	if err != nil {
		// The reader's own limit trips before the part is parsed, so "too large" arrives here as a
		// parse failure. Reported as what it is: a trader told their 8MB screenshot was malformed
		// would resize nothing and try again.
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			response.Error(ctx, apperror.New("FILE_TOO_LARGE"))
			return
		}
		response.Error(ctx, apperror.WithFields("VALIDATION_FAILED", []apperror.FieldError{
			{Field: "file", Rule: "required", Message: "attach an image in a multipart field named file"},
		}))
		return
	}
	if file.Size > c.maxBody {
		response.Error(ctx, apperror.New("FILE_TOO_LARGE"))
		return
	}
	opened, err := file.Open()
	if err != nil {
		response.Error(ctx, apperror.New("UNSUPPORTED_MEDIA_TYPE"))
		return
	}
	defer func() { _ = opened.Close() }()
	// LimitReader as well as the cap above: FormFile's size is what the client declared in the
	// part header, and a client that lies about it should run out of bytes rather than out of heap.
	upload, err := io.ReadAll(io.LimitReader(opened, c.maxBody+1))
	if err != nil {
		response.Error(ctx, apperror.Wrap(err, "INTERNAL_ERROR"))
		return
	}
	entry, err := c.journal.AttachMedia(ctx, actor(ctx).UserID, entryID, upload)
	if err != nil {
		response.Error(ctx, err)
		return
	}
	response.OK(ctx, "JOURNAL_MEDIA_ATTACHED", c.render(*entry))
}

// serveMedia godoc
// @Summary Fetch a journal image by signed link
// @Tags journal
// @Param key path string true "Storage key"
// @Param exp query string true "Expiry"
// @Param sig query string true "Signature"
// @Success 200 {file} file
// @Failure 403 {object} response.Envelope
// @Router /journal-media/{key} [get]
func (c *controller) serveMedia(ctx *gin.Context) {
	key := strings.TrimPrefix(ctx.Param("key"), "/")
	if err := c.signer.Verify(key, ctx.Query("exp"), ctx.Query("sig")); err != nil {
		response.Error(ctx, apperror.New("MEDIA_LINK_INVALID"))
		return
	}
	content, contentType, err := c.journal.Media(ctx, key)
	if err != nil {
		response.Error(ctx, err)
		return
	}
	// Private and short-lived: the URL expires, so a shared cache holding the bytes past that point
	// would outlive the authority that released them. nosniff because the content type is decided
	// by what the decoder produced, and a browser guessing differently is how an image becomes a
	// document.
	ctx.Header("Cache-Control", "private, max-age=60")
	ctx.Header("X-Content-Type-Options", "nosniff")
	ctx.Data(http.StatusOK, contentType, content)
}

// multipartOverhead is the slack for the part headers and boundaries wrapping the file itself. The
// cap is about the image, and a request rejected for its boundary bytes would be a limit that
// behaves differently depending on the field name.
const multipartOverhead = 8 << 10

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
