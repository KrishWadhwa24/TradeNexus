-- Delivery (full cash, hold indefinitely) vs intraday (20% margin, long or
-- short, auto-squared-off at the daily cutoff) — see internal/paper/intraday.go.
ALTER TABLE paper_trades ADD COLUMN IF NOT EXISTS product_type TEXT NOT NULL DEFAULT 'DELIVERY';
