# Sprint 08 — Market data archive and ingestion pipeline (backend)

**Status:** PLANNED · **Estimate:** 10–12 dev-days
**Requirements:** FR-FEED-01 (hardening), **FR-FEED-02 (the outstanding half)**, BR-01, NFR-05
**PRD:** §1.2, §3.1
**Depends on:** Sprint 02 (the blinding domain already exists) · **Blocks:** nothing in 04–07
**Companion document:** [`docs/data-sources.md`](../data-sources.md)

## Why this sprint exists

The product works. It has two feeds.

FR-FEED-02 asks for "randomized slicing across 1,200+ cycles spanning 2008–2025", and the register
has carried it as PARTIAL since Sprint 02 with the note "the builder and selection are done; the
1,200-cycle archive is a data-loading exercise, not code". That note was true and is now the
binding constraint: `FR-FEED-07` picks an *unseen* feed for the trader, and with two feeds in the
catalogue a trader exhausts the product in two sessions. Every downstream sprint — the journal, the
reveal, cross-iteration analytics — is measuring behaviour across a sample size of two.

It is also not purely a data-loading exercise any more. Reviewing Sprints 01–04 turned up three
defects in the ingest path that only bite at volume, and one of them silently corrupts data from
the single best free crypto source. Those are in scope here.

## Sequencing

**This sprint is parallelisable with 04–06 and blocks none of them.** It touches `cmd/loader`, a
new `cmd/ingest`, and the feed builder — no API surface the terminal depends on, and no frontend
work at all. Two reasonable orderings:

- **Before Sprint 04** if the priority is having a product worth demonstrating. Execution against
  two feeds demonstrates the order gate; execution against twelve hundred demonstrates the product.
- **Alongside Sprint 04** if there is a second pair of hands. The overlap is zero.

What it must not be is "after Sprint 07", because analytics built and tuned against two feeds will
be tuned against noise.

## Goal

Turn ingestion from a single-file CLI into a pipeline that can build and maintain an archive of
1,200+ blinded windows, and make the ingest path trustworthy enough that nobody has to spot-check
what it loaded.

## Tasks

### 08.1 Fix the ingest defects found in review (blocking everything else)

These are carried from `docs/reviews/sprint-02-market-data.md`; nothing else in this sprint is safe
until they are done.

- **`BE-02-1` — millisecond epochs are parsed as seconds.** `parseTime` tries `ParseInt` first and
  treats any bare integer as unix *seconds*, so `1700000000000` becomes a date in the year 55,840
  rather than November 2023. Binance — the source Sprint 08 leans on hardest — emits milliseconds
  everywhere. Detect the unit by magnitude, and **reject** anything that lands outside a sane
  window (say 1990–2100) rather than storing it.
- **`BE-02-2` — `ValidateSeries` is never called.** It exists, it is correct, it is unit-tested, and
  no production path invokes it. The loader checks OHLC consistency per row and nothing about the
  series: duplicates, non-monotonic timestamps and gaps all load silently. `Aggregate` assumes
  sorted input, so an out-of-order CSV yields quietly wrong higher timeframes.
- **`BE-02-3` — instrument metadata is hardcoded.** Every instrument is upserted with
  `TickSize 0.00001` and `QuoteCurrency "USD"` regardless of asset class. That is right for EURUSD,
  wrong for USDJPY (0.001), an equity (0.01) and BTC. It has not mattered yet because nothing reads
  tick size; Sprint 04's order gate will.

### 08.2 Provider adapters — `cmd/ingest`

A fetch layer in front of the existing loader, one adapter per source, sharing one interface:

```go
type Source interface {
    // Fetch writes normalized 1m bars for the window to the channel. Bulk sources stream a file;
    // paginated ones page. The caller does not care which.
    Fetch(ctx context.Context, req FetchRequest, out chan<- market.Bar) error
    Describe() SourceInfo   // name, licence, granularity, the depth it actually has
}
```

- **`binance-dump`** — the bulk path. Monthly ZIPs from the public data host, no key, 2017→present.
  This is the least-friction source available and should be the one the pipeline is proven on.
- **`histdata`** — monthly M1 FX ZIPs, ~2000→present. The only free source that spans the PRD's
  full date range, so it carries the 2008–2015 half of the archive alone.
- **`stooq`** — daily CSV, no key. For breadth of instrument, not depth of granularity.
- Adapters are **resumable and rate-aware**: a fetch that dies at month 140 of 200 restarts at 140.
  Progress belongs in a small `ingest_runs` table, not in a log file somebody has to read.

### 08.3 Validation gate

Ingest already rejects a bad *row*. This adds rejection of a bad *series*, before it can become a
feed:

- Run `ValidateSeries` (see 08.1) and refuse the file on any structural problem.
- **Gap classification.** A hole in the data is either a market closure (a weekend in FX, an
  exchange holiday, the CME maintenance break) or missing data. The first is normal and must be
  preserved; the second must fail the load. Classify against a per-asset-class session calendar
  rather than a fixed threshold, because "no bars for 48 hours" is routine in FX and alarming in
  crypto.
- **Split and dividend artefacts.** For equities, an unadjusted series has a step change at every
  corporate action. The affine blinding preserves ratios exactly, so that step survives
  normalization and reaches the trader looking like a genuine gap-down — the product would be
  teaching something untrue. Either ingest adjusted series or detect and reject the step.
- Outlier detection: a bar whose range is many multiples of trailing volatility is a bad print far
  more often than it is a real event. Flag rather than delete, and record the decision.

### 08.4 Window selection at scale — the archive builder

Today `cmd/loader -build` makes one feed from explicit flags. This makes twelve hundred, and the
interesting question is *which* twelve hundred.

- **Stratified sampling, not uniform.** Windows are drawn to fill a grid across asset class,
  volatility band, trend persistence and year. Uniform random sampling over 2008–2025 would produce
  an archive that is mostly quiet ranging markets, because most markets are mostly quiet — and a
  trader who never meets a crisis has not been tested.
- **Overlap policy.** Two windows from the same instrument sharing 90% of their bars are, for
  training purposes, one window. Enforce a minimum stride between windows on the same instrument.
- **Difficulty is measured, not assigned.** `difficultyFor` already derives it from realized
  volatility and trend persistence; the builder records the inputs alongside the verdict so the
  banding can be re-tuned later without re-deriving every feed.
- **Macro labels from FRED**, not by hand. A window overlapping a recession date, a policy meeting
  or a CPI print gets the label attached at build time — withheld until the reveal, as ever.

### 08.5 Archive operations

- `cmd/ingest plan` — prints what would be fetched and built, and from where, without doing it.
- `cmd/ingest apply` — does it, resumably, with progress.
- `cmd/ingest verify` — re-runs the validation gate over what is already stored and reports drift.
- A `data_sources` table recording, per instrument-window, **which provider it came from and when it
  was fetched**. Provenance matters the first time two sources disagree about a print.

### 08.6 Leak audit at volume

NFR-05 is currently proven by unit guards over one hand-built fixture. With 1,200 real feeds it can
be proven over the corpus: sample the catalogue and detail payloads for every published feed and
assert the same properties. A leak that only appears for, say, feeds whose macro label is set is
exactly the kind of thing a single fixture misses.

## Data model additions

- `ingest_runs` — source, instrument, requested window, cursor, status, error. Resumability.
- `data_sources` — provenance per instrument-window: provider, fetched-at, licence tag.
- `blinded_feeds.selection_inputs JSONB` — the volatility and persistence measurements behind the
  difficulty verdict, so banding can be re-tuned without a rebuild.
- `market_bars` may need partitioning by instrument before this lands. 1,200 windows × 800 bars is
  small; the *underlying* 1m series behind them is not — roughly 5M rows per instrument-year of FX.

## Acceptance criteria

| # | Given | When | Then |
|---|---|---|---|
| 08-AC-1 | A Binance CSV with millisecond timestamps | Ingested | Bars land in the correct year, or the load fails loudly — never a year-55,840 row |
| 08-AC-2 | A CSV with rows out of order | Ingested | Refused with the offending row named; nothing is written |
| 08-AC-3 | An FX series spanning a weekend | Ingested | Loads; the closure is preserved, not filled |
| 08-AC-4 | A crypto series with a 6-hour hole | Ingested | Refused — crypto does not close |
| 08-AC-5 | An interrupted 200-month backfill | Restarted | Resumes at the last completed month, and re-running a completed month is a no-op |
| 08-AC-6 | The built archive | Inspected | ≥ 1,200 published feeds; every difficulty band and every year 2008–2025 populated above a floor |
| 08-AC-7 | Two windows from one instrument | Compared | Overlap below the configured stride |
| 08-AC-8 | Every published feed | Payload sampled | Zero identity leakage (NFR-05) across the whole corpus, not one fixture |
| 08-AC-9 | An unadjusted equity series across a split | Ingested | Refused or adjusted; never blinded with the step intact |

## Test plan

- **Unit:** timestamp-unit detection (the seconds/milliseconds boundary and both sides of the sanity
  window), gap classification per asset class, stratified sampler distribution.
- **Integration:** a fixture month per adapter, ingested against a real PostgreSQL, asserting bar
  counts and boundaries.
- **Corpus:** the NFR-05 sweep over every published feed.
- **Non-vacuity:** each new guard is verified by feeding it the exact defect it exists to catch —
  the practice the earlier sprints established, and the reason the leak guards are trustworthy.

## Definition of done

A single command builds the archive from nothing, resumably; the catalogue holds 1,200+ feeds
spanning 2008–2025 with every difficulty band populated; the validation gate refuses each defect in
08-AC-1..5 and 08-AC-9; and NFR-05 is proven over the corpus rather than over a fixture.

## Risks

| Risk | Mitigation |
|---|---|
| **Free equity intraday does not reach 2008.** Nothing free spans it — see `data-sources.md` §4. | Ship FX (HistData, 2000→) and crypto (Binance, 2017→) first; make the equity decision — daily bars, or a paid vendor — with the archive already useful. Do not let it block the sprint. |
| A provider changes or withdraws its free tier mid-build | Provenance in `data_sources` plus resumable runs means re-fetching a subset from a second source is a normal operation, not a rebuild. |
| Undocumented endpoints (HistData's form POST, Dukascopy's file scheme) break | Both are bulk file fetches; pin the fixtures in tests so a change fails CI rather than a 3am archive job. |
| Ingest volume dwarfs the current schema | Partition `market_bars` before the first full run, not after. |
| Blinding a corporate-action artefact teaches something untrue | 08.3 refuses it; 08-AC-9 proves the refusal. |
