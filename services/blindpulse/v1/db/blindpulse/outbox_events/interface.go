package outbox_events

import (
	"context"
	"time"

	"github.com/google/uuid"
	accountdomain "github.com/jblabs/blindpulse-be/services/blindpulse/v1/domain/account"
	"github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/event"
)

type Repository interface {
	Create(context.Context, *event.OutboxEvent) error
	// ClaimDue locks a batch of due pending rows FOR UPDATE SKIP LOCKED so several relay replicas
	// can drain the outbox concurrently without publishing the same event twice.
	ClaimDue(ctx context.Context, limit int) ([]event.OutboxEvent, error)
	MarkPublished(ctx context.Context, ids []uuid.UUID, at time.Time) error
	MarkFailed(ctx context.Context, id uuid.UUID, reason string, retryAt time.Time, exhausted bool) error
	CountByAggregateID(context.Context, uuid.UUID) (int64, error)
}

var _ Repository = (*adapterGormPostgresql)(nil)
var _ accountdomain.OutboxRepository = (*adapterGormPostgresql)(nil)
