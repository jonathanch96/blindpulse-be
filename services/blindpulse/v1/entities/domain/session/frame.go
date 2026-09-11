package session

import (
	"time"

	"github.com/google/uuid"
	domainfeed "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/feed"
)

// FrameKind names what a frame carries. The kinds are deliberately few: a stream the client has to
// interpret in many ways is a stream the client will interpret wrongly.
type FrameKind string

const (
	// FrameSync is the first frame on every connection, and the only answer to a reconnect. It
	// states where the *server* is. A client that reconnects announcing its last index gets this
	// back regardless of what it claimed (BR-02).
	FrameSync FrameKind = "sync"
	// FrameBar carries one newly released bar at the session's viewing timeframe.
	FrameBar FrameKind = "bar"
	// FrameState carries a transition the client did not initiate — a pause, a speed change, the
	// end of the feed.
	FrameState FrameKind = "state"
	// FrameHeartbeat proves the socket is alive during a pause, when no bars flow.
	FrameHeartbeat FrameKind = "heartbeat"
)

// Frame is the internal envelope that crosses the pub/sub bus between API replicas.
//
// It carries ReleasedAt, a server wall clock, which is how the socket layer computes the latency
// the UI displays. That instant is "when this replay engine released the bar", never anything
// about when the bar traded — but it is still a date, and a date is exactly what must not reach a
// client (NFR-05). So this type stops at the socket: the wire type in entities/response/session
// carries a latency in milliseconds and no date at all, and a leak test holds that line.
type Frame struct {
	SessionID uuid.UUID `json:"session_id"`
	Kind      FrameKind `json:"kind"`
	Sequence  int64     `json:"sequence"`
	Status    Status    `json:"status"`
	Timeframe string    `json:"timeframe"`
	Speed     string    `json:"speed"`
	// CursorIndex is where the session is: both the bar the trader is on and the furthest bar
	// released, which are the same number because the cursor only moves forward.
	CursorIndex int `json:"cursor_index"`
	TotalBars   int `json:"total_bars"`
	// Bar is the tail bar of the session's current timeframe view. On a higher timeframe it is
	// the forming bucket and its index repeats across frames until the bucket closes, so a client
	// replaces its last bar by index rather than appending blindly.
	Bar        *domainfeed.Bar `json:"bar,omitempty"`
	ReleasedAt time.Time       `json:"released_at"`
}

// StreamTicket is the short-lived, single-use credential a browser presents to open a replay
// websocket. It names the session and the trader it was issued to, and nothing else: it is not a
// session token and cannot be used to reach anything but this one stream.
type StreamTicket struct {
	SessionID uuid.UUID
	UserID    uuid.UUID
}
