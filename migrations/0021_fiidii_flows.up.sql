-- Daily NSE FII/DII cash-market buy/sell/net (₹ crore), one row per trade
-- date, so weekly/monthly net-positive-vs-negative trend can be computed.
CREATE TABLE IF NOT EXISTS fiidii_flows (
    trade_date DATE NOT NULL PRIMARY KEY,
    dii_buy    DOUBLE PRECISION NOT NULL,
    dii_sell   DOUBLE PRECISION NOT NULL,
    dii_net    DOUBLE PRECISION NOT NULL,
    fii_buy    DOUBLE PRECISION NOT NULL,
    fii_sell   DOUBLE PRECISION NOT NULL,
    fii_net    DOUBLE PRECISION NOT NULL,
    fetched_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
