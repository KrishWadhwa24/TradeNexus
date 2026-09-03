-- A DELIVERY position closed while the market is shut schedules the sell
-- instead of executing at a stale price — mirrors how a DELIVERY buy
-- already schedules for next open when the market's closed. NULL = nothing
-- pending; a value is how many shares of this OPEN position are queued to
-- close at the next market-open price.
ALTER TABLE paper_trades ADD COLUMN IF NOT EXISTS pending_close_qty INTEGER;
