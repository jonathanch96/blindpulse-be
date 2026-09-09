CREATE SCHEMA IF NOT EXISTS blindpulse;
CREATE EXTENSION IF NOT EXISTS citext;
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE blindpulse.schema_meta (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

INSERT INTO blindpulse.schema_meta (key, value)
VALUES ('bootstrapped_at', now()::text);
