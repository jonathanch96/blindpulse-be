package trades

import (
	domainexec "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/execution"
	"github.com/shopspring/decimal"
)

func fromDomain(entity domainexec.Trade) Trade {
	var reason *string
	if entity.ExitReason != nil {
		value := string(*entity.ExitReason)
		reason = &value
	}
	return Trade{
		ID: entity.ID, SessionID: entity.SessionID, AccountID: entity.AccountID,
		EntryOrderID: entity.EntryOrderID, ExitOrderID: entity.ExitOrderID,
		Side: string(entity.Side), Quantity: entity.Quantity,
		EntryPrice: entity.EntryPrice, ExitPrice: entity.ExitPrice,
		StopLoss: entity.StopLoss, InitialStopLoss: initialStopColumn(entity.InitialStopLoss), TakeProfit: entity.TakeProfit,
		Status: entity.Status, ExitReason: reason, BehaviorTag: entity.BehaviorTag,
		RealizedPnL: entity.RealizedPnL, RMultiple: entity.RMultiple,
		MaxAdverseExcursion: entity.MaxAdverseExcursion, MaxFavorableExcursion: entity.MaxFavorableExcursion,
		OpenedBarIndex: entity.OpenedBarIndex, ClosedBarIndex: entity.ClosedBarIndex,
		BarsHeld: entity.BarsHeld, OpenedAt: entity.OpenedAt, ClosedAt: entity.ClosedAt,
		CreatedAt: entity.CreatedAt, UpdatedAt: entity.UpdatedAt, Version: entity.Version,
	}
}

func toDomain(model Trade) domainexec.Trade {
	var reason *domainexec.ExitReason
	if model.ExitReason != nil {
		value := domainexec.ExitReason(*model.ExitReason)
		reason = &value
	}
	return domainexec.Trade{
		ID: model.ID, SessionID: model.SessionID, AccountID: model.AccountID,
		EntryOrderID: model.EntryOrderID, ExitOrderID: model.ExitOrderID,
		Side: domainexec.Side(model.Side), Quantity: model.Quantity,
		EntryPrice: model.EntryPrice, ExitPrice: model.ExitPrice,
		StopLoss: model.StopLoss, InitialStopLoss: initialStopValue(model.InitialStopLoss), TakeProfit: model.TakeProfit,
		Status: model.Status, ExitReason: reason, BehaviorTag: model.BehaviorTag,
		RealizedPnL: model.RealizedPnL, RMultiple: model.RMultiple,
		MaxAdverseExcursion: model.MaxAdverseExcursion, MaxFavorableExcursion: model.MaxFavorableExcursion,
		OpenedBarIndex: model.OpenedBarIndex, ClosedBarIndex: model.ClosedBarIndex,
		BarsHeld: model.BarsHeld, OpenedAt: model.OpenedAt, ClosedAt: model.ClosedAt,
		CreatedAt: model.CreatedAt, UpdatedAt: model.UpdatedAt, Version: model.Version,
	}
}

func toDomains(models []Trade) []domainexec.Trade {
	entities := make([]domainexec.Trade, 0, len(models))
	for _, model := range models {
		entities = append(entities, toDomain(model))
	}
	return entities
}

// The domain carries the initial stop as a plain decimal with zero meaning "not recorded", because
// every code path that reads it already has to handle that case. The column carries NULL for the
// same state, so these two translate between them in one place.
func initialStopColumn(value decimal.Decimal) *decimal.Decimal {
	if value.IsZero() {
		return nil
	}
	return &value
}

func initialStopValue(value *decimal.Decimal) decimal.Decimal {
	if value == nil {
		return decimal.Zero
	}
	return *value
}
