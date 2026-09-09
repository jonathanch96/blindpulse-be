package refresh_tokens

import (
	"context"

	"github.com/google/uuid"
	userdomain "github.com/jblabs/blindpulse-be/services/blindpulse/v1/domain/user"
	domainuser "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/user"
)

type Repository interface {
	Store(context.Context, domainuser.RefreshToken) error
	GetByHash(context.Context, string) (*domainuser.RefreshToken, error)
	Revoke(context.Context, uuid.UUID) error
	RevokeAllForUser(context.Context, uuid.UUID) error
}

var _ userdomain.TokenRepository = (*adapterGormPostgresql)(nil)
var _ Repository = (*adapterGormPostgresql)(nil)
