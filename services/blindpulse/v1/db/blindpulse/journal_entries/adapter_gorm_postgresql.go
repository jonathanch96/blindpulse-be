package journal_entries

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jblabs/blindpulse-be/pkg/apperror"
	appdb "github.com/jblabs/blindpulse-be/services/blindpulse/v1/db"
	domainjournal "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/journal"
	"gorm.io/gorm"
)

type adapterGormPostgresql struct{ db *gorm.DB }

func NewGormPostgresqlAdapter(db *gorm.DB) *adapterGormPostgresql {
	return &adapterGormPostgresql{db: db}
}

func New(db *gorm.DB) *adapterGormPostgresql { return NewGormPostgresqlAdapter(db) }

func (a *adapterGormPostgresql) Create(ctx context.Context, entity *domainjournal.Entry) (*domainjournal.Entry, error) {
	model := fromDomain(*entity)
	if err := appdb.FromContext(ctx, a.db).WithContext(ctx).Create(&model).Error; err != nil {
		return nil, apperror.Wrap(err, "INTERNAL_ERROR")
	}
	created := toDomain(model)
	return &created, nil
}

func (a *adapterGormPostgresql) GetByID(ctx context.Context, id uuid.UUID) (*domainjournal.Entry, error) {
	var model JournalEntry
	err := appdb.FromContext(ctx, a.db).WithContext(ctx).Where("id = ?", id).First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, apperror.New("JOURNAL_NOT_FOUND")
	}
	if err != nil {
		return nil, apperror.Wrap(err, "INTERNAL_ERROR")
	}
	entity := toDomain(model)
	return &entity, nil
}

// ListBySessionID returns a session's entries in chart order, which is the order the post-mortem
// reads them in. Two notes on the same bar fall back to write order, so a revised thesis follows
// the note it revised.
func (a *adapterGormPostgresql) ListBySessionID(ctx context.Context, sessionID uuid.UUID) ([]domainjournal.Entry, error) {
	var models []JournalEntry
	err := appdb.FromContext(ctx, a.db).WithContext(ctx).
		Where("session_id = ?", sessionID).
		Order("bar_index ASC, created_at ASC").Find(&models).Error
	if err != nil {
		return nil, apperror.Wrap(err, "INTERNAL_ERROR")
	}
	return toDomains(models), nil
}

// Update writes the edit under optimistic locking. The version the caller read is part of the
// WHERE clause, so two tabs editing the same note cannot silently overwrite each other — the
// second one is told the row moved instead of winning by arriving later.
func (a *adapterGormPostgresql) Update(ctx context.Context, entity *domainjournal.Entry) error {
	model := fromDomain(*entity)
	result := appdb.FromContext(ctx, a.db).WithContext(ctx).Model(&JournalEntry{}).
		Where("id = ? AND version = ?", model.ID, model.Version-1).
		Updates(map[string]any{
			"bar_index": model.BarIndex, "thesis": model.Thesis, "note": model.Note,
			"emotion": model.Emotion, "conviction": model.Conviction, "tags": model.Tags,
			"media_key": model.MediaKey, "updated_at": model.UpdatedAt, "version": model.Version,
		})
	if result.Error != nil {
		return apperror.Wrap(result.Error, "INTERNAL_ERROR")
	}
	if result.RowsAffected == 0 {
		return apperror.New("CONCURRENT_MODIFICATION")
	}
	return nil
}

func (a *adapterGormPostgresql) Delete(ctx context.Context, id uuid.UUID) error {
	result := appdb.FromContext(ctx, a.db).WithContext(ctx).Where("id = ?", id).Delete(&JournalEntry{})
	if result.Error != nil {
		return apperror.Wrap(result.Error, "INTERNAL_ERROR")
	}
	if result.RowsAffected == 0 {
		return apperror.New("JOURNAL_NOT_FOUND")
	}
	return nil
}

func (a *adapterGormPostgresql) CreateRevision(ctx context.Context, revision *domainjournal.Revision) error {
	model := revisionFromDomain(*revision)
	if err := appdb.FromContext(ctx, a.db).WithContext(ctx).Create(&model).Error; err != nil {
		return apperror.Wrap(err, "INTERNAL_ERROR")
	}
	return nil
}

func (a *adapterGormPostgresql) ListRevisions(ctx context.Context, entryID uuid.UUID) ([]domainjournal.Revision, error) {
	var models []JournalEntryRevision
	err := appdb.FromContext(ctx, a.db).WithContext(ctx).
		Where("entry_id = ?", entryID).Order("version ASC").Find(&models).Error
	if err != nil {
		return nil, apperror.Wrap(err, "INTERNAL_ERROR")
	}
	return revisionsToDomain(models), nil
}
