package session

import (
	"reflect"
	"testing"

	domainsession "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/session"
)

func frameAt(index int) domainsession.Frame {
	return domainsession.Frame{Kind: domainsession.FrameBar, CursorIndex: index}
}

// The socket half of FR-REPLAY-05's backpressure rule. When frames are already queued the client
// is not keeping up, so only the newest is written — the skipped bars are recoverable, because the
// client sees the jump in bar index and backfills over HTTP, but a stale frame is not.
func TestDrainToLatestKeepsOnlyTheNewestQueuedFrame(t *testing.T) {
	t.Parallel()
	frames := make(chan domainsession.Frame, 8)
	for index := 11; index <= 15; index++ {
		frames <- frameAt(index)
	}

	kept, dropped := drainToLatest(frames, frameAt(10))
	if kept.CursorIndex != 15 {
		t.Errorf("kept index %d, want 15", kept.CursorIndex)
	}
	// Five frames were behind the one in hand; the one in hand is not a drop.
	if dropped != 5 {
		t.Errorf("dropped = %d, want 5", dropped)
	}
	if len(frames) != 0 {
		t.Errorf("%d frames left queued after the drain", len(frames))
	}
}

// The normal case: a client keeping up has nothing queued, so the drain must be a no-op rather
// than a mechanism that quietly discards bars from a healthy connection.
func TestDrainToLatestIsANoOpForAClientKeepingUp(t *testing.T) {
	t.Parallel()
	frames := make(chan domainsession.Frame, 8)
	kept, dropped := drainToLatest(frames, frameAt(7))
	if kept.CursorIndex != 7 || dropped != 0 {
		t.Errorf("drain returned (%d, %d), want (7, 0)", kept.CursorIndex, dropped)
	}
}

func TestDrainToLatestStopsAtAClosedChannel(t *testing.T) {
	t.Parallel()
	frames := make(chan domainsession.Frame, 4)
	frames <- frameAt(2)
	close(frames)

	kept, dropped := drainToLatest(frames, frameAt(1))
	if kept.CursorIndex != 2 || dropped != 1 {
		t.Errorf("drain returned (%d, %d), want (2, 1)", kept.CursorIndex, dropped)
	}
}

// The websocket handshake checks Origin, and the configured CORS origins are full URLs while the
// handshake wants bare hosts. Getting this wrong fails open or fails shut, and both are bad.
func TestOriginPatternsReduceConfiguredOriginsToHosts(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		given  []string
		expect []string
	}{
		{"strips the scheme", []string{"https://app.blindpulse.io"}, []string{"app.blindpulse.io"}},
		{"keeps the port", []string{"http://localhost:3100"}, []string{"localhost:3100"}},
		{"drops blanks", []string{"", "  ", "http://localhost:3100"}, []string{"localhost:3100"}},
		{"passes a bare host through", []string{"localhost:3100"}, []string{"localhost:3100"}},
		{"wildcard wins outright", []string{"http://a.example", "*"}, []string{"*"}},
		{"nothing configured means same-origin only", nil, []string{}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()
			if got := originPatterns(testCase.given); !reflect.DeepEqual(got, testCase.expect) {
				t.Errorf("originPatterns(%q) = %q, want %q", testCase.given, got, testCase.expect)
			}
		})
	}
}
