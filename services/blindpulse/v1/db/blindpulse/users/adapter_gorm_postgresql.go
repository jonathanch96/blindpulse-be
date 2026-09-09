package users

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jblabs/blindpulse-be/pkg/apperror"
	domainuser "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/user"
	"gorm.io/gorm"
)

type adapterGormPostgresql struct{ db *gorm.DB }

func NewGormPostgresqlAdapter(db *gorm.DB) *adapterGormPostgresql {
	return &adapterGormPostgresql{db: db}
}

func (a *adapterGormPostgresql) Create(ctx context.Context, entity *domainuser.User) (*domainuser.User, error) {
	model := fromDomain(*entity)
	if err := a.db.WithContext(ctx).Create(&model).Error; err != nil {
		return nil, translate(err)
	}
	result := toDomain(model)
	return &result, nil
}

func (a *adapterGormPostgresql) GetByID(ctx context.Context, id uuid.UUID) (*domainuser.User, error) {
	var model User
	if err := a.db.WithContext(ctx).First(&model, "id = ?", id).Error; err != nil {
		return nil, translate(err)
	}
	result := toDomain(model)
	return &result, nil
}

func (a *adapterGormPostgresql) GetByEmail(ctx context.Context, email string) (*domainuser.User, error) {
	var model User
	if err := a.db.WithContext(ctx).First(&model, "email = ?", email).Error; err != nil {
		return nil, translate(err)
	}
	result := toDomain(model)
	return &result, nil
}

func (a *adapterGormPostgresql) GetByGoogleID(ctx context.Context, googleID string) (*domainuser.User, error) {
	var model User
	if err := a.db.WithContext(ctx).First(&model, "google_id = ?", googleID).Error; err != nil {
		return nil, translate(err)
	}
	result := toDomain(model)
	return &result, nil
}

func (a *adapterGormPostgresql) ExistsByEmail(ctx context.Context, email string) (bool, error) {
	var count int64
	if err := a.db.WithContext(ctx).Model(&User{}).Where("email = ?", email).Count(&count).Error; err != nil {
		return false, translate(err)
	}
	return count > 0, nil
}

func (a *adapterGormPostgresql) Update(ctx context.Context, entity *domainuser.User) (*domainuser.User, error) {
	updates := map[string]any{"name": entity.Name, "avatar_url": entity.AvatarURL, "updated_at": gorm.Expr("now()")}
	result := a.db.WithContext(ctx).Model(&User{}).Where("id = ?", entity.ID).Updates(updates)
	if result.Error != nil {
		return nil, translate(result.Error)
	}
	if result.RowsAffected == 0 {
		return nil, apperror.New("USER_NOT_FOUND")
	}
	return a.GetByID(ctx, entity.ID)
}

// SetGoogleID links an existing (password-based) account to a Google identity the first time its
// owner signs in with Google, so future Google sign-ins resolve straight to this user by GoogleID.
func (a *adapterGormPostgresql) SetGoogleID(ctx context.Context, id uuid.UUID, googleID string) error {
	result := a.db.WithContext(ctx).Model(&User{}).Where("id = ?", id).Update("google_id", googleID)
	if result.Error != nil {
		return translate(result.Error)
	}
	if result.RowsAffected == 0 {
		return apperror.New("USER_NOT_FOUND")
	}
	return nil
}

// SetPasswordHash gives a password-less account (Google-only, or an invite placeholder) a
// password without touching its other fields.
func (a *adapterGormPostgresql) SetPasswordHash(ctx context.Context, id uuid.UUID, hash string) error {
	result := a.db.WithContext(ctx).Model(&User{}).Where("id = ?", id).Update("password_hash", hash)
	if result.Error != nil {
		return translate(result.Error)
	}
	if result.RowsAffected == 0 {
		return apperror.New("USER_NOT_FOUND")
	}
	return nil
}

// TouchLastLogin records that a user has actually signed in - called after a successful
// Authenticate/AuthenticateGoogle, never on token refresh.
func (a *adapterGormPostgresql) TouchLastLogin(ctx context.Context, id uuid.UUID, at time.Time) error {
	result := a.db.WithContext(ctx).Model(&User{}).Where("id = ?", id).Update("last_login_at", at)
	if result.Error != nil {
		return translate(result.Error)
	}
	if result.RowsAffected == 0 {
		return apperror.New("USER_NOT_FOUND")
	}
	return nil
}

func (a *adapterGormPostgresql) ListByIDs(ctx context.Context, ids []uuid.UUID) ([]domainuser.User, error) {
	if len(ids) == 0 {
		return []domainuser.User{}, nil
	}
	var models []User
	if err := a.db.WithContext(ctx).Where("id IN ?", ids).Find(&models).Error; err != nil {
		return nil, translate(err)
	}
	result := make([]domainuser.User, len(models))
	for index, model := range models {
		result[index] = toDomain(model)
	}
	return result, nil
}

func translate(err error) error {
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return apperror.New("USER_NOT_FOUND")
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return apperror.Wrap(err, "EMAIL_ALREADY_REGISTERED")
	}
	return apperror.Wrap(err, "INTERNAL_ERROR")
}
