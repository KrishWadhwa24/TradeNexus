-- Promoter/Director/KMP insider-trading tracker (source: NSE PIT disclosure
-- feed). Only tracked market buys/sells are stored — pledges, ESOPs, gifts,
-- off-market transfers and buybacks are filtered out before this table.
CREATE TABLE IF NOT EXISTS promoter_trades (
    id             TEXT PRIMARY KEY,             -- "{appId}:{contextRef}", unique per disclosure
    app_id         BIGINT NOT NULL,              -- NSE filing id (one filing can hold several disclosures)
    symbol         TEXT NOT NULL,
    company_name   TEXT NOT NULL,
    isin           TEXT NOT NULL DEFAULT '',
    person_name    TEXT NOT NULL DEFAULT '',
    category       TEXT NOT NULL,                -- raw NSE category, e.g. "Promoter Group", "Director"
    event_type     TEXT NOT NULL,                -- promoter_buy | promoter_sell | kmp_buy | kmp_sell
    mode           TEXT NOT NULL DEFAULT '',      -- "Market Purchase" | "Market Sale"
    quantity       BIGINT NOT NULL DEFAULT 0,
    value_inr      DOUBLE PRECISION NOT NULL DEFAULT 0,
    qty_before     BIGINT NOT NULL DEFAULT 0,
    pct_before     DOUBLE PRECISION NOT NULL DEFAULT 0,   -- percentage, e.g. 14.67
    qty_after      BIGINT NOT NULL DEFAULT 0,
    pct_after      DOUBLE PRECISION NOT NULL DEFAULT 0,
    trade_date_from DATE,
    trade_date_to   DATE,
    regulation     TEXT NOT NULL DEFAULT '',
    filing_url     TEXT NOT NULL DEFAULT '',      -- iXBRL viewer link
    broadcast_at   TIMESTAMPTZ NOT NULL,          -- NSE broadcastDateTime
    alerted        BOOLEAN NOT NULL DEFAULT false,
    alerted_at     TIMESTAMPTZ,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS promoter_trades_broadcast_idx ON promoter_trades (broadcast_at DESC);
CREATE INDEX IF NOT EXISTS promoter_trades_symbol_idx ON promoter_trades (symbol);

-- Filings we've already downloaded + parsed, so a poll never re-fetches the
-- same XBRL document twice. Short-lived — pruned independently of the
-- 60-day trade retention.
CREATE TABLE IF NOT EXISTS promoter_seen_filings (
    app_id  BIGINT PRIMARY KEY,
    seen_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
