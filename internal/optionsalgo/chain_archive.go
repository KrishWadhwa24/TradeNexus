package optionsalgo

import (
	"context"
	"time"

	"tradenexus/internal/market"
)

// snapshotRows converts one built chain into storable rows stamped at `at`,
// truncated to the minute so the primary key lands on a clean 1-minute grid
// (and re-archiving within the same minute upserts rather than duplicating).
//
// Greeks become NULL when absent rather than 0: Angel's Greeks endpoint is
// live-computed and unavailable outside market hours, and a stored 0 delta
// would be indistinguishable from a genuine deep-OTM 0 to anything reading
// this back — including a future backtest's strike selection.
func snapshotRows(chain []OptionQuote, at time.Time) []ChainSnapshot {
	minute := at.In(market.IST).Truncate(time.Minute)
	rows := make([]ChainSnapshot, 0, len(chain))
	for _, q := range chain {
		s := ChainSnapshot{
			InstrumentID: q.InstrumentID,
			SnapshotTime: minute,
			LTP:          q.LTP,
			Bid:          q.Bid,
			Ask:          q.Ask,
			Volume:       q.Volume,
			OpenInterest: q.OpenInterest,
		}
		// Greeks arrive as a set — either the Greeks call succeeded for this
		// strike or it didn't, so one non-zero value is enough to tell a
		// real (possibly partly-zero) reading from an absent one.
		if q.Delta != 0 || q.IV != 0 || q.Theta != 0 || q.Gamma != 0 || q.Vega != 0 {
			d, g, th, v, iv := q.Delta, q.Gamma, q.Theta, q.Vega, q.IV
			s.Delta, s.Gamma, s.Theta, s.Vega, s.IV = &d, &g, &th, &v, &iv
		}
		rows = append(rows, s)
	}
	return rows
}

// ArchiveChainSnapshot captures the current option chain to
// option_chain_snapshots — the historical record a future backtest needs.
// Angel's history API serves OHLCV only, and only for ~21 trading days, so
// bid/ask/open-interest/Greeks are unrecoverable once the moment passes:
// this is the only place they are ever written down.
//
// Deliberately returns nothing. Archival is strictly a side activity — a
// failure here (DB blip, Greeks outage, empty chain) must never propagate
// into, or abort, the trading tick that calls it.
func (s *Service) ArchiveChainSnapshot(ctx context.Context) {
	_, inputs, err := s.EvaluateDirection(ctx)
	if err != nil {
		s.log.Warn().Err(err).Msg("chain archive: direction/spot unavailable, skipping this minute")
		return
	}
	chain, err := s.BuildOptionChain(ctx, inputs.Spot)
	if err != nil {
		s.log.Warn().Err(err).Msg("chain archive: chain build failed, skipping this minute")
		return
	}
	rows := snapshotRows(chain, time.Now())
	n, err := s.repo.InsertChainSnapshot(ctx, rows)
	if err != nil {
		s.log.Error().Err(err).Msg("chain archive: insert failed")
		return
	}
	s.log.Debug().Int("contracts", n).Msg("chain archive: snapshot stored")
}
