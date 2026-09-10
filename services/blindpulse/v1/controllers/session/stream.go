package session

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jblabs/blindpulse-be/pkg/apperror"
	"github.com/jblabs/blindpulse-be/pkg/response"
	domainsession "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/session"
	sessionresponse "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/response/session"
)

const (
	// heartbeatInterval keeps the socket demonstrably alive while a session is paused, when no
	// bars flow. Ten seconds is comfortably inside the idle timeout of every proxy worth caring
	// about, and cheap enough that it costs nothing at rest.
	heartbeatInterval = 10 * time.Second
	// writeTimeout is the backpressure boundary. A client that cannot absorb one frame in this
	// long is not slow, it is gone, and holding the socket open for it only pins memory.
	writeTimeout = 5 * time.Second
)

// RegisterStreamRoutes mounts the websocket outside the bearer middleware.
//
// It has to be outside: a browser cannot set an Authorization header on a WebSocket handshake.
// Authentication happens instead through a single-use ticket the caller obtained over authenticated
// HTTP, redeemed below before a single frame is sent.
func (c *controller) RegisterStreamRoutes(group *gin.RouterGroup) {
	group.GET("/ws/sessions/:id", c.stream)
}

// streamTicket godoc
// @Summary Mint a websocket ticket
// @Description Trades the caller's bearer for a single-use, short-lived ticket for one session's stream.
// @Tags sessions
// @Security BearerAuth
// @Param id path string true "Session ID"
// @Success 200 {object} response.Envelope{data=sessionresponse.StreamTicket}
// @Failure 404 {object} response.Envelope
// @Router /sessions/{id}/stream-ticket [post]
func (c *controller) streamTicket(ctx *gin.Context) {
	id, ok := sessionID(ctx)
	if !ok {
		return
	}
	token, ttl, err := c.sessions.IssueStreamTicket(ctx, actor(ctx).UserID, id)
	if err != nil {
		response.Error(ctx, err)
		return
	}
	response.OK(ctx, "STREAM_TICKET_ISSUED", sessionresponse.StreamTicket{
		Ticket: token, ExpiresInSeconds: int(ttl.Seconds()), Path: "/ws/sessions/" + id.String(),
	})
}

// stream is the replay socket. The server drives; the client listens.
//
// The only thing a client may send is a hello announcing where it thinks it is, and the answer to
// that is a sync frame stating where the *server* is — the client's claim is read for logging and
// nothing else (BR-02). A client that could move the cursor by asserting a position would be able
// to walk itself forward through the feed.
func (c *controller) stream(ctx *gin.Context) {
	id, ok := sessionID(ctx)
	if !ok {
		return
	}
	ticket, err := c.sessions.RedeemStreamTicket(ctx, ctx.Query("ticket"))
	if err != nil {
		response.Error(ctx, err)
		return
	}
	// A ticket is minted for one session. Redeeming a valid ticket against a different session id
	// would otherwise turn any ticket into a key to every session that trader owns.
	if ticket.SessionID != id {
		response.Error(ctx, apperror.New("STREAM_TICKET_INVALID"))
		return
	}

	conn, err := websocket.Accept(ctx.Writer, ctx.Request, &websocket.AcceptOptions{
		OriginPatterns: c.origins,
	})
	if err != nil {
		slog.Debug("websocket upgrade failed", "session_id", id, "error", err)
		return
	}
	defer conn.CloseNow() //nolint:errcheck // the deferred close is a backstop for the paths below

	// Detached from the request context: gin cancels that when the handler returns, and for a
	// hijacked connection the handler returning is not the connection ending.
	streamCtx, cancel := context.WithCancel(context.WithoutCancel(ctx.Request.Context()))
	defer cancel()

	frames, unsubscribe, err := c.sessions.Stream(streamCtx, ticket.UserID, id)
	if err != nil {
		closeWith(conn, err)
		return
	}
	defer unsubscribe()

	// The opening frame is a sync, always. A fresh connection and a reconnect ask the same
	// question — where am I? — and get the same answer from the same place.
	if snapshot, err := c.sessions.Snapshot(streamCtx, ticket.UserID, id); err == nil {
		if err := writeFrame(streamCtx, conn, *snapshot); err != nil {
			return
		}
	}

	go c.readClient(streamCtx, cancel, conn, ticket.UserID, id)

	heartbeat := time.NewTicker(heartbeatInterval)
	defer heartbeat.Stop()

	for {
		select {
		case <-streamCtx.Done():
			_ = conn.Close(websocket.StatusNormalClosure, "")
			return
		case <-heartbeat.C:
			if err := writeFrame(streamCtx, conn, domainsession.Frame{Kind: domainsession.FrameHeartbeat}); err != nil {
				return
			}
		case frame, ok := <-frames:
			if !ok {
				_ = conn.Close(websocket.StatusNormalClosure, "")
				return
			}
			// Backpressure, per FR-REPLAY-05: if frames are already queued behind this one the
			// client is not keeping up, so collapse them and send only the newest. A stale frame
			// is worse than a skipped one — the trader acts on what is on screen. The skipped
			// bars are recoverable: the client sees the jump in bar index and backfills over HTTP.
			frame, dropped := drainToLatest(frames, frame)
			if dropped > 0 {
				slog.Debug("replay socket fell behind", "session_id", id, "dropped_frames", dropped)
			}
			if err := writeFrame(streamCtx, conn, frame); err != nil {
				return
			}
			if frame.Status == domainsession.StatusClosed || frame.Status == domainsession.StatusAbandoned {
				_ = conn.Close(websocket.StatusNormalClosure, "session closed")
				return
			}
		}
	}
}

// drainToLatest takes everything already queued and keeps only the newest frame.
//
// When the client is keeping up the channel is empty and this is a no-op, so it costs nothing in
// the normal case and only engages for a socket that is genuinely behind.
func drainToLatest(frames <-chan domainsession.Frame, current domainsession.Frame) (domainsession.Frame, int) {
	dropped := 0
	for {
		select {
		case next, ok := <-frames:
			if !ok {
				return current, dropped
			}
			dropped++
			current = next
		default:
			return current, dropped
		}
	}
}

// readClient drains the inbound direction. It exists mostly so close frames are processed — a
// websocket that is never read never notices the peer going away — and so a reconnecting client
// can ask where it is.
func (c *controller) readClient(ctx context.Context, cancel context.CancelFunc, conn *websocket.Conn, userID, id uuid.UUID) {
	defer cancel()
	for {
		_, raw, err := conn.Read(ctx)
		if err != nil {
			return
		}
		var hello clientMessage
		if err := json.Unmarshal(raw, &hello); err != nil || hello.Type != "hello" {
			continue
		}
		slog.Debug("replay client announced a position",
			"session_id", id, "claimed_index", hello.LastIndex)
		snapshot, err := c.sessions.Snapshot(ctx, userID, id)
		if err != nil {
			return
		}
		if err := writeFrame(ctx, conn, *snapshot); err != nil {
			return
		}
	}
}

type clientMessage struct {
	Type string `json:"type"`
	// LastIndex is what the client believes it last saw. It is logged and then discarded: the
	// answer is a sync frame carrying the server's cursor, whatever the client claimed.
	LastIndex int `json:"last_index"`
}

func writeFrame(ctx context.Context, conn *websocket.Conn, frame domainsession.Frame) error {
	writeCtx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	payload, err := json.Marshal(sessionresponse.FrameFromDomain(frame, time.Now().UTC()))
	if err != nil {
		return err
	}
	return conn.Write(writeCtx, websocket.MessageText, payload)
}

// closeWith maps a domain refusal onto a close code, so a client can tell "your session is over"
// from "the server fell over" without parsing prose.
func closeWith(conn *websocket.Conn, err error) {
	code := websocket.StatusInternalError
	var appErr *apperror.Error
	if errors.As(err, &appErr) {
		switch appErr.Code {
		case "SESSION_NOT_FOUND", "SESSION_CLOSED":
			code = websocket.StatusPolicyViolation
		case "STREAMING_UNAVAILABLE":
			code = websocket.StatusTryAgainLater
		}
		_ = conn.Close(code, appErr.Code)
		return
	}
	_ = conn.Close(code, "stream failed")
}

// originPatterns turns configured CORS origins into the host patterns the websocket handshake
// checks. An empty list means same-origin only, which is the right default: a websocket that
// accepts any origin is a CSRF vector that survives every other precaution.
func originPatterns(origins []string) []string {
	patterns := make([]string, 0, len(origins))
	for _, origin := range origins {
		trimmed := strings.TrimSpace(origin)
		if trimmed == "" {
			continue
		}
		if trimmed == "*" {
			return []string{"*"}
		}
		if parsed, err := url.Parse(trimmed); err == nil && parsed.Host != "" {
			patterns = append(patterns, parsed.Host)
			continue
		}
		patterns = append(patterns, trimmed)
	}
	return patterns
}
