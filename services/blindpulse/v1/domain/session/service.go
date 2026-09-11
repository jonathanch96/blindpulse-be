package session

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"time"

	"github.com/google/uuid"
	"github.com/jblabs/blindpulse-be/pkg/apperror"
	domainfeed "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/feed"
	"github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/market"
	domainsession "github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/domain/session"
	"github.com/jblabs/blindpulse-be/services/blindpulse/v1/entities/event"
	"github.com/shopspring/decimal"
)

func NewService(deps Dependencies) Service {
	if deps.Clock == nil {
		deps.Clock = func() time.Time { return time.Now().UTC() }
	}
	if deps.Seeds == nil {
		deps.Seeds = cryptoSeed
	}
	if deps.Topic == nil {
		deps.Topic = func(name string) string { return name }
	}
	if deps.MinSpeed.IsZero() {
		deps.MinSpeed = decimal.RequireFromString("0.5")
	}
	if deps.MaxSpeed.IsZero() {
		deps.MaxSpeed = decimal.NewFromInt(10)
	}
	if deps.CheckpointEvery < 1 {
		deps.CheckpointEvery = 50
	}
	if deps.MaxBarsPerRequest < 1 {
		deps.MaxBarsPerRequest = 1500
	}
	return &service{deps: deps}
}

// Start opens a session at the end of the feed's warmup window: the trader sees the lookback and
// nothing beyond it. Everything after that they have to earn one step at a time.
func (s *service) Start(ctx context.Context, userID uuid.UUID, in StartInput) (*domainsession.Session, error) {
	if err := s.deps.Accounts.OwnedActiveAccount(ctx, userID, in.AccountID); err != nil {
		return nil, err
	}
	// One live session per account. The database enforces this too, but checking here turns a
	// constraint violation into an error that names the actual problem.
	if existing, err := s.deps.Repo.GetLiveByAccountID(ctx, in.AccountID); err == nil && existing != nil {
		return nil, apperror.New("SESSION_ALREADY_OPEN")
	} else if err != nil && !apperror.Is(err, "SESSION_NOT_FOUND") {
		return nil, err
	}

	feed, err := s.deps.Feeds.Get(ctx, in.FeedID)
	if err != nil {
		return nil, err
	}
	timeframe := in.Timeframe
	if timeframe == "" {
		timeframe = feed.BaseTimeframe
	}
	if !timeframe.Valid() {
		return nil, apperror.New("INVALID_TIMEFRAME")
	}

	// The cursor starts at the last warmup bar: index warmupBars-1 is the newest bar the trader is
	// handed for free, and index warmupBars is the first one they must step to.
	start := feed.WarmupBars - 1
	if start < 0 {
		start = 0
	}
	now := s.deps.Clock()
	entity := &domainsession.Session{
		ID: uuid.New(), UserID: userID, AccountID: in.AccountID, FeedID: in.FeedID,
		Status: domainsession.StatusOpen, Timeframe: timeframe, Speed: decimal.NewFromInt(1),
		CursorIndex: start,
		Seed:        s.deps.Seeds(),
		StartedAt:   now, LastActiveAt: now, LastCheckpointIndex: start,
		CreatedAt: now, UpdatedAt: now, Version: 1,
	}
	created, err := s.deps.Repo.Create(ctx, entity)
	if err != nil {
		return nil, err
	}
	_ = s.deps.State.Save(ctx, created.Snapshot())
	if err := s.emit(ctx, created.ID, event.TypeSessionStarted, event.TopicSessions, map[string]any{
		"session_id": created.ID, "user_id": userID, "account_id": in.AccountID,
		"feed_id": in.FeedID, "timeframe": string(timeframe), "seed": created.Seed,
		"started_at": now,
	}); err != nil {
		return nil, err
	}
	return created, nil
}

func (s *service) Get(ctx context.Context, userID, sessionID uuid.UUID) (*domainsession.Session, error) {
	return s.load(ctx, userID, sessionID)
}

func (s *service) ListOpen(ctx context.Context, userID uuid.UUID) ([]domainsession.Session, error) {
	return s.deps.Repo.ListLiveByUserID(ctx, userID)
}

// Step advances the cursor. Forward only: a negative count is refused rather than clamped, because
// a clamp would turn "let me look back" into a successful-looking no-op instead of telling the
// client the thing it needs to know.
//
// There is no going back. Once a bar is stepped past it is history, the way it is on a live chart,
// and a trader who wants a different setup randomizes a new feed rather than rewinding this one.
func (s *service) Step(ctx context.Context, userID, sessionID uuid.UUID, count int) (*domainsession.Session, error) {
	entity, err := s.loadLive(ctx, userID, sessionID)
	if err != nil {
		return nil, err
	}
	return s.stepLoaded(ctx, entity, count)
}

// stepLoaded is Step once the session is in hand. The playback clock needs to check the status and
// move the cursor against one loaded entity rather than two: it sleeps between ticks, and a pause
// arriving during that sleep must not be overtaken by the step that follows it.
func (s *service) stepLoaded(ctx context.Context, entity *domainsession.Session, count int) (*domainsession.Session, error) {
	if count == 0 {
		return entity, nil
	}
	feed, err := s.deps.Feeds.Get(ctx, entity.FeedID)
	if err != nil {
		return nil, err
	}
	if count < 0 {
		return nil, apperror.Newf("CURSOR_IS_FORWARD_ONLY",
			"the replay cursor cannot move backward; randomize a new feed to trade a different setup")
	}
	target := entity.CursorIndex + count
	if target > feed.TotalBars-1 {
		return nil, apperror.New("BARS_EXHAUSTED")
	}
	entity.CursorIndex = target
	// Every accepted step releases bars, so every accepted step carries one.
	return s.persistCursor(ctx, entity, domainsession.FrameBar)
}

func (s *service) SetSpeed(ctx context.Context, userID, sessionID uuid.UUID, raw string) (*domainsession.Session, error) {
	entity, err := s.loadLive(ctx, userID, sessionID)
	if err != nil {
		return nil, err
	}
	speed, err := decimal.NewFromString(raw)
	if err != nil || speed.LessThan(s.deps.MinSpeed) || speed.GreaterThan(s.deps.MaxSpeed) {
		return nil, apperror.Newf("INVALID_PLAYBACK_SPEED",
			"speed must be between %s and %s", s.deps.MinSpeed, s.deps.MaxSpeed)
	}
	entity.Speed = speed
	return s.persistCursor(ctx, entity, domainsession.FrameState)
}

func (s *service) Pause(ctx context.Context, userID, sessionID uuid.UUID) (*domainsession.Session, error) {
	return s.transition(ctx, userID, sessionID, domainsession.StatusPaused)
}

func (s *service) Resume(ctx context.Context, userID, sessionID uuid.UUID) (*domainsession.Session, error) {
	return s.transition(ctx, userID, sessionID, domainsession.StatusOpen)
}

// Close ends the session for good. It is the precondition for the reveal (BR-08), so it is
// deliberately one-way: a closed session cannot be reopened, only replayed as history.
func (s *service) Close(ctx context.Context, userID, sessionID uuid.UUID) (*domainsession.Session, error) {
	entity, err := s.loadLive(ctx, userID, sessionID)
	if err != nil {
		return nil, err
	}
	now := s.deps.Clock()
	entity.Status = domainsession.StatusClosed
	entity.ClosedAt = &now
	entity.UpdatedAt = now
	if err := s.deps.Repo.UpdateCursor(ctx, entity); err != nil {
		return nil, err
	}
	_ = s.deps.State.Clear(ctx, entity.ID)
	// Published before the outbox write: a socket watching a session that just closed should be
	// told so even if the event write then fails, because the alternative is a terminal that sits
	// there waiting for bars that will never come.
	s.publish(ctx, entity, domainsession.FrameState)
	if err := s.emit(ctx, entity.ID, event.TypeSessionClosed, event.TopicSessions, map[string]any{
		"session_id": entity.ID, "user_id": userID, "account_id": entity.AccountID,
		"cursor_index": entity.CursorIndex, "closed_at": now,
	}); err != nil {
		return nil, err
	}
	return entity, nil
}

// Bars serves candles bounded by the revealed edge. This is the enforcement point for BR-02: the
// only path from a client to price data runs through here, and it will not hand over a bar the
// session has not released, whatever the client asks for.
func (s *service) Bars(ctx context.Context, userID, sessionID uuid.UUID, from, to int) ([]domainfeed.Bar, error) {
	entity, err := s.load(ctx, userID, sessionID)
	if err != nil {
		return nil, err
	}
	if to < 0 {
		to = entity.CursorIndex
	}
	if from < 0 {
		from = 0
	}
	if from > to {
		return nil, apperror.Newf("INVALID_CURSOR", "from (%d) is after to (%d)", from, to)
	}
	if !entity.CanRead(to) {
		return nil, apperror.Newf("INVALID_CURSOR",
			"bar %d has not been released; the session has reached %d", to, entity.CursorIndex)
	}
	if to-from+1 > s.deps.MaxBarsPerRequest {
		return nil, apperror.Newf("VALIDATION_FAILED",
			"requested %d bars; the maximum is %d", to-from+1, s.deps.MaxBarsPerRequest)
	}
	return s.deps.Feeds.Bars(ctx, entity.FeedID, from, to)
}

func (s *service) transition(ctx context.Context, userID, sessionID uuid.UUID, status domainsession.Status) (*domainsession.Session, error) {
	entity, err := s.loadLive(ctx, userID, sessionID)
	if err != nil {
		return nil, err
	}
	if entity.Status == status {
		return entity, nil
	}
	if err := s.deps.Repo.UpdateStatus(ctx, entity.ID, status, entity.Version); err != nil {
		return nil, err
	}
	entity.Status = status
	entity.Version++
	entity.UpdatedAt = s.deps.Clock()
	_ = s.deps.State.Save(ctx, entity.Snapshot())
	s.publish(ctx, entity, domainsession.FrameState)
	return entity, nil
}

// persistCursor writes the move. Redis always gets it; PostgreSQL gets it on a checkpoint boundary
// or a status-bearing change, which bounds what a cache flush can cost to CheckpointEvery bars.
func (s *service) persistCursor(ctx context.Context, entity *domainsession.Session, kind domainsession.FrameKind) (*domainsession.Session, error) {
	now := s.deps.Clock()
	entity.LastActiveAt = now
	entity.UpdatedAt = now

	if entity.CursorIndex-entity.LastCheckpointIndex >= s.deps.CheckpointEvery {
		entity.LastCheckpointIndex = entity.CursorIndex
		if err := s.deps.Repo.UpdateCursor(ctx, entity); err != nil {
			return nil, err
		}
	} else if err := s.deps.Repo.UpdateCursor(ctx, entity); err != nil {
		return nil, err
	}
	_ = s.deps.State.Save(ctx, entity.Snapshot())
	// Every cursor move fans out, whichever path produced it: the playback clock and a trader
	// pressing the step key publish the same frame, so a second screen watching the same session
	// cannot drift from the one being driven.
	s.publish(ctx, entity, kind)
	return entity, nil
}

func (s *service) load(ctx context.Context, userID, sessionID uuid.UUID) (*domainsession.Session, error) {
	entity, err := s.deps.Repo.GetByID(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if entity.UserID != userID {
		// Not-found rather than forbidden: confirming a session id exists is itself information
		// about somebody else's trading.
		return nil, apperror.New("SESSION_NOT_FOUND")
	}
	// Redis holds the live cursor between checkpoints, so prefer it — but only ever forward. A
	// stale cache must never be able to un-show a bar the trader has already been given, which is
	// the same rule the cursor itself now follows.
	if state, ok := s.deps.State.Load(ctx, sessionID); ok && state.CursorIndex >= entity.CursorIndex {
		entity.CursorIndex = state.CursorIndex
	}
	return entity, nil
}

func (s *service) loadLive(ctx context.Context, userID, sessionID uuid.UUID) (*domainsession.Session, error) {
	entity, err := s.load(ctx, userID, sessionID)
	if err != nil {
		return nil, err
	}
	if !entity.Status.Live() {
		return nil, apperror.New("SESSION_CLOSED")
	}
	return entity, nil
}

func (s *service) emit(ctx context.Context, aggregateID uuid.UUID, eventType, topic string, payload any) error {
	if s.deps.Outbox == nil {
		return nil
	}
	record, err := event.New("session", aggregateID, eventType, s.deps.Topic(topic), payload)
	if err != nil {
		return apperror.Wrap(err, "INTERNAL_ERROR")
	}
	return s.deps.Outbox.Create(ctx, record)
}

// cryptoSeed draws a seed from the system CSPRNG. It falls back to the clock only if that fails,
// which in practice does not happen — but a session with no seed at all would be non-reproducible,
// and reproducibility is a stated guarantee (BR-10).
func cryptoSeed() int64 {
	var buffer [8]byte
	if _, err := rand.Read(buffer[:]); err != nil {
		return time.Now().UnixNano()
	}
	return int64(binary.LittleEndian.Uint64(buffer[:]) >> 1)
}

// SetTimeframe changes the viewing lens. It deliberately does not touch the cursor: a session's
// position is one number in base bars, and switching between 15m and 1h must not move it. If the
// cursor were re-expressed per timeframe, switching to a coarser view and back would round it —
// and rounding a cursor forward is a hindsight leak.
func (s *service) SetTimeframe(ctx context.Context, userID, sessionID uuid.UUID, timeframe market.Timeframe) (*domainsession.Session, error) {
	entity, err := s.loadLive(ctx, userID, sessionID)
	if err != nil {
		return nil, err
	}
	if !timeframe.Valid() {
		return nil, apperror.New("INVALID_TIMEFRAME")
	}
	feed, err := s.deps.Feeds.Get(ctx, entity.FeedID)
	if err != nil {
		return nil, err
	}
	viewInterval, _ := timeframe.Duration()
	baseInterval, _ := feed.BaseTimeframe.Duration()
	if viewInterval < baseInterval {
		return nil, apperror.Newf("INVALID_TIMEFRAME",
			"this feed's finest available timeframe is %s", feed.BaseTimeframe)
	}
	entity.Timeframe = timeframe
	// A timeframe change re-renders the whole series, so the frame carries the new tail bar: the
	// client that asked for it already refetches, but a second screen on the same session needs
	// to be told the lens moved.
	return s.persistCursor(ctx, entity, domainsession.FrameBar)
}

// ViewBars renders the session at a timeframe, bounded by the cursor. One index bounds it because
// there is only one: the trader is on the furthest bar released, and nothing can put them behind it.
func (s *service) ViewBars(ctx context.Context, userID, sessionID uuid.UUID, timeframe market.Timeframe) ([]domainfeed.Bar, error) {
	entity, err := s.load(ctx, userID, sessionID)
	if err != nil {
		return nil, err
	}
	return s.viewBars(ctx, entity, timeframe)
}

// viewBars is ViewBars once the session is already loaded — the streaming path builds a frame from
// an entity it has in hand and must not re-read the session for every bar it releases.
func (s *service) viewBars(ctx context.Context, entity *domainsession.Session, timeframe market.Timeframe) ([]domainfeed.Bar, error) {
	if timeframe == "" {
		timeframe = entity.Timeframe
	}
	if !timeframe.Valid() {
		return nil, apperror.New("INVALID_TIMEFRAME")
	}
	return s.deps.Feeds.ViewBars(ctx, entity.FeedID, timeframe, entity.CursorIndex)
}
