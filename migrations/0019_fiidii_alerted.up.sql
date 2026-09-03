-- One row per trade_date that the FII/DII auto alert has been Telegram-sent
-- for, so a server restart after the daily send never double-sends.
CREATE TABLE IF NOT EXISTS fiidii_alerted (
    trade_date DATE NOT NULL PRIMARY KEY,
    alerted_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
