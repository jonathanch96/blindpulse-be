package session_reveals

import revealdomain "github.com/jblabs/blindpulse-be/services/blindpulse/v1/domain/reveal"

var _ revealdomain.Repository = (*adapterGormPostgresql)(nil)
