package accounts

import (
	domainaccount "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/account"
)

func fromDomain(entity domainaccount.Account) Account {
	return Account{
		ID: entity.ID, UserID: entity.UserID, RootAccountID: entity.RootAccountID,
		ParentAccountID: entity.ParentAccountID, IterationIndex: entity.IterationIndex,
		Name: entity.Name, StrategyProfile: entity.StrategyProfile, Currency: entity.Currency,
		InitialBalance: entity.InitialBalance, CurrentBalance: entity.CurrentBalance,
		CurrentEquity: entity.CurrentEquity, PeakEquity: entity.PeakEquity,
		RiskPerTradePct: entity.Risk.RiskPerTradePct, MaxDailyDrawdownPct: entity.Risk.MaxDailyDrawdownPct,
		MinRiskReward: entity.Risk.MinRiskReward, MaxOpenPositions: entity.Risk.MaxOpenPositions,
		Leverage: entity.Risk.Leverage, Status: string(entity.Status), ResetReason: entity.ResetReason,
		ResetAt: entity.ResetAt, SealedAt: entity.SealedAt, RootHash: entity.RootHash,
		CreatedAt: entity.CreatedAt, UpdatedAt: entity.UpdatedAt, Version: entity.Version,
	}
}

func toDomain(model Account) domainaccount.Account {
	return domainaccount.Account{
		ID: model.ID, UserID: model.UserID, RootAccountID: model.RootAccountID,
		ParentAccountID: model.ParentAccountID, IterationIndex: model.IterationIndex,
		Name: model.Name, StrategyProfile: model.StrategyProfile, Currency: model.Currency,
		InitialBalance: model.InitialBalance, CurrentBalance: model.CurrentBalance,
		CurrentEquity: model.CurrentEquity, PeakEquity: model.PeakEquity,
		Risk: domainaccount.RiskPolicy{
			RiskPerTradePct: model.RiskPerTradePct, MaxDailyDrawdownPct: model.MaxDailyDrawdownPct,
			MinRiskReward: model.MinRiskReward, MaxOpenPositions: model.MaxOpenPositions, Leverage: model.Leverage,
		},
		Status: domainaccount.Status(model.Status), ResetReason: model.ResetReason,
		ResetAt: model.ResetAt, SealedAt: model.SealedAt, RootHash: model.RootHash,
		CreatedAt: model.CreatedAt, UpdatedAt: model.UpdatedAt, Version: model.Version,
	}
}

func toDomains(models []Account) []domainaccount.Account {
	entities := make([]domainaccount.Account, 0, len(models))
	for _, model := range models {
		entities = append(entities, toDomain(model))
	}
	return entities
}
