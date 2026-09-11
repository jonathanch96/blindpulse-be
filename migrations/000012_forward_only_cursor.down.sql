-- Restores the two-index model. The rewound position is not recoverable — it was collapsed onto
-- the edge on the way up, which is the only direction that is safe to lose information in.

ALTER TABLE blindpulse.replay_sessions
    ADD COLUMN revealed_index INTEGER NOT NULL DEFAULT 0 CHECK (revealed_index >= 0);

UPDATE blindpulse.replay_sessions SET revealed_index = cursor_index;

ALTER TABLE blindpulse.replay_sessions
    ADD CONSTRAINT replay_sessions_cursor_within_revealed CHECK (cursor_index <= revealed_index);
