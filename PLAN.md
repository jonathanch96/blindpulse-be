# BlindPulse Replay Lab — backend delivery plan

Companion to `../blindpulse-fe/PLAN.md`. Sprints are numbered so the two plans line up: FE sprint
*n* consumes BE sprint *n*.

---

## 1. What this service is responsible for

One rule decides where logic lives: **anything a trader could gain by lying about is computed on
the server.** The browser renders a chart and collects intent; it never decides what time it is in
the replay, what a fill price was, or whether an order passed the risk gates.

Concretely the backend owns:

| Concern | Why it cannot live in the client |
|---|---|
| The replay cursor | A client-held cursor can be rewound after seeing the next bar. Hindsight is the entire thing the product exists to prevent. |
| Blinding | Masking happens by *never sending* the symbol, dates or macro label, not by hiding them in the UI. A masked field in a JSON response is not masked. |
| Fill prices and slippage | A client-computed fill is a client-chosen fill. |
| Risk gates | The bracket dock shows the rules; the order gate applies them. A hand-rolled API call must hit the same wall the UI does. |
| The ledger and its hash chain | An integrity claim the client could produce is not an integrity claim. |
| Discipline scoring | Derived from the order stream the server recorded, not from what the client reports it did. |

---

## 2. Architecture

Same layering as `tripmate-be`, enforced by `tools/archlint` in CI:

```
adapters/rest/        composition root — config, gin engine, routes, swagger
cmd/migrate           migration runner (production entrypoint)
cmd/worker            outbox relay + event projectors
cmd/healthcheck       container probe
pkg/                  shared infrastructure, no domain knowledge
services/blindpulse/v1/
  controllers/        HTTP only: bind, authorize, delegate, render. No SQL, no rules.
  domain/             rules only. No gin, no gorm.
  db/blindpulse/      one package per table: entities, mapper, interface, adapter
  entities/           domain / request / response / event types
  events/emitters     outbox relay
  events/subscribers  projectors
migrations/           authoritative schema, versioned up + down
```

`archlint` fails the build on: a controller importing a `db/` package, a domain package importing
gin or gorm, and a GORM model escaping its own package. These are the three ways this structure
rots in practice.

### Request path

```
client → gin → middleware (request-id, logging, recovery, CORS, authenticate, rate limit)
      → controller (bind + authorize)
      → domain service (rules, decides)
      → repository adapter (SQL) ─┬→ PostgreSQL   (durable truth)
                                  └→ outbox row   (same transaction)
                                                        │
                                  Redis (hot state) ←───┘ (read path, not truth)
                                                        │
                            cmd/worker relay ────────────→ Kafka → projectors
```

---

## 3. Infrastructure: what Redis and Kafka are each for

They are not interchangeable, and using either for the other's job is the main way this design goes
wrong.

### Redis — the hot path (`pkg/cache`)

A replay session steps a cursor over a bar series many times a second. Answering "where am I, what
is open, what is my equity" from PostgreSQL on every tick would put the database in the render loop.

| Key | Holds | TTL |
|---|---|---|
| `blindpulse:session:{id}:state` | cursor index, speed, status, open position summary, equity | `REDIS_SESSION_TTL` (12h) |
| `blindpulse:session:{id}:window` | the decoded, normalized bar window around the cursor | `REDIS_BAR_WINDOW_TTL` (1h) |
| `blindpulse:feed:{id}:meta` | feed alias, timeframe, normalization params — never the symbol | 1h |
| `blindpulse:order:{sessionId}:{clientKey}` | idempotency reservation for a submit | 10m |
| `blindpulse:ratelimit:{scope}:{subject}` | fixed-window counter | window |
| `blindpulse:session:{id}:frames` (pub/sub) | replay frames fanned to every API replica holding a socket | — |

**Everything in Redis is a cache or a lease.** Losing Redis costs speed and cross-instance fan-out;
it must never cost a trade. Every read falls back to PostgreSQL. An empty `REDIS_ADDR` disables it
entirely and the service runs single-instance — supported for a laptop, not behind a load balancer,
which is why `/readyz` fails when Redis is *configured* but unreachable.

### Kafka — the durable spine (`pkg/events/kafka`)

Trades, resets and reveals are facts other systems need: the analytics projector, the reveal
builder, exports, and eventually a coaching feed.

**Nothing publishes from a request handler.** A domain write appends to `blindpulse.outbox_events`
in the *same transaction* as its state change, and `cmd/worker` drains that table. This is the
whole reason the outbox exists:

- A rolled-back transaction publishes nothing — no event for a trade that never happened.
- A broker outage delays projections and loses nothing — the rows stay `pending`.
- The relay claims rows `FOR UPDATE SKIP LOCKED`, so replicas drain concurrently without
  double-publishing, and a relay that dies mid-publish simply makes its rows due again.

Delivery is therefore **at-least-once**, and every consumer must be idempotent — they key their
upserts on the `event_id` header.

| Topic (prefixed by `KAFKA_TOPIC_PREFIX`) | Events | Partition key |
|---|---|---|
| `accounts.v1` | `account.opened`, `account.reset`, `account.sealed` | root account id |
| `sessions.v1` | `session.started`, `session.advanced`, `session.closed` | session id |
| `orders.v1` | `order.placed`, `order.rejected`, `order.filled`, `risk.gate_breached` | session id |
| `trades.v1` | `trade.opened`, `trade.closed` | session id |
| `journal.v1` | `journal.written` | session id |
| `reveals.v1` | `session.revealed` | session id |

Keying by aggregate is what keeps a session's events strictly ordered on one partition — a
projector that saw `trade.closed` before `trade.opened` would compute nonsense.

---

## 4. Data model

Nine migrations, all applied and rolled back cleanly against PostgreSQL 16. The invariants worth
naming are enforced by the *database*, not by a handler that could be bypassed:

- `orders.stop_loss` is `NOT NULL`. The PRD's central rule is that no entry exists without a hard
  stop; a nullable column makes that a check somebody can forget.
- `accounts_single_active_iteration` — a partial unique index on `(root_account_id) WHERE status =
  'active'`. Two live iterations in one tree would make the drawdown gate meaningless.
- `accounts_tree_iteration_key` — iteration numbers are unique within a tree.
- `replay_sessions_single_open` — one live session per account, for the same reason.
- `orders (session_id, client_key)` unique — turns an at-least-once network into exactly-once
  execution: a retried submit carries the same key and the second one is rejected.
- `account_ledger_entries` has no update or delete path in the adapter at all. `(account_id,
  sequence)` and `entry_hash` are both unique.
- `account_ledger_entries.payload` is `json`, **not** `jsonb`, and `recorded_at` is written
  truncated to microseconds. Both are deliberate: the chain hash covers those exact bytes, and
  jsonb reorders keys while PostgreSQL rounds nanoseconds away. Getting this wrong silently breaks
  every chain on read-back — it did, during the first end-to-end run, and the regression test in
  `domain/account/service_test.go` guards it.

### The reset tree

A reset never edits or deletes. It seals the live iteration — terminal ledger entry, `status =
'reset'`, root hash frozen — and inserts a child pointing at it, in one transaction. The first
iteration is its own `root_account_id`, so the tree has a stable identity from the start.

The ledger is hash-chained: `entry_hash = SHA256(previous_hash ‖ account ‖ sequence ‖ kind ‖ amount
‖ balance ‖ equity ‖ payload ‖ recorded_at)`, joined with a separator that cannot appear in any
field. The seal entry's hash becomes the iteration's published root hash, so one string attests the
whole history. `GET /accounts/{id}/ledger/verify` recomputes the chain and names the first sequence
that disagrees — verified end to end, including against a row tampered with directly in psql.

---

## 5. Sprint plan

**Sprint 00 — foundation — DONE.** Config with Redis/Kafka/replay bounds, gin server, middleware,
error catalog, logging, health and readiness, swagger, archlint, Docker, compose (Postgres, Redis,
Redpanda, adminer), CI.

**Sprint 01 — identity and accounts — DONE.** Argon2id hashing, rotating refresh tokens, HS256
access tokens, Google ID-token sign-in, profile and password. Accounts as reset trees with the
hash-chained ledger and verification endpoint. Outbox writes on open and reset; relay draining to
Kafka.

**Sprint 02 — market data and blinded feeds.**
- `cmd/loader`: ingest OHLCV into `market_bars` from CSV/parquet, idempotent on
  `(instrument, timeframe, opened_at)`.
- Feed builder: pick a window, generate an alias (`Asset #842`), derive `price_scale` /
  `price_offset` so the series cannot be reverse-searched from a screenshot, classify difficulty.
- `GET /feeds` returns **alias, timeframe class, difficulty, bar count** and nothing else. The
  instrument id is not in the response body at any point before the reveal — this is the one
  endpoint where a leak destroys the product.
- Redis caches decoded windows; PostgreSQL stays the source.
- Tests: a normalized series must not be recoverable to real prices from the response alone.

**Sprint 03 — the replay session engine.**
- `POST /sessions` (account + feed → seeded session), `GET /sessions/{id}`, `POST /sessions/{id}/step`,
  `/seek`, `/speed`, `/pause`, `/close`.
- `GET /sessions/{id}/bars?to={cursor}` returns bars **up to the cursor only**. Requesting past it
  is `INVALID_CURSOR`, not a clamp — a clamp hides a client bug that is indistinguishable from an
  attempt to peek.
- `GET /ws/sessions/{id}`: server streams frames at playback speed; Redis pub/sub fans out across
  replicas. Heartbeats, resume-from-cursor on reconnect, idle timeout.
- Session state in Redis, checkpointed to PostgreSQL on every state transition and periodically, so
  a Redis flush costs at most a few bars.
- Determinism: same feed + same seed ⇒ same slippage draws. A disputed fill is recomputable rather
  than arguable. Property test asserts it.

**Sprint 04 — execution and the risk gate.**
- `POST /sessions/{id}/orders` (idempotent on `client_key`), `PATCH` for bracket adjustment,
  `DELETE` to cancel, `POST /positions/{id}/close`.
- Gate order, all rejections carrying a specific code, never a silent resize: stop present and on
  the correct side → risk-per-trade → minimum R:R → open-position cap → margin → daily drawdown.
- A rejected order is **recorded**, not discarded: the discipline index needs the attempts.
- Fill engine walks bars from the cursor; stop and target resolution is explicit about the
  same-bar case (worst-case-first, documented and tested).
- Equity snapshot per bar; MAE/MFE tracked per open trade.
- Breaching the daily drawdown gate halts the session and emits `risk.gate_breached`.

**Sprint 05 — journal, drawings, and the reveal.**
- Journal CRUD anchored to the bar the trader was looking at, not wall-clock time.
- Drawings stored as opaque validated payloads by kind, so the charting toolkit can add tools
  without a migration.
- `POST /sessions/{id}/reveal`: allowed only once the session is closed (`REVEAL_LOCKED` otherwise),
  written once (`ALREADY_REVEALED`). Computes buy-and-hold over the same window, alpha, and freezes
  the comparison.
- Journal media upload with EXIF stripping and signed URLs.

**Sprint 06 — analytics projectors.**
- Kafka consumers in `cmd/worker` building `session_metrics`: win rate, profit factor, expectancy,
  R distribution, streaks, drawdown, plus the discipline components (stop respect, risk consistency,
  overtrading, plan adherence) and the weighted index.
- Behaviour tagging (`fomo_entry`, `revenge_trade`, `moved_stop`, `early_cut`) from order telemetry.
- Cross-session aggregates for the analytics screen; CSV export.
- Projectors are replayable from the topic — rebuilding metrics must never require re-trading.

**Sprint 07 — institutional access and hardening.**
- SAML 2.0 / Okta and TradingView SSO onto the existing `sso_provider` / `sso_subject` columns.
- Redis-backed distributed rate limiting replacing the in-process limiter.
- Load test to the NFR: 60 FPS tick rendering at 10x implies a sustained frame budget the stream
  must hold; sub-15ms simulated feed latency measured at the socket.
- OpenTelemetry traces across API → Redis → Kafka → projector.

---

## 6. Non-functional requirements and how each is met

| NFR | Mechanism | How it is proven |
|---|---|---|
| 60 FPS at 10x playback | Server streams pre-decoded frames from Redis at a fixed tick interval; the client never computes candle geometry from raw ticks | Load test in Sprint 07 measures dropped frames under sustained 10x |
| Sub-15ms feed latency | Bar windows served from Redis, never PostgreSQL, on the streaming path | Histogram at the socket, p99 asserted in the load test |
| Deterministic session hashes | Seeded PRNG for slippage; session `root_hash` over the ordered event stream | Property test: same feed + seed ⇒ identical fills and hash |
| Immutable history | Append-only ledger, hash chain, no update/delete path in the adapter | `verify` endpoint recomputes; regression test covers persistence fidelity |
| Zero hindsight | Symbol/date/macro never in a pre-reveal response body | Response-shape test per endpoint; the reveal is the only source |
| At-least-once events, no loss | Transactional outbox + relay with backoff and a terminal `failed` state | Relay tests; a broker outage leaves rows `pending`, verified locally |

---

## 7. Testing strategy

- **Domain tests** (fast, no database) cover the rules: reset-tree invariants, ledger chaining and
  tamper detection, gate ordering, fill resolution. This is where most tests belong.
- **Adapter integration tests** (`-tags=integration`, testcontainers) cover the things only a real
  PostgreSQL can catch — exactly the class of bug the `jsonb`/nanosecond issue was. Every table
  whose round-trip fidelity the domain depends on needs one.
- **Contract tests** assert response *shape*, especially that no pre-reveal payload carries an
  instrument identifier.
- **Property tests** for determinism and for money arithmetic.
- **`make lint-arch`** in CI on every PR.

## 8. Operational notes

- `make up` runs Postgres, Redis and adminer; `make up-events` adds Redpanda and the relay.
- Production runs three processes from one image: `migrate` (to completion), `api`, `worker`.
- Migrations are authoritative; `DB_AUTO_MIGRATE` is refused outside local/test.
- Outbox depth is the alert that matters — a growing `pending` count means the relay or the broker
  is down, and projections are stale even though the API looks healthy.
