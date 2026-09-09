# Sprint 04 — Execution and the risk gate (backend)

**Status:** PLANNED · **Estimate:** 10–12 dev-days
**Requirements:** FR-EXEC-01..05, FR-EXEC-07..09, FR-EXEC-11, BR-03, BR-04, BR-05, BR-09
**PRD:** §3.3
**Depends on:** Sprint 03 · **Blocks:** Sprints 05, 06

## Goal

Let the trader act, and make the account's own rules the thing that stops them. The gate is the
product's discipline mechanism; if it lives anywhere but the server it is advice, not a rule.

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

### 04.3 Fill engine (FR-EXEC-09)
- Market orders fill at the next bar's open plus spread and seeded slippage.
- Limit and stop orders rest and are resolved as the cursor advances.
- **Same-bar stop and target resolution:** when a bar's range covers both, the **stop is taken
  first**. This is the conservative assumption and it is documented, tested, and stated in the UI —
  the alternative flatters every result and teaches the wrong lesson. Sub-bar data can refine this
  later; guessing favourably cannot.
- Gap handling: a gap through a stop fills at the gap price, not the stop price.
- Per-bar: update open positions, MAE/MFE, unrealized PnL, and write an `equity_snapshots` row.

### 04.4 Drawdown halt (BR-05)
- Daily drawdown measured against the iteration's high-water mark, not its starting balance —
  measuring from the start would let a profitable account give back an unlimited amount unnoticed.
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

## Test plan
- **Domain:** the gate as a table test — every check, its order, and the exact code, including the
  case where two checks would both fail and only the first is reported.
- **Property:** replaying a session's orders against the same seed reproduces every fill.
- **Integration:** idempotency under concurrent duplicate submits against a real database.

## Definition of done
All criteria met; a full session can be traded end to end with the gate refusing every rule
violation and the ledger verifying afterwards.

## Risks

| Risk | Mitigation |
|---|---|
| Same-bar ambiguity disputed as "unfair" | Documented, surfaced in the UI, and conservative by choice; sub-bar refinement is a later option |
| Concurrent submits racing the idempotency lease | The database unique index is authoritative; Redis is only the fast path |
| Floating-point drift in PnL | `shopspring/decimal` throughout; no float arithmetic on money (NFR-08) |
