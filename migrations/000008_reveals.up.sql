-- The reveal is written once, when the trader chooses to unblind. Storing the computed benchmark
-- and discipline numbers (rather than deriving them on read) freezes the comparison against the
-- session as it was actually traded.
CREATE TABLE blindpulse.session_reveals (
    session_id        UUID PRIMARY KEY REFERENCES blindpulse.replay_sessions (id) ON DELETE CASCADE,
    instrument_id     UUID NOT NULL REFERENCES blindpulse.instruments (id) ON DELETE RESTRICT,
    revealed_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    symbol            TEXT NOT NULL,
    timeframe         TEXT NOT NULL,
    window_start      TIMESTAMPTZ NOT NULL,
    window_end        TIMESTAMPTZ NOT NULL,
    macro_label       TEXT,
    macro_notes       TEXT,
    macro_tags        TEXT[] NOT NULL DEFAULT '{}',
    benchmark_label   TEXT NOT NULL DEFAULT 'buy_and_hold',
    strategy_return_pct  NUMERIC(12, 4) NOT NULL DEFAULT 0,
    benchmark_return_pct NUMERIC(12, 4) NOT NULL DEFAULT 0,
    alpha_pct            NUMERIC(12, 4) NOT NULL DEFAULT 0,
    discipline_index     SMALLINT NOT NULL DEFAULT 0 CHECK (discipline_index BETWEEN 0 AND 100),
    metrics           JSONB NOT NULL DEFAULT '{}'::jsonb
);

-- Per-session behavioural aggregates, rebuilt by the analytics projector from the event stream.
-- Kept separate from session_reveals so a projector replay can rewrite them without touching the
-- trader-facing reveal record.
CREATE TABLE blindpulse.session_metrics (
    session_id          UUID PRIMARY KEY REFERENCES blindpulse.replay_sessions (id) ON DELETE CASCADE,
    trades_total        INTEGER NOT NULL DEFAULT 0,
    trades_won          INTEGER NOT NULL DEFAULT 0,
    trades_lost         INTEGER NOT NULL DEFAULT 0,
    win_rate            NUMERIC(6, 3) NOT NULL DEFAULT 0,
    profit_factor       NUMERIC(10, 4),
    expectancy_r        NUMERIC(10, 4),
    expectancy_cash     NUMERIC(20, 8),
    average_r           NUMERIC(10, 4),
    planned_risk_reward NUMERIC(8, 3),
    realized_risk_reward NUMERIC(8, 3),
    sharpe_ratio        NUMERIC(10, 4),
    sortino_ratio       NUMERIC(10, 4),
    recovery_factor     NUMERIC(10, 4),
    max_consecutive_wins INTEGER NOT NULL DEFAULT 0,
    max_drawdown_pct    NUMERIC(8, 4) NOT NULL DEFAULT 0,
    max_consecutive_losses INTEGER NOT NULL DEFAULT 0,
    -- Discipline components: each is 0-100 and the index is their weighted mean, so a low score
    -- can always be traced to the behaviour that caused it.
    stop_respect_score      SMALLINT NOT NULL DEFAULT 0 CHECK (stop_respect_score BETWEEN 0 AND 100),
    risk_consistency_score  SMALLINT NOT NULL DEFAULT 0 CHECK (risk_consistency_score BETWEEN 0 AND 100),
    overtrading_score       SMALLINT NOT NULL DEFAULT 0 CHECK (overtrading_score BETWEEN 0 AND 100),
    plan_adherence_score    SMALLINT NOT NULL DEFAULT 0 CHECK (plan_adherence_score BETWEEN 0 AND 100),
    patience_score          SMALLINT NOT NULL DEFAULT 0 CHECK (patience_score BETWEEN 0 AND 100),
    tilt_risk_pct           NUMERIC(6, 3) NOT NULL DEFAULT 0,
    discipline_index        SMALLINT NOT NULL DEFAULT 0 CHECK (discipline_index BETWEEN 0 AND 100),
    rejected_orders     INTEGER NOT NULL DEFAULT 0,
    gate_breaches       INTEGER NOT NULL DEFAULT 0,
    computed_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    version             INTEGER NOT NULL DEFAULT 1
);
