-- Admin-curated stock list shown on the public landing page (pre-signin) and
-- to every user, independent of any individual watchlist or the algorithmic
-- top-movers ranking. rank controls display order (lower first).
CREATE TABLE IF NOT EXISTS featured_stocks (
    instrument_id BIGINT PRIMARY KEY REFERENCES instruments(id) ON DELETE CASCADE,
    rank          INT NOT NULL DEFAULT 0,
    added_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS featured_stocks_rank_idx ON featured_stocks (rank);
