CREATE TABLE algo_position_state (
    trade_id      BIGINT PRIMARY KEY, -- no FK: paper_trades isn't owned by this package,
                                       -- same read-only-borrow convention as algo_decisions.trade_id
    highest_price DOUBLE PRECISION NOT NULL,
    current_stop  DOUBLE PRECISION NOT NULL,
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
