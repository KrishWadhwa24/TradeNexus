CREATE TABLE algo_decisions (
    id                BIGSERIAL PRIMARY KEY,
    evaluated_at      TIMESTAMPTZ NOT NULL,
    -- underlying (NIFTY) context at evaluation time
    nifty_spot        DOUBLE PRECISION NOT NULL DEFAULT 0,
    or_high           DOUBLE PRECISION NOT NULL DEFAULT 0,
    or_low            DOUBLE PRECISION NOT NULL DEFAULT 0,
    vwap              DOUBLE PRECISION NOT NULL DEFAULT 0,
    ema_fast          DOUBLE PRECISION NOT NULL DEFAULT 0,
    ema_slow          DOUBLE PRECISION NOT NULL DEFAULT 0,
    atr               DOUBLE PRECISION NOT NULL DEFAULT 0,
    atr_avg           DOUBLE PRECISION NOT NULL DEFAULT 0,
    direction         TEXT NOT NULL,
    direction_reason  TEXT NOT NULL DEFAULT '',
    -- selection/entry context (only meaningful once direction != NONE)
    selected_symbol   TEXT NOT NULL DEFAULT '',
    selected_strike   DOUBLE PRECISION NOT NULL DEFAULT 0,
    selected_delta    DOUBLE PRECISION NOT NULL DEFAULT 0,
    selected_iv       DOUBLE PRECISION NOT NULL DEFAULT 0,
    selected_theta    DOUBLE PRECISION NOT NULL DEFAULT 0,
    selection_reason  TEXT NOT NULL DEFAULT '',
    entry_ok          BOOLEAN NOT NULL DEFAULT FALSE,
    entry_reason      TEXT NOT NULL DEFAULT '',
    -- outcome
    action            TEXT NOT NULL, -- NO_DIRECTION | DAILY_LOSS_LIMIT | WEEKLY_LOSS_LIMIT |
                                      -- MAX_POSITIONS | MAX_TRADES_TODAY | NO_CONTRACT |
                                      -- ENTRY_REJECTED | SIZED_ZERO | EXECUTED | ORDER_REJECTED |
                                      -- ERROR | EXIT
    trade_id          BIGINT,        -- set for EXECUTED/EXIT; no FK, paper_trades isn't owned here
    -- exit-specific (only set when action = EXIT)
    exit_price        DOUBLE PRECISION,
    exit_reason       TEXT NOT NULL DEFAULT '',
    pnl               DOUBLE PRECISION,
    mfe               DOUBLE PRECISION, -- max favorable excursion seen over the position's life
    mae               DOUBLE PRECISION, -- max adverse excursion seen over the position's life
    detail            TEXT NOT NULL DEFAULT ''
);

CREATE INDEX ON algo_decisions (evaluated_at DESC);
