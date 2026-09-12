# Sprint 04 — Execution and the risk gate (backend)

**Status:** PLANNED — **decided, not built** (see *What is missing*, below) · **Estimate:** 10–12 dev-days · **Plan reviewed:** [docs/reviews/sprint-04-execution-risk.md](../reviews/sprint-04-execution-risk.md) — all five decisions settled; the plan is ready to build
**Requirements:** FR-EXEC-01..05, FR-EXEC-07..09, FR-EXEC-11, BR-03, BR-04, BR-05, BR-09
**PRD:** §3.3
**Depends on:** Sprint 03 · **Blocks:** Sprints 05, 06

## Goal

Let the trader act, and make the account's own rules the thing that stops them. The gate is the
product's discipline mechanism; if it lives anywhere but the server it is advice, not a rule.

## What is missing

**None of this sprint is implemented.** The five plan decisions are settled and the dependencies are
cleared, so what remains is the building. Recording it plainly because "PLANNED" next to a plan this
detailed reads, at a glance, like something that got done.

The `orders`, `trades` and `equity_snapshots` tables exist (`migrations/000006_execution.up.sql`)
and **nothing writes to them**. Specifically absent, in both repositories:

| | Missing |
|---|---|
| 04.1 | Order intake, bracket `PATCH`, cancel, position close / breakeven / close-all, and the read endpoints |
| 04.2 | The ten-row gate, in order, each with its own code — the product's discipline mechanism |
| 04.2b | The margin maths behind `INSUFFICIENT_MARGIN` (decided in this document, not written) |
| 04.3 | The fill engine: spread, seeded slippage, resting orders, gaps, the two same-bar rules |
| 04.4 | The daily drawdown halt, its market-day boundary, and the leak test that keeps the boundary off the wire |
| 04.5 | `trade` ledger entries and all six events |
| — | NFR-03's `(seed, bar_index, order_sequence)` PRNG contract and its replay-determinism test |
| FE | The whole execution dock: order ticket, compliance panel, on-chart brackets, position actions, tables, shortcuts, mobile slip |

**What this costs the sprints after it.** A session can be started, stepped, journalled, closed and
revealed — but nothing can be *traded* in it. So every number downstream that comes from a fill is
absent rather than wrong: Sprint 05's `strategy_return_pct` is 0 for every session, which makes
`alpha_pct` the negative of the benchmark, and a journal entry's `trade_id` is always null. Those
are arithmetically correct for a session in which nobody traded, and they are also uninformative.
Sprint 06's discipline index has nothing to measure at all.

Nothing downstream is *blocked* by this, which is why Sprint 05 proceeds — but the reveal's headline
comparison does not become meaningful until this sprint lands.

## Tasks

### 04.1 Order intake (FR-EXEC-01, FR-EXEC-11)
```
POST   /sessions/{id}/orders          {client_key, side, type, quantity|risk_pct,
                                       limit_price?, stop_loss, take_profit?}
PATCH  /sessions/{id}/orders/{oid}    {stop_loss?, take_profit?}
DELETE /sessions/{id}/orders/{oid}
POST   /sessions/{id}/positions/{pid}/close     {fraction?}       → close all or 50%
POST   /sessions/{id}/positions/{pid}/breakeven
POST   /sessions/{id}/positions/close-all
GET    /sessions/{id}/orders | /trades | /positions
```
- `client_key` is required. The unique index `(session_id, client_key)` turns an at-least-once
  network into exactly-once execution: a retried submit is rejected as `DUPLICATE_ORDER` rather
  than filled twice. A Redis lease short-circuits the common case before the database sees it.

### 04.2 The gate (BR-03, BR-04, BR-05, BR-09)

Evaluated in this order, each returning a **specific** code so the UI can say what failed:

| Order | Check | Failure code |
|---|---|---|
| 1 | Session open and not halted | `SESSION_CLOSED` |
| 2 | Stop loss present | `ORDER_STOP_REQUIRED` |
| 3 | Stop on the correct side of entry | `ORDER_STOP_INVALID` |
| 4 | Target on the correct side, if present | `ORDER_TARGET_INVALID` |
| 5 | Quantity > 0 and within precision | `ORDER_QUANTITY_INVALID` |
| 6 | Risk ≤ account `risk_per_trade_pct` | `RISK_STOP_TOO_WIDE` |
| 7 | R:R ≥ account `min_risk_reward` | `RISK_REWARD_TOO_LOW` |
| 8 | Open positions < `max_open_positions` | `MAX_POSITIONS_REACHED` |
| 9 | Equity supports the position | `INSUFFICIENT_MARGIN` |
| 10 | Daily drawdown gate not breached | `DAILY_DRAWDOWN_BREACHED` |

**A rejection is never a silent correction.** No check clamps a quantity or widens an R:R to make
an order acceptable — the order is refused. A trader who learns that oversized orders quietly
shrink learns nothing about sizing.

**A rejected order is persisted** (BR-09) with its `rejection_code`. Sprint 06's discipline index
is largely built from what the trader *tried* to do, and discarding rejections would erase it.

### 04.2a Where an order resolves — settled (`SP4-2`)

**There is no rewound cursor to resolve against.** The replay cursor is forward-only as of this
sprint's decision on review finding `SP4-2`: backward stepping and `/seek` are gone, and the session
carries one index rather than two. An order therefore always resolves at the cursor, because the
cursor is the only position there is.

This was the sharpest of the open questions, because one plausible reading of the earlier spec
permitted trading a bar whose outcome the trader had already seen. Removing the rewind removes the
reading rather than guarding against it, which is why the gate table below has no row for it: a
check that can never fail is a check that will eventually be deleted by someone who cannot see why
it is there.

A trader who wants a different setup randomizes a new feed. That is the product's answer to "I want
to try that again", and it is a better one than a rewind: a fresh feed is a fresh test, where a
replayed one is a memory test.

### 04.2b Margin and available equity — settled (`SP4-3`)

Gate row 9 reads "equity supports the position", which needs three numbers defined. Two are
formulas; the third is a product decision and is the reason this was a decision rather than an
implementation detail.

- **Notional** is `quantity × price × contract_size`. `market.Conventions.ContractSize` is expressed
  in **units of the base asset, not lots** — one unit of EUR, not a 100,000-unit standard lot — so a
  trader asking for 10,000 is asking for 10,000 EUR. This is not a rounding concern: reading units
  as lots inflates every requirement by a factor of 100,000 and the gate then refuses everything.
- **Required margin** is `notional ÷ leverage`. `RiskPolicy.Leverage` defaults to `1`, so a new
  account is cash-only and an order must be fully funded. Per-asset-class initial margin (FX 2%,
  index 5%, crypto 20%) is the more realistic model and is deliberately **not** adopted now: it
  needs a per-contract margin table the reference data does not have, and it can replace this
  formula later without changing the gate's shape or its error code.
- **Available equity** is `current_equity` — balance plus unrealized PnL on open positions — less
  the margin already committed to those positions.

**Unrealized losses count against the next order. That is the decision.** Sizing against
`current_balance` instead would let a trader keep adding to a position that is deep underwater,
because the paper loss is invisible to the gate. That is pyramiding into a loser, which is precisely
the behaviour Sprint 06's discipline index exists to flag — and a gate that permits what the journal
later scolds is the product contradicting itself. Sizing against equity makes the account run out of
margin on its own, which teaches the lesson instead of lecturing about it afterwards.

### 04.3 Fill engine (FR-EXEC-09)
- Market orders fill at the next bar's open plus spread and seeded slippage.
- Limit and stop orders rest and are resolved as the cursor advances.
- **Same-bar stop and target resolution:** when a bar's range covers both, the **stop is taken
  first**. This is the conservative assumption and it is documented, tested, and stated in the UI —
  the alternative flatters every result and teaches the wrong lesson. Sub-bar data can refine this
  later; guessing favourably cannot.
- **Same-bar entry and stop for a resting order (`SP4-5`, settled):** a limit or stop order rests,
  and one bar's range covers both its trigger price and its stop loss. **It fills and then stops
  out** — the same adverse assumption as above, applied one step earlier. With OHLC bars the path
  inside the bar is unknown, so this is an assumption whichever way it goes; written down it stays an
  assumption anyone can argue with, and left to the code it becomes whatever order the branches
  happen to be in and changes silently at the next refactor. A gap that clears both levels at once
  resolves the same way.
- Gap handling: a gap through a stop fills at the gap price, not the stop price.
- Per-bar: update open positions, MAE/MFE, unrealized PnL, and write an `equity_snapshots` row.

### 04.4 Drawdown halt (BR-05) — "daily" settled (`SP4-1`)
- Daily drawdown measured against the iteration's high-water mark, not its starting balance —
  measuring from the start would let a profitable account give back an unlimited amount unnoticed.
- **A day is a market day, derived server-side from the bar's real timestamp.** A session walks ~800
  bars of 15m data, roughly eight market days, in about three minutes of wall clock at 1× — so a
  wall-clock day is meaningless here, and a fixed bar count is unambiguous but is not a day, which
  leaves the prop-firm persona practising a rule they will not face. The server already holds what
  it needs: bars carry real timestamps that never leave it, and `orders.placed_bar_at` is the real
  instant of the placing bar.
- **The client is told that a limit binds and how much room is left. It is never told when the
  window turns over.** This is BR-01, not tidiness. A reset countdown — "daily drawdown resets in 14
  bars" — lets a trader watch where the resets fall, and a two-day gap every five days is a weekend:
  that rules out crypto outright and narrows everything else. So the risk payload carries
  `room_remaining_pct` and a halted flag, and carries no timestamp, no day ordinal, no bar count to
  the boundary and no countdown. `placed_bar_at` lives on the orders row and must not reach the wire.
  This is the kind of field that arrives in a payload because a progress meter needed a denominator,
  not because anyone decided to ship it, which is why it gets a leak test rather than a code review.
- On breach: open positions closed at market with `exit_reason = 'drawdown_halt'`, the session
  refuses new orders, and `risk.gate_breached` is emitted. The session is not deleted; the
  post-mortem needs it.

### 04.5 Ledger and events
- Every realized PnL movement appends a `trade` ledger entry, extending the Sprint 01 hash chain.
- Events: `order.placed`, `order.rejected`, `order.filled`, `trade.opened`, `trade.closed`,
  `risk.gate_breached`.

## Acceptance criteria

| # | Given | When | Then |
|---|---|---|---|
| 04-AC-1 | An order with no stop loss | Submitted | `ORDER_STOP_REQUIRED`; nothing is created |
| 04-AC-2 | A long with the stop above entry | Submitted | `ORDER_STOP_INVALID` |
| 04-AC-3 | An account with `min_risk_reward` 2.0 and an order at 1.5 | Submitted | `RISK_REWARD_TOO_LOW`; the order is **stored as rejected**, not discarded |
| 04-AC-4 | An oversized order | Submitted | Rejected — quantity is never silently reduced |
| 04-AC-5 | The same `client_key` twice | Submitted | One order exists; the second returns `DUPLICATE_ORDER` |
| 04-AC-6 | A bar whose range covers both stop and target | Advanced | Stop fills; documented and asserted |
| 04-AC-7 | An account at 4.9% daily drawdown, then a losing trade past 5% | Advanced | Positions closed, session halted, `risk.gate_breached` emitted |
| 04-AC-8 | A halted session | New order submitted | `DAILY_DRAWDOWN_BREACHED` |
| 04-AC-9 | A closed trade | Inspected | `realized_pnl`, `r_multiple`, MAE and MFE all populated |
| 04-AC-10 | Any fill | Ledger read | A `trade` entry exists and the chain still verifies |
| 04-AC-11 | An open position at an unrealized loss, and a second order that the starting balance would fund but current equity will not | Submitted | `INSUFFICIENT_MARGIN` — pyramiding into a loser is refused by the margin maths, per `SP4-3` |
| 04-AC-12 | A session whose bars cross a market-day boundary | Every risk and order payload inspected | Room remaining is reported; nothing identifies the boundary — asserted by a leak test, per `SP4-1` |
| 04-AC-13 | A resting limit order and a bar whose range covers both the limit price and the stop | Advanced | One filled-and-stopped trade at −1R, not an unfilled order, per `SP4-5` |
| 04-AC-14 | A position whose stop has been moved to breakeven | Closed at a small loss | The R-multiple is measured against the stop the position was **sized on**, so it is a small negative and not `0.0000`. 1R is fixed at entry; moving a stop does not rewrite it |
| 04-AC-15 | A position whose stop has been moved to breakeven | Its target amended | Accepted. `Breakeven` puts the stop *at* the entry by design, so an amend validates only the level it is actually given — otherwise the feature is a one-way door that freezes the target |
| 04-AC-16 | An open position | Closed in part | The **caller's** trade id stays open holding the remainder; the closed portion is the new row and carries the realized PnL, R-multiple and bars held. A fraction that rounds to nothing or to everything is refused, never promoted to a full close |
| 04-AC-17 | Any open position across any bar | Excursions inspected | MAE and MFE are **non-negative price distances**, in the same units as the stop, so "it went 0.6 of my stop against me" is a comparison the post-mortem can make directly |
| 04-AC-18 | A session's very first bar, on which a position fills and loses | Risk read | The loss has spent its share of the daily allowance. An unmarked market day takes its reference from the account's balance, because the first bar's own mark cannot be its own reference |

## Verified end to end

Driven against PostgreSQL with a real blinded feed, not only in unit tests: register → open an
account → start a session → place market and resting orders → step → breakeven → partial close →
amend → close all → verify the ledger. Worth recording because five of the defects above were
invisible to the stub-based tests and only appeared against real rows:

- the domain pre-incremented `version` before calling `Update`, which the stubs ignored and every
  adapter rejects — so every close, cancel and amend would have failed with
  `CONCURRENT_MODIFICATION` in production while the unit suite stayed green;
- `ClosePosition` rebuilt its own return value instead of using what the close computed, so a client
  closing a position was told it closed for nothing while the row held the real P&L;
- excursions were signed money figures, so an adverse excursion was negative and in different units
  from the stop it exists to be compared against;
- the R-multiple divided by the live stop, so `Breakeven` silently destroyed it;
- the partial close handed the caller's id to the closed half.

The stubs now enforce the optimistic lock and mirror the adapters' filtering, so the first of those
cannot recur silently.

## Test plan
- **Domain:** the gate as a table test — every check, its order, and the exact code, including the
  case where two checks would both fail and only the first is reported.
- **Property:** replaying a session's orders against the same seed reproduces every fill.
- **Integration:** idempotency under concurrent duplicate submits against a real database.
- **Leak:** a payload-and-struct guard over the order, position and risk response types in the style
  of `entities/response/feed/leak_test.go`, asserting no timestamp, day ordinal or boundary
  countdown reaches the wire (`SP4-1`). Verified non-vacuous by reintroducing the field it forbids.

## Definition of done
All criteria met; a full session can be traded end to end with the gate refusing every rule
violation and the ledger verifying afterwards. Specifically:

- the gate table test covers every row, its order, and the case where two rows would both fail and
  only the first is reported;
- the day-boundary leak test exists and has been shown to fail when the forbidden field is put back;
- the `(seed, bar_index, order_sequence)` PRNG contract is documented and its replay determinism
  asserted (NFR-03, carried in from review finding `BE-03-3`).

## Risks

| Risk | Mitigation |
|---|---|
| Same-bar ambiguity disputed as "unfair" | Documented, surfaced in the UI, and conservative by choice; sub-bar refinement is a later option |
| Concurrent submits racing the idempotency lease | The database unique index is authoritative; Redis is only the fast path |
| Floating-point drift in PnL | `shopspring/decimal` throughout; no float arithmetic on money (NFR-08) |
