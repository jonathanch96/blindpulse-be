package session_reveals

import (
	domainreveal "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/reveal"
	"github.com/lib/pq"
)

func fromDomain(entity domainreveal.Reveal) SessionReveal {
	return SessionReveal{
		SessionID: entity.SessionID, InstrumentID: entity.InstrumentID, RevealedAt: entity.RevealedAt,
		Symbol: entity.Symbol, Timeframe: entity.Timeframe,
		WindowStart: entity.WindowStart, WindowEnd: entity.WindowEnd,
		MacroLabel: entity.MacroLabel, MacroNotes: entity.MacroNotes, MacroTags: pq.StringArray(entity.MacroTags),
		BenchmarkLabel: entity.BenchmarkLabel, StrategyReturnPct: entity.StrategyReturnPct,
		BenchmarkReturnPct: entity.BenchmarkReturnPct, AlphaPct: entity.AlphaPct,
		DisciplineIndex: entity.DisciplineIndex, Metrics: []byte("{}"),
	}
}

func toDomain(model SessionReveal) domainreveal.Reveal {
	return domainreveal.Reveal{
		SessionID: model.SessionID, InstrumentID: model.InstrumentID, RevealedAt: model.RevealedAt,
		Symbol: model.Symbol, Timeframe: model.Timeframe,
		WindowStart: model.WindowStart, WindowEnd: model.WindowEnd,
		MacroLabel: model.MacroLabel, MacroNotes: model.MacroNotes, MacroTags: macroTags(model.MacroTags),
		BenchmarkLabel: model.BenchmarkLabel, StrategyReturnPct: model.StrategyReturnPct,
		BenchmarkReturnPct: model.BenchmarkReturnPct, AlphaPct: model.AlphaPct,
		DisciplineIndex: model.DisciplineIndex,
	}
}

func macroTags(values pq.StringArray) []string {
	if values == nil {
		return []string{}
	}
	return []string(values)
}
