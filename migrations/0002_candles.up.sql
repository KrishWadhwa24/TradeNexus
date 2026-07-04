-- Module 3: candle storage. Daily is the source of truth; weekly/monthly are
-- derived (materialized) by aggregating daily bars.

CREATE TABLE daily_candles (
    instrument_id BIGINT NOT NULL REFERENCES instruments(id) ON DELETE CASCADE,
    trade_date    DATE   NOT NULL,
    open          DOUBLE PRECISION NOT NULL,
    high          DOUBLE PRECISION NOT NULL,
    low           DOUBLE PRECISION NOT NULL,
    close         DOUBLE PRECISION NOT NULL,
    volume        BIGINT NOT NULL DEFAULT 0,
    PRIMARY KEY (instrument_id, trade_date)
);

CREATE TABLE weekly_candles (
    instrument_id BIGINT NOT NULL REFERENCES instruments(id) ON DELETE CASCADE,
    period_start  DATE   NOT NULL,   -- first trading day of the ISO week
    period_end    DATE   NOT NULL,   -- last trading day seen in the week
    open          DOUBLE PRECISION NOT NULL,
    high          DOUBLE PRECISION NOT NULL,
    low           DOUBLE PRECISION NOT NULL,
    close         DOUBLE PRECISION NOT NULL,
    volume        BIGINT NOT NULL DEFAULT 0,
    is_confirmed  BOOLEAN NOT NULL DEFAULT FALSE,
    PRIMARY KEY (instrument_id, period_start)
);

CREATE TABLE monthly_candles (
    instrument_id BIGINT NOT NULL REFERENCES instruments(id) ON DELETE CASCADE,
    period_start  DATE   NOT NULL,   -- first trading day of the calendar month
    period_end    DATE   NOT NULL,
    open          DOUBLE PRECISION NOT NULL,
    high          DOUBLE PRECISION NOT NULL,
    low           DOUBLE PRECISION NOT NULL,
    close         DOUBLE PRECISION NOT NULL,
    volume        BIGINT NOT NULL DEFAULT 0,
    is_confirmed  BOOLEAN NOT NULL DEFAULT FALSE,
    PRIMARY KEY (instrument_id, period_start)
);
