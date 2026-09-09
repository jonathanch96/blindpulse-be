package outbox_events

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jblabs/blindpulse-be/pkg/apperror"
	appdb "github.com/jblabs/blindpulse-be/services/blindpulse/v1/db"
	"github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/event"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type adapterGormPostgresql struct{ db *gorm.DB }

func NewGormPostgresqlAdapter(db *gorm.DB) *adapterGormPostgresql {
	return &adapterGormPostgresql{db: db}
}

func New(db *gorm.DB) *adapterGormPostgresql { return NewGormPostgresqlAdapter(db) }

func (a *adapterGormPostgresql) Create(ctx context.Context, entity *event.OutboxEvent) error {
	if entity.ID == uuid.Nil {
		entity.ID = uuid.New()
	}
	if entity.Status == "" {
		entity.Status = event.StatusPending
	}
	model := fromDomain(*entity)
	if err := appdb.FromContext(ctx, a.db).WithContext(ctx).Create(&model).Error; err != nil {
		return apperror.Wrap(err, "INTERNAL_ERROR")
	}
	return nil
}

// ClaimDue is deliberately a read under a row lock rather than an UPDATE ... RETURNING: the relay
// must be able to fail mid-publish and have the rows simply become due again, which is what makes
// the whole pipeline at-least-once instead of at-most-once.
func (a *adapterGormPostgresql) ClaimDue(ctx context.Context, limit int) ([]event.OutboxEvent, error) {
	var models []OutboxEvent
	err := appdb.FromContext(ctx, a.db).WithContext(ctx).
		Clauses(clause.Locking{Strength: "UPDATE", Options: "SKIP LOCKED"}).
		Where("status = ? AND available_at <= now()", event.StatusPending).
		Order("created_at").
		Limit(limit).
		Find(&models).Error
	if err != nil {
		return nil, apperror.Wrap(err, "INTERNAL_ERROR")
	}
	events := make([]event.OutboxEvent, 0, len(models))
	for _, model := range models {
		events = append(events, toDomain(model))
	}
	return events, nil
}

func (a *adapterGormPostgresql) MarkPublished(ctx context.Context, ids []uuid.UUID, at time.Time) error {
	if len(ids) == 0 {
		return nil
	}
	err := appdb.FromContext(ctx, a.db).WithContext(ctx).Model(&OutboxEvent{}).
		Where("id IN ?", ids).
		Updates(map[string]any{"status": event.StatusPublished, "published_at": at, "last_error": nil}).Error
	if err != nil {
		return apperror.Wrap(err, "INTERNAL_ERROR")
	}
	return nil
}

// MarkFailed backs the row off rather than dropping it. Once attempts are exhausted the row moves
// to 'failed', where it stops being retried but stays on disk - an operator can requeue it after
// fixing whatever rejected it.
func (a *adapterGormPostgresql) MarkFailed(ctx context.Context, id uuid.UUID, reason string, retryAt time.Time, exhausted bool) error {
	status := event.StatusPending
	if exhausted {
		status = event.StatusFailed
	}
	err := appdb.FromContext(ctx, a.db).WithContext(ctx).Model(&OutboxEvent{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"status":       status,
			"attempts":     gorm.Expr("attempts + 1"),
			"available_at": retryAt,
			"last_error":   reason,
		}).Error
	if err != nil {
		return apperror.Wrap(err, "INTERNAL_ERROR")
	}
	return nil
}

func (a *adapterGormPostgresql) CountByAggregateID(ctx context.Context, aggregateID uuid.UUID) (int64, error) {
	var count int64
	err := appdb.FromContext(ctx, a.db).WithContext(ctx).Model(&OutboxEvent{}).
		Where("aggregate_id = ?", aggregateID).Count(&count).Error
	if err != nil {
		return 0, apperror.Wrap(err, "INTERNAL_ERROR")
	}
	return count, nil
}
