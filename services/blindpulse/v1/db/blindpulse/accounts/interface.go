package accounts

import (
	accountdomain "github.com/jblabs/blindpulse-be/services/blindpulse/v1/domain/account"
)

var _ accountdomain.Repository = (*adapterGormPostgresql)(nil)
