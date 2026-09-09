# Sprint 03 — Replay session engine (backend)

**Status:** PLANNED · **Estimate:** 12–15 dev-days
**Requirements:** FR-REPLAY-01..08, BR-02, BR-10, BR-11, NFR-01, NFR-03
**PRD:** §3.1, §6.1, §6.3
**Depends on:** Sprint 02 · **Blocks:** Sprints 04, 05, 06

## Goal

Make time move, and make the server the only thing that decides how far it has moved (BR-02). This
is the largest backend sprint and the one carrying the two hardest NFRs: sub-15ms feed latency and
a stream fast enough for 60 FPS at 10x.

## Tasks

### 03.1 Session lifecycle (FR-REPLAY-01, FR-REPLAY-07, BR-11)
```
POST   /sessions                  {account_id, feed_id}      → session (seeded)
GET    /sessions/{id}                                        → state
POST   /sessions/{id}/step        {direction, count?}        → new cursor + released bars
POST   /sessions/{id}/seek        {bar_index}                → refused past the cursor
POST   /sessions/{id}/speed       {speed}                    → 0.5–10, else INVALID_PLAYBACK_SPEED
POST   /sessions/{id}/pause
POST   /sessions/{id}/close                                  → final equity, seals the session
```
- The unique index `replay_sessions_single_open` enforces one live session per account: two cursors
  over the same equity would make the drawdown gate meaningless.
- Idle sessions past `REPLAY_SESSION_IDLE_TIMEOUT` are closed by a sweeper in `cmd/worker` and
  marked `abandoned`, so an abandoned session cannot hold an account hostage.

### 03.2 The cursor is authoritative (BR-02)
- `GET /sessions/{id}/bars?from=&to=` returns bars **at or before** `cursor_index` only.
- A request past the cursor returns `INVALID_CURSOR` — it is **not** clamped. A clamp silently
  turns an attempt to peek into a successful, plausible-looking response, which is exactly the
  failure mode this rule exists to prevent, and it hides a genuine client bug too.
- `seek` backwards is allowed (reviewing what happened); `seek` forwards is `INVALID_CURSOR`.
- Multi-timeframe (FR-REPLAY-06): a higher-timeframe request resolves against the **same** cursor;
  a partially-formed higher bar is either withheld or returned explicitly flagged as forming.
  It must never be returned complete, because a complete 1h bar reveals 59 minutes of future.

### 03.3 Streaming (FR-REPLAY-05, NFR-01)
- `GET /ws/sessions/{id}` — authenticated by the same bearer, upgraded to a websocket.
- Server advances the cursor on its own clock at the session's speed and pushes frames;
  the client never requests a bar during playback.
- Frames are pre-decoded and normalized in Redis, so the streaming path never touches PostgreSQL.
- Redis pub/sub (`blindpulse:session:{id}:frames`) fans out across API replicas, so a reconnect
  landing on a different instance still receives the stream.
- Backpressure: if a client is behind, **drop intermediate frames and send the latest** rather than
  queueing. A stale frame is worse than a skipped one — the trader acts on what is on screen.
- Heartbeat every 10s; on reconnect the client sends its last seen index and the server resumes
  from the *server's* cursor, never from the client's claim.
- `latency_ms` in the frame envelope feeds the UI readout (FR-REPLAY-08).

### 03.4 State and durability
- Hot state in Redis (`blindpulse:session:{id}:state`): cursor, speed, status, open-position
  summary, equity.
- Checkpointed to PostgreSQL on every state transition and at least every 50 bars, so a Redis
  flush costs at most a few bars and never a trade.
- On a cache miss, state is rebuilt from PostgreSQL — Redis is never the source of truth.

### 03.5 Determinism (BR-10, NFR-03)
- Every session carries a `seed`. Slippage and spread draws come from a PRNG seeded with
  `(seed, bar_index, order_sequence)`, so a replay of the same inputs produces identical outputs
  regardless of timing or machine.
- `root_hash` over the ordered event stream, updated on close: this is the PRD's `ROOT_HASH`
  proof-of-skill claim, and it is only a claim if it is reproducible.

### 03.6 Events
`session.started`, `session.advanced` (throttled — one per N bars, not one per bar, or the topic
becomes the tick stream), `session.closed`.

## Data model additions
- `replay_sessions.last_checkpoint_index INT`
- `replay_sessions.timeframe TEXT` — the timeframe currently being viewed.
- Index `(status, last_active_at)` for the idle sweeper.

## Acceptance criteria

| # | Given | When | Then |
|---|---|---|---|
| 03-AC-1 | A session at bar 142 | `GET /bars?to=200` | `INVALID_CURSOR`; no bars returned |
| 03-AC-2 | A session at bar 142 | `seek` to 100 then `GET /bars?to=142` | Seek succeeds; the bar request is now refused |
| 03-AC-3 | A session on 15m at bar 142 | Switch to 1h | Only fully-formed 1h bars at or before the cursor are returned |
| 03-AC-4 | Same feed and seed | Replayed twice with identical orders | Identical fills, equity curve and `root_hash` |
| 03-AC-5 | An account with an open session | `POST /sessions` again | `SESSION_ALREADY_OPEN` |
| 03-AC-6 | A streaming client at 10x | Sustained 5 minutes | p99 frame latency < 15ms; no frame arrives out of order |
| 03-AC-7 | A client disconnecting at bar 300 | Reconnect | Resumes from the server's cursor, not the client's claim |
| 03-AC-8 | A slow client | Falls 20 frames behind | Intermediate frames dropped; the latest is delivered |
| 03-AC-9 | Redis flushed mid-session | Next step | State rebuilt from PostgreSQL; cursor within the checkpoint interval |
| 03-AC-10 | Speed set to 12 | Request | `INVALID_PLAYBACK_SPEED` |

## Test plan
- **Property:** determinism (03-AC-4) over randomized feeds, seeds and order sequences.
- **Integration:** cursor enforcement, checkpoint/recovery with a real Redis and Postgres.
- **Load:** 50 concurrent sessions at 10x, measuring frame latency p50/p95/p99 and drop rate.
  This is where NFR-01 is proven or found wanting; it runs this sprint, not in Sprint 07.
- **Contract:** the leak test from Sprint 02 extended to session and bar responses.

## Definition of done
All criteria met, load test recorded with numbers in the PR, and the frontend can drive a full
session end to end over the websocket.

## Risks

| Risk | Mitigation |
|---|---|
| 15ms p99 unreachable through the current stack | Measured this sprint, not deferred. Fallbacks in order: binary frame encoding, wider pre-decode window, dedicated stream process |
| Clock drift between server tick and client render | Server stamps each frame with its bar index; the client renders by index, never by arrival time |
| A partially-formed higher-timeframe bar leaking future | Explicit flag plus 03-AC-3; withholding is the default |
