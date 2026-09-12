package orders

import executiondomain "github.com/jblabs/blindpulse-be/services/blindpulse/v1/domain/execution"

var _ executiondomain.OrderRepository = (*adapterGormPostgresql)(nil)
