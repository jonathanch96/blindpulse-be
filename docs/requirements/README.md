# BlindPulse Replay Lab — backend requirements

Everything the backend owes, traced from the approved PRD down to a sprint task and an acceptance
criterion. Nothing here is a summary of the code; it is the specification the code is measured
against.

## How to read this folder

| File | What it is | When you read it |
|---|---|---|
| [`PRD.md`](PRD.md) | The approved PRD v1.0.0, verbatim | The source of truth for *what* and *why*. Mirrored from the FE repo — edit both together |
| [`requirements-register.md`](requirements-register.md) | Every requirement with a stable ID, its PRD section, owner and sprint | To find which sprint owns a requirement, or to check nothing was dropped |
| `sprint-NN-*.md` | One sprint: tasks, API contracts, data model, acceptance criteria, tests, risks | When you pick up the sprint |

Sprint documents cite register IDs (`FR-EXEC-03`) and PRD sections (`PRD §3.3`). If you are about
to build something that has neither, it is not agreed work — add it to the register first.

## Sprints

| Sprint | Theme | Status | Doc |
|---|---|---|---|
| 00 | Foundation and infrastructure | **DONE** | [sprint-00-foundation.md](sprint-00-foundation.md) |
| 01 | Identity, accounts and reset trees | **DONE** | [sprint-01-identity-accounts.md](sprint-01-identity-accounts.md) |
| 02 | Market data and blinded feeds | Planned | [sprint-02-market-data-feeds.md](sprint-02-market-data-feeds.md) |
| 03 | Replay session engine | Planned | [sprint-03-replay-engine.md](sprint-03-replay-engine.md) |
| 04 | Execution and the risk gate | Planned | [sprint-04-execution-risk.md](sprint-04-execution-risk.md) |
| 05 | Journal, drawings and the reveal | Planned | [sprint-05-journal-reveal.md](sprint-05-journal-reveal.md) |
| 06 | Analytics and the discipline index | Planned | [sprint-06-analytics-discipline.md](sprint-06-analytics-discipline.md) |
| 07 | Institutional access and hardening | Planned | [sprint-07-institutional-access.md](sprint-07-institutional-access.md) |

Sprint numbers are shared with `blindpulse-fe`: backend sprint *n* is what frontend sprint *n*
consumes. Each frontend sprint doc names the contract it needs; each backend sprint doc publishes it.

## The rule that decides where logic lives

**Anything a trader could gain by lying about is computed on the server.** The browser renders a
chart and collects intent; it never decides what time it is in the replay, what a fill price was,
or whether an order passed the gates.

That single rule explains most of this specification: why the cursor is server-authoritative
(BR-02), why the gate refuses rather than resizes (BR-04), why blinding works by *not sending* the
symbol rather than by hiding it (BR-01), and why the ledger is hash-chained (BR-07).

## Delivery order and why

1. **Accounts before the engine** — a session belongs to an account, and the account's risk policy
   is what the order gate will enforce. Building the engine first would mean retrofitting ownership
   and limits onto sessions that already exist.
2. **Feeds before the engine** — the engine has nothing to replay without them, and blinding is
   easier to get right in isolation than inside a streaming loop.
3. **Execution before journal** — a journal with no trades in it cannot be designed honestly.
4. **Analytics last** — projections are built from the event stream, so the stream has to exist and
   be stable first. Building them earlier means rebuilding them.

## Status at a glance

Done: the foundation, both infrastructure paths (Redis hot path, Kafka outbox spine), the full
schema, authentication, and accounts with reset trees and a verified hash chain.

Next: Sprint 02, which owns the product's central promise. It is also the sprint where a single
leaked field would undo the whole premise, so its leak test (NFR-05) lands in CI before any feed is
published.
