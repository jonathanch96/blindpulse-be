# Review — Sprint 04: Execution and the risk gate

> **This is a plan review, not a code review.** Sprint 04 has not been implemented: there is no
> order intake, no fill engine and no gate in either repository. The `orders` and `trades` tables
> exist (`migrations/000006_execution.up.sql`) and nothing writes to them. What follows reviews
> `docs/requirements/sprint-04-execution-risk.md` as a specification, and flags the decisions that
> need making before it starts rather than during it.

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

Five things need deciding first. None is a flaw in the plan so much as a question it does not ask.

| | ID | Open question |
|---|---|---|
| **High** | `SP4-1` | What is "daily" in replay time — and does answering it leak the asset class? |
| ~~High~~ **DECIDED** | `SP4-2` | ~~Can a trader place an order while rewound?~~ — **the cursor is forward-only.** Backward stepping and `/seek` removed, the two indices collapsed to one; there is no rewound state for an order to land in |
| Medium | `SP4-3` | The margin model behind `INSUFFICIENT_MARGIN` is unspecified |
| Medium | `SP4-4` | Position sizing needs tick size, which is currently hardcoded for every instrument |
| Low | `SP4-5` | Same-bar entry-and-stop for resting orders is unaddressed |

---

## Open questions

### `SP4-1` · High · "Daily" drawdown has no defined meaning in a replay, and defining it may leak

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

**Decide before building.** The safe shape is that the server computes the boundary from real
timestamps, enforces the halt, and the client is told only *that* a limit binds and how much room is
left — never when the window turns over. Worth an explicit note in the sprint doc and a leak test in
the same style as `feed/leak_test.go`, because this is precisely the kind of derived field that
reaches a payload without anyone deciding it should.

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

### `SP4-3` · Medium · `INSUFFICIENT_MARGIN` has no model behind it

**Where:** gate row 9, "Equity supports the position".

`RiskRule.Leverage` exists on the account (both the Go request type and the frontend schema), and
nothing in the plan says how it converts a position into a margin requirement. Notional ÷ leverage?
Per-asset-class initial margin? Is a losing open position's unrealized loss deducted from available
equity before the next order is sized?

That last one is not a detail — it decides whether a trader can pyramid into a losing position, and
that is exactly the behaviour the discipline index in Sprint 06 will want to measure.

**Fix.** One paragraph in §04.2 defining available equity, required margin, and whether unrealized
PnL counts. It is a product decision, not an implementation one, which is why it belongs in the plan.

### `SP4-4` · Medium · Sizing depends on tick size, which Sprint 02 hardcodes

**Where:** gate rows 5 and 6; `cmd/loader/main.go:74-75`; review finding `BE-02-3`.

Row 5 checks "quantity > 0 and within precision" and row 6 computes risk from stop distance. Both
quantize against the instrument's tick size and contract size. Every instrument in the database
currently has `TickSize 0.00001` and `QuoteCurrency "USD"`, because the loader hardcodes them.

For EURUSD that is right. For USDJPY (0.001), an equity (0.01) or BTC it is wrong, and the wrongness
is the dangerous kind: risk-per-trade will compute to a plausible-looking number that is off by a
factor of a hundred.

**Fix `BE-02-3` first.** It is a small change in Sprint 02's loader and it is a prerequisite here,
not a parallel concern. It is scoped into Sprint 08 §08.1 — if Sprint 08 runs after Sprint 04, pull
that one item forward.

### `SP4-5` · Low · Same-bar entry-and-stop for resting orders

**Where:** §04.3.

The plan handles the same-bar stop-versus-target case carefully. It does not handle the analogous
case one step earlier: a limit order rests, and a single bar's range covers both the limit price and
the stop. Did the order fill and then stop out within that bar, or not fill at all?

The consistent answer is the same conservative one — assume the adverse sequence, fill then stop —
but it should be written down beside the other rule rather than decided in code.

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

## Dependencies to settle before starting

1. `BE-02-3` (tick size) — blocking, per `SP4-4`.
2. `BE-03-1` (the idle sweeper) — not blocking, but Sprint 04 makes an abandoned session more
   expensive, because it will hold open positions and a drawdown state rather than just a cursor.
3. `BE-03-3` — NFR-03's deterministic session hash belongs to this sprint; **already re-dated** in
   the register. Make the `(seed, bar_index, order_sequence)` PRNG contract part of the sprint's
   Definition of Done.
4. `SP4-2` — **settled**: the cursor is forward-only, shipped ahead of the sprint. Three decisions
   remain (`SP4-1`, `SP4-3`, `SP4-5`).
