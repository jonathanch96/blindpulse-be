package trades

import revealdomain "github.com/jblabs/blindpulse-be/services/blindpulse/v1/domain/reveal"

var _ revealdomain.TradeReader = (*adapterGormPostgresql)(nil)
