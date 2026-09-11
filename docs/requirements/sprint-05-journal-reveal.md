# Sprint 05 — Journal, drawings and the mystery reveal (backend)

**Status:** **DONE** · **Estimate:** 8–10 dev-days
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

### 05.5 Journal media (FR-JOURNAL-06)
- `POST /sessions/{id}/journal/{jid}/media` — JPEG or PNG, capped by bytes *and* by pixels: a 50KB
  PNG can declare 40,000 × 40,000, so the header is read and refused before anything is decoded.
- **EXIF stripped on ingest, by decoding and re-encoding from the pixels.** Not by stripping
  metadata segments: a stripper has to know every marker that can carry metadata (EXIF in APP1, XMP
  in another APP1, IPTC in APP13, ICC in APP2, PNG's tEXt/iTXt/zTXt/eXIf/tIME), and a format that
  gains one gets through. Re-encoding drops everything by construction — what comes out is a
  function of the image and nothing else survives. The cost is a re-compression, and a slightly
  softer screenshot is worth more than a date the trader did not mean to publish.
- The stored filename never comes from the upload: it is the entry id plus the sniffed format. A
  client-supplied name is a path traversal waiting to happen, and also somewhere a trader could
  publish the window by calling their file `eurusd-2023-03-14.png`.
- **Signed, expiring URLs.** An `<img src>` cannot carry an Authorization header — the same
  constraint that produced the websocket ticket — so the link is the authority: an HMAC over the key
  and an expiry, minted per response when the owner reads the entry, valid for minutes.
- **Local filesystem.** S3/MinIO is what `StorageConfig.Endpoint` anticipates and is deliberately
  not written: it cannot be exercised from this environment, and an object-store adapter that has
  never talked to an object store is a claim rather than a feature. The `storage.Store` interface is
  the part that makes adding it a new file.
- One image per entry, and a replacement gets a fresh key so a cache still holding the old one
  cannot serve it. Empty `STORAGE_SIGN_SECRET` disables uploads rather than signing with a known
  key: a predictable signature looks like protection and is not.

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
| 05-AC-7 | An image with EXIF GPS and a capture date | Uploaded | Stored file contains no EXIF — **verified live**, fetched back through the signed link |
| 05-AC-8 | A revealed session | Benchmark recomputed by hand | Matches the stored value to 4 decimal places |
| 05-AC-9 | An edited journal entry | Inspected | `version` incremented; the original is still retrievable |

## Test plan
- **Domain:** reveal preconditions as a state table; benchmark maths against hand-computed windows.
- **Contract:** the Sprint 02 leak test asserts pre-reveal endpoints stay clean **and** that
  post-reveal responses use the separate disclosed type.
- **Integration:** EXIF stripping against real image fixtures — a JPEG with a spliced APP1/Exif
  segment and a PNG with a spliced tEXt chunk, asserted over the *stored bytes* rather than over a
  parsed structure, because the claim is that nothing survived rather than that one library can no
  longer find it. Verified non-vacuous by making Sanitize pass the upload through.

## What the build changed about the plan

Four things the plan did not anticipate, recorded because each is a decision rather than a detail:

- **Journal revisions needed a table.** 05-AC-9 asks that an edited entry's original stay
  retrievable, and `version` alone cannot do that — it records *that* an edit happened, not what was
  replaced. `journal_entry_revisions` (migration `000013`) files the superseded content in the same
  transaction as the edit.
- **The drawing `kind` CHECK had drifted, and moved out of the database.** It allowed seven kinds
  while the terminal ships eleven, so a ray, a polyline or a brush stroke could not have been saved
  at all. The plan's own reason for opaque payloads is "so the toolkit can add a tool without a
  migration", and a CHECK listing tools is a migration per tool — it is now a shape constraint, with
  the vocabulary in the domain where adding a tool is a line in a list.
- **Media needed a wildcard route, and that bug was invisible from the outside.** A storage key
  contains slashes, and Go normalizes `%2F` back to `/` before routing — so a `:key` parameter never
  matched and every signed link 404'd while its signature was perfectly valid. It was caught only by
  fetching an image back and looking at what arrived: the EXIF assertion had been passing against 18
  bytes of a 404 page.
- **The macro annotation had no source.** The reveal promises notes and tags; `blinded_feeds` only
  carried `macro_label`. `macro_notes` and `macro_tags` were added to the feed, with `-macro-notes`
  and `-macro-tags` flags on the loader, so a curated window can carry them.

## Definition of done
All criteria met; a trader can run a session, journal through it, close it, reveal it, and see a
benchmark comparison that reconciles with a manual calculation.

**Met.** Verified live end to end: a note past the cursor is `INVALID_CURSOR`, an edit
files its revision and leaves unmentioned fields alone, a drawing carrying a date is refused with the
reason stated, a reveal on an open session is `REVEAL_LOCKED` and a second one is
`ALREADY_REVEALED`, and the stored benchmark matched a hand recomputation from the disclosed series
to four decimal places (5.6160 against 5.616). The blinded bars endpoint still carries no timestamp
and no symbol after the same session has been revealed.

For media specifically, with a JPEG carrying a real APP1 segment holding `DateTimeOriginal
2023:03:14 08:31:00` and a GPS tag: the upload is accepted, the signed link fetches back 376 bytes
of `image/jpeg`, and none of `Exif`, the date, the time, `GPSLatitude`, the coordinate or the
software name survives in the stored bytes. An unsigned link, a tampered signature and a different
key under the same signature are all refused; an SVG and a shell script renamed `.png` are
`UNSUPPORTED_MEDIA_TYPE`; a 6MB upload is `FILE_TOO_LARGE`; a replacement gets a new key and the old
link stops working; and a stranger's upload to someone else's entry is `JOURNAL_NOT_FOUND`.

## Risks

| Risk | Mitigation |
|---|---|
| Benchmark definition disputed | Stated explicitly in the response (`benchmark_label`) and in the UI; buy-and-hold, unlevered, same window |
| An early reveal destroying a session's value | Three ordered preconditions plus 05-AC-1; the reveal is also irreversible by design and the UI confirms it |
| Reveal disclosure leaking back into blinded endpoints | Separate response types, not conditional fields |
