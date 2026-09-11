package feed

import (
	"time"

	"github.com/google/uuid"
	"github.com/jblabs/blindpulse-be/pkg/stats"
	domainfeed "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/feed"
	"github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/market"
)

type Dependencies struct {
	Repo        Repository
	Bars        BarRepository
	Instruments InstrumentRepository
	// Windows caches the immutable bar window. Optional: nil means every read goes to PostgreSQL,
	// which is correct and slower.
	Windows WindowCache
	// Rand draws the normalization parameters. Injected so the builder is testable and so a feed
	// can be rebuilt deterministically from a recorded seed.
	Rand  stats.Source
	Clock func() time.Time
}

type service struct{ deps Dependencies }

type BuildInput struct {
	InstrumentID uuid.UUID
	Timeframe    market.Timeframe
	WindowStart  time.Time
	WindowEnd    time.Time
	WarmupBars   int
	MacroLabel   string
	MacroNotes   string
	MacroTags    []string
	Publish      bool
}

type ListFilter struct {
	Difficulty    domainfeed.Difficulty
	BaseTimeframe market.Timeframe
	Limit         int
}

// MinTradeableBars is the floor for a usable session. A window the trader can exhaust in a few
// steps teaches nothing, so the builder refuses it rather than publishing a feed that disappoints.
const MinTradeableBars = 200
