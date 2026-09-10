package market_bars

import feeddomain "github.com/jblabs/blindpulse-be/services/blindpulse/v1/domain/feed"

var _ feeddomain.BarRepository = (*adapterGormPostgresql)(nil)
