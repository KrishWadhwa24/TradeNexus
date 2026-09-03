-- Permanent, incrementally-accumulated per-(person,symbol) promoter/KMP
-- stake position, built from promoter_trades disclosures as they're
-- ingested. Survives promoter_trades' own retention prune
-- (PROMOTER_RETENTION_DAYS, 60 days) — "stake when first observed" must
-- never be lost just because the disclosure that established it aged out.
CREATE TABLE IF NOT EXISTS promoter_positions (
    person_key       TEXT NOT NULL, -- normalized (upper, trimmed) person_name
    symbol           TEXT NOT NULL,
    person_name      TEXT NOT NULL DEFAULT '', -- display name
    company_name     TEXT NOT NULL DEFAULT '',
    category         TEXT NOT NULL DEFAULT '', -- most recent disclosure's raw category
    buy_qty          BIGINT NOT NULL DEFAULT 0,
    sell_qty         BIGINT NOT NULL DEFAULT 0,
    buy_value        DOUBLE PRECISION NOT NULL DEFAULT 0,
    sell_value       DOUBLE PRECISION NOT NULL DEFAULT 0,
    first_pct        DOUBLE PRECISION NOT NULL DEFAULT 0,
    first_date       DATE NOT NULL,
    latest_pct       DOUBLE PRECISION NOT NULL DEFAULT 0,
    latest_date      DATE NOT NULL,
    disclosure_count INT NOT NULL DEFAULT 0,
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (person_key, symbol)
);
CREATE INDEX IF NOT EXISTS promoter_positions_symbol_idx ON promoter_positions (symbol);
