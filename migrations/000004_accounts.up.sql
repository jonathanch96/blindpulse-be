-- Accounts form a reset tree, not a mutable record. A reset never edits or deletes the blown
-- iteration: it seals that row and inserts a child pointing back at it, so "Iteration 04" and
-- "#01..#03" all coexist and every historical mistake stays queryable forever.
CREATE TABLE blindpulse.accounts (
    id                      UUID PRIMARY KEY,
    user_id                 UUID NOT NULL REFERENCES blindpulse.users (id) ON DELETE CASCADE,
    root_account_id         UUID NOT NULL REFERENCES blindpulse.accounts (id) ON DELETE CASCADE,
    parent_account_id       UUID REFERENCES blindpulse.accounts (id) ON DELETE RESTRICT,
    iteration_index         INTEGER NOT NULL CHECK (iteration_index >= 1),
    name                    TEXT NOT NULL,
    -- The human label the accounts screen shows beside the iteration ("Swing Replay (Liquidity
    -- Sweep)", "Scalp Drill (VWAP Bands)"): the trader's stated intent for this branch, recorded
    -- up front so the post-mortem can ask whether the trades matched the plan.
    strategy_profile        TEXT,
    currency                TEXT NOT NULL DEFAULT 'USD',
    initial_balance         NUMERIC(20, 8) NOT NULL CHECK (initial_balance > 0),
    current_balance         NUMERIC(20, 8) NOT NULL,
    current_equity          NUMERIC(20, 8) NOT NULL,
    peak_equity             NUMERIC(20, 8) NOT NULL,
    -- Server-side mirrors of the bracket dock's gates. The client shows them; this row enforces
    -- them, so a hand-rolled API call cannot place a trade the UI would have refused.
    risk_per_trade_pct      NUMERIC(6, 3) NOT NULL DEFAULT 1.000 CHECK (risk_per_trade_pct > 0 AND risk_per_trade_pct <= 100),
    max_daily_drawdown_pct  NUMERIC(6, 3) NOT NULL DEFAULT 5.000 CHECK (max_daily_drawdown_pct > 0 AND max_daily_drawdown_pct <= 100),
    min_risk_reward         NUMERIC(6, 2) NOT NULL DEFAULT 2.00 CHECK (min_risk_reward > 0),
    max_open_positions      INTEGER NOT NULL DEFAULT 5 CHECK (max_open_positions >= 1),
    leverage                NUMERIC(8, 2) NOT NULL DEFAULT 1 CHECK (leverage > 0),
    status                  TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'reset', 'archived')),
    reset_reason            TEXT,
    reset_at                TIMESTAMPTZ,
    sealed_at               TIMESTAMPTZ,
    root_hash               TEXT,
    created_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at              TIMESTAMPTZ NOT NULL DEFAULT now(),
    version                 INTEGER NOT NULL DEFAULT 1,
    -- An iteration past the first must name its parent; the first must not have one.
    CONSTRAINT accounts_parent_matches_iteration CHECK (
        (iteration_index = 1 AND parent_account_id IS NULL)
        OR (iteration_index > 1 AND parent_account_id IS NOT NULL)
    ),
    CONSTRAINT accounts_sealed_when_reset CHECK (status <> 'reset' OR sealed_at IS NOT NULL)
);

CREATE INDEX accounts_user_idx ON blindpulse.accounts (user_id, status);
CREATE INDEX accounts_root_idx ON blindpulse.accounts (root_account_id, iteration_index);
CREATE INDEX accounts_parent_idx ON blindpulse.accounts (parent_account_id);
CREATE UNIQUE INDEX accounts_tree_iteration_key ON blindpulse.accounts (root_account_id, iteration_index);
-- Exactly one live iteration per tree: a reset must seal the old one in the same transaction that
-- opens the new one, or the constraint rejects the write.
CREATE UNIQUE INDEX accounts_single_active_iteration
    ON blindpulse.accounts (root_account_id)
    WHERE status = 'active';

-- The append-only ledger. Every balance movement is one row, chained by hash to the row before
-- it, so an iteration can be proven untampered after the fact without trusting the application.
CREATE TABLE blindpulse.account_ledger_entries (
    id             UUID PRIMARY KEY,
    account_id     UUID NOT NULL REFERENCES blindpulse.accounts (id) ON DELETE CASCADE,
    sequence       BIGINT NOT NULL CHECK (sequence >= 1),
    kind           TEXT NOT NULL CHECK (kind IN ('open', 'trade', 'fee', 'adjustment', 'reset', 'seal')),
    reference_type TEXT,
    reference_id   UUID,
    amount         NUMERIC(20, 8) NOT NULL,
    balance_after  NUMERIC(20, 8) NOT NULL,
    equity_after   NUMERIC(20, 8) NOT NULL,
    -- json, not jsonb, and deliberately so: jsonb re-serializes on write (it reorders keys and
    -- normalizes whitespace), which would change the very bytes the entry_hash covers and break
    -- every chain on read-back. Ledger payloads are read wholesale as an audit record, never
    -- queried by key, so the indexing jsonb would buy is worth nothing here.
    payload        JSON NOT NULL DEFAULT '{}'::json,
    previous_hash  TEXT,
    entry_hash     TEXT NOT NULL,
    recorded_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (account_id, sequence),
    UNIQUE (entry_hash)
);

CREATE INDEX account_ledger_entries_reference_idx ON blindpulse.account_ledger_entries (reference_type, reference_id);
