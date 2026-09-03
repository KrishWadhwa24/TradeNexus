DROP INDEX IF EXISTS instruments_underlying_idx;
ALTER TABLE instruments DROP COLUMN IF EXISTS strike_price;
ALTER TABLE instruments DROP COLUMN IF EXISTS expiry_date;
ALTER TABLE instruments DROP COLUMN IF EXISTS option_type;
ALTER TABLE instruments DROP COLUMN IF EXISTS underlying_symbol;
