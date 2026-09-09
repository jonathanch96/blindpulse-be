package users

import (
	"context"

	"github.com/google/uuid"
	userdomain "github.com/jblabs/blindpulse-be/services/blindpulse/v1/domain/user"
	domainuser "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/user"
)

type Repository interface {
	Create(context.Context, *domainuser.User) (*domainuser.User, error)
	GetByID(context.Context, uuid.UUID) (*domainuser.User, error)
	GetByEmail(context.Context, string) (*domainuser.User, error)
	ExistsByEmail(context.Context, string) (bool, error)
	Update(context.Context, *domainuser.User) (*domainuser.User, error)
	ListByIDs(context.Context, []uuid.UUID) ([]domainuser.User, error)
}

var _ userdomain.Repository = (*adapterGormPostgresql)(nil)
var _ Repository = (*adapterGormPostgresql)(nil)
