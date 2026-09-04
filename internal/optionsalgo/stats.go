package optionsalgo

import (
	"context"
	"time"

	"tradenexus/internal/paper"
)

// minTradesForStats is the script's own governance rule: don't tune
// parameters (or trust the stats enough to act on them) before this many
// trades have accumulated. Stats are still computed below that count —
// ComputeStats always returns real numbers — but callers (the frontend)
// should treat them as provisional and say so until TotalTrades reaches this.
const minTradesForStats = 30

// Stats is the performance breakdown the script asks for, computed over
// CLOSED options-algo trades. Deliberately a subset of the script's full
// wishlist (win rate, avg winner/loser, profit factor, expectancy, max
// drawdown, avg holding time, CE vs PE) — "trend vs range" and "time of
// entry" buckets need dimensions we don't tag on a trade today and are cut
// for now, not silently dropped.
type Stats struct {
	TotalTrades          int
	ReadyForTuning       bool // TotalTrades >= minTradesForStats
	WinRate              float64
	AvgWinner            float64
	AvgLoser             float64 // negative
	ProfitFactor         float64 // gross profit / gross loss; 0 if no losses
	Expectancy           float64 // average P&L per trade
	MaxDrawdown          float64 // largest peak-to-trough decline in cumulative P&L (positive number)
	AvgHoldingTime       time.Duration
	CETrades, PETrades   int
	CEWinRate, PEWinRate float64
}

// ComputeStats is pure — the caller (Service.Stats) is responsible for
// filtering `trades` down to CLOSED, source=options-algo rows first.
func ComputeStats(trades []paper.Trade) Stats {
	var s Stats
	s.TotalTrades = len(trades)
	if s.TotalTrades == 0 {
		return s
	}
	s.ReadyForTuning = s.TotalTrades >= minTradesForStats

	var wins, losses int
	var grossProfit, grossLoss, totalPnL float64
	var totalHold time.Duration
	var holdCount int
	var ceWins, peWins int

	cumPnL := 0.0
	peak := 0.0
	maxDD := 0.0

	for _, t := range trades {
		totalPnL += t.PnL
		if t.PnL >= 0 {
			wins++
			grossProfit += t.PnL
		} else {
			losses++
			grossLoss += -t.PnL
		}
		if t.OptionType == "CE" {
			s.CETrades++
			if t.PnL >= 0 {
				ceWins++
			}
		} else if t.OptionType == "PE" {
			s.PETrades++
			if t.PnL >= 0 {
				peWins++
			}
		}
		if t.EntryTime != nil && t.ExitTime != nil {
			totalHold += t.ExitTime.Sub(*t.EntryTime)
			holdCount++
		}

		cumPnL += t.PnL
		if cumPnL > peak {
			peak = cumPnL
		}
		if dd := peak - cumPnL; dd > maxDD {
			maxDD = dd
		}
	}

	s.WinRate = float64(wins) / float64(s.TotalTrades) * 100
	if wins > 0 {
		s.AvgWinner = grossProfit / float64(wins)
	}
	if losses > 0 {
		s.AvgLoser = -grossLoss / float64(losses)
	}
	if grossLoss > 0 {
		s.ProfitFactor = grossProfit / grossLoss
	}
	s.Expectancy = totalPnL / float64(s.TotalTrades)
	s.MaxDrawdown = maxDD
	if holdCount > 0 {
		s.AvgHoldingTime = totalHold / time.Duration(holdCount)
	}
	if s.CETrades > 0 {
		s.CEWinRate = float64(ceWins) / float64(s.CETrades) * 100
	}
	if s.PETrades > 0 {
		s.PEWinRate = float64(peWins) / float64(s.PETrades) * 100
	}
	return s
}

// Stats computes the performance breakdown for one user's CLOSED algo
// trades — filters paper.Trades down to source=options-algo, status=CLOSED
// before handing off to the pure ComputeStats.
func (s *Service) Stats(ctx context.Context, algoUserID string) (Stats, error) {
	trades, err := s.paper.Trades(ctx, algoUserID)
	if err != nil {
		return Stats{}, err
	}
	closed := make([]paper.Trade, 0, len(trades))
	for _, t := range trades {
		if t.Source == paper.SourceOptionsAlgo && t.Status == "CLOSED" {
			closed = append(closed, t)
		}
	}
	return ComputeStats(closed), nil
}
