-- A replay session is a deterministic walk over one blinded feed. cursor_index is the only
-- authority on "now": bars past it are unwritten future, and every fill, equity point and gate
-- check is resolved against it server-side. The client never decides what time it is.
CREATE TABLE blindpulse.replay_sessions (
    id             UUID PRIMARY KEY,
    user_id        UUID NOT NULL REFERENCES blindpulse.users (id) ON DELETE CASCADE,
    account_id     UUID NOT NULL REFERENCES blindpulse.accounts (id) ON DELETE CASCADE,
    feed_id        UUID NOT NULL REFERENCES blindpulse.blinded_feeds (id) ON DELETE RESTRICT,
    status         TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'paused', 'closed', 'abandoned')),
    playback_speed NUMERIC(4, 2) NOT NULL DEFAULT 1.00 CHECK (playback_speed >= 0.5 AND playback_speed <= 10),
    cursor_index   INTEGER NOT NULL DEFAULT 0 CHECK (cursor_index >= 0),
    cursor_at      TIMESTAMPTZ,
    -- The seed makes a session reproducible: same feed, same seed, same slippage draws, so a
    -- disputed fill can be recomputed exactly instead of argued about.
    seed           BIGINT NOT NULL,
    started_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_active_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    closed_at      TIMESTAMPTZ,
    revealed_at    TIMESTAMPTZ,
    root_hash      TEXT,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    version        INTEGER NOT NULL DEFAULT 1,
    CONSTRAINT replay_sessions_closed_at CHECK (status NOT IN ('closed', 'abandoned') OR closed_at IS NOT NULL)
);

CREATE INDEX replay_sessions_user_idx ON blindpulse.replay_sessions (user_id, status);
CREATE INDEX replay_sessions_account_idx ON blindpulse.replay_sessions (account_id);
CREATE INDEX replay_sessions_feed_idx ON blindpulse.replay_sessions (feed_id);
-- One live session per account: two cursors moving over the same equity would make the drawdown
-- gate meaningless.
CREATE UNIQUE INDEX replay_sessions_single_open
    ON blindpulse.replay_sessions (account_id)
    WHERE status IN ('open', 'paused');

-- Equity is snapshotted per bar rather than recomputed on read, because the analytics screen
-- overlays several iterations' normalized curves and needs them as stored series.
CREATE TABLE blindpulse.equity_snapshots (
    session_id     UUID NOT NULL REFERENCES blindpulse.replay_sessions (id) ON DELETE CASCADE,
    bar_index      INTEGER NOT NULL CHECK (bar_index >= 0),
    bar_at         TIMESTAMPTZ NOT NULL,
    balance        NUMERIC(20, 8) NOT NULL,
    equity         NUMERIC(20, 8) NOT NULL,
    drawdown_pct   NUMERIC(8, 4) NOT NULL DEFAULT 0,
    open_positions INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (session_id, bar_index)
);
