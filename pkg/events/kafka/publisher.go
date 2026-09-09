// Package kafka carries the durable event spine. Nothing in the request path publishes directly:
// domain transactions append to the outbox in the same commit as their state change, and the
// relay in cmd/worker drains that table. That ordering is what makes an event exactly as durable
// as the trade that produced it - a broker outage delays projections, it never loses them.
package kafka

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/segmentio/kafka-go"
)

type Config struct {
	Brokers      []string
	ClientID     string
	BatchSize    int
	BatchTimeout time.Duration
	WriteTimeout time.Duration
}

// Message is a broker-agnostic envelope. Key decides partitioning, and every publisher in this
// service keys by aggregate ID so all events for one session or account stay strictly ordered.
type Message struct {
	Topic   string
	Key     string
	Value   []byte
	Headers map[string]string
}

type Publisher struct {
	writer *kafka.Writer
}

// NewPublisher returns (nil, nil) when no brokers are configured, so local development runs
// without a broker and the outbox simply accumulates.
func NewPublisher(cfg Config) (*Publisher, error) {
	if len(cfg.Brokers) == 0 {
		return nil, nil
	}
	writer := &kafka.Writer{
		Addr:         kafka.TCP(cfg.Brokers...),
		Balancer:     &kafka.Hash{},
		BatchSize:    cfg.BatchSize,
		BatchTimeout: cfg.BatchTimeout,
		WriteTimeout: cfg.WriteTimeout,
		// RequireAll: an event the broker has not replicated is an event we would report as
		// published and then lose on a leader failure.
		RequiredAcks:           kafka.RequireAll,
		Async:                  false,
		Compression:            kafka.Snappy,
		AllowAutoTopicCreation: true,
	}
	return &Publisher{writer: writer}, nil
}

func (p *Publisher) Enabled() bool { return p != nil && p.writer != nil }

func (p *Publisher) Close() error {
	if !p.Enabled() {
		return nil
	}
	return p.writer.Close()
}

// Publish writes a batch and returns only once the brokers have acknowledged all of it. The relay
// marks outbox rows published on the strength of that acknowledgement, so a partial failure must
// surface as an error rather than a silent drop.
func (p *Publisher) Publish(ctx context.Context, messages ...Message) error {
	if !p.Enabled() {
		return ErrDisabled
	}
	if len(messages) == 0 {
		return nil
	}
	batch := make([]kafka.Message, 0, len(messages))
	for _, message := range messages {
		headers := make([]kafka.Header, 0, len(message.Headers))
		for key, value := range message.Headers {
			headers = append(headers, kafka.Header{Key: key, Value: []byte(value)})
		}
		batch = append(batch, kafka.Message{
			Topic:   message.Topic,
			Key:     []byte(message.Key),
			Value:   message.Value,
			Headers: headers,
			Time:    time.Now().UTC(),
		})
	}
	if err := p.writer.WriteMessages(ctx, batch...); err != nil {
		return fmt.Errorf("publish %d messages: %w", len(batch), err)
	}
	return nil
}

// ErrDisabled marks a publish attempted with no broker configured. The relay treats it as
// "try again later" rather than as a poisoned event, so nothing is retried into the dead letter.
var ErrDisabled = errors.New("kafka publisher is not configured")
