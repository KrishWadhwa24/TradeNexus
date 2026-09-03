-- Add a 30-trading-day (≈1 month) forward-return horizon.
ALTER TABLE signal_outcomes ADD COLUMN IF NOT EXISTS ret_30d DOUBLE PRECISION;
