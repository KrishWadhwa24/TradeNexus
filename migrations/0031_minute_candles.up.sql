-- 1-minute OHLCV bars — a separate table from daily_candles/weekly_candles/
-- monthly_candles, deliberately: this exists purely for the options-algo
-- underlying feed (Nifty/Sensex), never touched by the equity scan/reconcile
-- pipeline, which stays entirely on the daily tables. No schema change to
-- anything the equity flow reads or writes.
CREATE TABLE IF NOT EXISTS minute_candles (
    instrument_id BIGINT NOT NULL REFERENCES instruments(id) ON DELETE CASCADE,
    candle_time   TIMESTAMPTZ NOT NULL,
    open          DOUBLE PRECISION NOT NULL,
    high          DOUBLE PRECISION NOT NULL,
    low           DOUBLE PRECISION NOT NULL,
    close         DOUBLE PRECISION NOT NULL,
    volume        BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (instrument_id, candle_time)
);
