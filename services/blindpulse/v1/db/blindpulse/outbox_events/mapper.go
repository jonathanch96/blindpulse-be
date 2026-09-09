package outbox_events

import "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/event"

func fromDomain(entity event.OutboxEvent) OutboxEvent {
	return OutboxEvent{ID: entity.ID, AggregateType: entity.AggregateType, AggregateID: entity.AggregateID,
		EventType: entity.EventType, Topic: entity.Topic, PartitionKey: entity.PartitionKey,
		Payload: entity.Payload, Headers: entity.Headers, Status: entity.Status, Attempts: entity.Attempts,
		AvailableAt: entity.AvailableAt, PublishedAt: entity.PublishedAt, LastError: entity.LastError,
		CreatedAt: entity.CreatedAt}
}

func toDomain(model OutboxEvent) event.OutboxEvent {
	return event.OutboxEvent{ID: model.ID, AggregateType: model.AggregateType, AggregateID: model.AggregateID,
		EventType: model.EventType, Topic: model.Topic, PartitionKey: model.PartitionKey,
		Payload: model.Payload, Headers: model.Headers, Status: model.Status, Attempts: model.Attempts,
		AvailableAt: model.AvailableAt, PublishedAt: model.PublishedAt, LastError: model.LastError,
		CreatedAt: model.CreatedAt}
}
