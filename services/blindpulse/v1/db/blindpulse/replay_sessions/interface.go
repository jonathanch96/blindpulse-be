package replay_sessions

import sessiondomain "github.com/jblabs/blindpulse-be/services/blindpulse/v1/domain/session"

var _ sessiondomain.Repository = (*adapterGormPostgresql)(nil)
