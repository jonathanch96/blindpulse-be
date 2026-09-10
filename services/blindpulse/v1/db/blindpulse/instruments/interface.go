package instruments

import feeddomain "github.com/jblabs/blindpulse-be/services/blindpulse/v1/domain/feed"

var _ feeddomain.InstrumentRepository = (*adapterGormPostgresql)(nil)
