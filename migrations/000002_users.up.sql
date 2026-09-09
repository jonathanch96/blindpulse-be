CREATE TABLE blindpulse.users (
    id               UUID PRIMARY KEY,
    email            CITEXT NOT NULL UNIQUE,
    password_hash    TEXT,
    display_name     TEXT NOT NULL,
    google_sub       TEXT UNIQUE,
    sso_provider     TEXT,
    sso_subject      TEXT,
    timezone         TEXT NOT NULL DEFAULT 'UTC',
    last_login_at    TIMESTAMPTZ,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at       TIMESTAMPTZ,
    version          INTEGER NOT NULL DEFAULT 1,
    -- A federated identity carries no password, and a local account carries no assertion. One of
    -- the two must exist or the row is an account nobody can ever sign in to.
    CONSTRAINT users_credential_present CHECK (
        password_hash IS NOT NULL OR google_sub IS NOT NULL OR sso_subject IS NOT NULL
    ),
    CONSTRAINT users_sso_pair CHECK ((sso_provider IS NULL) = (sso_subject IS NULL))
);

CREATE UNIQUE INDEX users_sso_identity_key
    ON blindpulse.users (sso_provider, sso_subject)
    WHERE sso_provider IS NOT NULL;

CREATE TABLE blindpulse.refresh_tokens (
    id          UUID PRIMARY KEY,
    user_id     UUID NOT NULL REFERENCES blindpulse.users (id) ON DELETE CASCADE,
    token_hash  TEXT NOT NULL UNIQUE,
    issued_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at  TIMESTAMPTZ NOT NULL,
    revoked_at  TIMESTAMPTZ,
    replaced_by UUID REFERENCES blindpulse.refresh_tokens (id) ON DELETE SET NULL,
    user_agent  TEXT,
    ip_address  INET
);

CREATE INDEX refresh_tokens_user_id_idx ON blindpulse.refresh_tokens (user_id);
CREATE INDEX refresh_tokens_expires_at_idx ON blindpulse.refresh_tokens (expires_at);
