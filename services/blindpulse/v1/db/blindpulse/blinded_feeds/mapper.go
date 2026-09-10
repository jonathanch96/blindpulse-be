package blinded_feeds

import (
	domainfeed "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/feed"
	"github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/market"
)

func fromDomain(entity domainfeed.Feed) BlindedFeed {
	return BlindedFeed{
		ID: entity.ID, InstrumentID: entity.InstrumentID, AliasLabel: entity.AliasLabel,
		BaseTimeframe: string(entity.BaseTimeframe), WindowStart: entity.WindowStart, WindowEnd: entity.WindowEnd,
		WarmupBars: entity.WarmupBars, TotalBars: entity.TotalBars,
		PriceScale: entity.Normalization.Scale, PriceOffset: entity.Normalization.Offset,
		VolumeScale: entity.Normalization.VolumeScale,
		Difficulty:  string(entity.Difficulty), MacroLabel: entity.MacroLabel, IsPublished: entity.IsPublished,
		RealizedVolatility: entity.RealizedVolatility, TrendPersistence: entity.TrendPersistence,
		BuilderVersion: entity.BuilderVersion, BuiltAt: entity.BuiltAt,
		CreatedAt: entity.CreatedAt, UpdatedAt: entity.UpdatedAt, Version: entity.Version,
	}
}

func toDomain(model BlindedFeed) domainfeed.Feed {
	return domainfeed.Feed{
		ID: model.ID, InstrumentID: model.InstrumentID, AliasLabel: model.AliasLabel,
		BaseTimeframe: market.Timeframe(model.BaseTimeframe),
		WindowStart:   model.WindowStart, WindowEnd: model.WindowEnd,
		WarmupBars: model.WarmupBars, TotalBars: model.TotalBars,
		Normalization: domainfeed.Normalization{
			Offset: model.PriceOffset, Scale: model.PriceScale, VolumeScale: model.VolumeScale,
		},
		Difficulty: domainfeed.Difficulty(model.Difficulty), MacroLabel: model.MacroLabel,
		IsPublished:        model.IsPublished,
		RealizedVolatility: model.RealizedVolatility, TrendPersistence: model.TrendPersistence,
		BuilderVersion: model.BuilderVersion, BuiltAt: model.BuiltAt,
		CreatedAt: model.CreatedAt, UpdatedAt: model.UpdatedAt, Version: model.Version,
	}
}

func toDomains(models []BlindedFeed) []domainfeed.Feed {
	feeds := make([]domainfeed.Feed, 0, len(models))
	for _, model := range models {
		feeds = append(feeds, toDomain(model))
	}
	return feeds
}
