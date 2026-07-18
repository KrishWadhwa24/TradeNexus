-- Fix: migration 0008 tried to drop the old per-day dedup constraint using its
-- full generated name, but Postgres truncates identifiers to 63 characters, so
-- the real constraint name is truncated ("...candle_da_key", not
-- "...candle_date_key"). The 0008 DROP was therefore a silent no-op and the
-- stale UNIQUE (user_id, instrument_id, timeframe, candle_date) survived — it
-- blocks recording a second delivery when two different signals fire on the
-- same stock + timeframe + candle date (SQLSTATE 23505).
--
-- Drop it by its ACTUAL (truncated) name. Per-signal dedup lives on
-- signal_deliveries_user_signal_channel_uidx (added in 0008) and is unaffected.
ALTER TABLE signal_deliveries
DROP CONSTRAINT IF EXISTS signal_deliveries_user_id_instrument_id_timeframe_candle_da_key;

-- Defensive: also drop the full (untruncated) name in case a future/other
-- Postgres build named it differently.
ALTER TABLE signal_deliveries
DROP CONSTRAINT IF EXISTS signal_deliveries_user_id_instrument_id_timeframe_candle_date_key;
