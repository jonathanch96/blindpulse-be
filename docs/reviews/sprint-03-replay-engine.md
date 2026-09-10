# Review — Sprint 03: Replay engine and terminal

> **Update:** `BE-03-1` and `FE-03-1` were both fixed after this review. The sweeper abandoned 23
> leftover sessions on its first tick against the development database — the lockout in evidence —
> and the affected account then started a new session successfully.
>
> Writing the mobile spec `FE-03-1` asked for immediately found what the empty project had been
> hiding: **the transport controls sat underneath the fixed workspace nav and could not be tapped**,
> so tap-to-step was unreachable on a phone. The cause was `.mobile-page-bottom`, a utility written
> for exactly this and applied to nothing — the same pattern as `BE-01-1`'s unwired rate limiter and
> `BE-02-2`'s uncalled validator, which is now three instances of the same failure mode in one
> codebase. NFR-04 is PARTIAL rather than DONE: pinch-zoom and swipe-up sheets are still not built,
> and the register no longer implies otherwise.

**Reviewed:** `services/blindpulse/v1/domain/session`, `db/blindpulse/replay_sessions`,
`controllers/session`, `pkg/cache`, and the frontend's `chart`, `drawing` and `session` features.
**Against:** `docs/requirements/sprint-03-replay-engine.md` (backend),
`sprint-03-replay-terminal.md` (frontend), register rows FR-REPLAY-01..08, FR-TA-01..10,
BR-02/10, NFR-01..04.

## Verdict

**The strongest sprint in the project, and the one that most repays reading.** The cursor model is
the good decision: two indices, a database CHECK enforcing their relationship, and refusal rather
than clamping when a client asks for something it should not have. Everything downstream inherits
that discipline.

Three things are worth acting on. One is architectural debt that Sprint 03E knowingly took and
recorded. One is a requirement that is listed as proven by a test project containing no tests. One
is a state the system can enter and cannot leave.

| | ID | Finding |
|---|---|---|
| ~~High~~ **FIXED** | `BE-03-1` | ~~An abandoned session locks its account out permanently~~ — the worker now sweeps idle sessions, using the index migration 000011 built for it |
| ~~High~~ **FIXED** | `FE-03-1` | ~~NFR-04's stated proof is a Playwright project with no spec files~~ — `mobile-replay-terminal.spec.ts` now drives a full session at 390px, and writing it found a real bug: the transport controls were unreachable behind the workspace nav |
| ~~Medium~~ **FIXED** | `BE-03-2` | ~~Every released bar costs a full-window database read~~ — the window is cached in Redis under the TTL that was already configured for it |
| ~~Medium~~ **FIXED** | `BE-03-3` | ~~NFR-03 is listed under Sprint 03 and is not implemented~~ — re-dated to Sprint 04, where fills first exist |
| ~~Low~~ **FIXED** | `BE-03-4` | ~~`pkg/cache`'s lease scripts are untested~~ — eight tests against a real Redis, covering renewal, expiry handover and release-by-non-holder |
| ~~Low~~ **PARTLY FIXED** | `FE-03-2` | ~~Chart and drawing renderers are untested~~ — `render.test.ts` on both sides now asserts against a recording canvas context, and catches the `withAlpha` bug. The React *components* (`price-chart.tsx`, `drawing-toolbar.tsx`) remain untested |

## What holds up

**BR-02, the cursor model.** `cursor_index` (where the trader looks) and `revealed_index` (the
high-water mark) are separate, reads are bounded by the second, and
`replay_sessions_cursor_within_revealed` enforces `cursor <= revealed` in PostgreSQL. Rewinding
moves one and never the other. This resolves a genuine contradiction in the PRD — "step backward"
versus "cursor bounds visibility" — rather than picking one and hoping.

**Refusal, not clamping.** `Seek` past the edge returns `INVALID_CURSOR` naming both indices
(`domain/session/service.go:143`). A clamp would turn an attempt to peek into a
successful-looking response and hide a client bug at the same time. This is stated in the comment
and honoured everywhere it applies.

**The Redis cache is a cache.** `load()` prefers cached state **only forward** — `if
state.RevealedIndex >= entity.RevealedIndex` — so a stale cache can never un-reveal a bar the trader
has already been shown. Every `StateStore` method is a safe no-op when Redis is off. The distinction
between "a cache miss" and "a failure" is made correctly and consistently.

**The pause race was found by running it, not by reading it.** The clock read the status, slept a
tick, then called `Step` — which reloads, and `loadLive` accepts a paused session because pausing
and stepping bar-by-bar is a deliberate workflow. Exactly one bar leaked past every pause. The
regression test needed a realistic 200ms tick to reproduce; at the 2ms the other streaming tests
use, the window is too small to hit, and the first version of the test **passed against the broken
code**. That was caught and corrected rather than shipped as false assurance.

**Backpressure has a stated direction and a reason.** Latest-wins at both the bus and the socket,
because a trader acts on what is on screen — and skipped bars are recoverable via index-gap
detection and HTTP backfill, where a stale frame is not recoverable at all.

**The drawing tools share one shape function** between the renderer and the hit-tester, so what is
drawn is what answers a click. Anchors are bar indices and never timestamps, guarded both over the
serialized drawing and structurally by refusing any use of the clock in the feature.

---

## Findings

### `BE-03-1` · High · An abandoned session locks the account out

**Where:** `entities/domain/session/session.go:22` (`StatusAbandoned`),
`adapters/rest/config/config.go:124` (`SessionIdleTimeout`), `migrations/000011_session_cursor.up.sql:27`
(the `(status, last_active_at)` index).

Three pieces of a sweeper exist and the sweeper does not:

- `StatusAbandoned` is defined and handled in read paths. **Nothing ever sets it.**
- `REPLAY_SESSION_IDLE_TIMEOUT` defaults to 30m and is read by no code.
- The index built to make the sweep efficient has no query to serve.

Meanwhile `replay_sessions_single_open` and the domain's `SESSION_ALREADY_OPEN` check enforce one
live session per account. So a trader who closes the browser mid-replay has an account that **will
never start another session** until that one is explicitly closed.

**Why it matters.** It is reachable by the most ordinary user action there is — closing a tab — and
the recovery is non-obvious. The frontend does list open sessions, so a trader who thinks to look
can close it, but nothing tells them that is what is wrong.

**Fix.** The sweeper the schema is already built for: a periodic job in `cmd/worker` flipping
`open`/`paused` sessions past `last_active_at + idle timeout` to `abandoned`, emitting
`session.abandoned`. The sprint doc calls for it (§03.1, FR-REPLAY-07) and the register has recorded
it as "DONE except the idle sweeper" throughout — this is the item that note refers to, raised to
High because the consequence is a lockout rather than a tidiness problem.

### `FE-03-1` · High · The mobile test project contains no tests

**Where:** `playwright.config.ts:31-35`, `e2e/`.

```ts
{ name: "mobile-chromium", testMatch: /mobile-.*\.spec\.ts/, use: { ...devices["iPhone 13"] } },
```

There is no `mobile-*.spec.ts` file. `e2e/` contains exactly one spec, for the account reset tree.
The mobile project therefore matches nothing, runs nothing, and **passes**.

The register lists NFR-04 ("Full touch gestures on mobile web / PWA") with "Playwright mobile
project" in the *How it is proven* column. That proof does not exist. This is the same failure mode
as a leak guard that never fires — worse, because a green project name reads as coverage.

**Fix.** Either write the mobile spec driving pinch, drag-crosshair and tap-to-step at 390px, or
change NFR-04's proof column to say what is actually true. The first is the intent; the second is
the minimum honest step, and should happen immediately either way.

### `BE-03-2` · Medium · Every released bar costs a full-window database read

**Where:** `domain/session/stream.go` (`frameFor`) → `domain/feed/service.go:283` (`ViewBars` →
`ListWindow`).

Building one frame does: `Feeds.Get` (a row read), then `ViewBars`, which calls `ListWindow` to
fetch **the entire feed window** — up to 800 rows — then normalizes the revealed prefix and
aggregates it. At 10× playback with a 250ms base tick that is roughly 40 full-window reads per
second, per session.

Sprint 03's own document (§03.3) says the opposite: "Frames are pre-decoded and normalized in Redis,
so the streaming path never touches PostgreSQL." `REDIS_BAR_WINDOW_TTL` was configured for exactly
this and is read by no code.

**In fairness**, this was recorded as a known limitation when 03E shipped rather than discovered
here, and the measured end-to-end latency is 4–6ms against a 15ms budget — so it is not currently
hurting anyone. It is a scaling ceiling, not a defect: the cost is per concurrent session, and the
current concurrency is one.

**Fix.** Cache the normalized series per feed in Redis under the TTL that already exists. The
series is immutable once built, so it is the easiest possible cache — no invalidation question.

### `BE-03-3` · Medium · NFR-03 is attributed to Sprint 03 and is not implemented

**Where:** register row NFR-03; `entities/domain/session/session.go:57` (`RootHash *string`).

The register lists NFR-03 ("Deterministic session hashes over every action and fill") against Sprint
03, proven by "Property test: same feed + seed ⇒ identical fills and hash". `Session.RootHash` is a
field that is never written. `Session.Seed` is drawn and stored but nothing consumes it.

This is defensible — there are no fills until Sprint 04, so the hash *cannot* be computed yet — but
the register does not say that, and a reader would reasonably take the row as delivered. The
sprint's Definition of Done does not mention it either way.

**Fix.** Re-date NFR-03 to Sprint 04 in both register copies. The seed plumbing being in place
already is the right groundwork; it just is not the requirement.

### `BE-03-4` · Low · The Redis lease scripts are untested

**Where:** `pkg/cache/redis.go` (`Hold`, `ReleaseHold`, `Take`) — `pkg/cache` has no test file.

`Hold` and `ReleaseHold` are Lua scripts implementing the driver lease that stops two replicas
double-stepping one session — which the code correctly identifies as a hindsight leak rather than a
display glitch. The domain-level lease behaviour *is* tested against a stub, so the logic that uses
the lease is covered; the scripts themselves are not.

They are short and conditional-correctness matters (`GET == holder or not exists`), so they are
worth an integration test against a real Redis, which the compose stack already provides.

### `FE-03-2` · Low · The chart and drawing components have no tests

**Where:** `src/features/chart/price-chart.tsx`, `render.ts`; `src/features/drawing/render.ts`,
`drawing-toolbar.tsx`.

The *kernels* are covered thoroughly — `chart-math`, `chart-geometry`, `fib`, `hit-test`,
`projection`, `reducer`, `stream` all have real tests, and the fib work includes a blinding-invariance
property test. The components that compose them do not, and canvas rendering is genuinely awkward to
test.

This is a reasonable trade rather than an oversight, and it is named because it is the reason
`withAlpha` stayed broken for a whole slice: a bug in a *rendering* concern, invisible to every
kernel test, found only by looking at a screenshot. A small number of component tests over
`drawFrame` against a stub context — asserting that a forming bar strokes rather than fills, that a
tinted fill actually carries an alpha — would have caught it.

**Since fixed for the renderers.** `src/test/recording-canvas.ts` captures every call *and the style
in force at that call*, which is the distinction that matters: the renderers set `fillStyle`
immediately before each shape, so the final value says nothing about what any given shape was
painted with. Restoring the original `withAlpha` now fails six tests, among them "tints the volume
bars rather than painting them solid" and "zone fill … is opaque". The React components that compose
these renderers are still untested; that part of the finding stands.

---

## Documentation drift

- The frontend sprint doc's §03.5 says drawing anchors must be "bar indices, never timestamps" and
  the implementation honours it. Worth noting the doc does **not** mention that higher timeframes
  have their own index space; 03F discovered that and solved it by scoping drawings per timeframe.
  The doc should record the constraint, not just the solution.
- NFR-01 is marked PARTIAL correctly (measured and displayed; no p99-under-load histogram). That is
  the one honest PARTIAL in the register and it should stay that way until the load test exists.

## Not checked

Multi-replica behaviour (the lease and the pub/sub fan-out were exercised against a single instance
plus stubs, never two real API processes), the frame path under genuine network loss as opposed to a
killed process, and NFR-01 under concurrent load.
