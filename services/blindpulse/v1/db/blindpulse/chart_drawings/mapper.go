package chart_drawings

import (
	domaindrawing "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/drawing"
)

func fromDomain(entity domaindrawing.Drawing) ChartDrawing {
	return ChartDrawing{
		ID: entity.ID, SessionID: entity.SessionID, Kind: string(entity.Kind),
		Timeframe: entity.Timeframe, Payload: entity.Payload,
		CreatedBarIndex: entity.CreatedBarIndex,
		CreatedAt:       entity.CreatedAt, UpdatedAt: entity.UpdatedAt, Version: entity.Version,
	}
}

func toDomain(model ChartDrawing) domaindrawing.Drawing {
	return domaindrawing.Drawing{
		ID: model.ID, SessionID: model.SessionID, Kind: domaindrawing.Kind(model.Kind),
		Timeframe: model.Timeframe, Payload: model.Payload,
		CreatedBarIndex: model.CreatedBarIndex,
		CreatedAt:       model.CreatedAt, UpdatedAt: model.UpdatedAt, Version: model.Version,
	}
}

func toDomains(models []ChartDrawing) []domaindrawing.Drawing {
	drawings := make([]domaindrawing.Drawing, 0, len(models))
	for _, model := range models {
		drawings = append(drawings, toDomain(model))
	}
	return drawings
}
