-- Option-specific metadata, populated only for NFO/BFO derivative rows
-- (index options) — equities leave all four columns NULL. underlying_symbol
-- is stored on index-spot rows too (e.g. "NIFTY" on the Nifty 50 index
-- instrument itself), so a stock's option chain and its underlying's own
-- instrument row can both be found by the same underlying_symbol lookup.
ALTER TABLE instruments ADD COLUMN IF NOT EXISTS strike_price DOUBLE PRECISION;
ALTER TABLE instruments ADD COLUMN IF NOT EXISTS expiry_date DATE;
ALTER TABLE instruments ADD COLUMN IF NOT EXISTS option_type TEXT;
ALTER TABLE instruments ADD COLUMN IF NOT EXISTS underlying_symbol TEXT;

CREATE INDEX IF NOT EXISTS instruments_underlying_idx ON instruments (underlying_symbol) WHERE underlying_symbol IS NOT NULL;
