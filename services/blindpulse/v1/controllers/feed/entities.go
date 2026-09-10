package feed

import (
	feeddomain "github.com/jblabs/blindpulse-be/services/blindpulse/v1/domain/feed"
)

type controller struct {
	feeds       feeddomain.Service
	instruments feeddomain.InstrumentRepository
}
