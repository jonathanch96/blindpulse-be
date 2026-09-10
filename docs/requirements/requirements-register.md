# Requirements register

Every requirement the product owes, given a stable ID, traced back to its PRD section and forward
to the sprint that delivers it. Sprint documents cite these IDs; nothing gets built that is not on
this list, and nothing on this list is left without a sprint.

Mirrored in both repositories — update both in the same change.

**Legend.** Owner: `BE` backend, `FE` frontend, `BOTH` a contract across the seam.
Status: `DONE`, `PLANNED`, `DEFERRED` (explicitly out of v1).

---

## 1. Business rules (BR)

The invariants the product exists to enforce. Every one of them is enforced **server-side**; the
UI mirrors them for the trader's benefit but is never the thing that applies them. A rule that only
the browser enforces is a rule a `curl` command ignores.

| ID | Rule | PRD | Owner | Sprint | Status |
|---|---|---|---|---|---|
| BR-01 | The instrument, real dates and macro context of an active session are never disclosed before an explicit reveal | §1.2, §3.1 | BE | 02 | DONE |
| BR-02 | The replay cursor is server-authoritative; a client cannot obtain a bar past it | §3.1 | BE | 03A | DONE |
| BR-03 | No entry order is accepted without a hard stop loss | §3.3 | BE | 04 | PLANNED |
| BR-04 | An order below the account's minimum R:R is rejected, never silently resized | §3.3 | BE | 04 | PLANNED |
| BR-05 | Breaching the account's max daily drawdown halts trading for the session | §3.3 | BE | 04 | PLANNED |
| BR-06 | A reset never overwrites or deletes history; it seals the iteration and forks a child | §3.5 | BE | 01 | DONE |
| BR-07 | Sealed iterations are read-only and provable via a hash chain | §3.5, §6.3 | BE | 01 | DONE |
| BR-08 | A session may be revealed exactly once, and only after it is closed | §3.4 | BE | 05 | PLANNED |
| BR-09 | A rejected order is recorded, not discarded — the discipline index needs the attempts | §3.4 | BE | 04 | PLANNED |
| BR-10 | Replay is deterministic: same feed and seed produce identical fills | §6.3 | BE | 03 | PLANNED |
| BR-11 | Exactly one live iteration per account tree, and one open session per account | §3.5 | BE | 01 / 03A | DONE |

## 2. Functional requirements (FR)

### 2.1 Authentication and access — `FR-AUTH` (PRD §3.6)

| ID | Requirement | Owner | Sprint | Status |
|---|---|---|---|---|
| FR-AUTH-01 | Email + password registration and sign-in, Argon2id hashing | BOTH | 01 | DONE |
| FR-AUTH-02 | Rotating refresh tokens, short-lived HS256 access tokens | BOTH | 01 | DONE |
| FR-AUTH-03 | Google OAuth sign-in | BOTH | 01 | DONE |
| FR-AUTH-04 | Profile read/update and password change | BOTH | 01 | PARTIAL — API and BFF routes done; the account settings screen is not built |
| FR-AUTH-05 | TradingView SSO | BOTH | 07 | PLANNED |
| FR-AUTH-06 | Enterprise SAML 2.0 / Okta for prop desks and academies | BOTH | 07 | PLANNED |
| FR-AUTH-07 | GitHub and Apple OAuth | BOTH | 07 | PLANNED |
| FR-AUTH-08 | Unauthenticated sandbox mode for a trial replay | BOTH | 07 | DEFERRED — needs a rate-limit and abuse story first; tracked, not dropped |

### 2.2 Market data and blinded feeds — `FR-FEED` (PRD §3.1)

| ID | Requirement | Owner | Sprint | Status |
|---|---|---|---|---|
| FR-FEED-01 | Ingest historical OHLCV for FX majors, equity indices, commodities and crypto | BE | 02 | DONE |
| FR-FEED-02 | Randomized slicing across 1,200+ cycles spanning 2008–2025 | BE | 02 | PARTIAL — builder and selection done; the 1,200-cycle archive is a data-loading exercise, not code |
| FR-FEED-03 | Price normalization (scale + offset) so price levels cannot identify the instrument | BE | 02 | DONE |
| FR-FEED-04 | Synthetic alias (`Asset #842 [FX/Crypto Masked]`) in place of the ticker | BE | 02 | DONE |
| FR-FEED-05 | Relative tick offsets (`T-140`, `T-0`) in place of calendar dates | BOTH | 02 | DONE |
| FR-FEED-06 | Feed catalogue exposing alias, asset-class hint, difficulty and bar count — and nothing else | BOTH | 02 | DONE |
| FR-FEED-07 | "Randomize new starting point" picks an unseen feed for the trader | BOTH | 02 | DONE |

### 2.3 Replay engine — `FR-REPLAY` (PRD §3.1)

| ID | Requirement | Owner | Sprint | Status |
|---|---|---|---|---|
| FR-REPLAY-01 | Create a session binding an account to a feed with a deterministic seed | BE | 03A | DONE |
| FR-REPLAY-02 | Step forward and backward one bar (`Spacebar`) | BOTH | 03A/03C | DONE |
| FR-REPLAY-03 | Continuous playback at 0.5x / 1x / 3x / 5x / 10x | BOTH | 03A/03E | Speed control DONE; continuous streaming in 03E |
| FR-REPLAY-04 | Progress indicator (`142 / 500 bars scanned`) | FE | 03C | DONE |
| FR-REPLAY-05 | Websocket streams frames at playback speed, resumable from the cursor | BOTH | 03E | **DONE** — server-driven clock, Redis pub/sub fan-out, single-use ticket auth, latest-wins backpressure |
| FR-REPLAY-06 | Multi-timeframe toggle (1m / 5m / 15m / 1h / 4h / 1D) over the same cursor | BOTH | 03B/03C | DONE |
| FR-REPLAY-07 | Pause, seek, close; idle sessions time out | BOTH | 03A/03C | DONE except the idle sweeper |
| FR-REPLAY-08 | Simulated feed latency surfaced in the UI | BOTH | 03E | **DONE** — `latency_ms` per frame, measured per connection, shown beside the connection state |

### 2.4 Technical analysis ribbon — `FR-TA` (PRD §3.2)

| ID | Requirement | Owner | Sprint | Status |
|---|---|---|---|---|
| FR-TA-01 | Candlestick canvas holding 60 FPS at 10x with overlays | FE | 03D | **DONE** — measured 60.3 FPS / 0 dropped frames at 10x with EMAs + RSI |
| FR-TA-02 | Trendline suite: freehand, horizontal, ray, extended, vertical | FE | 03F | **DONE** |
| FR-TA-03 | Fibonacci retracement (0, .236, .382, .5, .618, .786, 1.0) and trend-based extensions | FE | 03F | **DONE** — decimal levels, tested invariant under the blinding map |
| FR-TA-04 | Order block / supply-demand boxes with adjustable shading | FE | 03F | **DONE** |
| FR-TA-05 | Polyline, brush, annotation note | FE | 03F | **DONE** — brush strokes thinned before they become anchors |
| FR-TA-06 | On-chart interactive long/short brackets showing live target pips, stop risk and R:R | FE | 04 | PLANNED |
| FR-TA-07 | RSI(14) sub-chart with OB/OS thresholds | FE | 03D | **DONE** — Wilder's RSI(14), 30/70 bands, undefined through the warmup rather than faked |
| FR-TA-08 | Dual EMAs (20/50/200) and volume profile | FE | 03D | PARTIAL — EMA 20/50/200 and the volume pane done; the by-price volume *profile* is not built |
| FR-TA-09 | MACD momentum histogram | FE | 06 | PLANNED |
| FR-TA-10 | Scale modes: `LOG`, `AUTO`, `%` | FE | 03D | **DONE** |
| FR-TA-11 | Drawings persist per session and reload with it | BOTH | 05 | PLANNED |

### 2.5 Execution and risk — `FR-EXEC` (PRD §3.3)

| ID | Requirement | Owner | Sprint | Status |
|---|---|---|---|---|
| FR-EXEC-01 | Market, limit and stop orders | BE | 04 | PLANNED |
| FR-EXEC-02 | Sizing by lots or by % equity risk | BOTH | 04 | PLANNED |
| FR-EXEC-03 | Pre-entry gate: hard stop present (BR-03) | BE | 04 | PLANNED |
| FR-EXEC-04 | Pre-entry gate: minimum R:R (BR-04) | BE | 04 | PLANNED |
| FR-EXEC-05 | Pre-entry gate: max daily drawdown (BR-05) | BE | 04 | PLANNED |
| FR-EXEC-06 | Rule-compliance panel reflecting server gate results, including rejections | FE | 04 | PLANNED |
| FR-EXEC-07 | Position actions: set breakeven, close 50%, close all | BOTH | 04 | PLANNED |
| FR-EXEC-08 | Bracket adjustment (move stop / target) on an open position | BOTH | 04 | PLANNED |
| FR-EXEC-09 | Realistic spread and slippage simulation | BE | 04 | PLANNED |
| FR-EXEC-10 | Open positions / pending orders / session trades tables | FE | 04 | PLANNED |
| FR-EXEC-11 | Idempotent order submission (client key) | BOTH | 04 | PLANNED |
| FR-EXEC-12 | Keyboard execution: `B` buy, `S` sell, `Space` step | FE | 04 | PLANNED |

### 2.6 Accounts and reset trees — `FR-ACCT` (PRD §3.5)

| ID | Requirement | Owner | Sprint | Status |
|---|---|---|---|---|
| FR-ACCT-01 | Open a replay portfolio with starting equity, currency and risk policy | BOTH | 01 | DONE |
| FR-ACCT-02 | Reset seals the active iteration and forks a child (BR-06) | BOTH | 01 | DONE |
| FR-ACCT-03 | Read-only sealed iterations with their full history intact | BOTH | 01 | DONE |
| FR-ACCT-04 | Append-only hash-chained ledger with a verification endpoint (BR-07) | BOTH | 01 | DONE |
| FR-ACCT-05 | Branch & reset tree view with per-iteration return and drawdown | FE | 01 | DONE |
| FR-ACCT-06 | Cross-iteration normalized equity-curve overlay | BOTH | 06 | PLANNED |
| FR-ACCT-07 | Fork strategy parameters from a previous iteration | BOTH | 06 | PLANNED |
| FR-ACCT-08 | Meta-learning matrix: per-branch strategy profile, planned risk, max DD, return | FE | 06 | PLANNED |
| FR-ACCT-09 | Quick-fork balance templates ($10k / $50k / $100k) | FE | 06 | PLANNED |

### 2.7 Journal, reveal and audit — `FR-JOURNAL` / `FR-REVEAL` (PRD §3.4)

| ID | Requirement | Owner | Sprint | Status |
|---|---|---|---|---|
| FR-JOURNAL-01 | Per-trade journal entries anchored to the bar being viewed | BOTH | 05 | PLANNED |
| FR-JOURNAL-02 | Emotion, conviction and free tags on an entry | BOTH | 05 | PLANNED |
| FR-JOURNAL-03 | Candle-by-candle trade log with entry/exit, duration, PnL and R | FE | 05 | PLANNED |
| FR-JOURNAL-04 | Session post-mortem free text with prompt chips | FE | 05 | PLANNED |
| FR-JOURNAL-05 | Execution footprint chart marking fills with their R-multiples | FE | 05 | PLANNED |
| FR-JOURNAL-06 | Journal media upload with EXIF stripped and signed URLs | BOTH | 05 | PLANNED |
| FR-REVEAL-01 | Reveal the real ticker, timeframe and date window (BR-08) | BOTH | 05 | PLANNED |
| FR-REVEAL-02 | Macro driver annotation explaining the period | BOTH | 05 | PLANNED |
| FR-REVEAL-03 | Benchmark alpha vs buy-and-hold over the same window | BE | 05 | PLANNED |
| FR-REVEAL-04 | Behavioral Discipline Index 0–100 with its components | BE | 06 | PLANNED |
| FR-REVEAL-05 | Behaviour tagging: FOMO entry, revenge trade, early cut, moved stop | BE | 06 | PLANNED |

### 2.8 Performance analytics — `FR-ANALYTICS` (PRD §4)

| ID | Requirement | Owner | Sprint | Status |
|---|---|---|---|---|
| FR-ANALYTICS-01 | Session metrics: win rate, profit factor, expectancy, max drawdown | BE | 06 | PLANNED |
| FR-ANALYTICS-02 | R-multiple outcome distribution | BOTH | 06 | PLANNED |
| FR-ANALYTICS-03 | Sharpe and Sortino across sessions | BE | 06 | PLANNED |
| FR-ANALYTICS-04 | Streak dispersion / clustering matrix | BOTH | 06 | PLANNED |
| FR-ANALYTICS-05 | Rule adherence grade | BOTH | 06 | PLANNED |
| FR-ANALYTICS-06 | Session alpha by liquidity window (NY / London / Asia) | BOTH | 06 | PLANNED |
| FR-ANALYTICS-07 | Asset-class split, post-reveal only | BOTH | 06 | PLANNED |
| FR-ANALYTICS-08 | Cumulative growth vs drawdown baseline chart | FE | 06 | PLANNED |
| FR-ANALYTICS-09 | CSV export of the quant dataset | BOTH | 06 | PLANNED |

### 2.9 Screens and platform — `FR-UI` (PRD §4, §5)

| ID | Requirement | Owner | Sprint | Status |
|---|---|---|---|---|
| FR-UI-01 | Dark theme `Terminal Precision` as implemented tokens | FE | 01 | DONE |
| FR-UI-02 | Light theme `Institutional Precision Replay` as implemented tokens | FE | 01 | DONE |
| FR-UI-03 | Replay Terminal, desktop, both themes | FE | 03 | PLANNED |
| FR-UI-04 | Trade Journal & Reveal, desktop, both themes | FE | 05 | PLANNED |
| FR-UI-05 | Accounts & Resets, desktop, both themes | FE | 01 | DONE |
| FR-UI-06 | Performance Analytics, desktop, both themes | FE | 06 | PLANNED |
| FR-UI-07 | Login & SSO screen | FE | 01 / 07 | 01 DONE |
| FR-UI-08 | Mobile Replay Terminal (390px) | FE | 03 | PLANNED |
| FR-UI-09 | Mobile Trade & Execution slip | FE | 04 | PLANNED |
| FR-UI-10 | Mobile Journal & Reveal | FE | 05 | PLANNED |
| FR-UI-11 | Mobile Accounts & Resets with branch switcher | FE | 06 | PLANNED |
| FR-UI-12 | Touch gestures: pinch-zoom, drag crosshair, tap-to-step | FE | 03 | PLANNED |

## 3. Non-functional requirements (NFR)

| ID | Requirement | PRD | Owner | Sprint | How it is proven |
|---|---|---|---|---|---|
| NFR-01 | Simulated tick latency under 15ms, displayed in the UI | §6.1 | BOTH | 03E | PARTIAL — measured 4–6ms at the socket against a live stack and displayed in the terminal; the p99 histogram under load is still owed |
| NFR-02 | Candlestick canvas sustains 60 FPS at 10x with overlays | §6.2 | FE | 03D | **DONE** — measured in-browser at 10x: 60.3 FPS over 182 frames, 0 dropped, gap p99 16.80ms; draw cost 4.4ms p99 per the frontend's `docs/adr/0001-chart-rendering.md` |
| NFR-03 | Deterministic session hashes over every action and fill | §6.3 | BE | 03 | Property test: same feed + seed ⇒ identical fills and hash |
| NFR-04 | Full touch gestures on mobile web / PWA | §6.4 | FE | 03 | Playwright mobile project |
| NFR-05 | Zero hindsight leakage in any pre-reveal payload | §1.2, §3.1 | BE | 02 | **DONE** — payload + struct guards in `entities/response/feed/leak_test.go`, verified non-vacuous |
| NFR-06 | Events are durable and at-least-once; a broker outage loses nothing | — | BE | 00 | Outbox rows stay pending through an outage; verified locally | 
| NFR-07 | Immutable, verifiable history | §6.3 | BE | 01 | Chain recomputation; tamper test against a row altered in-database |
| NFR-08 | Decimal precision preserved end to end for price, size and balance | — | BOTH | 01 | ESLint ban on float conversion in features; decimal.js and Go decimal throughout |

---

## 4. Sprint map

| Sprint | Theme | Requirements | Status |
|---|---|---|---|
| 00 | Foundation and infrastructure | NFR-06, NFR-08 | DONE |
| 01 | Identity, accounts and reset trees | FR-AUTH-01..04, FR-ACCT-01..05, FR-UI-01/02/05/07, BR-06/07/11, NFR-07 | DONE except the account settings screen (FR-AUTH-04) |
| 02 | Market data and blinded feeds | FR-FEED-01..07, BR-01, NFR-05 | DONE — the 1,200-cycle archive (FR-FEED-02) is data loading, not code |
| 03 | Replay session engine and terminal | FR-REPLAY-01..08, FR-TA-01..05/07/08/10, FR-UI-03/08/12, BR-02/10, NFR-01..04 | DONE — 03A–03F all delivered; NFR-01's p99-under-load histogram is the one item still owed |
| 04 | Execution and the risk gate | FR-EXEC-01..12, FR-TA-06, FR-UI-09, BR-03/04/05/09 | PLANNED |
| 05 | Journal, drawings and the mystery reveal | FR-JOURNAL-01..06, FR-REVEAL-01..03, FR-TA-11, FR-UI-04/10, BR-08 | PLANNED |
| 06 | Analytics, discipline index and cross-iteration | FR-ANALYTICS-01..09, FR-REVEAL-04/05, FR-ACCT-06..09, FR-TA-09, FR-UI-06/11 | PLANNED |
| 07 | Institutional access and hardening | FR-AUTH-05..08 | PLANNED |

## 5. Deferred from v1

Recorded rather than dropped, each with the reason:

| ID | Requirement | Why deferred |
|---|---|---|
| FR-AUTH-08 | Unauthenticated sandbox mode | Needs an abuse and rate-limiting story before an unauthenticated endpoint can create sessions |
| — | Prop Firm Challenge Simulator (PRD §7 Phase 3) | Depends on the whole gate and analytics stack; the risk policy on `accounts` is already shaped for it |
| — | Trader Edge Sharing Cards (PRD §7 Phase 4) | Depends on the reveal being complete; sharing a pre-reveal card would leak the answer |
