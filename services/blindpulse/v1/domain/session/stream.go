package session

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jblabs/blindpulse-be/pkg/apperror"
	domainsession "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/session"
	"github.com/shopspring/decimal"
)

const (
	defaultBaseTick  = 250 * time.Millisecond
	defaultTicketTTL = 30 * time.Second
	// minLeaseTTL keeps the driver lease long enough to survive a slow tick. A lease shorter than
	// the work it guards would be handed to a second replica while the first is still stepping.
	minLeaseTTL = 5 * time.Second
)

// IssueStreamTicket trades the caller's bearer for a single-use websocket ticket, after checking
// that the session is theirs. The check happens here, not at the socket: by the time the ticket is
// redeemed there is no bearer left to check anything against.
func (s *service) IssueStreamTicket(ctx context.Context, userID, sessionID uuid.UUID) (string, time.Duration, error) {
	if s.deps.Ticket == nil || s.deps.Bus == nil {
		return "", 0, apperror.New("STREAMING_UNAVAILABLE")
	}
	entity, err := s.Get(ctx, userID, sessionID)
	if err != nil {
		return "", 0, err
	}
	if !entity.Status.Live() {
		return "", 0, apperror.New("SESSION_CLOSED")
	}
	ttl := s.ticketTTL()
	token, err := s.deps.Ticket.Issue(ctx, StreamTicket{SessionID: sessionID, UserID: userID}, ttl)
	if err != nil {
		return "", 0, err
	}
	return token, ttl, nil
}

func (s *service) RedeemStreamTicket(ctx context.Context, token string) (*StreamTicket, error) {
	if s.deps.Ticket == nil {
		return nil, apperror.New("STREAMING_UNAVAILABLE")
	}
	ticket, err := s.deps.Ticket.Redeem(ctx, token)
	if err != nil {
		return nil, err
	}
	if ticket == nil {
		return nil, apperror.New("STREAM_TICKET_INVALID")
	}
	return ticket, nil
}

// Snapshot is the sync frame: where the server says this session is. It is the answer to a fresh
// connection and to a reconnect alike, because those are the same question (BR-02).
func (s *service) Snapshot(ctx context.Context, userID, sessionID uuid.UUID) (*domainsession.Frame, error) {
	entity, err := s.Get(ctx, userID, sessionID)
	if err != nil {
		return nil, err
	}
	frame := s.frameFor(ctx, entity, domainsession.FrameSync)
	return &frame, nil
}

// Stream subscribes to a session's frames and makes sure some replica is driving its clock.
func (s *service) Stream(ctx context.Context, userID, sessionID uuid.UUID) (<-chan domainsession.Frame, func(), error) {
	if s.deps.Bus == nil {
		return nil, nil, apperror.New("STREAMING_UNAVAILABLE")
	}
	entity, err := s.Get(ctx, userID, sessionID)
	if err != nil {
		return nil, nil, err
	}
	if !entity.Status.Live() {
		return nil, nil, apperror.New("SESSION_CLOSED")
	}
	frames, unsubscribe, err := s.deps.Bus.Subscribe(ctx, sessionID)
	if err != nil {
		return nil, nil, err
	}
	s.retainDriver(sessionID, entity.UserID)
	once := false
	return frames, func() {
		if once {
			return
		}
		once = true
		unsubscribe()
		s.releaseDriver(sessionID)
	}, nil
}

// retainDriver starts this session's clock if it is not already running on this replica, and
// counts one more socket against it.
func (s *service) retainDriver(sessionID, userID uuid.UUID) {
	s.driversMu.Lock()
	defer s.driversMu.Unlock()
	if s.drivers == nil {
		s.drivers = make(map[uuid.UUID]*driver)
	}
	if existing, ok := s.drivers[sessionID]; ok {
		existing.refs++
		return
	}
	// The driver outlives the request that started it — a socket's context is cancelled the
	// moment that HTTP handler returns, and the clock has to keep running.
	ctx, cancel := context.WithCancel(context.Background())
	s.drivers[sessionID] = &driver{cancel: cancel, refs: 1}
	go s.drive(ctx, sessionID, userID)
}

func (s *service) releaseDriver(sessionID uuid.UUID) {
	s.driversMu.Lock()
	defer s.driversMu.Unlock()
	existing, ok := s.drivers[sessionID]
	if !ok {
		return
	}
	existing.refs--
	if existing.refs > 0 {
		return
	}
	delete(s.drivers, sessionID)
	existing.cancel()
}

// drive is the session's clock. It advances the cursor and nothing else: the bar frames come out
// of Step, so a bar released by this loop and a bar released by a trader pressing the step key
// travel the same path and cannot drift apart.
func (s *service) drive(ctx context.Context, sessionID, userID uuid.UUID) {
	holder := s.holder()
	leaseTTL := s.leaseTTL()
	defer func() {
		if s.deps.Lease != nil {
			// A fresh context: ctx is already cancelled by the time this runs, and releasing the
			// lease is the difference between the next replica waiting 0s and waiting a full TTL.
			release, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			_ = s.deps.Lease.Release(release, sessionID, holder)
		}
	}()

	for {
		if ctx.Err() != nil {
			return
		}
		driving := true
		if s.deps.Lease != nil {
			ok, err := s.deps.Lease.Acquire(ctx, sessionID, holder, leaseTTL)
			// A broken lease store must not stall playback on a single-instance deployment, but
			// it must not let two replicas drive either. Failing closed for one tick and retrying
			// is the compromise: the session pauses rather than double-steps.
			driving = ok && err == nil
		}
		entity, err := s.Get(ctx, userID, sessionID)
		if err != nil || entity == nil {
			return
		}
		if !entity.Status.Live() {
			return
		}
		if !driving || entity.Status == domainsession.StatusPaused {
			if !sleep(ctx, s.baseTick()) {
				return
			}
			continue
		}
		if !sleep(ctx, s.tickFor(entity.Speed)) {
			return
		}
		// Reload after the sleep. The status read at the top of this iteration is a tick old, and
		// a pause that arrived during the sleep has to win: a bar released after the trader paused
		// is a bar they can then read, which is the hindsight pause exists to withhold.
		entity, err = s.loadLive(ctx, userID, sessionID)
		if err != nil || entity.Status != domainsession.StatusOpen {
			continue
		}
		if _, err := s.stepLoaded(ctx, entity, 1); err != nil {
			// BARS_EXHAUSTED is the feed ending, which is a normal way for a replay to finish.
			if apperror.Is(err, "BARS_EXHAUSTED") || apperror.Is(err, "INVALID_CURSOR") {
				s.publish(ctx, entity, domainsession.FrameState)
				return
			}
			slog.Debug("replay driver step failed", "session_id", sessionID, "error", err)
			if !sleep(ctx, s.baseTick()) {
				return
			}
		}
	}
}

// tickFor converts playback speed into wall-clock time per bar. Speed is replay time, not market
// time: at 1x one bar takes BaseTick regardless of whether it represents a minute or a day.
func (s *service) tickFor(speed decimal.Decimal) time.Duration {
	base := s.baseTick()
	if speed.LessThanOrEqual(decimal.Zero) {
		return base
	}
	scaled := decimal.NewFromInt(int64(base)).Div(speed).IntPart()
	if scaled < int64(time.Millisecond) {
		scaled = int64(time.Millisecond)
	}
	return time.Duration(scaled)
}

func sleep(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (s *service) baseTick() time.Duration {
	if s.deps.BaseTick > 0 {
		return s.deps.BaseTick
	}
	return defaultBaseTick
}

func (s *service) ticketTTL() time.Duration {
	if s.deps.TicketTTL > 0 {
		return s.deps.TicketTTL
	}
	return defaultTicketTTL
}

func (s *service) leaseTTL() time.Duration {
	ttl := 3 * s.baseTick()
	if ttl < minLeaseTTL {
		return minLeaseTTL
	}
	return ttl
}

func (s *service) holder() string {
	if s.deps.Holder != "" {
		return s.deps.Holder
	}
	return uuid.NewString()
}

// publish fans a frame out to every socket watching this session, on this replica and every other.
// It is best-effort by design: a session that cannot publish must still step.
func (s *service) publish(ctx context.Context, entity *domainsession.Session, kind domainsession.FrameKind) {
	if s.deps.Bus == nil || entity == nil {
		return
	}
	frame := s.frameFor(ctx, entity, kind)
	if err := s.deps.Bus.Publish(ctx, frame); err != nil {
		slog.Debug("replay frame publish failed", "session_id", entity.ID, "error", err)
	}
}

// frameFor builds the envelope, including the tail bar of the session's current timeframe view.
//
// The tail bar rather than the newly released base bar: on a higher timeframe the tail is the
// forming bucket, whose index repeats across frames until it closes. A client that replaces its
// last bar by index therefore gets the forming bar animating and the closed bar landing, from one
// rule, with no aggregation logic of its own to get wrong.
func (s *service) frameFor(ctx context.Context, entity *domainsession.Session, kind domainsession.FrameKind) domainsession.Frame {
	frame := domainsession.Frame{
		SessionID: entity.ID, Kind: kind, Status: entity.Status,
		Timeframe: string(entity.Timeframe), Speed: entity.Speed.String(),
		CursorIndex: entity.CursorIndex, RevealedIndex: entity.RevealedIndex,
		ReleasedAt: s.deps.Clock(),
	}
	if feed, err := s.deps.Feeds.Get(ctx, entity.FeedID); err == nil {
		frame.TotalBars = feed.TotalBars
	}
	if kind == domainsession.FrameState {
		return frame
	}
	bars, err := s.viewBars(ctx, entity, entity.Timeframe)
	if err != nil || len(bars) == 0 {
		return frame
	}
	tail := bars[len(bars)-1]
	frame.Bar = &tail
	return frame
}
