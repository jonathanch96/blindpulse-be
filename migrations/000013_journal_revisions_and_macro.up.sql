-- A journal entry stays editable after the session closes, because reflection is the point. But the
-- discipline projector reads what the trader thought *at the time*, so an edit must not be able to
-- replace that: every edit files the superseded content here first.
--
-- Without this, "version is incremented" would be the only trace of an edit, and a trader could
-- rewrite a panicked thesis into a calm one and score better for it. The row that the projector
-- reads is the one written while the outcome was still unknown.
CREATE TABLE blindpulse.journal_entry_revisions (
    id            UUID PRIMARY KEY,
    -- ON DELETE CASCADE: deleting an entry takes its revisions with it. The deletion itself is
    -- recorded in the event stream, which is where a projector learns that history was removed
    -- rather than never written.
    entry_id      UUID NOT NULL REFERENCES blindpulse.journal_entries (id) ON DELETE CASCADE,
    -- The version this content *was*, not the version that replaced it: revision 1 is what the
    -- entry said before its first edit.
    version       INTEGER NOT NULL CHECK (version >= 1),
    bar_index     INTEGER NOT NULL CHECK (bar_index >= 0),
    thesis        TEXT,
    note          TEXT,
    emotion       TEXT,
    conviction    SMALLINT,
    tags          TEXT[] NOT NULL DEFAULT '{}',
    superseded_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (entry_id, version)
);

CREATE INDEX journal_entry_revisions_entry_idx ON blindpulse.journal_entry_revisions (entry_id, version);

-- The reveal promises a macro annotation — the narrative and the tags that explain what the trader
-- just lived through. macro_label alone is a headline ("SVB Contagion"); these are the body. They
-- live on the feed because they describe the window, and they are withheld exactly as the label is:
-- nothing here crosses a pre-reveal response type.
ALTER TABLE blindpulse.blinded_feeds ADD COLUMN macro_notes TEXT;
ALTER TABLE blindpulse.blinded_feeds ADD COLUMN macro_tags TEXT[] NOT NULL DEFAULT '{}';

-- Drawings: the kind vocabulary moves out of the database.
--
-- The plan's reason for storing payloads opaquely is "so the charting toolkit can add a tool
-- without a migration" — and a CHECK that lists the tools is a migration per tool. It had already
-- drifted: the constraint allowed seven kinds and the terminal ships eleven, so persisting a ray,
-- a polyline or a brush stroke would have been rejected by the database.
--
-- What replaces it still guarantees something, which is the point: the column holds a lower-snake
-- identifier and nothing else, so it cannot become free text or a JSON blob. Which identifiers are
-- meaningful is the domain's business, where adding a tool is a line in a list.
ALTER TABLE blindpulse.chart_drawings DROP CONSTRAINT chart_drawings_kind_check;
ALTER TABLE blindpulse.chart_drawings ADD CONSTRAINT chart_drawings_kind_shape
    CHECK (kind ~ '^[a-z][a-z0-9_]{0,39}$');
