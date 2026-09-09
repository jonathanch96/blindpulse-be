package kafka

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/segmentio/kafka-go"
)

// Handler processes one message. Returning an error leaves the offset uncommitted, so the message
// is redelivered: every handler must therefore be idempotent, which for the projectors here means
// keying their upserts on the event ID they already recorded.
type Handler func(ctx context.Context, message Message) error

type ConsumerConfig struct {
	Brokers []string
	Group   string
	Topic   string
	// MaxAttempts bounds redelivery of a single message before it is logged and skipped, so one
	// poisoned payload cannot wedge a partition forever.
	MaxAttempts int
}

type Consumer struct {
	reader      *kafka.Reader
	log         *slog.Logger
	maxAttempts int
}

func NewConsumer(cfg ConsumerConfig, log *slog.Logger) *Consumer {
	if len(cfg.Brokers) == 0 {
		return nil
	}
	attempts := cfg.MaxAttempts
	if attempts < 1 {
		attempts = 5
	}
	return &Consumer{
		reader: kafka.NewReader(kafka.ReaderConfig{
			Brokers:        cfg.Brokers,
			GroupID:        cfg.Group,
			Topic:          cfg.Topic,
			MinBytes:       1,
			MaxBytes:       10 << 20,
			CommitInterval: 0, // commit explicitly, only after the handler succeeds
			StartOffset:    kafka.FirstOffset,
		}),
		log:         log,
		maxAttempts: attempts,
	}
}

func (c *Consumer) Close() error {
	if c == nil || c.reader == nil {
		return nil
	}
	return c.reader.Close()
}

// Run blocks until ctx is cancelled, dispatching each message to the handler and committing the
// offset only after it succeeds.
func (c *Consumer) Run(ctx context.Context, handle Handler) error {
	if c == nil || c.reader == nil {
		return ErrDisabled
	}
	for {
		message, err := c.reader.FetchMessage(ctx)
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil
			}
			return fmt.Errorf("fetch message: %w", err)
		}
		if err := c.dispatch(ctx, message, handle); err != nil {
			return err
		}
	}
}

func (c *Consumer) dispatch(ctx context.Context, message kafka.Message, handle Handler) error {
	headers := make(map[string]string, len(message.Headers))
	for _, header := range message.Headers {
		headers[header.Key] = string(header.Value)
	}
	decoded := Message{Topic: message.Topic, Key: string(message.Key), Value: message.Value, Headers: headers}
	var lastErr error
	for attempt := 1; attempt <= c.maxAttempts; attempt++ {
		lastErr = handle(ctx, decoded)
		if lastErr == nil {
			break
		}
		if errors.Is(lastErr, context.Canceled) {
			return nil
		}
		c.log.WarnContext(ctx, "event handler failed", "topic", message.Topic, "attempt", attempt, "err", lastErr)
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(backoff(attempt)):
		}
	}
	if lastErr != nil {
		// Skipping keeps the partition moving. The message is not lost: the outbox row it came
		// from stays queryable, and the log line names the offset for a manual replay.
		c.log.ErrorContext(ctx, "event abandoned after retries", "topic", message.Topic,
			"partition", message.Partition, "offset", message.Offset, "err", lastErr)
	}
	if err := c.reader.CommitMessages(ctx, message); err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("commit offset: %w", err)
	}
	return nil
}

func backoff(attempt int) time.Duration {
	delay := time.Duration(1<<uint(attempt-1)) * 100 * time.Millisecond
	if delay > 5*time.Second {
		return 5 * time.Second
	}
	return delay
}
