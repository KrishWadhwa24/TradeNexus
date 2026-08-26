-- Long-lived snapshot of each signal's forward performance. Signals themselves
-- are pruned after ~30 days, so a daily recorder copies the directional forward
-- return (5/10/20 trading days) here as each horizon matures — surviving the
-- signals cleanup so scanner performance can be analysed over the long run.
CREATE TABLE IF NOT EXISTS signal_outcomes (
    signal_id     BIGINT PRIMARY KEY,          -- original signals.id (snapshot)
    instrument_id BIGINT NOT NULL,
    source        TEXT NOT NULL,               -- pine | weekly | patterns
    scanner_name  TEXT NOT NULL,
    timeframe     TEXT NOT NULL,               -- 1D | 1W | 1M
    direction     TEXT NOT NULL,               -- BUY | SELL
    candle_date   DATE NOT NULL,
    entry_close   DOUBLE PRECISION NOT NULL,   -- close of the signal's candle
    -- Directional forward return %, signed so >0 always means the signal was
    -- right (BUY: price up; SELL: price down). NULL until that horizon matures.
    ret_5d        DOUBLE PRECISION,
    ret_10d       DOUBLE PRECISION,
    ret_20d       DOUBLE PRECISION,
    recorded_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS signal_outcomes_src_tf_idx ON signal_outcomes (source, timeframe);
CREATE INDEX IF NOT EXISTS signal_outcomes_candle_idx ON signal_outcomes (candle_date);
