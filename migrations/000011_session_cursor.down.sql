DROP INDEX IF EXISTS blindpulse.replay_sessions_idle_idx;
ALTER TABLE blindpulse.replay_sessions
    DROP CONSTRAINT IF EXISTS replay_sessions_cursor_within_revealed,
    DROP COLUMN IF EXISTS last_checkpoint_index,
    DROP COLUMN IF EXISTS revealed_index,
    DROP COLUMN IF EXISTS timeframe;
