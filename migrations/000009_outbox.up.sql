-- The transactional outbox. A domain write and its event land in one commit; the relay in
-- cmd/worker publishes to Kafka afterwards and marks the row. Direct publishing from a handler
-- would make an event either lost on rollback or emitted for a trade that never happened.
CREATE TABLE blindpulse.outbox_events (
    id             UUID PRIMARY KEY,
    aggregate_type TEXT NOT NULL,
    aggregate_id   UUID NOT NULL,
    event_type     TEXT NOT NULL,
    topic          TEXT NOT NULL,
    partition_key  TEXT NOT NULL,
    payload        JSONB NOT NULL,
    headers        JSONB NOT NULL DEFAULT '{}'::jsonb,
    status         TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'published', 'failed')),
    attempts       INTEGER NOT NULL DEFAULT 0,
    available_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    published_at   TIMESTAMPTZ,
    last_error     TEXT,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- The relay's only query: oldest pending rows that are due. Partial index keeps it cheap once the
-- table is mostly published history.
CREATE INDEX outbox_events_due_idx
    ON blindpulse.outbox_events (available_at, created_at)
    WHERE status = 'pending';

CREATE INDEX outbox_events_aggregate_idx ON blindpulse.outbox_events (aggregate_type, aggregate_id);
