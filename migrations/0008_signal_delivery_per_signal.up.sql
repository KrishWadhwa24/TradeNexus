-- Deliver each distinct signal once per user/channel.
--
-- The previous dedupe key was user + stock + timeframe + candle day, which
-- suppressed valid alerts when multiple scanners fired on the same stock,
-- timeframe, and candle date.

ALTER TABLE signal_deliveries
DROP CONSTRAINT IF EXISTS signal_deliveries_user_id_instrument_id_timeframe_candle_date_key;

CREATE UNIQUE INDEX IF NOT EXISTS signal_deliveries_user_signal_channel_uidx
ON signal_deliveries (user_id, signal_id, channel);
