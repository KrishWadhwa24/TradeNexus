package optionsalgo

import "context"

// AlgoConfig is every tunable value from the script, frontend-editable —
// nothing here is a hardcoded Go constant. Defaults (seeded by the
// migration) match the script's exact starting values.
type AlgoConfig struct {
	RiskPerTradePercent     float64
	MaxDailyLossPercent     float64
	MaxWeeklyLossPercent    float64
	InitialStopLossPercent  float64
	BreakevenTriggerPercent float64
	TrailingTriggerPercent  float64
	TrailingDistancePercent float64
	DeltaTarget             float64
	DeltaMin                float64
	DeltaMax                float64
	MaxSpreadPercent        float64
	MinVolumeMultiplier     float64
	EMAFastPeriod           int
	EMASlowPeriod           int
	ATRPeriod               int
	ATRAvgSpan              int
	ORStartHour             int
	ORStartMin              int
	OREndHour               int
	OREndMin                int
	ORMinRangePercent       float64
	MaxDistanceFromVWAPATR  float64
	StrikesEachSide         int
	MaxTradesPerDay         int
}

// GetConfig reads the single algo_config row — seeded by migration
// (0032_algo_config.up.sql), so this always finds exactly one row.
func (r *Repo) GetConfig(ctx context.Context) (AlgoConfig, error) {
	var c AlgoConfig
	err := r.pool.QueryRow(ctx, `
		SELECT risk_per_trade_percent, max_daily_loss_percent, max_weekly_loss_percent,
		       initial_stop_loss_percent, breakeven_trigger_percent, trailing_trigger_percent,
		       trailing_distance_percent, delta_target, delta_min, delta_max,
		       max_spread_percent, min_volume_multiplier, ema_fast_period, ema_slow_period,
		       atr_period, atr_avg_span, or_start_hour, or_start_min, or_end_hour, or_end_min,
		       or_min_range_percent, max_distance_from_vwap_atr, strikes_each_side, max_trades_per_day
		FROM algo_config WHERE id = 1`).Scan(
		&c.RiskPerTradePercent, &c.MaxDailyLossPercent, &c.MaxWeeklyLossPercent,
		&c.InitialStopLossPercent, &c.BreakevenTriggerPercent, &c.TrailingTriggerPercent,
		&c.TrailingDistancePercent, &c.DeltaTarget, &c.DeltaMin, &c.DeltaMax,
		&c.MaxSpreadPercent, &c.MinVolumeMultiplier, &c.EMAFastPeriod, &c.EMASlowPeriod,
		&c.ATRPeriod, &c.ATRAvgSpan, &c.ORStartHour, &c.ORStartMin, &c.OREndHour, &c.OREndMin,
		&c.ORMinRangePercent, &c.MaxDistanceFromVWAPATR, &c.StrikesEachSide, &c.MaxTradesPerDay,
	)
	return c, err
}

// UpdateConfig overwrites the single row — the frontend settings form always
// sends the full config back (mirroring what it read), avoiding partial-
// update ambiguity.
func (r *Repo) UpdateConfig(ctx context.Context, c AlgoConfig) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE algo_config SET
			risk_per_trade_percent=$1, max_daily_loss_percent=$2, max_weekly_loss_percent=$3,
			initial_stop_loss_percent=$4, breakeven_trigger_percent=$5, trailing_trigger_percent=$6,
			trailing_distance_percent=$7, delta_target=$8, delta_min=$9, delta_max=$10,
			max_spread_percent=$11, min_volume_multiplier=$12, ema_fast_period=$13, ema_slow_period=$14,
			atr_period=$15, atr_avg_span=$16, or_start_hour=$17, or_start_min=$18, or_end_hour=$19, or_end_min=$20,
			or_min_range_percent=$21, max_distance_from_vwap_atr=$22, strikes_each_side=$23, max_trades_per_day=$24,
			updated_at=now()
		WHERE id = 1`,
		c.RiskPerTradePercent, c.MaxDailyLossPercent, c.MaxWeeklyLossPercent,
		c.InitialStopLossPercent, c.BreakevenTriggerPercent, c.TrailingTriggerPercent,
		c.TrailingDistancePercent, c.DeltaTarget, c.DeltaMin, c.DeltaMax,
		c.MaxSpreadPercent, c.MinVolumeMultiplier, c.EMAFastPeriod, c.EMASlowPeriod,
		c.ATRPeriod, c.ATRAvgSpan, c.ORStartHour, c.ORStartMin, c.OREndHour, c.OREndMin,
		c.ORMinRangePercent, c.MaxDistanceFromVWAPATR, c.StrikesEachSide, c.MaxTradesPerDay,
	)
	return err
}
