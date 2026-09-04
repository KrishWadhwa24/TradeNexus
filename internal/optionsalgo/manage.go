package optionsalgo

// InitialStop is the stop-loss level set once, right when a position opens —
// entryPrice reduced by the configured stop-loss %.
func InitialStop(entryPrice, stopLossPercent float64) float64 {
	return entryPrice * (1 - stopLossPercent/100)
}

// ManagementInputs bundles everything ManagePosition needs, all resolved
// fresh each tick. HighestPrice/CurrentStop are the persisted state from
// algo_position_state — the caller is responsible for loading them before
// this call and saving ManagementResult's updated values after.
type ManagementInputs struct {
	EntryPrice   float64
	CurrentPrice float64
	HighestPrice float64
	CurrentStop  float64
	// MFE/MAE are the running max favorable/adverse excursion (in price
	// points, currentPrice-entryPrice) seen so far — persisted state, same
	// as HighestPrice/CurrentStop.
	MFE, MAE float64

	BreakevenTriggerPercent float64
	TrailingTriggerPercent  float64
	TrailingDistancePercent float64
}

// ManagementResult carries the updated trailing state plus the exit
// decision — always returned together so the caller persists exactly what
// was actually evaluated, never a stale highest-price/stop pair.
type ManagementResult struct {
	NewHighestPrice float64
	NewStop         float64
	NewMFE, NewMAE  float64
	ShouldExit      bool
	ExitReason      string
}

// ManagePosition applies the script's exact stop/breakeven/trailing rules
// for one tick of an already-open long position:
//  1. track the highest price seen since entry
//  2. once profit >= BreakevenTriggerPercent, move the stop up to entry
//     price (never down — breakeven never loosens an already-tighter stop)
//  3. once profit >= TrailingTriggerPercent, trail the stop
//     TrailingDistancePercent behind the highest price seen (again, never
//     loosens an existing tighter stop)
//  4. exit if the current price has fallen to or below the (possibly
//     updated) stop
func ManagePosition(in ManagementInputs) ManagementResult {
	highest := in.HighestPrice
	if in.CurrentPrice > highest {
		highest = in.CurrentPrice
	}
	stop := in.CurrentStop

	excursion := in.CurrentPrice - in.EntryPrice
	mfe, mae := in.MFE, in.MAE
	if excursion > mfe {
		mfe = excursion
	}
	if excursion < mae {
		mae = excursion
	}

	profitPercent := excursion / in.EntryPrice * 100

	if profitPercent >= in.BreakevenTriggerPercent && in.EntryPrice > stop {
		stop = in.EntryPrice
	}
	if profitPercent >= in.TrailingTriggerPercent {
		trailingStop := highest * (1 - in.TrailingDistancePercent/100)
		if trailingStop > stop {
			stop = trailingStop
		}
	}

	if in.CurrentPrice <= stop {
		return ManagementResult{NewHighestPrice: highest, NewStop: stop, NewMFE: mfe, NewMAE: mae, ShouldExit: true, ExitReason: "stop hit"}
	}
	return ManagementResult{NewHighestPrice: highest, NewStop: stop, NewMFE: mfe, NewMAE: mae, ShouldExit: false}
}
