-- Bulk & block deals tracker (source: NSE historicalOR/bulk-block-short-deals
-- CSV feed). One row = one client's one side (BUY or SELL) of a deal in a stock
-- on a day. Cards/alerts aggregate these into per-client net positions.
CREATE TABLE IF NOT EXISTS market_deals (
    id            BIGSERIAL PRIMARY KEY,
    deal_type     TEXT NOT NULL,                       -- 'bulk' | 'block'
    deal_date     DATE NOT NULL,                       -- BD_DT_DATE
    symbol        TEXT NOT NULL,
    security_name TEXT NOT NULL DEFAULT '',
    client_name   TEXT NOT NULL,
    buy_sell      TEXT NOT NULL,                       -- 'BUY' | 'SELL'
    quantity      BIGINT NOT NULL DEFAULT 0,
    price         DOUBLE PRECISION NOT NULL DEFAULT 0, -- trade / wght. avg. price
    remarks       TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    -- NSE gives no row id, so the natural key dedups a re-fetched day.
    UNIQUE (deal_type, deal_date, symbol, client_name, buy_sell, quantity, price)
);
CREATE INDEX IF NOT EXISTS market_deals_type_date_idx ON market_deals (deal_type, deal_date DESC);
CREATE INDEX IF NOT EXISTS market_deals_type_symbol_idx ON market_deals (deal_type, symbol);

-- One row per (deal_type, deal_date, symbol) that has been Telegram-alerted, so
-- a re-run of the daily job (cron + startup catch-up) never double-sends.
CREATE TABLE IF NOT EXISTS market_deals_alerted (
    deal_type  TEXT NOT NULL,
    deal_date  DATE NOT NULL,
    symbol     TEXT NOT NULL,
    alerted_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (deal_type, deal_date, symbol)
);
