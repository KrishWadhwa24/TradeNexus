CREATE TABLE algo_config (
    id INT PRIMARY KEY DEFAULT 1 CHECK (id = 1), -- singleton row
    risk_per_trade_percent      DOUBLE PRECISION NOT NULL DEFAULT 1.0,
    max_daily_loss_percent      DOUBLE PRECISION NOT NULL DEFAULT 2.0,
    max_weekly_loss_percent     DOUBLE PRECISION NOT NULL DEFAULT 5.0,
    initial_stop_loss_percent   DOUBLE PRECISION NOT NULL DEFAULT 20.0,
    breakeven_trigger_percent   DOUBLE PRECISION NOT NULL DEFAULT 25.0,
    trailing_trigger_percent    DOUBLE PRECISION NOT NULL DEFAULT 40.0,
    trailing_distance_percent   DOUBLE PRECISION NOT NULL DEFAULT 25.0,
    delta_target                DOUBLE PRECISION NOT NULL DEFAULT 0.60,
    delta_min                   DOUBLE PRECISION NOT NULL DEFAULT 0.55,
    delta_max                   DOUBLE PRECISION NOT NULL DEFAULT 0.70,
    max_spread_percent          DOUBLE PRECISION NOT NULL DEFAULT 1.0,
    min_volume_multiplier       DOUBLE PRECISION NOT NULL DEFAULT 1.2,
    ema_fast_period             INT NOT NULL DEFAULT 20,
    ema_slow_period             INT NOT NULL DEFAULT 50,
    atr_period                  INT NOT NULL DEFAULT 14,
    atr_avg_span                INT NOT NULL DEFAULT 20,
    or_start_hour                INT NOT NULL DEFAULT 9,
    or_start_min                 INT NOT NULL DEFAULT 15,
    or_end_hour                  INT NOT NULL DEFAULT 9,
    or_end_min                   INT NOT NULL DEFAULT 45,
    or_min_range_percent         DOUBLE PRECISION NOT NULL DEFAULT 0.15,
    max_distance_from_vwap_atr  DOUBLE PRECISION NOT NULL DEFAULT 1.5,
    strikes_each_side           INT NOT NULL DEFAULT 5,
    max_trades_per_day          INT NOT NULL DEFAULT 1,
    updated_at                  TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO algo_config (id) VALUES (1);
