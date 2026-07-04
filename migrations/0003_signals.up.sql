-- Module 6: signal audit store + trading-holiday calendar.

CREATE TABLE signals (
    id            BIGSERIAL PRIMARY KEY,
    instrument_id BIGINT NOT NULL REFERENCES instruments(id) ON DELETE CASCADE,
    source        TEXT NOT NULL,          -- pine | weekly
    scanner_name  TEXT NOT NULL,          -- pine | weekly_1..weekly_4 | csv of fired
    timeframe     TEXT NOT NULL,          -- 1D | 1W | 1M
    direction     TEXT NOT NULL,          -- BUY | SELL
    candle_date   DATE NOT NULL,          -- bar the signal is attributed to
    confidence    INTEGER,                -- weekly N/4; NULL for pine
    reasons       JSONB NOT NULL DEFAULT '{}',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- Idempotency: the same bar/scanner can't produce duplicate signals.
    UNIQUE (instrument_id, source, scanner_name, timeframe, candle_date)
);
CREATE INDEX signals_created_idx    ON signals (created_at);
CREATE INDEX signals_instrument_idx ON signals (instrument_id);

-- Exchange holidays. Weekends are handled in code; this table holds the rest.
-- Seed via POST /v1/admin/holidays (kept empty here to avoid stale data).
CREATE TABLE market_holidays (
    exchange     TEXT NOT NULL DEFAULT 'NSE',
    holiday_date DATE NOT NULL,
    description  TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (exchange, holiday_date)
);
