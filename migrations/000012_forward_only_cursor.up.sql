-- Sprint 04 decision (review finding SP4-2): the replay cursor is forward-only.
--
-- Sprint 03 carried two indices because the PRD allowed candle-by-candle review in both
-- directions: cursor_index was where the trader was looking, revealed_index the high-water mark of
-- what they had been shown. That split existed to stop a rewound trader acting on a bar whose
-- outcome they had already seen.
--
-- The product decision removes the premise. A trader cannot go back at all: once a bar is stepped
-- past it is history, the way it is on a live chart, and a trader who wants a different setup
-- randomizes a new feed rather than rewinding this one. With no way to look backward the two
-- indices are always equal, and a second column that can never differ from the first is a model
-- that lies about what the system does.
--
-- Collapsing onto cursor_index rather than revealed_index keeps the name the domain already uses
-- everywhere (BR-02 is "the replay cursor is server-authoritative"). The value kept is the *edge*:
-- for any session that had been rewound, what the trader has actually been shown is
-- revealed_index, and that cannot be un-shown.

ALTER TABLE blindpulse.replay_sessions
    DROP CONSTRAINT IF EXISTS replay_sessions_cursor_within_revealed;

UPDATE blindpulse.replay_sessions
   SET cursor_index = revealed_index
 WHERE revealed_index > cursor_index;

ALTER TABLE blindpulse.replay_sessions
    DROP COLUMN revealed_index;
