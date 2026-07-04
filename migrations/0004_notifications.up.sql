-- Module 7: per-user Telegram config + signal delivery ledger (fan-out).

CREATE TABLE telegram_configs (
    user_id    UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    bot_token  TEXT NOT NULL DEFAULT '',
    chat_id    TEXT NOT NULL DEFAULT '',
    enabled    BOOLEAN NOT NULL DEFAULT TRUE,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- One row per (user, stock, timeframe, day) that was delivered. The UNIQUE
-- constraint enforces the dedup rule: the same stock + timeframe + candle day
-- is never sent twice to a user; different timeframes are separate rows and DO
-- get sent.
CREATE TABLE signal_deliveries (
    id            BIGSERIAL PRIMARY KEY,
    signal_id     BIGINT NOT NULL REFERENCES signals(id) ON DELETE CASCADE,
    user_id       UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    instrument_id BIGINT NOT NULL,
    timeframe     TEXT NOT NULL,
    candle_date   DATE NOT NULL,
    channel       TEXT NOT NULL DEFAULT 'telegram',
    delivered_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, instrument_id, timeframe, candle_date)
);
CREATE INDEX signal_deliveries_user_idx ON signal_deliveries (user_id);
