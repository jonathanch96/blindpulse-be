-- Sprint 03A: the cursor becomes server-authoritative state.

ALTER TABLE blindpulse.replay_sessions
    -- The timeframe currently being viewed. Separate from the feed's base timeframe because a
    -- trader switches between them over one cursor (03B).
    ADD COLUMN timeframe TEXT NOT NULL DEFAULT '15m',

    -- The high-water mark: the furthest bar this session has ever released.
    --
    -- This is the column that makes stepping backward safe. The PRD allows candle-by-candle review
    -- in both directions, but if reads and fills followed a rewound cursor a trader could step
    -- back and trade a bar whose outcome they had already seen — which is precisely the hindsight
    -- the product exists to remove. So cursor_index is where the trader is *looking*, and
    -- revealed_index is what they are permitted to know. Rewinding moves the first and never the
    -- second, and Sprint 04 fills orders at revealed_index, never at cursor_index.
    ADD COLUMN revealed_index INTEGER NOT NULL DEFAULT 0 CHECK (revealed_index >= 0),

    -- Last index durably written to PostgreSQL. Redis holds the live cursor; this bounds how much
    -- a cache flush can cost.
    ADD COLUMN last_checkpoint_index INTEGER NOT NULL DEFAULT 0 CHECK (last_checkpoint_index >= 0),

    -- A trader may never look further forward than they have been shown.
    ADD CONSTRAINT replay_sessions_cursor_within_revealed CHECK (cursor_index <= revealed_index);

-- The idle sweeper's query: sessions still open, ordered by how long they have been quiet.
CREATE INDEX replay_sessions_idle_idx
    ON blindpulse.replay_sessions (status, last_active_at)
    WHERE status IN ('open', 'paused');
