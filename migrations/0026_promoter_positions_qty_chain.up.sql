-- promoter_positions previously picked "first"/"latest" stake purely by
-- each disclosure's self-reported trade_date_to, but NSE sometimes files a
-- batch of disclosures whose stated dates don't match the true execution
-- order implied by the qty_before/qty_after chain (each disclosure's
-- qty_after must equal the next one's qty_before, since it's cumulative
-- shareholding bookkeeping). These two columns let accumulatePosition
-- detect true chain endpoints instead of trusting dates.
ALTER TABLE promoter_positions ADD COLUMN IF NOT EXISTS first_qty_before BIGINT NOT NULL DEFAULT 0;
ALTER TABLE promoter_positions ADD COLUMN IF NOT EXISTS latest_qty_after BIGINT NOT NULL DEFAULT 0;
