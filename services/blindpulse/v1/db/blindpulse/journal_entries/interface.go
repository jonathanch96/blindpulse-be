package journal_entries

import journaldomain "github.com/jblabs/blindpulse-be/services/blindpulse/v1/domain/journal"

var _ journaldomain.Repository = (*adapterGormPostgresql)(nil)
