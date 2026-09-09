-- stop_loss is NOT NULL by design: the PRD's central discipline rule is that no entry exists
-- without a hard stop, and a nullable column would make that a handler check somebody could skip.
CREATE TABLE blindpulse.orders (
    id                UUID PRIMARY KEY,
    session_id        UUID NOT NULL REFERENCES blindpulse.replay_sessions (id) ON DELETE CASCADE,
    account_id        UUID NOT NULL REFERENCES blindpulse.accounts (id) ON DELETE CASCADE,
    client_key        TEXT NOT NULL,
    side              TEXT NOT NULL CHECK (side IN ('buy', 'sell')),
    order_type        TEXT NOT NULL CHECK (order_type IN ('market', 'limit', 'stop')),
    quantity          NUMERIC(20, 8) NOT NULL CHECK (quantity > 0),
    limit_price       NUMERIC(20, 10),
    stop_loss         NUMERIC(20, 10) NOT NULL,
    take_profit       NUMERIC(20, 10),
    risk_reward       NUMERIC(8, 3),
    risk_amount       NUMERIC(20, 8),
    status            TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'filled', 'cancelled', 'rejected', 'expired')),
    rejection_code    TEXT,
    placed_bar_index  INTEGER NOT NULL CHECK (placed_bar_index >= 0),
    placed_bar_at     TIMESTAMPTZ NOT NULL,
    filled_bar_index  INTEGER,
    filled_price      NUMERIC(20, 10),
    slippage          NUMERIC(20, 10) NOT NULL DEFAULT 0,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    version           INTEGER NOT NULL DEFAULT 1,
    -- A retried submit carries the same client_key, so the unique index turns an at-least-once
    -- network into exactly-once execution.
    UNIQUE (session_id, client_key),
    CONSTRAINT orders_limit_price_present CHECK (order_type = 'market' OR limit_price IS NOT NULL),
    CONSTRAINT orders_rejection_code CHECK (status <> 'rejected' OR rejection_code IS NOT NULL)
);

CREATE INDEX orders_session_idx ON blindpulse.orders (session_id, placed_bar_index);
CREATE INDEX orders_account_idx ON blindpulse.orders (account_id);
CREATE INDEX orders_status_idx ON blindpulse.orders (session_id, status);

CREATE TABLE blindpulse.trades (
    id                UUID PRIMARY KEY,
    session_id        UUID NOT NULL REFERENCES blindpulse.replay_sessions (id) ON DELETE CASCADE,
    account_id        UUID NOT NULL REFERENCES blindpulse.accounts (id) ON DELETE CASCADE,
    entry_order_id    UUID NOT NULL REFERENCES blindpulse.orders (id) ON DELETE RESTRICT,
    exit_order_id     UUID REFERENCES blindpulse.orders (id) ON DELETE SET NULL,
    side              TEXT NOT NULL CHECK (side IN ('buy', 'sell')),
    quantity          NUMERIC(20, 8) NOT NULL CHECK (quantity > 0),
    entry_price       NUMERIC(20, 10) NOT NULL,
    exit_price        NUMERIC(20, 10),
    stop_loss         NUMERIC(20, 10) NOT NULL,
    take_profit       NUMERIC(20, 10),
    status            TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'closed')),
    exit_reason       TEXT CHECK (exit_reason IN ('stop', 'target', 'manual', 'session_end', 'drawdown_halt')),
    -- The behavioural classification the journal shows per trade. Assigned by the analytics
    -- projector from the order's own telemetry (time to entry after the signal bar, whether the
    -- stop moved, whether the exit beat the target), and correctable by the trader afterwards.
    behavior_tag      TEXT CHECK (behavior_tag IN ('followed_plan', 'fomo_entry', 'good_stop_management', 'early_cut', 'revenge_trade', 'moved_stop', 'oversized')),
    realized_pnl      NUMERIC(20, 8),
    r_multiple        NUMERIC(10, 4),
    -- Excursions are what the post-session audit reads to separate "the idea was wrong" from
    -- "the idea was right and the stop was in the wrong place".
    max_adverse_excursion    NUMERIC(20, 10),
    max_favorable_excursion  NUMERIC(20, 10),
    opened_bar_index  INTEGER NOT NULL,
    closed_bar_index  INTEGER,
    bars_held         INTEGER,
    opened_at         TIMESTAMPTZ NOT NULL,
    closed_at         TIMESTAMPTZ,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    version           INTEGER NOT NULL DEFAULT 1,
    CONSTRAINT trades_closed_fields CHECK (
        status <> 'closed' OR (exit_price IS NOT NULL AND exit_reason IS NOT NULL AND realized_pnl IS NOT NULL)
    )
);

CREATE INDEX trades_session_idx ON blindpulse.trades (session_id, opened_bar_index);
CREATE INDEX trades_account_idx ON blindpulse.trades (account_id, status);
CREATE INDEX trades_entry_order_idx ON blindpulse.trades (entry_order_id);
CREATE INDEX trades_exit_order_idx ON blindpulse.trades (exit_order_id);
