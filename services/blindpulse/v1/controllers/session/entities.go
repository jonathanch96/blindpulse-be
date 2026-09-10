package session

import (
	feeddomain "github.com/jblabs/blindpulse-be/services/blindpulse/v1/domain/feed"
	sessiondomain "github.com/jblabs/blindpulse-be/services/blindpulse/v1/domain/session"
)

type controller struct {
	sessions sessiondomain.Service
	feeds    feeddomain.Service
}
