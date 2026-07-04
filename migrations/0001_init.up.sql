-- Module 1 foundational schema. Later modules add candles, indicators,
-- signals, deliveries, paper trading, etc. as their own migrations.

CREATE EXTENSION IF NOT EXISTS pgcrypto;   -- gen_random_uuid()

-- ── Users ───────────────────────────────────────────────────────────
CREATE TABLE users (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email         TEXT NOT NULL,
    password_hash TEXT NOT NULL DEFAULT '',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX users_email_lower_uidx ON users (lower(email));

-- ── Instruments (populated from Angel scrip master in Module 2) ──────
CREATE TABLE instruments (
    id             BIGSERIAL PRIMARY KEY,
    symbol_token   TEXT NOT NULL,
    exchange       TEXT NOT NULL,
    trading_symbol TEXT NOT NULL,
    name           TEXT NOT NULL DEFAULT '',
    lot_size       INTEGER NOT NULL DEFAULT 1,
    active         BOOLEAN NOT NULL DEFAULT TRUE,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (exchange, symbol_token)
);
-- Fast prefix/substring search for the watchlist autocomplete.
CREATE INDEX instruments_symbol_trgm_idx ON instruments (lower(trading_symbol) text_pattern_ops);
CREATE INDEX instruments_name_idx        ON instruments (lower(name) text_pattern_ops);

-- ── Watchlists ──────────────────────────────────────────────────────
CREATE TABLE watchlists (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, name)
);

CREATE TABLE watchlist_items (
    watchlist_id  UUID NOT NULL REFERENCES watchlists(id) ON DELETE CASCADE,
    instrument_id BIGINT NOT NULL REFERENCES instruments(id) ON DELETE CASCADE,
    added_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (watchlist_id, instrument_id)
);
-- Reverse lookup for fan-out: "which users watch this instrument?"
CREATE INDEX watchlist_items_instrument_idx ON watchlist_items (instrument_id);

-- ── Per-user scanner preferences (drives signal fan-out) ────────────
CREATE TABLE user_scanner_prefs (
    user_id     UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    scanner_key TEXT NOT NULL,   -- pine_1d|pine_1w|pine_1m|weekly_1..weekly_4
    enabled     BOOLEAN NOT NULL DEFAULT TRUE,
    PRIMARY KEY (user_id, scanner_key)
);
