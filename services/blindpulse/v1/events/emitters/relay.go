// Package emitters contains the outbox relay: the only component in the system that publishes to
// Kafka. Domain code records facts in the outbox inside its own transaction, and this loop moves
// them to the broker. That split is what makes an event exactly as durable as the trade that
// caused it - and why no handler ever calls a producer directly.
package emitters

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	appkafka "github.com/jblabs/blindpulse-be/pkg/events/kafka"
	outboxdb "github.com/jblabs/blindpulse-be/services/blindpulse/v1/db/blindpulse/outbox_events"
	"github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/event"
)

type RelayConfig struct {
	Interval    time.Duration
	BatchSize   int
	MaxAttempts int
}

type Relay struct {
	outbox    outboxdb.Repository
	publisher *appkafka.Publisher
	log       *slog.Logger
	cfg       RelayConfig
}

func NewRelay(outbox outboxdb.Repository, publisher *appkafka.Publisher, log *slog.Logger, cfg RelayConfig) *Relay {
	if cfg.Interval <= 0 {
		cfg.Interval = time.Second
	}
	if cfg.BatchSize < 1 {
		cfg.BatchSize = 100
	}
	if cfg.MaxAttempts < 1 {
		cfg.MaxAttempts = 8
	}
	return &Relay{outbox: outbox, publisher: publisher, log: log, cfg: cfg}
}

// Run drains the outbox until the context is cancelled. It never returns an error for a failed
// publish: a broker outage is a delay, and the rows stay pending until it ends.
func (r *Relay) Run(ctx context.Context) error {
	ticker := time.NewTicker(r.cfg.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			drained, err := r.drain(ctx)
			if err != nil {
				r.log.ErrorContext(ctx, "outbox drain failed", "err", err)
				continue
			}
			// A full batch means more is waiting; keep going rather than sleeping a whole tick
			// with a backlog on disk.
			for drained == r.cfg.BatchSize {
				if drained, err = r.drain(ctx); err != nil {
					r.log.ErrorContext(ctx, "outbox drain failed", "err", err)
					break
				}
			}
		}
	}
}

func (r *Relay) drain(ctx context.Context) (int, error) {
	if !r.publisher.Enabled() {
		return 0, nil
	}
	events, err := r.outbox.ClaimDue(ctx, r.cfg.BatchSize)
	if err != nil || len(events) == 0 {
		return 0, err
	}
	messages := make([]appkafka.Message, 0, len(events))
	for _, entity := range events {
		messages = append(messages, appkafka.Message{
			Topic: entity.Topic,
			Key:   entity.PartitionKey,
			Value: entity.Payload,
			Headers: map[string]string{
				"event_id":       entity.ID.String(),
				"event_type":     entity.EventType,
				"aggregate_type": entity.AggregateType,
				"aggregate_id":   entity.AggregateID.String(),
				"occurred_at":    entity.CreatedAt.UTC().Format(time.RFC3339Nano),
			},
		})
	}
	if err := r.publisher.Publish(ctx, messages...); err != nil {
		// The whole batch is retried together. Marking rows individually here would mean
		// guessing which of them the broker accepted, and a wrong guess drops an event.
		r.retryBatch(ctx, events, err)
		return 0, nil
	}
	ids := make([]uuid.UUID, 0, len(events))
	for _, entity := range events {
		ids = append(ids, entity.ID)
	}
	if err := r.outbox.MarkPublished(ctx, ids, time.Now().UTC()); err != nil {
		// Published but not marked: the next drain re-publishes these. Consumers are idempotent
		// precisely so this window is survivable.
		r.log.ErrorContext(ctx, "published events could not be marked", "count", len(ids), "err", err)
		return 0, nil
	}
	return len(events), nil
}

func (r *Relay) retryBatch(ctx context.Context, events []event.OutboxEvent, cause error) {
	r.log.WarnContext(ctx, "outbox publish failed", "count", len(events), "err", cause)
	for _, entity := range events {
		attempts := entity.Attempts + 1
		exhausted := attempts >= r.cfg.MaxAttempts
		retryAt := time.Now().UTC().Add(backoff(attempts))
		if err := r.outbox.MarkFailed(ctx, entity.ID, cause.Error(), retryAt, exhausted); err != nil {
			r.log.ErrorContext(ctx, "outbox retry bookkeeping failed", "event_id", entity.ID, "err", err)
		}
	}
}

// backoff grows exponentially and caps at a minute, so a long broker outage costs one retry per
// minute per event rather than a hot loop against a dead socket.
func backoff(attempt int) time.Duration {
	delay := time.Duration(1<<uint(min(attempt, 10))) * time.Second
	if delay > time.Minute {
		return time.Minute
	}
	return delay
}
