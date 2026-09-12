// Package orders persists the order log — every instruction the trader gave, accepted or refused.
//
// A refused order is a row here (BR-09), which is why Create is not conditional on the gate passing.
// Sprint 06's discipline index is built largely from what the trader *tried* to do: an oversized
// order refused is a fact about the trader, and a log that kept only the fills would erase it.
package orders

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jblabs/blindpulse-be/pkg/apperror"
	appdb "github.com/jblabs/blindpulse-be/services/blindpulse/v1/db"
	domainexec "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/execution"
	"gorm.io/gorm"
)

type adapterGormPostgresql struct{ db *gorm.DB }

func NewGormPostgresqlAdapter(db *gorm.DB) *adapterGormPostgresql {
	return &adapterGormPostgresql{db: db}
}

func New(db *gorm.DB) *adapterGormPostgresql { return NewGormPostgresqlAdapter(db) }

// Create inserts one order. The unique index on (session_id, client_key) is the real idempotency
// guarantee — the service checks for a duplicate first, but two concurrent submits race past that
// check and exactly one of them reaches this row.
func (a *adapterGormPostgresql) Create(ctx context.Context, entity *domainexec.Order) (*domainexec.Order, error) {
	model := fromDomain(*entity)
	if err := appdb.FromContext(ctx, a.db).WithContext(ctx).Create(&model).Error; err != nil {
		if isUniqueViolation(err) {
			return nil, apperror.New("DUPLICATE_ORDER")
		}
		return nil, apperror.Wrap(err, "INTERNAL_ERROR")
	}
	created := toDomain(model)
	return &created, nil
}

func (a *adapterGormPostgresql) GetByID(ctx context.Context, id uuid.UUID) (*domainexec.Order, error) {
	var model Order
	err := appdb.FromContext(ctx, a.db).WithContext(ctx).Where("id = ?", id).First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, apperror.New("ORDER_NOT_FOUND")
	}
	if err != nil {
		return nil, apperror.Wrap(err, "INTERNAL_ERROR")
	}
	entity := toDomain(model)
	return &entity, nil
}

// GetByClientKey is the idempotency lookup. A missing row is not an error here — it is the normal
// case for a first submit — so it returns (nil, nil) rather than ORDER_NOT_FOUND.
func (a *adapterGormPostgresql) GetByClientKey(ctx context.Context, sessionID uuid.UUID, clientKey string) (*domainexec.Order, error) {
	var model Order
	err := appdb.FromContext(ctx, a.db).WithContext(ctx).
		Where("session_id = ? AND client_key = ?", sessionID, clientKey).First(&model).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, apperror.Wrap(err, "INTERNAL_ERROR")
	}
	entity := toDomain(model)
	return &entity, nil
}

// ListBySessionID returns the whole log in the order it was placed, rejections included.
func (a *adapterGormPostgresql) ListBySessionID(ctx context.Context, sessionID uuid.UUID) ([]domainexec.Order, error) {
	var models []Order
	err := appdb.FromContext(ctx, a.db).WithContext(ctx).
		Where("session_id = ?", sessionID).
		Order("placed_bar_index ASC, created_at ASC").Find(&models).Error
	if err != nil {
		return nil, apperror.Wrap(err, "INTERNAL_ERROR")
	}
	return toDomains(models), nil
}

// ListRestingBySessionID returns every order still awaiting a fill, oldest first.
//
// The ordering is load-bearing, not cosmetic: an order's position in this list is the third
// coordinate of its deterministic slippage draw (NFR-03), so a non-deterministic ORDER BY would make
// the same session replay to different fills. created_at alone can tie at the resolution Postgres
// stores, so id breaks the tie.
func (a *adapterGormPostgresql) ListRestingBySessionID(ctx context.Context, sessionID uuid.UUID) ([]domainexec.Order, error) {
	var models []Order
	err := appdb.FromContext(ctx, a.db).WithContext(ctx).
		Where("session_id = ? AND status = ?", sessionID, string(domainexec.StatusPending)).
		Order("created_at ASC, id ASC").Find(&models).Error
	if err != nil {
		return nil, apperror.Wrap(err, "INTERNAL_ERROR")
	}
	return toDomains(models), nil
}

// Update writes the fill or the cancellation under optimistic locking. Entry-time fields are absent
// from the update set on purpose: a side or a quantity cannot change after the gate passed on them.
func (a *adapterGormPostgresql) Update(ctx context.Context, entity *domainexec.Order) error {
	result := appdb.FromContext(ctx, a.db).WithContext(ctx).Model(&Order{}).
		Where("id = ? AND version = ?", entity.ID, entity.Version).
		Updates(map[string]any{
			"status":           string(entity.Status),
			"rejection_code":   entity.RejectionCode,
			"stop_loss":        entity.StopLoss,
			"take_profit":      entity.TakeProfit,
			"filled_bar_index": entity.FilledBarIndex,
			"filled_price":     entity.FilledPrice,
			"slippage":         entity.Slippage,
			"version":          gorm.Expr("version + 1"),
			"updated_at":       gorm.Expr("now()"),
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

func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}
	return strings.Contains(err.Error(), "23505")
}
