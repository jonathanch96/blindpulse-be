// Package event defines the shape of everything this service publishes. Payloads live here rather
// than in the domain packages that raise them, so a projector can depend on the contract without
// depending on the aggregate that produced it.
package event

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// Topic names, unprefixed. config.KafkaConfig.Topic adds the environment prefix, so a topic is
// never written as a bare string at a call site.
const (
	TopicAccounts = "accounts.v1"
	TopicSessions = "sessions.v1"
	TopicOrders   = "orders.v1"
	TopicTrades   = "trades.v1"
	TopicJournal  = "journal.v1"
	TopicReveals  = "reveals.v1"
)

// Event types. The suffix is a past-tense fact: an event records what happened, never what should
// happen next - a consumer that needs a command should not be reading this stream.
const (
	TypeAccountOpened   = "account.opened"
	TypeAccountReset    = "account.reset"
	TypeAccountSealed   = "account.sealed"
	TypeSessionStarted  = "session.started"
	TypeSessionAdvanced = "session.advanced"
	TypeSessionClosed   = "session.closed"
	// TypeSessionAbandoned is emitted by the idle sweeper, not by the trader. A session that ends
	// this way has no reveal and no post-mortem — it was walked away from.
	TypeSessionAbandoned = "session.abandoned"
	TypeOrderPlaced      = "order.placed"
	TypeOrderRejected    = "order.rejected"
	TypeOrderFilled      = "order.filled"
	TypeTradeOpened      = "trade.opened"
	TypeTradeClosed      = "trade.closed"
	TypeGateBreached     = "risk.gate_breached"
	TypeJournalWritten   = "journal.written"
	TypeSessionRevealed  = "session.revealed"
)

const (
	StatusPending   = "pending"
	StatusPublished = "published"
	StatusFailed    = "failed"
)

// OutboxEvent is a fact recorded in the same transaction as the state change that produced it.
// The relay reads it, publishes it, and marks it - so an event is exactly as durable as its trade.
type OutboxEvent struct {
	ID            uuid.UUID
	AggregateType string
	AggregateID   uuid.UUID
	EventType     string
	Topic         string
	PartitionKey  string
	Payload       json.RawMessage
	Headers       json.RawMessage
	Status        string
	Attempts      int
	AvailableAt   time.Time
	PublishedAt   *time.Time
	LastError     *string
	CreatedAt     time.Time
}

// New builds a pending event. PartitionKey defaults to the aggregate ID, which is what keeps all
// events for one session or account on a single partition and therefore strictly ordered.
func New(aggregateType string, aggregateID uuid.UUID, eventType, topic string, payload any) (*OutboxEvent, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	return &OutboxEvent{
		ID:            uuid.New(),
		AggregateType: aggregateType,
		AggregateID:   aggregateID,
		EventType:     eventType,
		Topic:         topic,
		PartitionKey:  aggregateID.String(),
		Payload:       encoded,
		Headers:       json.RawMessage(`{}`),
		Status:        StatusPending,
		AvailableAt:   now,
		CreatedAt:     now,
	}, nil
}
