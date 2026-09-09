package account_ledger_entries

import (
	accountdomain "github.com/jblabs/blindpulse-be/services/blindpulse/v1/domain/account"
)

var _ accountdomain.LedgerRepository = (*adapterGormPostgresql)(nil)
