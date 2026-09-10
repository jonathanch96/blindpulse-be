package instruments

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jblabs/blindpulse-be/pkg/apperror"
	appdb "github.com/jblabs/blindpulse-be/services/blindpulse/v1/db"
	"github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/market"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type adapterGormPostgresql struct{ db *gorm.DB }

func NewGormPostgresqlAdapter(db *gorm.DB) *adapterGormPostgresql {
	return &adapterGormPostgresql{db: db}
}

func New(db *gorm.DB) *adapterGormPostgresql { return NewGormPostgresqlAdapter(db) }

// Upsert keys on the symbol so re-running the loader over the same instrument list updates its
// metadata instead of failing on the unique index or creating a duplicate under a new id.
func (a *adapterGormPostgresql) Upsert(ctx context.Context, entity *market.Instrument) (*market.Instrument, error) {
	if entity.ID == uuid.Nil {
		entity.ID = uuid.New()
	}
	model := fromDomain(*entity)
	err := appdb.FromContext(ctx, a.db).WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "symbol"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"display_name", "asset_class", "venue", "quote_currency", "tick_size", "contract_size", "updated_at",
			}),
		}).Create(&model).Error
	if err != nil {
		return nil, apperror.Wrap(err, "INTERNAL_ERROR")
	}
	// The conflict path leaves the in-memory model holding the id we proposed rather than the
	// stored one, so read the row back instead of returning a possibly-wrong id.
	return a.GetBySymbol(ctx, entity.Symbol)
}

func (a *adapterGormPostgresql) GetByID(ctx context.Context, id uuid.UUID) (*market.Instrument, error) {
	var model Instrument
	err := appdb.FromContext(ctx, a.db).WithContext(ctx).Where("id = ?", id).First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, apperror.New("INSTRUMENT_NOT_FOUND")
	}
	if err != nil {
		return nil, apperror.Wrap(err, "INTERNAL_ERROR")
	}
	entity := toDomain(model)
	return &entity, nil
}

func (a *adapterGormPostgresql) GetBySymbol(ctx context.Context, symbol string) (*market.Instrument, error) {
	var model Instrument
	err := appdb.FromContext(ctx, a.db).WithContext(ctx).Where("symbol = ?", symbol).First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, apperror.New("INSTRUMENT_NOT_FOUND")
	}
	if err != nil {
		return nil, apperror.Wrap(err, "INTERNAL_ERROR")
	}
	entity := toDomain(model)
	return &entity, nil
}

func (a *adapterGormPostgresql) List(ctx context.Context) ([]market.Instrument, error) {
	var models []Instrument
	if err := appdb.FromContext(ctx, a.db).WithContext(ctx).Order("symbol").Find(&models).Error; err != nil {
		return nil, apperror.Wrap(err, "INTERNAL_ERROR")
	}
	entities := make([]market.Instrument, 0, len(models))
	for _, model := range models {
		entities = append(entities, toDomain(model))
	}
	return entities, nil
}
