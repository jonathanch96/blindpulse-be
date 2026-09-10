package session

import (
	feeddomain "github.com/jblabs/blindpulse-be/services/blindpulse/v1/domain/feed"
	sessiondomain "github.com/jblabs/blindpulse-be/services/blindpulse/v1/domain/session"
)

type controller struct {
	sessions sessiondomain.Service
	feeds    feeddomain.Service
	// origins are the host patterns the websocket handshake accepts. Empty means same-origin
	// only — a websocket that accepts any origin is a CSRF vector that survives every other
	// precaution the API takes.
	origins []string
}
