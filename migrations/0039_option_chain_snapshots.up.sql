-- Per-minute option chain snapshots: the bid/ask/open-interest/Greeks that
-- Angel's historical API has never offered and never will. Unlike OHLCV
-- (recoverable for ~21 trading days via GetIntradayCandles), these fields
-- exist only for as long as we hold them, so this table is the sole record.
CREATE TABLE IF NOT EXISTS option_chain_snapshots (
    instrument_id BIGINT NOT NULL REFERENCES instruments(id) ON DELETE CASCADE,
    snapshot_time TIMESTAMPTZ NOT NULL,
    ltp           DOUBLE PRECISION NOT NULL DEFAULT 0,
    bid           DOUBLE PRECISION NOT NULL DEFAULT 0,
    ask           DOUBLE PRECISION NOT NULL DEFAULT 0,
    volume        BIGINT NOT NULL DEFAULT 0,
    open_interest DOUBLE PRECISION NOT NULL DEFAULT 0,
    -- Greeks are nullable on purpose: Angel's Greeks endpoint is
    -- live-computed and returns "No Data Available" outside market hours,
    -- and BuildOptionChain deliberately degrades to prices-only rather than
    -- failing. A snapshot with real prices and no Greeks is still worth
    -- keeping, and NULL distinguishes "unavailable" from a real 0.
    delta         DOUBLE PRECISION,
    gamma         DOUBLE PRECISION,
    theta         DOUBLE PRECISION,
    vega          DOUBLE PRECISION,
    iv            DOUBLE PRECISION,
    PRIMARY KEY (instrument_id, snapshot_time)
);

-- Backtests scan by time across the whole chain ("give me every contract at
-- 09:47 on this date"), which the (instrument_id, snapshot_time) primary key
-- can't serve efficiently.
CREATE INDEX IF NOT EXISTS option_chain_snapshots_time_idx
    ON option_chain_snapshots (snapshot_time);
