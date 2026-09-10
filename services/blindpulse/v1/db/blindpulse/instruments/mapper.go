package instruments

import "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/market"

func fromDomain(entity market.Instrument) Instrument {
	return Instrument{
		ID: entity.ID, Symbol: entity.Symbol, DisplayName: entity.DisplayName,
		AssetClass: string(entity.AssetClass), Venue: entity.Venue, QuoteCurrency: entity.QuoteCurrency,
		TickSize: entity.TickSize, ContractSize: entity.ContractSize,
		CreatedAt: entity.CreatedAt, UpdatedAt: entity.UpdatedAt,
	}
}

func toDomain(model Instrument) market.Instrument {
	return market.Instrument{
		ID: model.ID, Symbol: model.Symbol, DisplayName: model.DisplayName,
		AssetClass: market.AssetClass(model.AssetClass), Venue: model.Venue,
		QuoteCurrency: model.QuoteCurrency, TickSize: model.TickSize, ContractSize: model.ContractSize,
		CreatedAt: model.CreatedAt, UpdatedAt: model.UpdatedAt,
	}
}
