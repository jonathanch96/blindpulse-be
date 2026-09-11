package sessionresponse

import (
	"time"

	domainfeed "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/feed"
	domainsession "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/session"
	feedresponse "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/response/feed"
)

// Frame is the replay stream's wire envelope.
//
// It is a separate type from the internal domainsession.Frame for one reason: the internal frame
// carries ReleasedAt, a server wall clock. That instant says nothing about when the bar traded —
// it is when this engine released it — but it is still a date, and a date on a blinded feed is the
// thread a determined trader pulls (NFR-05). So the conversion drops it and keeps the only part
// the UI actually needs: how long the frame took to arrive.
//
// The absence of a date here is a property under test, not a convention. See frame_leak_test.go.
type Frame struct {
	Kind      string `json:"kind"`
	Status    string `json:"status"`
	Timeframe string `json:"timeframe"`
	Speed     string `json:"speed"`
	// CursorIndex is the session's one forward-only position, and BarsScanned is that same
	// position counted from one for the "142 / 500 bars scanned" readout. Both are sent so the
	// client's progress display and its bar-window bound cannot disagree about the off-by-one.
	CursorIndex int `json:"cursor_index"`
	BarsScanned int `json:"bars_scanned"`
	TotalBars   int `json:"total_bars"`
	// LatencyMs is the simulated feed latency the terminal displays (FR-REPLAY-08): the time
	// between this engine releasing the bar and this frame being written to this socket. Measured
	// per connection, so a client on a congested replica sees its own number rather than an
	// average that flatters it.
	LatencyMs int `json:"latency_ms"`
	// Bar is the tail bar of the session's current timeframe view — on a higher timeframe, the
	// forming bucket, whose index repeats until it closes. Replace your last bar by index.
	Bar *feedresponse.Bar `json:"bar,omitempty"`
}

// FrameFromDomain converts at the socket boundary, taking the latency from the caller's clock so
// the number reflects this connection rather than the moment the frame was built.
func FrameFromDomain(frame domainsession.Frame, now time.Time) Frame {
	wire := Frame{
		Kind:        string(frame.Kind),
		Status:      string(frame.Status),
		Timeframe:   frame.Timeframe,
		Speed:       frame.Speed,
		CursorIndex: frame.CursorIndex,
		BarsScanned: frame.CursorIndex + 1,
		TotalBars:   frame.TotalBars,
		LatencyMs:   latencyMs(frame.ReleasedAt, now),
	}
	if frame.Bar != nil {
		bars := feedresponse.FromDomainBars([]domainfeed.Bar{*frame.Bar})
		if len(bars) == 1 {
			wire.Bar = &bars[0]
		}
	}
	return wire
}

// latencyMs floors at zero: clock skew between the replica that released the bar and the one
// serving the socket can make the difference negative, and a negative latency in the UI reads as
// a bug in the product rather than in the clocks.
func latencyMs(releasedAt, now time.Time) int {
	if releasedAt.IsZero() {
		return 0
	}
	elapsed := now.Sub(releasedAt).Milliseconds()
	if elapsed < 0 {
		return 0
	}
	return int(elapsed)
}

// StreamTicket is the handshake credential handed to a browser. Path is returned alongside it so
// the client does not build the socket URL from its own idea of the route.
type StreamTicket struct {
	Ticket           string `json:"ticket"`
	ExpiresInSeconds int    `json:"expires_in_seconds"`
	Path             string `json:"path"`
}
