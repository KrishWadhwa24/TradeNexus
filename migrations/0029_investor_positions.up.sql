-- Tracked big-investor holdings, one row per (investor, symbol) — the latest
-- disclosed shareholding-pattern snapshot for a curated list of well-known
-- Indian investors (see internal/investors.Tracked). Unlike promoter/mutual
-- fund positions this isn't accumulated from buy/sell deltas: NSE's
-- corporate-share-holdings-master feed reports a point-in-time %-holding
-- snapshot each quarter, so a row is simply overwritten by a newer
-- report_date (see Repo.UpsertHolding).
CREATE TABLE IF NOT EXISTS investor_positions (
    investor_key    TEXT NOT NULL,
    symbol          TEXT NOT NULL,
    investor_name   TEXT NOT NULL,
    company_name    TEXT NOT NULL DEFAULT '',
    shares          BIGINT NOT NULL DEFAULT 0,
    pct_holding     DOUBLE PRECISION NOT NULL DEFAULT 0,
    report_date     DATE NOT NULL,
    first_seen_date DATE NOT NULL,
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (investor_key, symbol)
);
CREATE INDEX IF NOT EXISTS investor_positions_symbol_idx ON investor_positions (symbol);

-- Filings already inspected, so a poll's catch-up window doesn't re-download
-- and re-parse the same XBRL document every run — same pattern as
-- promoter_seen_filings.
CREATE TABLE IF NOT EXISTS investor_seen_filings (
    record_id TEXT PRIMARY KEY,
    seen_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
