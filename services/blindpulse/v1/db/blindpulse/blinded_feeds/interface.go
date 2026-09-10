package blinded_feeds

import feeddomain "github.com/jblabs/blindpulse-be/services/blindpulse/v1/domain/feed"

var _ feeddomain.Repository = (*adapterGormPostgresql)(nil)
