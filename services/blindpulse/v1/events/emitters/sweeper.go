package emitters

import (
	"context"
	"log/slog"
	"time"

	sessiondomain "github.com/jblabs/blindpulse-be/services/blindpulse/v1/domain/session"
)

// SessionSweeper abandons replay sessions nobody has touched.
//
// It runs in the worker rather than the API because it is periodic background work with no request
// behind it — and because running it on every API replica would mean N processes contending for the
// same rows every tick. The claim in `AbandonIdle` makes that safe rather than merely unlikely, but
// once is still better than N times.
type SessionSweeper struct {
	sessions sessiondomain.Service
	log      *slog.Logger
	cfg      SweeperConfig
}

type SweeperConfig struct {
	// Interval is how often to look. It is deliberately much coarser than IdleAfter: nothing
	// depends on abandoning a session promptly, only on abandoning it eventually.
	Interval time.Duration
	// IdleAfter is how long a session may go untouched before it is considered walked away from.
	IdleAfter time.Duration
	// BatchSize bounds one sweep, so a backlog after an outage drains across ticks.
	BatchSize int
}

func NewSessionSweeper(sessions sessiondomain.Service, log *slog.Logger, cfg SweeperConfig) *SessionSweeper {
	if cfg.Interval <= 0 {
		cfg.Interval = time.Minute
	}
	if cfg.BatchSize < 1 {
		cfg.BatchSize = 200
	}
	return &SessionSweeper{sessions: sessions, log: log, cfg: cfg}
}

func (s *SessionSweeper) Run(ctx context.Context) {
	if s.cfg.IdleAfter <= 0 {
		s.log.Warn("session sweeper disabled; an abandoned session will hold its account open indefinitely")
		return
	}
	ticker := time.NewTicker(s.cfg.Interval)
	defer ticker.Stop()
	s.log.Info("session sweeper started", "interval", s.cfg.Interval, "idle_after", s.cfg.IdleAfter)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			swept, err := s.sessions.SweepIdle(ctx, s.cfg.IdleAfter, s.cfg.BatchSize)
			if err != nil {
				// A failed sweep is retried on the next tick. It must not stop the worker: the
				// outbox relay runs in the same process and is the more important of the two.
				s.log.ErrorContext(ctx, "session sweep failed", "err", err)
				continue
			}
			if swept > 0 {
				s.log.InfoContext(ctx, "abandoned idle sessions", "count", swept)
			}
		}
	}
}
