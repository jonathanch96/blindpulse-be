# Sprint 06 — Analytics, discipline index and cross-iteration (backend)

**Status:** PLANNED · **Estimate:** 10–12 dev-days
**Requirements:** FR-ANALYTICS-01..09, FR-REVEAL-04/05, FR-ACCT-06/07
**PRD:** §3.4, §3.5, §4
**Depends on:** Sprints 04, 05

## Goal

Turn the event stream into the numbers that tell a trader whether they are actually improving.
Everything here is a **projection** — built by consumers from Kafka, never computed inline in a
request — so metrics can be rebuilt from history without re-trading a single session.

## Tasks

### 06.1 Projector infrastructure
- Consumer group in `cmd/worker` over `orders.v1`, `trades.v1`, `sessions.v1`, `reveals.v1`.
- Idempotent by `event_id`: delivery is at-least-once, so every handler upserts on an id it has
  already recorded rather than incrementing blindly.
- A `projector_offsets` table records progress, so a rebuild is: truncate the projection, reset the
  group, replay. This must be a routine operation, not an incident.

### 06.2 Session metrics (FR-ANALYTICS-01..03)
Into `session_metrics`: trades total/won/lost, win rate, profit factor, expectancy in R and cash,
average R, planned vs realized R:R, max drawdown, max consecutive wins and losses, recovery factor,
Sharpe and Sortino.

Documented conventions, because these numbers are quoted and must mean one thing:
- Profit factor = gross profit ÷ gross loss; undefined (not infinite) with zero losses.
- Expectancy = mean R across closed trades.
- Sharpe over per-trade returns, annualized by the session's simulated span, stated as such.
- A scratch trade (0R within a tolerance band) counts in neither wins nor losses but is reported.

### 06.3 Behavioural discipline index (FR-REVEAL-04, FR-REVEAL-05)

Four components, each 0–100, weighted into the index. Every one is derived from telemetry the
server recorded, never from anything the client asserts:

| Component | Derived from | Weight |
|---|---|---|
| Stop respect | Stops moved against the position; stops widened after entry; trades closed beyond the original stop | 30% |
| Risk consistency | Variance of realized risk-per-trade against the account's policy | 25% |
| Overtrading | Trade frequency vs the session's bar count; cooldown observed after a loss | 20% |
| Plan adherence | Entries matching the declared `strategy_profile`; rejected-order attempts against the gate | 25% |

Behaviour tags per trade (FR-REVEAL-05), assigned by the projector and correctable by the trader
afterwards (with the original retained):
- `fomo_entry` — entry within N bars of a large-range candle, against the declared profile.
- `revenge_trade` — entry within a short cooldown after a loss, with size above the trader's median.
- `early_cut` — exit before target with a positive R below their own median winner.
- `moved_stop` — stop moved away from entry while the position was open.
- `oversized` — realized risk above the policy, i.e. an order that only passed because the policy
  was loosened between orders.

The index is a weighted mean with its components always returned alongside it. A single number that
cannot be traced to a behaviour is a score, not feedback.

### 06.4 Cross-session analytics (FR-ANALYTICS-04..09)
```
GET /analytics/summary?scope=last_30|last_90|all
GET /analytics/r-distribution
GET /analytics/streaks
GET /analytics/session-alpha          → by liquidity window (NY / London / Asia)
GET /analytics/asset-class-split      → revealed sessions only
GET /analytics/export.csv
```
- Session alpha by liquidity window and asset-class split are computable **only for revealed
  sessions**. Including unrevealed ones would let a trader infer the asset class of a session they
  are still trading by watching an aggregate move — this is a real leak path, and the query filters
  on `session_reveals` existing.

### 06.5 Cross-iteration equity overlay (FR-ACCT-06, FR-ACCT-07)
- `GET /accounts/{id}/tree/equity` — every iteration's equity curve, normalized to percent of its
  own starting balance so branches with different starting capital are comparable.
- `POST /accounts/{id}/fork` — clone an earlier iteration's risk policy and strategy profile into a
  new iteration, without copying its trades.

## Acceptance criteria

| # | Given | When | Then |
|---|---|---|---|
| 06-AC-1 | A session with 6 closed trades | Projected | Win rate, profit factor and expectancy match a hand computation |
| 06-AC-2 | The same event delivered twice | Projected | Metrics are unchanged — idempotent |
| 06-AC-3 | A projection truncated and the group reset | Replayed | Identical metrics to before the rebuild |
| 06-AC-4 | A trade whose stop was moved away from entry | Projected | Tagged `moved_stop`; stop-respect score falls |
| 06-AC-5 | A session with zero losing trades | Projected | Profit factor reported as undefined, not infinity or null-as-zero |
| 06-AC-6 | An unrevealed session | Asset-class split queried | Not included in any aggregate |
| 06-AC-7 | Iterations starting at $10k and $50k | Equity overlay | Both normalized to percent and comparable |
| 06-AC-8 | A discipline index of 82 | Inspected | Its four components are returned and their weighted mean is 82 |

## Test plan
- **Domain:** each metric against hand-computed fixtures, including the degenerate cases (zero
  trades, all wins, all losses, a single scratch).
- **Integration:** projector replay determinism (06-AC-3) against a real broker.
- **Contract:** aggregates exclude unrevealed sessions (06-AC-6).

## Definition of done
All criteria met, a projection rebuild documented as a runbook step, and the analytics screen
rendering real numbers from real sessions.

## Risks

| Risk | Mitigation |
|---|---|
| Discipline scoring feeling arbitrary or unfair | Components always shown; weights documented here and in the UI; tags correctable by the trader |
| Projector lag making the UI look broken | Return `computed_at`; the UI shows staleness rather than pretending a number is live |
| A metric that silently changes meaning | Conventions written down in 06.2 and asserted in fixtures |
