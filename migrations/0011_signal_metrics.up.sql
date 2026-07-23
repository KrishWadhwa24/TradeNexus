-- Per-signal numeric metrics (body/ATR ratio, relative volume, EMA stack, etc.)
-- captured at scan time so Telegram alerts can show real figures, not booleans.
ALTER TABLE signals ADD COLUMN IF NOT EXISTS metrics JSONB NOT NULL DEFAULT '{}';
