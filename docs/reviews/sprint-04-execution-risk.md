# Review — Sprint 04: Execution and the risk gate

> **This is a plan review, not a code review.** Sprint 04 has not been implemented: there is no
> order intake, no fill engine and no gate in either repository. The `orders` and `trades` tables
> exist (`migrations/000006_execution.up.sql`) and nothing writes to them. What follows reviews
> `docs/requirements/sprint-04-execution-risk.md` as a specification, and flagged the decisions that
> needed making before it starts rather than during it. **All five have since been decided** — `SP4-2`
> by removing the rewind from the product, the other four recorded here and in the sprint plan.

**Reviewed:** `docs/requirements/sprint-04-execution-risk.md`, `sprint-04-execution-dock.md`
(frontend), `migrations/000006_execution.up.sql`, register rows FR-EXEC-01..12, FR-TA-06, FR-UI-09,
BR-03/04/05/09.

## Verdict

**The plan is the best-specified of the four and needs the least rewriting.** Two decisions in it
are unusually good and should survive contact with implementation intact:

- **The gate returns a specific code per check, in a defined order, and never silently corrects.**
  "A trader who learns that oversized orders quietly shrink learns nothing about sizing" is the
  whole product argument in one sentence, and the ten-row table makes it enforceable rather than
  aspirational.
- **A rejected order is persisted.** Sprint 06's discipline index is built largely from what the
  trader *tried* to do; discarding rejections would erase the most interesting data the product
  collects. Very few systems get this right, because a rejection feels like a non-event.

Also correct: **same-bar stop-and-target resolves the stop first**, documented as the conservative
assumption, with the reason stated — the alternative flatters every result. And `stop_loss NOT NULL`
in the schema, so "no entry without a stop" is a database guarantee rather than a validation rule
somebody can bypass.

Five things needed deciding first. None was a flaw in the plan so much as a question it did not ask.

**All five are now settled**, and each is recorded below with the decision and what it rules out. The
sprint plan carries the same wording, because a decision that lives only in a review is one the
implementer never reads.

| | ID | Question, and what was decided |
|---|---|---|
| ~~High~~ **DECIDED** | `SP4-1` | ~~What is "daily" in replay time?~~ — **a market day from the bar's real timestamp, computed server-side.** The client learns that a limit binds and how much room is left, never when the window turns over; a leak test holds the line |
| ~~High~~ **DECIDED** | `SP4-2` | ~~Can a trader place an order while rewound?~~ — **the cursor is forward-only.** Backward stepping and `/seek` removed, the two indices collapsed to one; there is no rewound state for an order to land in |
| ~~Medium~~ **DECIDED** | `SP4-3` | ~~The margin model behind `INSUFFICIENT_MARGIN`~~ — **required margin is notional ÷ leverage; available equity includes unrealized PnL.** A losing position reduces what the next order can size against, so the gate refuses pyramiding |
| ~~Medium~~ **FIXED** | `SP4-4` | ~~Sizing depends on tick size, which Sprint 02 hardcodes~~ — `BE-02-3` is fixed: `market.DeriveConventions` derives tick size and quote currency per asset class and symbol. The `ContractSize` units question it left behind is answered by `SP4-3` |
| ~~Low~~ **DECIDED** | `SP4-5` | ~~Same-bar entry-and-stop for resting orders~~ — **fill, then stop.** The same adverse assumption as the stop-versus-target rule, one step earlier |

---

## The decisions

### `SP4-1` · ~~High~~ **DECIDED** · A day is a market day, and the client is never told where it ends

**Decision: the server derives the boundary from real bar timestamps and reports only room
remaining.** No timestamp, day ordinal, bar count to the boundary or reset countdown reaches the
client, and a leak test over the order, position and risk response types asserts it.

**Where:** §04.4, BR-05, and the `DAILY_DRAWDOWN_BREACHED` gate row.

The plan says daily drawdown is measured against the iteration's high-water mark rather than its
starting balance — that part is right and well argued. It does not say what a **day** is.

A session walks 800 bars of 15m data, which is roughly eight market days, in about three minutes of
wall-clock time at 1×. So "daily" could mean:

- a **market day** derived from the bar timestamps the server holds, or
- a **wall-clock day**, which in a replay is meaningless, or
- a **fixed bar count**, which is neither but is at least unambiguous.

Market day is clearly the intent. But it collides with BR-01, and that is the part worth stopping on:
**exposing day boundaries to the client leaks the asset class.** If the UI shows "daily drawdown
resets in 14 bars", a trader watching where the resets fall sees a two-day gap every five days in FX
and never in crypto. That is a weekend, and a weekend narrows the instrument set considerably.

**Why a market day and not a bar count.** A fixed bar count would have dodged the leak entirely,
which is its only real argument. It is also not a day: the prop-firm persona would be practising a
rule they will not meet at a desk, which is most of what they came for. So the definition was never
really in doubt, and the leak is the part the decision has to carry into the code.

**What it costs.** A trader cannot see a reset coming, which is a real loss of affordance and
arguably more realistic than the alternative: a live desk does not show you a countdown to your own
risk limit resetting either. `room_remaining_pct` plus a halted flag is the whole contract.

The guard is a leak test in the style of `feed/leak_test.go` rather than a code-review convention,
because this is exactly the kind of derived field that reaches a payload without anyone deciding it
should — a progress meter needs a denominator and the denominator is the boundary.

### `SP4-2` · ~~High~~ **DECIDED** · The cursor is forward-only

**Decision: a trader cannot go back at all.**

Backward stepping is refused with `CURSOR_IS_FORWARD_ONLY`, `POST /sessions/{id}/seek` is gone, and
`cursor_index`/`revealed_index` have collapsed into one index (migration `000012`). Once a bar is
stepped past it is history, the way it is on a live chart. A trader who wants a different setup
randomizes a new feed.

This removes the question rather than answering it. The three readings the old spec permitted —
refuse while rewound, resolve at the edge, resolve at the cursor — reduce to one, because there is
no rewound state. The third reading was the dangerous one: it would have let somebody step back and
trade a bar whose outcome they had already seen, which is the hindsight the product exists to
remove, and it was reachable by a plausible reading of the document as written.

**What this cost.** Sprint 03 built the two-index model specifically to make rewinding safe, and the
frontend's "Reviewing T-N · live edge held" banner and step-back control went with it. That work was
not wasted — it was the correct design for the product as specified at the time, and the database
CHECK it introduced is what made the contradiction visible enough to decide. But a second index that
can never differ from the first is a model that lies about what the system does, so it went rather
than being left in place "in case".

**What this does not cost.** Forward-only bounds where the *cursor* can go, not what the trader may
look at. Everything already stepped past stays readable and on the chart; scrolling back over your
own history is ordinary charting and is unaffected.

### `SP4-3` · ~~Medium~~ **DECIDED** · Margin is notional ÷ leverage, against equity not balance

**Decision: required margin is `quantity × price × contract_size ÷ leverage`; available equity is
balance plus unrealized PnL less margin already committed.** `ContractSize` stays 1 unit of the base
asset. `Leverage` keeps its default of `1`, so a new account is cash-only until the trader opts in.

**Where:** gate row 9, "Equity supports the position".

`RiskRule.Leverage` exists on the account (both the Go request type and the frontend schema), and
nothing in the plan says how it converts a position into a margin requirement. Notional ÷ leverage?
Per-asset-class initial margin? Is a losing open position's unrealized loss deducted from available
equity before the next order is sized?

That last one is not a detail — it decides whether a trader can pyramid into a losing position, and
that is exactly the behaviour the discipline index in Sprint 06 will want to measure.

**The third question is the one that mattered, and it is answered "yes, unrealized PnL counts".** A
gate that sizes against balance lets a trader keep adding to a position that is deep underwater,
because the paper loss never reaches the check. Sizing against equity means the account runs out of
margin by itself — the simulator enforces the lesson instead of the journal delivering it as a
scolding three screens later.

Per-asset-class initial margin was the realistic alternative and was rejected for now, not
forgotten: it needs a per-contract margin table the reference data does not have, and it substitutes
for the divisor later without touching the gate's shape or its error code. Recorded in §04.2b of the
sprint plan, with `04-AC-11` asserting the pyramid case is refused.

### `SP4-4` · ~~Medium~~ **FIXED** · Tick size is derived, and the units question moved to `SP4-3`

**Resolution: `BE-02-3` is fixed.** `market.DeriveConventions` derives tick size and quote currency
from the asset class and symbol — 0.001 against the yen, 0.01 for an equity or a USD-quoted crypto
major, the five-decimal convention elsewhere — with property tests and a per-run override. What this
finding left open was whether `ContractSize` means units or lots, and that is decided in `SP4-3`:
units of the base asset.

**Where:** gate rows 5 and 6; `cmd/loader/main.go:74-75`; review finding `BE-02-3`.

Row 5 checks "quantity > 0 and within precision" and row 6 computes risk from stop distance. Both
quantize against the instrument's tick size and contract size. Every instrument in the database
currently has `TickSize 0.00001` and `QuoteCurrency "USD"`, because the loader hardcodes them.

For EURUSD that is right. For USDJPY (0.001), an equity (0.01) or BTC it is wrong, and the wrongness
is the dangerous kind: risk-per-trade will compute to a plausible-looking number that is off by a
factor of a hundred.

These are still *defaults* rather than reference data — a real venue's tick table is per-contract —
but they are no longer one FX convention applied to an equity, and the failure mode this finding
named (a plausible-looking risk number off by a factor of a hundred) is gone.

### `SP4-5` · ~~Low~~ **DECIDED** · A resting order that sees both its trigger and its stop fills, then stops

**Decision: fill, then stop — a closed trade at −1R, not an unfilled order.**

**Where:** §04.3.

The plan handles the same-bar stop-versus-target case carefully. It does not handle the analogous
case one step earlier: a limit order rests, and a single bar's range covers both the limit price and
the stop. Did the order fill and then stop out within that bar, or not fill at all?

The answer is the same conservative one the stop-versus-target rule already takes: assume the adverse
sequence. It is Low severity because it is rare and the answer is obvious — but *unwritten* means
decided by whatever order the branches happen to sit in, and then changed silently by the next person
who reorganises the fill loop. It is now beside the other rule in §04.3, with `04-AC-13` asserting it.

---

## Things the plan gets right that are worth not losing

- **`stop_loss NOT NULL` in the schema.** The discipline rule is a database constraint, not a
  validation that a future refactor can route around.
- **`UNIQUE (session_id, client_key)`** turns an at-least-once client retry into a no-op. Note that
  `cache.Reserve` was built as the idempotency primitive and, like `cache.Incr`, currently has no
  caller — the unique index is doing the work. That is the more durable of the two mechanisms, so
  consider deleting `Reserve` rather than finding it a job.
- **Ordered, individually-coded gate checks.** The order matters for the message the trader sees:
  telling someone their R:R is too low when they have not set a stop at all would be noise.

## Dependencies — all cleared

1. `BE-02-3` (tick size) — **fixed**, per `SP4-4`. This was the one blocking dependency.
2. `BE-03-1` (the idle sweeper) — **done**, running in `cmd/worker`. It was never blocking, but
   Sprint 04 makes an abandoned session more expensive, because it will hold open positions and a
   drawdown state rather than just a cursor.
3. `BE-03-3` — NFR-03's deterministic session hash belongs to this sprint; **already re-dated** in
   the register, and the `(seed, bar_index, order_sequence)` PRNG contract is now in the sprint's
   Definition of Done.
4. All five plan decisions — **settled**, and written into the sprint plan rather than left here.

**Nothing is outstanding against this plan.** The remaining pre-Sprint-04 item in the project is
unrelated to execution: `BE-01-3` / FR-AUTH-04, the account settings screen.
