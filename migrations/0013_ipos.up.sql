-- IPO tracker. Holds ONLY open + upcoming IPOs (source: InvestorGain GMP feed).
-- Closed/listed IPOs are pruned on every poll — no history is kept.
CREATE TABLE IF NOT EXISTS ipos (
CREATE TABLE ipos (
    id           BIGINT PRIMARY KEY,          -- InvestorGain ~id (stable per IPO)
    name         TEXT NOT NULL,
    board        TEXT NOT NULL DEFAULT '',    -- "IPO" | "BSE SME" | "NSE SME"
    category     TEXT NOT NULL DEFAULT '',    -- "IPO" | "SME"
    status       TEXT NOT NULL,               -- "open" | "upcoming"
    gmp          DOUBLE PRECISION NOT NULL DEFAULT 0,   -- grey-market premium (₹)
    gmp_percent  DOUBLE PRECISION NOT NULL DEFAULT 0,
    subscription TEXT NOT NULL DEFAULT '',    -- e.g. "1.31x" or "-"
    price        TEXT NOT NULL DEFAULT '',    -- issue price band/value
    ipo_size     TEXT NOT NULL DEFAULT '',    -- e.g. "170.00 Cr"
    lot          TEXT NOT NULL DEFAULT '',
    pe           TEXT NOT NULL DEFAULT '',
    rating       INT  NOT NULL DEFAULT 0,     -- 1..5 (fire icons)
    open_date    DATE,
    close_date   DATE,
    boa_date     DATE,
    listing_date DATE,
    url          TEXT NOT NULL DEFAULT '',
    updated_on   TEXT NOT NULL DEFAULT '',    -- source's "last updated" label
    -- Auto-signal state so the last-day alert is sent once (upgrade-only).
    -- '' | your_choice | apply | admin_apply
    signal_tier  TEXT NOT NULL DEFAULT '',
    signaled_at  TIMESTAMPTZ,
    last_polled  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS ipos_status_close_idx ON ipos (status, close_date);
CREATE INDEX ipos_status_close_idx ON ipos (status, close_date);
