package refresh_tokens

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jblabs/blindpulse-be/pkg/apperror"
	domainuser "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/user"
	"gorm.io/gorm"
)

type adapterGormPostgresql struct{ db *gorm.DB }

func NewGormPostgresqlAdapter(db *gorm.DB) *adapterGormPostgresql {
	return &adapterGormPostgresql{db: db}
}

func (a *adapterGormPostgresql) Store(ctx context.Context, entity domainuser.RefreshToken) error {
	if err := a.db.WithContext(ctx).Create(ptr(fromDomain(entity))).Error; err != nil {
		return apperror.Wrap(err, "INTERNAL_ERROR")
	}
	return nil
}

func (a *adapterGormPostgresql) GetByHash(ctx context.Context, hash string) (*domainuser.RefreshToken, error) {
	var model RefreshToken
	if err := a.db.WithContext(ctx).Where("token_hash = ?", hash).First(&model).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, apperror.Wrap(err, "INTERNAL_ERROR")
	}
	result := toDomain(model)
	return &result, nil
}

func (a *adapterGormPostgresql) Revoke(ctx context.Context, id uuid.UUID) error {
	if err := a.db.WithContext(ctx).Model(&RefreshToken{}).Where("id = ? AND revoked_at IS NULL", id).
		Update("revoked_at", gorm.Expr("now()")).Error; err != nil {
		return apperror.Wrap(err, "INTERNAL_ERROR")
	}
	return nil
}

func (a *adapterGormPostgresql) RevokeAllForUser(ctx context.Context, userID uuid.UUID) error {
	if err := a.db.WithContext(ctx).Model(&RefreshToken{}).Where("user_id = ? AND revoked_at IS NULL", userID).
		Update("revoked_at", gorm.Expr("now()")).Error; err != nil {
		return apperror.Wrap(err, "INTERNAL_ERROR")
	}
	return nil
}

func ptr[T any](value T) *T { return &value }
