// Package equity_snapshots stores one mark-to-market row per bar per session.
//
// Two things read it, and they want opposite things from it. Sprint 06's analytics overlays several
// iterations' equity curves and needs them as stored series rather than recomputed sums. The
// drawdown gate needs one number — the highest equity since the market day began — and gets it from
// PeakEquitySince, which is why the day boundary needs no state of its own anywhere.
//
// bar_at is a real instant and stays here. Nothing maps it onto a response: a client that could read
// these timestamps could date the daily window and, from the weekend gaps, name the asset class
// (SP4-1).
package equity_snapshots

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jblabs/blindpulse-be/pkg/apperror"
	appdb "github.com/jblabs/blindpulse-be/services/blindpulse/v1/db"
	domainexec "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/execution"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type adapterGormPostgresql struct{ db *gorm.DB }

func NewGormPostgresqlAdapter(db *gorm.DB) *adapterGormPostgresql {
	return &adapterGormPostgresql{db: db}
}

func New(db *gorm.DB) *adapterGormPostgresql { return NewGormPostgresqlAdapter(db) }

// Upsert writes the bar's snapshot, replacing one already there.
//
// Replacing rather than failing, because an advance that dies halfway and is retried re-settles bars
// it already settled, and the second computation of a bar is the same as the first — fills are
// deterministic (NFR-03). A conflict here means a retry, not a contradiction.
func (a *adapterGormPostgresql) Upsert(ctx context.Context, entity domainexec.EquitySnapshot) error {
	model := fromDomain(entity)
	err := appdb.FromContext(ctx, a.db).WithContext(ctx).
		Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "session_id"}, {Name: "bar_index"}},
			DoUpdates: clause.AssignmentColumns([]string{
				"bar_at", "balance", "equity", "drawdown_pct", "open_positions",
			}),
		}).Create(&model).Error
	if err != nil {
		return apperror.Wrap(err, "INTERNAL_ERROR")
	}
	return nil
}

// DayEquity reads the daily drawdown gate's two reference points in one round trip.
//
// One statement rather than two queries: the gate asks this on every bar of every advance, and it is
// the same question — where did this market day start, and how high has it been since. The subqueries
// scan the same primary-key range.
//
// COALESCE makes an empty side zero rather than NULL, and zero is meaningful to the caller: an opening
// of zero means the session has no marks before this day at all, and the day is seeded from the
// account's balance instead.
func (a *adapterGormPostgresql) DayEquity(ctx context.Context, sessionID uuid.UUID, dayStart time.Time) (decimal.Decimal, decimal.Decimal, error) {
	var row struct {
		Opening decimal.Decimal
		Peak    decimal.Decimal
	}
	err := appdb.FromContext(ctx, a.db).WithContext(ctx).
		Raw(`SELECT
		       COALESCE((SELECT equity FROM blindpulse.equity_snapshots
		                  WHERE session_id = ? AND bar_at < ?
		                  ORDER BY bar_at DESC, bar_index DESC LIMIT 1), 0) AS opening,
		       COALESCE((SELECT MAX(equity) FROM blindpulse.equity_snapshots
		                  WHERE session_id = ? AND bar_at >= ?), 0) AS peak`,
			sessionID, dayStart, sessionID, dayStart).
		Scan(&row).Error
	if err != nil {
		return decimal.Zero, decimal.Zero, apperror.Wrap(err, "INTERNAL_ERROR")
	}
	return row.Opening, row.Peak, nil
}

// ListBySessionID is the stored equity curve, oldest bar first.
func (a *adapterGormPostgresql) ListBySessionID(ctx context.Context, sessionID uuid.UUID) ([]domainexec.EquitySnapshot, error) {
	var models []EquitySnapshot
	err := appdb.FromContext(ctx, a.db).WithContext(ctx).
		Where("session_id = ?", sessionID).Order("bar_index ASC").Find(&models).Error
	if err != nil {
		return nil, apperror.Wrap(err, "INTERNAL_ERROR")
	}
	entities := make([]domainexec.EquitySnapshot, 0, len(models))
	for _, model := range models {
		entities = append(entities, toDomain(model))
	}
	return entities, nil
}
