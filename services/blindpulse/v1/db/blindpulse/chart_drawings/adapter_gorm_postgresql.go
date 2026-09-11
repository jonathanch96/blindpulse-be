package chart_drawings

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jblabs/blindpulse-be/pkg/apperror"
	appdb "github.com/jblabs/blindpulse-be/services/blindpulse/v1/db"
	domaindrawing "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/drawing"
	"gorm.io/gorm"
)

type adapterGormPostgresql struct{ db *gorm.DB }

func NewGormPostgresqlAdapter(db *gorm.DB) *adapterGormPostgresql {
	return &adapterGormPostgresql{db: db}
}

func New(db *gorm.DB) *adapterGormPostgresql { return NewGormPostgresqlAdapter(db) }

func (a *adapterGormPostgresql) Create(ctx context.Context, entity *domaindrawing.Drawing) (*domaindrawing.Drawing, error) {
	model := fromDomain(*entity)
	if err := appdb.FromContext(ctx, a.db).WithContext(ctx).Create(&model).Error; err != nil {
		return nil, apperror.Wrap(err, "INTERNAL_ERROR")
	}
	created := toDomain(model)
	return &created, nil
}

func (a *adapterGormPostgresql) GetByID(ctx context.Context, id uuid.UUID) (*domaindrawing.Drawing, error) {
	var model ChartDrawing
	err := appdb.FromContext(ctx, a.db).WithContext(ctx).Where("id = ?", id).First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, apperror.New("DRAWING_NOT_FOUND")
	}
	if err != nil {
		return nil, apperror.Wrap(err, "INTERNAL_ERROR")
	}
	entity := toDomain(model)
	return &entity, nil
}

// ListBySessionID returns every drawing on the session, across timeframes. The client filters to
// the timeframe it is showing: a drawing is only meaningful on the timeframe it was placed on, and
// which one that is changes as the trader switches lenses, so filtering here would mean refetching
// on every switch.
func (a *adapterGormPostgresql) ListBySessionID(ctx context.Context, sessionID uuid.UUID) ([]domaindrawing.Drawing, error) {
	var models []ChartDrawing
	err := appdb.FromContext(ctx, a.db).WithContext(ctx).
		Where("session_id = ?", sessionID).
		Order("created_bar_index ASC, created_at ASC").Find(&models).Error
	if err != nil {
		return nil, apperror.Wrap(err, "INTERNAL_ERROR")
	}
	return toDomains(models), nil
}

func (a *adapterGormPostgresql) Update(ctx context.Context, entity *domaindrawing.Drawing) error {
	model := fromDomain(*entity)
	result := appdb.FromContext(ctx, a.db).WithContext(ctx).Model(&ChartDrawing{}).
		Where("id = ? AND version = ?", model.ID, model.Version-1).
		Updates(map[string]any{
			"payload": model.Payload, "timeframe": model.Timeframe,
			"updated_at": model.UpdatedAt, "version": model.Version,
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
	result := appdb.FromContext(ctx, a.db).WithContext(ctx).Where("id = ?", id).Delete(&ChartDrawing{})
	if result.Error != nil {
		return apperror.Wrap(result.Error, "INTERNAL_ERROR")
	}
	if result.RowsAffected == 0 {
		return apperror.New("DRAWING_NOT_FOUND")
	}
	return nil
}
