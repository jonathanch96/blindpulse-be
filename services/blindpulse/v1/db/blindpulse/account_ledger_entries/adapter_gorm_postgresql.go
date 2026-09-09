package account_ledger_entries

import (
	"context"
	"errors"

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

// Append is insert-only - there is no Update or Delete on this adapter, and the domain never asks
// for one. The (account_id, sequence) unique index turns a concurrent double-append into a
// constraint violation rather than a forked chain.
func (a *adapterGormPostgresql) Append(ctx context.Context, entity *domainaccount.LedgerEntry) error {
	model := fromDomain(*entity)
	if err := appdb.FromContext(ctx, a.db).WithContext(ctx).Create(&model).Error; err != nil {
		return apperror.Wrap(err, "INTERNAL_ERROR")
	}
	return nil
}

func (a *adapterGormPostgresql) ListByAccountID(ctx context.Context, accountID uuid.UUID) ([]domainaccount.LedgerEntry, error) {
	var models []LedgerEntry
	err := appdb.FromContext(ctx, a.db).WithContext(ctx).
		Where("account_id = ?", accountID).
		Order("sequence").
		Find(&models).Error
	if err != nil {
		return nil, apperror.Wrap(err, "INTERNAL_ERROR")
	}
	entries := make([]domainaccount.LedgerEntry, 0, len(models))
	for _, model := range models {
		entries = append(entries, toDomain(model))
	}
	return entries, nil
}

// Last returns (nil, nil) for an account with no entries yet: the first append has no predecessor
// to chain from, and that is an ordinary state, not an error.
func (a *adapterGormPostgresql) Last(ctx context.Context, accountID uuid.UUID) (*domainaccount.LedgerEntry, error) {
	var model LedgerEntry
	err := appdb.FromContext(ctx, a.db).WithContext(ctx).
		Where("account_id = ?", accountID).
		Order("sequence DESC").
		First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, apperror.Wrap(err, "INTERNAL_ERROR")
	}
	entry := toDomain(model)
	return &entry, nil
}
