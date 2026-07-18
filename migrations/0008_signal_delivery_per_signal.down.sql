DROP INDEX IF EXISTS signal_deliveries_user_signal_channel_uidx;

ALTER TABLE signal_deliveries
ADD CONSTRAINT signal_deliveries_user_id_instrument_id_timeframe_candle_date_key
UNIQUE (user_id, instrument_id, timeframe, candle_date);
