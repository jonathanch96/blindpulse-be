package market_bars

import "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/market"

func fromDomain(entity market.Bar) MarketBar {
	return MarketBar{
		InstrumentID: entity.InstrumentID, Timeframe: string(entity.Timeframe),
		OpenedAt: entity.OpenedAt.UTC(), Open: entity.Open, High: entity.High,
		Low: entity.Low, Close: entity.Close, Volume: entity.Volume,
	}
}

func toDomain(model MarketBar) market.Bar {
	return market.Bar{
		InstrumentID: model.InstrumentID, Timeframe: market.Timeframe(model.Timeframe),
		OpenedAt: model.OpenedAt.UTC(), Open: model.Open, High: model.High,
		Low: model.Low, Close: model.Close, Volume: model.Volume,
	}
}
