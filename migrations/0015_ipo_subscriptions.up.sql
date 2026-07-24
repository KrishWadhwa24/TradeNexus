-- Per-category subscription snapshot (times subscribed), from InvestorGain's
-- subscription report. Zero until the IPO opens for bidding; overwritten on
-- every poll (a live snapshot, not history).
ALTER TABLE ipos
    ADD COLUMN IF NOT EXISTS qib DOUBLE PRECISION NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS shni DOUBLE PRECISION NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS bhni DOUBLE PRECISION NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS nii DOUBLE PRECISION NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS rii DOUBLE PRECISION NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS total_subscription DOUBLE PRECISION NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS anchor_positive BOOLEAN NOT NULL DEFAULT false;
