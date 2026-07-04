-- A fixed "system" user that owns catch-all (default safety-net chat) deliveries,
-- so the existing signal_deliveries UNIQUE(user_id, instrument, timeframe, day)
-- constraint dedups the default feed to one message per stock+timeframe+day.
INSERT INTO users (id, email)
VALUES ('00000000-0000-0000-0000-000000000000', 'system@tradenexus.local')
ON CONFLICT DO NOTHING;
