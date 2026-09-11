# Sprint 05 — Journal, drawings and the mystery reveal (backend)

**Status:** **DONE except journal media (05.5)** · **Estimate:** 8–10 dev-days
**Requirements:** FR-JOURNAL-01/02/06, FR-REVEAL-01..03, FR-TA-11, BR-08
**PRD:** §3.2, §3.4
**Depends on:** Sprint 03 (fully). Sprint 04 for *content*, not for function — see below · **Blocks:** Sprint 06

## Goal

Close the loop: capture what the trader was thinking while they could not see the answer, then give
them the answer. The reveal is the moment the product pays off, and it is one-way — which makes
getting its preconditions right more important than its presentation.

## What this sprint can and cannot know

Sprint 04 is decided and **not built**, so nothing has been traded in any session. This sprint is
built anyway, because none of it is blocked: a journal anchors to a bar, a drawing anchors to a bar,
and a reveal needs the feed and the session, all of which exist.

What it means is that two numbers are structurally correct and empty until execution lands:

- `strategy_return_pct` is the session's realized return, which is **0** when there are no fills, so
  `alpha_pct` comes out as the negative of the benchmark. That is the right answer for a session in
  which the trader watched and did not act, and it is why the reveal states the benchmark's
  definition rather than presenting alpha as a verdict.
- `trade_id` on a journal entry is always null, and the frontend's KPI strip and trade log have no
  rows to show.

Neither is stubbed and neither needs revisiting when Sprint 04 lands: the same computation picks up
real fills the day they exist. The reveal's headline comparison simply is not interesting yet.

## Tasks

### 05.1 Journal (FR-JOURNAL-01, FR-JOURNAL-02)
```
POST   /sessions/{id}/journal      {trade_id?, bar_index, thesis?, note?, emotion?, conviction?, tags[]}
GET    /sessions/{id}/journal
PATCH  /sessions/{id}/journal/{jid}
DELETE /sessions/{id}/journal/{jid}
```
- `bar_index` anchors the entry to the bar the trader was looking at, not to wall-clock time. A note
  written during a paused replay belongs to the moment on the chart; timestamping it by clock would
  put it in the wrong place in the story.
- The server validates that `bar_index <= cursor_index`: a note cannot be attached to a bar the
  trader has not been shown.
- Entries stay editable after the session closes — reflection is the point — but every edit bumps
  `version` and the original is retained for the discipline projector, so a trader cannot rewrite
  history into a better score.

### 05.2 Drawings (FR-TA-11)
```
POST|GET|PATCH|DELETE /sessions/{id}/drawings
```
- Payloads are opaque per `kind`, validated for envelope shape and anchoring bar range only. The
  server does not model every fibonacci level, so the charting toolkit can add a tool without a
  migration.
- Anchors are bar indices, never timestamps — a timestamp in a drawing payload is a date leak.
- Validated: `created_bar_index <= cursor_index`, payload size cap, known `kind`.

### 05.3 The reveal (FR-REVEAL-01..03, BR-08)
```
POST /sessions/{id}/reveal    → the unblinding, written once
GET  /sessions/{id}/reveal    → 403 REVEAL_LOCKED until it exists
```
Preconditions, in order:
1. Session belongs to the caller, else `SESSION_NOT_FOUND`.
2. Session status is `closed`, else `REVEAL_LOCKED`. Revealing mid-session would hand the trader
   the answer with bars still to trade.
3. Not already revealed, else `ALREADY_REVEALED`.

Computed once and frozen into `session_reveals`:
- Real symbol, timeframe, and the true window start/end.
- Macro label and notes, plus `macro_tags` (`#SVB_COLLAPSE`, `#DXY_DUMP`).
- `strategy_return_pct` — the session's realized return on starting equity.
- `benchmark_return_pct` — buy-and-hold over the identical window: enter at the first tradeable
  bar's open, exit at the last bar's close, no leverage.
- `alpha_pct` = strategy − benchmark.

Frozen rather than derived on read, so the comparison always reflects the session as it was
actually traded, even if the feed is later rebuilt.

### 05.4 Post-reveal disclosure
After a reveal, and only then, the session's bar responses may carry real timestamps and the
symbol. This is a **separate response type**, not a conditional field on the blinded one — a flag
that switches a field on is one refactor away from switching it on too early.

### 05.5 Journal media (FR-JOURNAL-06) — **NOT BUILT**
- `POST /sessions/{id}/journal/{jid}/media` — image upload, size and MIME allow-list.
- **EXIF stripped on ingest.** A screenshot can carry a capture timestamp, which would date the
  session from inside the trader's own upload.
- Signed, expiring URLs; local filesystem or S3/MinIO per config.

This is the one part of the sprint that is not built. `journal_entries.media_key` exists and nothing
writes it; the journal response carries a `media_url` that is always null. It is called out rather
than quietly folded in because it is the only task here needing infrastructure the service does not
otherwise have — object storage, a signing key, an image decoder — and because the EXIF rule is a
blinding guard in its own right: a trader's screenshot can date the window from inside their own
upload, which nothing else in this sprint has to defend against.

### 05.6 Events
`journal.written`, `session.revealed`.

## Acceptance criteria

| # | Given | When | Then |
|---|---|---|---|
| 05-AC-1 | An open session | `POST /reveal` | `REVEAL_LOCKED`; nothing written |
| 05-AC-2 | A closed session | `POST /reveal` | Symbol, timeframe, window, macro, and alpha returned and persisted |
| 05-AC-3 | An already-revealed session | `POST /reveal` | `ALREADY_REVEALED`; the stored row is unchanged |
| 05-AC-4 | An unrevealed session | `GET /reveal` | `REVEAL_LOCKED` |
| 05-AC-5 | A journal entry at bar 500, cursor at 142 | Submitted | `INVALID_CURSOR` |
| 05-AC-6 | A drawing payload with an ISO timestamp | Submitted | Rejected — anchors are bar indices |
| 05-AC-7 | An image with EXIF GPS and a capture date | Uploaded | Stored file contains no EXIF |
| 05-AC-8 | A revealed session | Benchmark recomputed by hand | Matches the stored value to 4 decimal places |
| 05-AC-9 | An edited journal entry | Inspected | `version` incremented; the original is still retrievable |

## Test plan
- **Domain:** reveal preconditions as a state table; benchmark maths against hand-computed windows.
- **Contract:** the Sprint 02 leak test asserts pre-reveal endpoints stay clean **and** that
  post-reveal responses use the separate disclosed type.
- **Integration:** EXIF stripping against real image fixtures.

## What the build changed about the plan

Three things the plan did not anticipate, recorded because each is a decision rather than a detail:

- **Journal revisions needed a table.** 05-AC-9 asks that an edited entry's original stay
  retrievable, and `version` alone cannot do that — it records *that* an edit happened, not what was
  replaced. `journal_entry_revisions` (migration `000013`) files the superseded content in the same
  transaction as the edit.
- **The drawing `kind` CHECK had drifted, and moved out of the database.** It allowed seven kinds
  while the terminal ships eleven, so a ray, a polyline or a brush stroke could not have been saved
  at all. The plan's own reason for opaque payloads is "so the toolkit can add a tool without a
  migration", and a CHECK listing tools is a migration per tool — it is now a shape constraint, with
  the vocabulary in the domain where adding a tool is a line in a list.
- **The macro annotation had no source.** The reveal promises notes and tags; `blinded_feeds` only
  carried `macro_label`. `macro_notes` and `macro_tags` were added to the feed, with `-macro-notes`
  and `-macro-tags` flags on the loader, so a curated window can carry them.

## Definition of done
All criteria met; a trader can run a session, journal through it, close it, reveal it, and see a
benchmark comparison that reconciles with a manual calculation.

**Met, less 05.5.** Verified live end to end: a note past the cursor is `INVALID_CURSOR`, an edit
files its revision and leaves unmentioned fields alone, a drawing carrying a date is refused with the
reason stated, a reveal on an open session is `REVEAL_LOCKED` and a second one is
`ALREADY_REVEALED`, and the stored benchmark matched a hand recomputation from the disclosed series
to four decimal places (5.6160 against 5.616). The blinded bars endpoint still carries no timestamp
and no symbol after the same session has been revealed.

## Risks

| Risk | Mitigation |
|---|---|
| Benchmark definition disputed | Stated explicitly in the response (`benchmark_label`) and in the UI; buy-and-hold, unlevered, same window |
| An early reveal destroying a session's value | Three ordered preconditions plus 05-AC-1; the reveal is also irreversible by design and the UI confirms it |
| Reveal disclosure leaking back into blinded endpoints | Separate response types, not conditional fields |
