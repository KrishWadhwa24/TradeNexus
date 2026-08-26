-- Permanent, incrementally-accumulated per-(fund,symbol) position, built
-- from mutual-fund client rows in market_deals as they're ingested. Survives
-- market_deals' own retention prune (DEALS_RETENTION_DAYS) — a fund's
-- position keeps growing/shrinking with each new buy/sell even after the
-- underlying raw deal rows that built it have been pruned away.
CREATE TABLE IF NOT EXISTS mutual_fund_positions (
    fund_name       TEXT NOT NULL,
    symbol          TEXT NOT NULL,
    security_name   TEXT NOT NULL DEFAULT '',
    buy_qty         BIGINT NOT NULL DEFAULT 0,
    sell_qty        BIGINT NOT NULL DEFAULT 0,
    buy_value       DOUBLE PRECISION NOT NULL DEFAULT 0,
    sell_value      DOUBLE PRECISION NOT NULL DEFAULT 0,
    deal_count      INT NOT NULL DEFAULT 0,
    first_deal_date DATE NOT NULL,
    last_deal_date  DATE NOT NULL,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (fund_name, symbol)
);
CREATE INDEX IF NOT EXISTS mutual_fund_positions_fund_idx ON mutual_fund_positions (fund_name);
