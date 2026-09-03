-- A SCHEDULED order (market closed) previously reserved nothing from
-- cash_balance until it actually filled at next market open — meaning
-- available cash didn't account for pending orders, so a user could
-- schedule more than they could actually afford. reserved_margin is the
-- amount debited immediately at schedule time (at the price then), refunded
-- on cancel, and trued-up against the actual fill price when it executes.
ALTER TABLE paper_trades ADD COLUMN IF NOT EXISTS reserved_margin DOUBLE PRECISION NOT NULL DEFAULT 0;
