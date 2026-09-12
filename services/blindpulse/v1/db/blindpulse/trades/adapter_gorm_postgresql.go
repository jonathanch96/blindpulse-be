// Package trades persists positions: open once an entry fills, closed when something takes them out.
//
// It began as the reveal's read side, written ahead of the sprint that fills the table so that "no
// fills yet" was a real answer rather than a missing feature. RealizedPnL below is that original
// query, unchanged now that execution writes real rows — which is the point: the post-mortem never
// needed to know whether the table was populated.
package trades

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jblabs/blindpulse-be/pkg/apperror"
	appdb "github.com/jblabs/blindpulse-be/services/blindpulse/v1/db"
	domainexec "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/execution"
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

func (a *adapterGormPostgresql) Create(ctx context.Context, entity *domainexec.Trade) (*domainexec.Trade, error) {
	model := fromDomain(*entity)
	if err := appdb.FromContext(ctx, a.db).WithContext(ctx).Create(&model).Error; err != nil {
		return nil, apperror.Wrap(err, "INTERNAL_ERROR")
	}
	created := toDomain(model)
	return &created, nil
}

func (a *adapterGormPostgresql) GetByID(ctx context.Context, id uuid.UUID) (*domainexec.Trade, error) {
	var model Trade
	err := appdb.FromContext(ctx, a.db).WithContext(ctx).Where("id = ?", id).First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, apperror.New("TRADE_NOT_FOUND")
	}
	if err != nil {
		return nil, apperror.Wrap(err, "INTERNAL_ERROR")
	}
	entity := toDomain(model)
	return &entity, nil
}

func (a *adapterGormPostgresql) ListBySessionID(ctx context.Context, sessionID uuid.UUID) ([]domainexec.Trade, error) {
	var models []Trade
	err := appdb.FromContext(ctx, a.db).WithContext(ctx).
		Where("session_id = ?", sessionID).
		Order("opened_bar_index ASC, created_at ASC").Find(&models).Error
	if err != nil {
		return nil, apperror.Wrap(err, "INTERNAL_ERROR")
	}
	return toDomains(models), nil
}

// ListOpenBySessionID is read once per bar during an advance and again by every gate evaluation, so
// it is the hottest query in the write path. trades_account_idx covers status; the session filter is
// what narrows it.
func (a *adapterGormPostgresql) ListOpenBySessionID(ctx context.Context, sessionID uuid.UUID) ([]domainexec.Trade, error) {
	var models []Trade
	err := appdb.FromContext(ctx, a.db).WithContext(ctx).
		Where("session_id = ? AND status = ?", sessionID, domainexec.TradeOpen).
		Order("opened_bar_index ASC, created_at ASC").Find(&models).Error
	if err != nil {
		return nil, apperror.Wrap(err, "INTERNAL_ERROR")
	}
	return toDomains(models), nil
}

// Update writes a close or a level move under optimistic locking.
//
// Entry price, side and entry order are not in the update set: a filled entry is history. Quantity
// is, because a partial close splits a position and the remainder keeps the row.
func (a *adapterGormPostgresql) Update(ctx context.Context, entity *domainexec.Trade) error {
	var reason *string
	if entity.ExitReason != nil {
		value := string(*entity.ExitReason)
		reason = &value
	}
	result := appdb.FromContext(ctx, a.db).WithContext(ctx).Model(&Trade{}).
		Where("id = ? AND version = ?", entity.ID, entity.Version).
		Updates(map[string]any{
			"quantity":                entity.Quantity,
			"exit_order_id":           entity.ExitOrderID,
			"exit_price":              entity.ExitPrice,
			"stop_loss":               entity.StopLoss,
			"take_profit":             entity.TakeProfit,
			"status":                  entity.Status,
			"exit_reason":             reason,
			"behavior_tag":            entity.BehaviorTag,
			"realized_pnl":            entity.RealizedPnL,
			"r_multiple":              entity.RMultiple,
			"max_adverse_excursion":   entity.MaxAdverseExcursion,
			"max_favorable_excursion": entity.MaxFavorableExcursion,
			"closed_bar_index":        entity.ClosedBarIndex,
			"bars_held":               entity.BarsHeld,
			"closed_at":               entity.ClosedAt,
			"version":                 gorm.Expr("version + 1"),
			"updated_at":              gorm.Expr("now()"),
		})
	if result.Error != nil {
		return apperror.Wrap(result.Error, "INTERNAL_ERROR")
	}
	if result.RowsAffected == 0 {
		return apperror.New("CONCURRENT_MODIFICATION")
	}
	entity.Version++
	return nil
}
