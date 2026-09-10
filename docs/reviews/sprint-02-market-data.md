# Review — Sprint 02: Market data and blinded feeds

> **Update:** `BE-02-1` and `BE-02-2` were fixed after this review, and verified end to end — a
> Binance-shaped millisecond CSV now lands in November 2023 rather than the year 55,840, and a
> shuffled one is refused with zero rows written. Sprint 08 §08.1 no longer blocks on them.

**Reviewed:** `services/blindpulse/v1/domain/feed`, `entities/domain/feed`, `entities/response/feed`,
`cmd/loader`, `pkg/stats`, and the frontend's feed catalogue.
**Against:** `docs/requirements/sprint-02-market-data-feeds.md`, register rows FR-FEED-01..07,
BR-01, NFR-05.

## Verdict

**The blinding is the best-defended thing in the codebase, and the ingest path that feeds it is the
weakest.** That asymmetry is the finding. Sprint 02 owns the product's central promise, and the
domain that implements it is careful, tested and guarded from two directions. But everything
upstream — the CLI that puts data in the table — is a first draft that its own sprint document
describes more rigorously than the code implements.

None of it has bitten yet because two feeds were loaded by hand from a known-good file. All three
ingest findings below bite at volume, which is exactly what **Sprint 08** is about to do.

| | ID | Finding |
|---|---|---|
| ~~High~~ **FIXED** | `BE-02-1` | ~~Millisecond epochs parse as seconds~~ — the unit is now detected by magnitude and an implausible value is refused rather than guessed at |
| ~~High~~ **FIXED** | `BE-02-2` | ~~`ValidateSeries` is never called~~ — the loader now refuses the whole file on any structural problem, before insert |
| ~~Medium~~ **FIXED** | `BE-02-3` | ~~Tick size and quote currency hardcoded for every asset class~~ — derived from asset class and symbol via `market.DeriveConventions`, overridable per run |
| ~~Medium~~ **FIXED** | `BE-02-4` | ~~`pkg/stats` has no tests~~ — property tests over volatility, trend persistence, range draws and index uniformity |
| ~~Low~~ **FIXED** | `BE-02-5` | ~~The sprint doc still reads `Status: PLANNED`~~ — corrected, along with sprint 03 which had drifted the same way |

## What holds up

**The leak guards are real, and they were proven real.** `entities/response/feed/leak_test.go`
asserts over the *serialized payload* rather than a field list, and pairs that with a reflection
walk that bans identity-shaped field names and any `time.Time` anywhere in the package. Both halves
were verified by adding `window_start` to `Detail` and confirming both fail — a guard nobody has
tried to defeat is a guard nobody knows works.

**The affine normalization is the right choice, for stated reasons.** `displayed = (real + offset)
× scale` preserves the ratios that fib levels, trendlines and R:R depend on, and deliberately breaks
percentage returns — which are the fingerprint. The trade-off is written down where the code is,
not assumed.

**The one structural safety property is in the right place.** `ViewBars` slices `real[:upto+1]`
*before* aggregating (`domain/feed/service.go:291`), so the roll-up physically cannot see a bar past
the cursor. The comment says so: "the slice is the enforcement". Compare that with checking the
result afterwards, which is the version that eventually leaks.

**Bucket alignment is to the UTC epoch**, so a 1h bar opens on the hour regardless of where the
input starts. Getting this wrong would offset every higher timeframe from every chart the trader has
ever seen, and it is the kind of thing that is invisible until someone compares against a real
broker.

---

## Findings

### `BE-02-1` · High · A millisecond timestamp is silently read as seconds

**Where:** `cmd/loader/main.go:211` (`parseTime`).

```go
if seconds, err := strconv.ParseInt(trimmed, 10, 64); err == nil {
    return time.Unix(seconds, 0).UTC(), nil
}
```

Any bare integer is treated as unix **seconds**. A millisecond epoch — `1700000000000`, which is
November 2023 — parses successfully as `time.Unix(1700000000000, 0)`, or **the year 55,840**. There
is no error, no rejection, and no downstream check: `Bar.Valid()` inspects OHLC relationships only,
and `ValidateSeries`, which does check ordering, is never called (`BE-02-2`).

**Why it matters now.** Binance — the single best free intraday crypto source, and the one Sprint 08
plans to build its bulk path on — emits milliseconds everywhere, in both the REST API and the CSV
dumps. Loading it today produces a table full of bars in the year 55,840 and a feed builder that
finds nothing in the requested window. The failure is silent at ingest and confusing much later.

**Fix.** Detect the unit by magnitude and **reject anything outside a sane window** rather than
storing it: a value that is neither plausible seconds nor plausible milliseconds is a column-order
mistake, and guessing is worse than failing.

### `BE-02-2` · High · The series validator is never called

**Where:** `services/blindpulse/v1/domain/feed/aggregate.go:102` (`ValidateSeries`), called only from
`aggregate_test.go:100`.

`ValidateSeries` checks OHLC consistency, non-negative prices and strictly increasing timestamps. It
is correct and unit-tested. No production path invokes it.

What the loader actually does is `bar.Valid()` per row — OHLC relationships only. So a CSV with
duplicate rows, out-of-order rows, or a gap loads without complaint. That matters more than it looks:
`Aggregate` assumes sorted input, so **an unsorted CSV yields silently wrong higher timeframes** —
a 1h candle whose open came from the wrong minute.

Sprint 02's own document (§02.1) specifies the missing behaviour precisely: "no duplicate or
non-monotonic timestamps, no gaps larger than the timeframe outside known market closures". The spec
is right; the code is a subset of it.

**Fix.** Call `ValidateSeries` on the parsed batch before insert and refuse the file on any
structural problem. Gap classification (the "outside known market closures" half) needs a session
calendar and is scoped into Sprint 08 §08.3.

### `BE-02-3` · Medium · Instrument metadata is the same for every asset class

**Where:** `cmd/loader/main.go:74-75`.

```go
AssetClass: market.AssetClass(*class), QuoteCurrency: "USD",
TickSize: decimal.RequireFromString("0.00001"), ContractSize: decimal.NewFromInt(1),
```

`-class` is honoured; tick size and quote currency are not. Every instrument gets a five-decimal
tick and a USD quote. That is right for EURUSD and wrong for USDJPY (0.001), an equity (0.01), an
index, and BTC.

**Why it matters.** Nothing reads `TickSize` today, which is why this has been harmless. Sprint 04's
order gate is the first consumer — stop distance, position size and R:R all quantize to it — so this
becomes a correctness bug in the *next* sprint, in a place where the wrong answer looks plausible.

**Fix.** Derive defaults from asset class and the symbol's quote currency, and let both be
overridden per-run. Do it before Sprint 04 rather than during it.

### `BE-02-4` · Medium · The difficulty kernel is untested

**Where:** `pkg/stats/stats.go` — no test file.

`pkg/stats` exists for a good reason: `archlint` refuses `float64` under `/domain/`, so the numeric
kernel was moved out rather than the rule weakened. But what lives there is `Volatility`,
`TrendPersistence` and `Index` — the functions that decide a feed's **difficulty band**, which is
what the trader sees in the catalogue and what Sprint 08's stratified sampler will draw against.

An untested numeric kernel whose output is a user-facing label, and shortly a sampling weight, is a
gap worth closing cheaply. Property tests are natural here: volatility of a constant series is zero,
trend persistence of a monotonic series is 1, `Index` is uniform over its range.

### `BE-02-5` · Low · The sprint document still says PLANNED

**Where:** `docs/requirements/sprint-02-market-data-feeds.md:3`.

The register says DONE; the sprint doc header said `Status: PLANNED`. A sweep during this review
found **sprint-03 had drifted the same way** — delivered across six slices and still labelled
PLANNED. Both are corrected, and every sprint doc now carries a link to its review. The lesson is
that the register and the sprint docs are two places recording the same fact, and only one of them
was being maintained.

---

## Notes carried to Sprint 08

`BE-02-1`, `BE-02-2` and `BE-02-3` are scoped into `sprint-08-market-data-archive.md` §08.1 as
blocking work, because the archive build is exactly the operation that turns all three from
theoretical into a corrupted table.

## Not checked

The correctness of the loaded data against a second source (network policy blocks every market-data
host), and the feed builder's behaviour at archive scale — there are two feeds, so nothing here has
been exercised at the volume that would expose an O(n²) selection or a memory ceiling.
