package market_bars

import (
	"context"
	"time"

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

// insertBatch bounds how many rows go in one statement. Postgres caps a query at 65535 bind
// parameters and each bar binds 8, so batches must stay well under that or a large load fails
// only once it reaches a big file.
const insertBatch = 2000

// Insert is idempotent by design: the primary key is (instrument, timeframe, opened_at) and a
// conflict does nothing. Re-running a file that partially loaded is therefore just re-running it,
// with no cleanup step that somebody has to remember.
func (a *adapterGormPostgresql) Insert(ctx context.Context, bars []market.Bar) (int64, error) {
	if len(bars) == 0 {
		return 0, nil
	}
	models := make([]MarketBar, 0, len(bars))
	for _, bar := range bars {
		models = append(models, fromDomain(bar))
	}
	result := appdb.FromContext(ctx, a.db).WithContext(ctx).
		Clauses(clause.OnConflict{DoNothing: true}).
		CreateInBatches(models, insertBatch)
	if result.Error != nil {
		return 0, apperror.Wrap(result.Error, "INTERNAL_ERROR")
	}
	return result.RowsAffected, nil
}

func (a *adapterGormPostgresql) ListWindow(ctx context.Context, instrumentID uuid.UUID,
	timeframe market.Timeframe, from, to int64) ([]market.Bar, error) {
	var models []MarketBar
	err := appdb.FromContext(ctx, a.db).WithContext(ctx).
		Where("instrument_id = ? AND timeframe = ? AND opened_at >= ? AND opened_at <= ?",
			instrumentID, string(timeframe), time.Unix(from, 0).UTC(), time.Unix(to, 0).UTC()).
		Order("opened_at").
		Find(&models).Error
	if err != nil {
		return nil, apperror.Wrap(err, "INTERNAL_ERROR")
	}
	bars := make([]market.Bar, 0, len(models))
	for _, model := range models {
		bars = append(bars, toDomain(model))
	}
	return bars, nil
}

func (a *adapterGormPostgresql) CountWindow(ctx context.Context, instrumentID uuid.UUID,
	timeframe market.Timeframe, from, to int64) (int64, error) {
	var count int64
	err := appdb.FromContext(ctx, a.db).WithContext(ctx).Model(&MarketBar{}).
		Where("instrument_id = ? AND timeframe = ? AND opened_at >= ? AND opened_at <= ?",
			instrumentID, string(timeframe), time.Unix(from, 0).UTC(), time.Unix(to, 0).UTC()).
		Count(&count).Error
	if err != nil {
		return 0, apperror.Wrap(err, "INTERNAL_ERROR")
	}
	return count, nil
}

// Bounds lets the feed builder pick a window without pulling the series into memory first.
func (a *adapterGormPostgresql) Bounds(ctx context.Context, instrumentID uuid.UUID,
	timeframe market.Timeframe) (int64, int64, error) {
	var row struct {
		First *time.Time
		Last  *time.Time
	}
	err := appdb.FromContext(ctx, a.db).WithContext(ctx).Model(&MarketBar{}).
		Select("min(opened_at) AS first, max(opened_at) AS last").
		Where("instrument_id = ? AND timeframe = ?", instrumentID, string(timeframe)).
		Scan(&row).Error
	if err != nil {
		return 0, 0, apperror.Wrap(err, "INTERNAL_ERROR")
	}
	if row.First == nil || row.Last == nil {
		return 0, 0, apperror.New("BARS_EXHAUSTED")
	}
	return row.First.Unix(), row.Last.Unix(), nil
}
