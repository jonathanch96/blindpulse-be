CREATE TABLE blindpulse.journal_entries (
    id           UUID PRIMARY KEY,
    session_id   UUID NOT NULL REFERENCES blindpulse.replay_sessions (id) ON DELETE CASCADE,
    trade_id     UUID REFERENCES blindpulse.trades (id) ON DELETE CASCADE,
    user_id      UUID NOT NULL REFERENCES blindpulse.users (id) ON DELETE CASCADE,
    -- Captured at the bar the trader was looking at, not at wall-clock write time: a note written
    -- during a paused replay still belongs to the moment on the chart.
    bar_index    INTEGER NOT NULL CHECK (bar_index >= 0),
    thesis       TEXT,
    note         TEXT,
    emotion      TEXT CHECK (emotion IN ('calm', 'confident', 'anxious', 'greedy', 'fearful', 'frustrated', 'bored')),
    conviction   SMALLINT CHECK (conviction BETWEEN 1 AND 5),
    tags         TEXT[] NOT NULL DEFAULT '{}',
    media_key    TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    version      INTEGER NOT NULL DEFAULT 1
);

CREATE INDEX journal_entries_session_idx ON blindpulse.journal_entries (session_id, bar_index);
CREATE INDEX journal_entries_trade_idx ON blindpulse.journal_entries (trade_id);
CREATE INDEX journal_entries_user_idx ON blindpulse.journal_entries (user_id, created_at DESC);
CREATE INDEX journal_entries_tags_idx ON blindpulse.journal_entries USING GIN (tags);

-- Drawings are stored as opaque payloads keyed by kind. The server validates the envelope and the
-- anchoring bar range; it deliberately does not model every fibonacci level, so the charting
-- toolkit can add tools without a migration.
CREATE TABLE blindpulse.chart_drawings (
    id          UUID PRIMARY KEY,
    session_id  UUID NOT NULL REFERENCES blindpulse.replay_sessions (id) ON DELETE CASCADE,
    kind        TEXT NOT NULL CHECK (kind IN ('fib_retracement', 'fib_extension', 'supply_zone', 'demand_zone', 'trendline', 'horizontal', 'note')),
    timeframe   TEXT NOT NULL,
    payload     JSONB NOT NULL,
    created_bar_index INTEGER NOT NULL CHECK (created_bar_index >= 0),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    version     INTEGER NOT NULL DEFAULT 1
);

CREATE INDEX chart_drawings_session_idx ON blindpulse.chart_drawings (session_id, timeframe);
