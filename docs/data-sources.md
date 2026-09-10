# Free market data sources

What BlindPulse needs is unusual: **intraday bars (1m or 15m), deep history (the PRD asks for
2008–2025), across several asset classes**. Daily bars are abundant and free; intraday going back
fifteen years mostly is not. This document is the survey behind Sprint 08, and it is honest about
where the free tier runs out.

> **Verification status.** The endpoints and limits below could not be checked from the development
> sandbox — its egress policy denies every market-data host (only GitHub is reachable), so each
> probe returned a 403 at CONNECT. Everything here is written from prior knowledge and should be
> confirmed against the provider before you rely on it. **Free-tier quotas and licence terms are the
> most volatile facts on this page** and change without notice; treat the specific numbers as
> "roughly this, verify before building against it".

---

## 1. The short answer

| Asset class | Best free source for our purpose | Intraday depth | Verdict |
|---|---|---|---|
| **FX** | HistData.com M1 CSVs | 1-minute, ~2000 → present | **Covers the whole PRD range.** Start here. |
| **Crypto** | Binance public data dumps | 1-minute, 2017 → present | **Excellent**, but history starts 2017. |
| **Equities / ETFs** | Stooq (daily), Alpha Vantage / Twelve Data (recent intraday) | daily deep; intraday ~2 years | **Free tier does not reach 2008 intraday.** |
| **Indices** | Stooq | daily deep | Same limitation as equities. |
| **Futures** | Nothing reliable and free at intraday depth | — | Paid vendor, or drop from v1. |
| **Macro labels** | FRED API | n/a (event dates) | Good for the post-reveal `macro_label`. |

**The consequence for the PRD.** FR-FEED-02 asks for 1,200+ cycles spanning 2008–2025. That is
reachable **for FX and (from 2017) crypto** on free data alone. It is *not* reachable for equity or
futures intraday without paying. Sprint 08 therefore proposes shipping the archive as FX + crypto
first and treating equities as a daily-bar or paid-vendor decision — see the sprint doc.

---

## 2. Foreign exchange

### HistData.com — the workhorse
- **Format:** ZIP of ASCII CSV, one month per file, per pair.
- **Granularity:** M1 (1-minute OHLC) and tick-with-bid/ask.
- **Depth:** roughly 2000 → present for the majors.
- **Access:** free download, no account. Bulk fetching is a form POST per month rather than a clean
  REST API, so the adapter is a scraper-shaped thing, not an API client.
- **Volume caveat:** FX has no consolidated tape, so the "volume" column is tick count, not traded
  size. Fine for us — it is normalized and blinded anyway — but it is not real volume.
- **Licence:** free for personal use; redistribution is restricted. **We ingest and blind it, we do
  not republish it**, which is the right side of that line, but confirm before any public dataset.

### Dukascopy — tick depth
- **Format:** `.bi5` (LZMA-compressed binary), one file per hour per instrument.
- **Depth:** ~2003 → present, tick level, genuinely high quality.
- **Access:** direct HTTP from their datafeed host; the URL scheme is stable but undocumented, so
  treat it as an unofficial interface that can move.
- **Cost of use:** you must decode bi5 and aggregate to bars yourself. Worth it only if tick-level
  fidelity matters; for 15m replay bars, HistData is far less work.

### TrueFX
- Free tick data with registration; monthly archives per pair. Narrower instrument coverage.

---

## 3. Crypto — the easiest free data anywhere

### Binance public data (`data.binance.vision`)
- **Format:** ZIP of CSV, published per symbol / per interval / per month and per day.
- **Granularity:** 1s through 1M — 1m is what we want.
- **Depth:** from each symbol's listing; BTCUSDT from 2017.
- **Access:** plain HTTPS file download, **no API key, no rate limit worth worrying about**. This is
  by far the least friction of any source here, and the reason Sprint 08 starts its bulk path with it.
- **Gotcha that matters to us:** timestamps are **milliseconds**, and our loader currently parses a
  bare integer as *seconds* — see the review finding `BE-02-1`. Loading Binance data today would
  silently date every bar to roughly the year 55,840.

### Binance REST (`/api/v3/klines`)
- ~1000 candles per request, weight-based rate limiting. Fine for incremental top-ups, wrong for
  backfilling years — use the dumps for that.

### Kraken
- Publishes downloadable OHLCVT CSV archives (updated periodically) and a public `OHLC` REST
  endpoint whose history is short. Useful as a **second venue** for cross-checking a suspicious
  Binance series.

### Coinbase Exchange
- Public `/products/{id}/candles`, 300 candles per request, a fixed set of granularities. Good
  coverage of USD pairs; slower to backfill than Binance dumps.

### CryptoDataDownload
- Free per-exchange, per-pair CSVs at daily/hourly/minute. Convenient, but it is a re-publisher —
  prefer the exchange's own dump where one exists, so there is one less party between us and the
  print.

---

## 4. Equities, ETFs and indices

### Stooq — best free no-key option
- **Access:** `https://stooq.com/q/d/l/?s=<symbol>&i=d` returns CSV directly. `i=d|w|m` for
  daily/weekly/monthly; intraday is limited.
- **Coverage:** wide — US and international equities, indices (`^spx`, `^ndx`), FX, commodities.
- **Depth:** daily, deep.
- **Limits:** informal per-IP throttling. No documented SLA. Free, no key.

### Alpha Vantage
- **Access:** REST + free API key. JSON or CSV (`datatype=csv`).
- **Useful endpoints:** `TIME_SERIES_INTRADAY` (1/5/15/30/60min, with a `month=` parameter for
  historical slices), `TIME_SERIES_DAILY_ADJUSTED`.
- **Limits:** the free quota has been **cut repeatedly** — it has been 5/min + 500/day in the past
  and much lower more recently. Verify the current number before planning a backfill around it.

### Twelve Data
- Free tier with a daily request budget; clean REST, decent intraday. Same warning about quotas.

### Tiingo
- Free tier with registration: EOD deep, IEX-sourced intraday with shorter history. Explicit,
  readable licence terms — a point in its favour.

### Polygon.io
- Free tier: heavily rate-limited, ~2 years of history. Their **paid** tier is one of the more
  reasonable routes to deep equity intraday if we decide to buy.

### Yahoo Finance (via `yfinance` and similar)
- Widely used, effectively free, **and not a supported API.** It is an undocumented endpoint behind
  a scraping library, and Yahoo's terms do not permit this use. Convenient for a throwaway
  experiment; **not something to build an archive on**, and this document does not recommend it.

### The honest limitation
Free equity **intraday** history is roughly two years everywhere. Nothing on this list gives 15-minute
SPY bars from 2008. That is a real constraint on FR-FEED-02, not an oversight.

---

## 5. Futures and commodities

There is no good free intraday source. Nasdaq Data Link's continuous-futures collections were the
historical answer and are largely frozen or withdrawn. Options are a paid vendor (Databento,
FirstRate Data, CQG) or leaving futures out of v1. Given the PRD lists futures as an asset-class
*hint* rather than a hard requirement, leaving them out is defensible.

---

## 6. Macro context for the reveal

### FRED (St. Louis Fed)
- Free API key, generous limits, excellent documentation, and an unambiguous licence.
- Not price data — this is where the **`macro_label`** the reveal shows comes from: recession dates,
  policy-rate decisions, CPI and NFP release dates. Pairing a feed window against FRED release dates
  is how "SVB Contagion" gets attached to a window automatically rather than by hand.

---

## 7. Choosing between them

The questions worth asking about any source, in the order they usually bite:

1. **Does it reach back far enough at the granularity we need?** This eliminates most free equity
   sources immediately.
2. **Bulk or paginated?** Backfilling 1,200 windows over a paginated, rate-limited API is a
   multi-day job with retry logic. Over a file dump it is an afternoon. Binance and HistData are
   bulk; everything else is paginated.
3. **What is the timestamp unit, and is it UTC?** Seconds vs milliseconds vs a local-time string is
   the single most common way a load silently corrupts. See `BE-02-1`.
4. **Are the bars adjusted?** For equities, an unadjusted series has a false gap at every split.
   Our blinding preserves ratios, so a split artefact survives normalization and looks like a real
   crash — which would teach the trader something untrue.
5. **May we store it?** All of these permit ingestion for internal analysis. Redistribution is a
   different question, and we do not redistribute: what leaves BlindPulse is a blinded, offset and
   rescaled series that is not the vendor's data any more.
