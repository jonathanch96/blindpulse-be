// Package trades reads the execution results Sprint 04 will write.
//
// It exists now, ahead of the sprint that fills the table, because the reveal needs a realized
// return and "no fills yet" is a real answer rather than a missing feature: a trader who watched a
// session without acting returned nothing, and that is what the post-mortem should say. The same
// query sums real trades the day execution ships, with no change here.
package trades

import (
	"context"

	"github.com/google/uuid"
	"github.com/jblabs/blindpulse-be/pkg/apperror"
	appdb "github.com/jblabs/blindpulse-be/services/blindpulse/v1/db"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
)

type adapterGormPostgresql struct{ db *gorm.DB }

func NewGormPostgresqlAdapter(db *gorm.DB) *adapterGormPostgresql {
	return &adapterGormPostgresql{db: db}
}

func New(db *gorm.DB) *adapterGormPostgresql { return NewGormPostgresqlAdapter(db) }

// RealizedPnL sums the closed trades of one session.
//
// Only closed trades: an open position's paper gain is not realized, and including it would let a
// session's headline return move after the trader walked away. COALESCE makes an empty result zero
// rather than NULL, so the caller gets a number in every case.
func (a *adapterGormPostgresql) RealizedPnL(ctx context.Context, sessionID uuid.UUID) (decimal.Decimal, error) {
	var total decimal.Decimal
	err := appdb.FromContext(ctx, a.db).WithContext(ctx).
		Table("blindpulse.trades").
		Where("session_id = ? AND status = ?", sessionID, "closed").
		Select("COALESCE(SUM(realized_pnl), 0)").
		Scan(&total).Error
	if err != nil {
		return decimal.Zero, apperror.Wrap(err, "INTERNAL_ERROR")
	}
	return total, nil
}
