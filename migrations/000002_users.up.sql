CREATE TABLE blindpulse.users (
    id            UUID PRIMARY KEY,
    email         CITEXT NOT NULL UNIQUE,
    name          TEXT NOT NULL,
    password_hash TEXT,
    google_id     TEXT UNIQUE,
    -- Institutional sign-in (SAML 2.0 / Okta, TradingView SSO). Kept alongside google_id rather
    -- than folded into it so a desk can be provisioned through its IdP without inheriting
    -- Google's token verification path.
    sso_provider  TEXT,
    sso_subject   TEXT,
    avatar_url    TEXT,
    timezone      TEXT NOT NULL DEFAULT 'UTC',
    last_login_at TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    deleted_at    TIMESTAMPTZ,
    -- A federated identity carries no password and a local account carries no assertion. One of
    -- the three must exist, or the row is an account nobody can ever sign in to.
    CONSTRAINT users_credential_present CHECK (
        password_hash IS NOT NULL OR google_id IS NOT NULL OR sso_subject IS NOT NULL
    ),
    CONSTRAINT users_sso_pair CHECK ((sso_provider IS NULL) = (sso_subject IS NULL))
);

CREATE UNIQUE INDEX users_sso_identity_key
    ON blindpulse.users (sso_provider, sso_subject)
    WHERE sso_provider IS NOT NULL;

CREATE INDEX users_deleted_at_idx ON blindpulse.users (deleted_at);

CREATE TABLE blindpulse.refresh_tokens (
    id          UUID PRIMARY KEY,
    user_id     UUID NOT NULL REFERENCES blindpulse.users (id) ON DELETE CASCADE,
    token_hash  TEXT NOT NULL UNIQUE,
    expires_at  TIMESTAMPTZ NOT NULL,
    revoked_at  TIMESTAMPTZ,
    user_agent  TEXT,
    ip          INET,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX refresh_tokens_user_id_idx ON blindpulse.refresh_tokens (user_id);
CREATE INDEX refresh_tokens_expires_at_idx ON blindpulse.refresh_tokens (expires_at);
