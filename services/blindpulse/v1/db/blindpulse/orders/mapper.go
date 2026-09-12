package orders

import (
	domainexec "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/execution"
)

func fromDomain(entity domainexec.Order) Order {
	return Order{
		ID: entity.ID, SessionID: entity.SessionID, AccountID: entity.AccountID,
		ClientKey: entity.ClientKey, Side: string(entity.Side), OrderType: string(entity.Type),
		Quantity: entity.Quantity, LimitPrice: entity.LimitPrice,
		StopLoss: entity.StopLoss, TakeProfit: entity.TakeProfit,
		RiskReward: entity.RiskReward, RiskAmount: entity.RiskAmount,
		Status: string(entity.Status), RejectionCode: entity.RejectionCode,
		PlacedBarIndex: entity.PlacedBarIndex, PlacedBarAt: entity.PlacedBarAt,
		FilledBarIndex: entity.FilledBarIndex, FilledPrice: entity.FilledPrice,
		Slippage:  entity.Slippage,
		CreatedAt: entity.CreatedAt, UpdatedAt: entity.UpdatedAt, Version: entity.Version,
	}
}

func toDomain(model Order) domainexec.Order {
	return domainexec.Order{
		ID: model.ID, SessionID: model.SessionID, AccountID: model.AccountID,
		ClientKey: model.ClientKey, Side: domainexec.Side(model.Side),
		Type:     domainexec.OrderType(model.OrderType),
		Quantity: model.Quantity, LimitPrice: model.LimitPrice,
		StopLoss: model.StopLoss, TakeProfit: model.TakeProfit,
		RiskReward: model.RiskReward, RiskAmount: model.RiskAmount,
		Status: domainexec.OrderStatus(model.Status), RejectionCode: model.RejectionCode,
		PlacedBarIndex: model.PlacedBarIndex, PlacedBarAt: model.PlacedBarAt,
		FilledBarIndex: model.FilledBarIndex, FilledPrice: model.FilledPrice,
		Slippage:  model.Slippage,
		CreatedAt: model.CreatedAt, UpdatedAt: model.UpdatedAt, Version: model.Version,
	}
}

func toDomains(models []Order) []domainexec.Order {
	entities := make([]domainexec.Order, 0, len(models))
	for _, model := range models {
		entities = append(entities, toDomain(model))
	}
	return entities
}
