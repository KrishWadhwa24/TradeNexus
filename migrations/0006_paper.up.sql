-- Module 9: paper trading.

CREATE TABLE paper_accounts (
    user_id          UUID PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    starting_capital DOUBLE PRECISION NOT NULL DEFAULT 0,
    cash_balance     DOUBLE PRECISION NOT NULL DEFAULT 0,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE paper_trades (
    id            BIGSERIAL PRIMARY KEY,
    user_id       UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    instrument_id BIGINT NOT NULL REFERENCES instruments(id) ON DELETE CASCADE,
    signal_id     BIGINT REFERENCES signals(id) ON DELETE SET NULL,
    side          TEXT NOT NULL DEFAULT 'BUY',
    quantity      INTEGER NOT NULL,
    entry_price   DOUBLE PRECISION NOT NULL DEFAULT 0,
    entry_time    TIMESTAMPTZ,
    exit_price    DOUBLE PRECISION,
    exit_time     TIMESTAMPTZ,
    status        TEXT NOT NULL DEFAULT 'OPEN',   -- OPEN | CLOSED | SCHEDULED
    pnl           DOUBLE PRECISION NOT NULL DEFAULT 0,
    source        TEXT NOT NULL DEFAULT 'web',    -- web | telegram
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX paper_trades_user_idx   ON paper_trades (user_id);
CREATE INDEX paper_trades_status_idx ON paper_trades (status);
