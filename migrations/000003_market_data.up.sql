-- Instruments and their bars are the unblinded truth. Nothing in this file is ever served to a
-- trader directly: the replay API reads it only through a blinded_feed, which strips the symbol
-- and rebases the prices. Keeping the real identity in its own tables makes that boundary a
-- schema-level fact rather than a convention a handler could forget.
CREATE TABLE blindpulse.instruments (
    id           UUID PRIMARY KEY,
    symbol       TEXT NOT NULL UNIQUE,
    display_name TEXT NOT NULL,
    asset_class  TEXT NOT NULL CHECK (asset_class IN ('fx', 'equity', 'crypto', 'futures', 'index', 'commodity')),
    venue        TEXT,
    quote_currency TEXT NOT NULL DEFAULT 'USD',
    tick_size    NUMERIC(20, 10) NOT NULL DEFAULT 0.00001,
    contract_size NUMERIC(20, 8) NOT NULL DEFAULT 1,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE blindpulse.market_bars (
    instrument_id UUID NOT NULL REFERENCES blindpulse.instruments (id) ON DELETE CASCADE,
    timeframe     TEXT NOT NULL CHECK (timeframe IN ('1m', '5m', '15m', '30m', '1h', '4h', '1d', '1w')),
    opened_at     TIMESTAMPTZ NOT NULL,
    open          NUMERIC(20, 10) NOT NULL,
    high          NUMERIC(20, 10) NOT NULL,
    low           NUMERIC(20, 10) NOT NULL,
    close         NUMERIC(20, 10) NOT NULL,
    volume        NUMERIC(24, 8) NOT NULL DEFAULT 0,
    PRIMARY KEY (instrument_id, timeframe, opened_at),
    CONSTRAINT market_bars_range CHECK (high >= low AND high >= open AND high >= close AND low <= open AND low <= close)
);

-- The replay cursor always walks forward through one (instrument, timeframe) in time order, so
-- the primary key above is already the access path; this index serves the reverse scan used when
-- seeding a session's initial lookback window.
CREATE INDEX market_bars_recent_idx ON blindpulse.market_bars (instrument_id, timeframe, opened_at DESC);

-- A blinded feed is the trader-facing object: an alias, a window, and the normalization that
-- makes the price series unrecognizable. price_scale/price_offset rebase the series so a
-- screenshot cannot be reverse-searched, and macro_label is withheld until the reveal.
CREATE TABLE blindpulse.blinded_feeds (
    id             UUID PRIMARY KEY,
    instrument_id  UUID NOT NULL REFERENCES blindpulse.instruments (id) ON DELETE CASCADE,
    alias_label    TEXT NOT NULL UNIQUE,
    base_timeframe TEXT NOT NULL CHECK (base_timeframe IN ('1m', '5m', '15m', '30m', '1h', '4h', '1d', '1w')),
    window_start   TIMESTAMPTZ NOT NULL,
    window_end     TIMESTAMPTZ NOT NULL,
    warmup_bars    INTEGER NOT NULL DEFAULT 200 CHECK (warmup_bars >= 0),
    total_bars     INTEGER NOT NULL CHECK (total_bars > 0),
    price_scale    NUMERIC(20, 10) NOT NULL DEFAULT 1 CHECK (price_scale > 0),
    price_offset   NUMERIC(20, 10) NOT NULL DEFAULT 0,
    difficulty     TEXT NOT NULL DEFAULT 'standard' CHECK (difficulty IN ('calm', 'standard', 'volatile', 'crisis')),
    macro_label    TEXT,
    is_published   BOOLEAN NOT NULL DEFAULT false,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    version        INTEGER NOT NULL DEFAULT 1,
    CONSTRAINT blinded_feeds_window CHECK (window_end > window_start)
);

CREATE INDEX blinded_feeds_instrument_idx ON blindpulse.blinded_feeds (instrument_id);
CREATE INDEX blinded_feeds_published_idx ON blindpulse.blinded_feeds (is_published, difficulty);
