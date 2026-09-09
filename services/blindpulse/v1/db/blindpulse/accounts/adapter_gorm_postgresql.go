package accounts

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jblabs/blindpulse-be/pkg/apperror"
	appdb "github.com/jblabs/blindpulse-be/services/blindpulse/v1/db"
	domainaccount "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/account"
	"gorm.io/gorm"
)

type adapterGormPostgresql struct{ db *gorm.DB }

func NewGormPostgresqlAdapter(db *gorm.DB) *adapterGormPostgresql {
	return &adapterGormPostgresql{db: db}
}

func New(db *gorm.DB) *adapterGormPostgresql { return NewGormPostgresqlAdapter(db) }

func (a *adapterGormPostgresql) Create(ctx context.Context, entity *domainaccount.Account) (*domainaccount.Account, error) {
	model := fromDomain(*entity)
	if err := appdb.FromContext(ctx, a.db).WithContext(ctx).Create(&model).Error; err != nil {
		return nil, apperror.Wrap(err, "INTERNAL_ERROR")
	}
	created := toDomain(model)
	return &created, nil
}

func (a *adapterGormPostgresql) GetByID(ctx context.Context, id uuid.UUID) (*domainaccount.Account, error) {
	var model Account
	err := appdb.FromContext(ctx, a.db).WithContext(ctx).Where("id = ?", id).First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, apperror.New("ACCOUNT_NOT_FOUND")
	}
	if err != nil {
		return nil, apperror.Wrap(err, "INTERNAL_ERROR")
	}
	entity := toDomain(model)
	return &entity, nil
}

// ListByUserID orders by tree then iteration so the caller can group without re-sorting, and the
// Accounts screen renders branches in the order they were forked.
func (a *adapterGormPostgresql) ListByUserID(ctx context.Context, userID uuid.UUID) ([]domainaccount.Account, error) {
	var models []Account
	err := appdb.FromContext(ctx, a.db).WithContext(ctx).
		Where("user_id = ?", userID).
		Order("root_account_id, iteration_index").
		Find(&models).Error
	if err != nil {
		return nil, apperror.Wrap(err, "INTERNAL_ERROR")
	}
	return toDomains(models), nil
}

func (a *adapterGormPostgresql) ListByRootID(ctx context.Context, rootID uuid.UUID) ([]domainaccount.Account, error) {
	var models []Account
	err := appdb.FromContext(ctx, a.db).WithContext(ctx).
		Where("root_account_id = ?", rootID).
		Order("iteration_index").
		Find(&models).Error
	if err != nil {
		return nil, apperror.Wrap(err, "INTERNAL_ERROR")
	}
	return toDomains(models), nil
}

func (a *adapterGormPostgresql) GetActiveByRootID(ctx context.Context, rootID uuid.UUID) (*domainaccount.Account, error) {
	var model Account
	err := appdb.FromContext(ctx, a.db).WithContext(ctx).
		Where("root_account_id = ? AND status = ?", rootID, string(domainaccount.StatusActive)).
		First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, apperror.New("ACCOUNT_NOT_FOUND")
	}
	if err != nil {
		return nil, apperror.Wrap(err, "INTERNAL_ERROR")
	}
	entity := toDomain(model)
	return &entity, nil
}

func (a *adapterGormPostgresql) ExistsByName(ctx context.Context, userID uuid.UUID, name string) (bool, error) {
	var count int64
	err := appdb.FromContext(ctx, a.db).WithContext(ctx).Model(&Account{}).
		Where("user_id = ? AND lower(name) = lower(?)", userID, name).
		Count(&count).Error
	if err != nil {
		return false, apperror.Wrap(err, "INTERNAL_ERROR")
	}
	return count > 0, nil
}

// Seal is guarded by the version the caller read. A concurrent reset would have bumped it, and
// losing that race must fail loudly rather than seal an iteration twice.
func (a *adapterGormPostgresql) Seal(ctx context.Context, id uuid.UUID, reason, rootHash string, version int) error {
	result := appdb.FromContext(ctx, a.db).WithContext(ctx).Model(&Account{}).
		Where("id = ? AND version = ? AND status = ?", id, version, string(domainaccount.StatusActive)).
		Updates(map[string]any{
			"status":       string(domainaccount.StatusReset),
			"reset_reason": reason,
			"reset_at":     time.Now().UTC(),
			"sealed_at":    time.Now().UTC(),
			"root_hash":    rootHash,
			"version":      gorm.Expr("version + 1"),
			"updated_at":   gorm.Expr("now()"),
		})
	if result.Error != nil {
		return apperror.Wrap(result.Error, "INTERNAL_ERROR")
	}
	if result.RowsAffected == 0 {
		return apperror.New("CONCURRENT_MODIFICATION")
	}
	return nil
}

func (a *adapterGormPostgresql) UpdateBalances(ctx context.Context, entity *domainaccount.Account) error {
	result := appdb.FromContext(ctx, a.db).WithContext(ctx).Model(&Account{}).
		Where("id = ? AND version = ?", entity.ID, entity.Version).
		Updates(map[string]any{
			"current_balance": entity.CurrentBalance,
			"current_equity":  entity.CurrentEquity,
			"peak_equity":     entity.PeakEquity,
			"version":         gorm.Expr("version + 1"),
			"updated_at":      gorm.Expr("now()"),
		})
	if result.Error != nil {
		return apperror.Wrap(result.Error, "INTERNAL_ERROR")
	}
	if result.RowsAffected == 0 {
		return apperror.New("CONCURRENT_MODIFICATION")
	}
	return nil
}
