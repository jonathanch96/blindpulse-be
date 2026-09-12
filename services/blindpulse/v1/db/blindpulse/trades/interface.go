package trades

import (
	executiondomain "github.com/jblabs/blindpulse-be/services/blindpulse/v1/domain/execution"
	revealdomain "github.com/jblabs/blindpulse-be/services/blindpulse/v1/domain/reveal"
)

var (
	_ revealdomain.TradeReader        = (*adapterGormPostgresql)(nil)
	_ executiondomain.TradeRepository = (*adapterGormPostgresql)(nil)
)
