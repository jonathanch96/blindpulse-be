package chart_drawings

import drawingdomain "github.com/jblabs/blindpulse-be/services/blindpulse/v1/domain/drawing"

var _ drawingdomain.Repository = (*adapterGormPostgresql)(nil)
