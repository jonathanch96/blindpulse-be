# Sprint 02 — Market data and blinded feeds (backend)

**Status:** DONE — the 1,200-cycle archive and three ingest defects move to Sprint 08 · **Estimate:** 8–10 dev-days · **Reviewed:** [docs/reviews/sprint-02-market-data.md](../reviews/sprint-02-market-data.md)
**Requirements:** FR-FEED-01..07, BR-01, NFR-05
**PRD:** §1.2, §3.1
**Depends on:** Sprint 01 (auth) · **Blocks:** Sprint 03 (the engine has nothing to replay without it)

## Goal

Turn raw historical OHLCV into **blinded feeds**: an alias, a window, and a normalization that
makes the series unrecognizable. This sprint owns the product's central promise (BR-01), and it is
the one place where a single leaked field destroys it — a trader who can identify the instrument
has the hindsight the whole product exists to remove.

## Tasks

### 02.1 Ingestion — `cmd/loader` (FR-FEED-01)
- CLI reading CSV/parquet into `blindpulse.market_bars`, batched, resumable.
- Idempotent on the `(instrument_id, timeframe, opened_at)` primary key: re-running a file is a
  no-op, so a partial load is fixed by re-running rather than by cleaning up first.
- Validation on ingest, rejecting the row rather than storing it: `high >= max(open, close)`,
  `low <= min(open, close)`, non-negative volume, no duplicate or non-monotonic timestamps, no gaps
  larger than the timeframe outside known market closures.
- Instrument upsert by symbol with asset class, venue, tick size and contract size.
- Sources for v1: G10 FX majors, equity indices (NQ/ES/DAX), crypto (BTC/ETH), gold and oil.

### 02.2 Higher timeframes (FR-REPLAY-06 support)
- Derive 5m/15m/1h/4h/1D from the 1m base at load time rather than at query time. Aggregating on
  read would put the cost inside the replay hot path, where the 15ms budget lives.
- Boundary rule documented and tested: bars align to UTC midnight, a partial trailing bar is
  excluded from the window.

### 02.3 Feed builder (FR-FEED-02..04, BR-01)
- Selects an instrument, a timeframe and a contiguous window with `warmup_bars` of lookback plus
  at least `REPLAY_BAR_WINDOW_SIZE` tradeable bars; refuses shorter windows with
  `FEED_WINDOW_TOO_SHORT`.
- **Alias**: `Asset #NNN` from a counter, never derived from the symbol — a hash of the ticker is
  reversible by anyone with a list of tickers.
- **Price normalization**: an affine map, `displayed = (real + price_offset) × price_scale`, with
  the scale drawn so the resulting magnitude does not itself identify the asset class (EUR/USD at
  1.08 and BTC at 60,000 must not be separable by magnitude alone).

  What an affine map preserves *exactly*, because every one of these is linear in price:
  fibonacci levels, trendlines, support/resistance geometry, EMAs, RSI (it reads differences), and
  risk-to-reward (a ratio of price distances, so the offset cancels and the scale divides out).

  What it deliberately does **not** preserve: percentage returns. That is the point — an identical
  percentage-return series is a fingerprint that can be matched against a database of real assets,
  so preserving it would leave the feed identifiable by anyone willing to run the comparison. The
  cost is that `LOG` scale mode is meaningful only within the normalized series, which is the only
  series the client ever sees.
- **Volume normalization**: rebased to a relative index for the same reason.
- **Date masking**: the API emits `bar_index` and relative offsets only. No absolute timestamp
  appears in any pre-reveal payload — not in a field, not in an id, not in an ETag.
- Difficulty classification from realized volatility and trend persistence: `calm`, `standard`,
  `volatile`, `crisis`.
- Macro label recorded at build time and withheld until the reveal.

### 02.4 Feed catalogue API (FR-FEED-06, FR-FEED-07)
```
GET  /feeds?difficulty=&timeframe_class=&unseen=true   → alias, timeframe class, difficulty, bar count
GET  /feeds/{id}                                        → the same, plus warmup and window length
POST /feeds/random                                      → picks a feed the caller has not traded
```
The response body carries **no** `instrument_id`, symbol, asset class beyond a coarse masked hint,
or any absolute date. This is enforced by a dedicated response type with no field that could carry
one — not by remembering to omit fields on a shared struct.

### 02.5 Caching
- `blindpulse:feed:{id}:meta` and decoded bar windows in Redis, PostgreSQL as the source.
- Cache key includes the normalization parameters, so a rebuilt feed can never serve stale prices.

### 02.6 Adapter integration tests (carried from Sprint 01)
Testcontainers-backed round-trip tests for every table the domain depends on byte-for-byte,
starting with `account_ledger_entries` — the class of bug the `jsonb`/nanosecond issue was.

## API contract

| Method | Path | Auth | Notes |
|---|---|---|---|
| `GET` | `/feeds` | Bearer | Paginated catalogue, blinded |
| `GET` | `/feeds/{id}` | Bearer | Blinded detail |
| `POST` | `/feeds/random` | Bearer | Excludes feeds already traded by this user |

New error codes: `FEED_NOT_FOUND`, `FEED_WINDOW_TOO_SHORT`, `INSTRUMENT_NOT_FOUND`,
`INVALID_TIMEFRAME` (all already in the catalog).

## Data model

Tables exist from Sprint 01's migrations (`instruments`, `market_bars`, `blinded_feeds`).
Additions this sprint:

- `blinded_feeds.volume_scale NUMERIC` — volume rebasing factor.
- `blinded_feeds.built_at TIMESTAMPTZ` and `builder_version INT` — so a normalization change can be
  identified and feeds rebuilt deliberately rather than silently.
- Index on `(is_published, difficulty, base_timeframe)` for catalogue filtering.

## Acceptance criteria

| # | Given | When | Then |
|---|---|---|---|
| 02-AC-1 | A CSV of 1m bars | Loaded twice | Row count is identical; no duplicates |
| 02-AC-2 | A bar with `high < low` | Loaded | Row is rejected and named in the error, load continues |
| 02-AC-3 | A published feed | `GET /feeds/{id}` | No response field contains the symbol, an absolute date, or the instrument id — asserted over the serialized JSON, not the struct |
| 02-AC-4 | A normalized series | Fib levels and R:R computed on it | Identical to the same computation on real prices, mapped through the transform (affine invariance) |
| 02-AC-5 | Two feeds from different asset classes | Compared | Price magnitudes overlap; class is not inferable from magnitude |
| 02-AC-6 | A window shorter than the minimum | Feed build | `FEED_WINDOW_TOO_SHORT`, no row written |
| 02-AC-7 | A user who has traded feed A | `POST /feeds/random` ×20 | Feed A is never returned |
| 02-AC-8 | 1m bars | Aggregated to 1h | Bar count, OHLC and volume match a reference computation |

## Test plan
- **Domain:** normalization maths (ratio preservation), difficulty classification, window selection.
- **Integration:** loader idempotency and validation against a real database.
- **Contract:** a leak test per endpoint that serializes the response and greps for every symbol in
  the instruments table and for any ISO-8601 date. This is the NFR-05 guard and it runs in CI.
- **Property:** for random windows, normalized-then-denormalized prices round-trip within tolerance,
  and percentage returns of the normalized series differ from the real ones — asserting the
  fingerprint is actually broken, not merely assumed to be.

## Definition of done
All criteria met, leak test in CI, at least 200 feeds built across four asset classes, and the
frontend can render the catalogue against the real API.

## Risks

| Risk | Mitigation |
|---|---|
| A leak through an unexpected field (error message, header, id) | The leak test greps the whole serialized response and headers, not a field list |
| Normalization that preserves a recognizable price level | Scale drawn per feed and asserted against a magnitude-overlap test (02-AC-5) |
| Data licensing for redistributable history | Confirm terms before ingest; the loader is source-agnostic so the set can be swapped |
