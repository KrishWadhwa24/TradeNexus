package optionsalgo

import (
	"testing"
	"time"

	"tradenexus/internal/paper"
)

func mkClosedTrade(optionType string, pnl float64, entry, exit time.Time) paper.Trade {
	return paper.Trade{
		OptionType: optionType, PnL: pnl,
		EntryTime: timePtr(entry), ExitTime: timePtr(exit),
	}
}

func TestComputeStats_Empty(t *testing.T) {
	s := ComputeStats(nil)
	if s.TotalTrades != 0 || s.WinRate != 0 {
		t.Errorf("expected zero-value stats for no trades, got %+v", s)
	}
}

func TestComputeStats_WinRateAndAverages(t *testing.T) {
	base := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	trades := []paper.Trade{
		mkClosedTrade("CE", 1000, base, base.Add(30*time.Minute)),
		mkClosedTrade("CE", -400, base, base.Add(10*time.Minute)),
		mkClosedTrade("PE", 500, base, base.Add(20*time.Minute)),
		mkClosedTrade("PE", -200, base, base.Add(5*time.Minute)),
	}
	s := ComputeStats(trades)

	if s.TotalTrades != 4 {
		t.Errorf("TotalTrades = %d, want 4", s.TotalTrades)
	}
	if s.WinRate != 50 {
		t.Errorf("WinRate = %v, want 50 (2 of 4)", s.WinRate)
	}
	if !closeEnough(s.AvgWinner, 750) { // (1000+500)/2
		t.Errorf("AvgWinner = %v, want 750", s.AvgWinner)
	}
	if !closeEnough(s.AvgLoser, -300) { // -(400+200)/2
		t.Errorf("AvgLoser = %v, want -300", s.AvgLoser)
	}
	wantPF := 1500.0 / 600.0 // gross profit / gross loss
	if !closeEnough(s.ProfitFactor, wantPF) {
		t.Errorf("ProfitFactor = %v, want %v", s.ProfitFactor, wantPF)
	}
	wantExpectancy := (1000 - 400 + 500 - 200) / 4.0
	if !closeEnough(s.Expectancy, wantExpectancy) {
		t.Errorf("Expectancy = %v, want %v", s.Expectancy, wantExpectancy)
	}
	if s.CETrades != 2 || s.PETrades != 2 {
		t.Errorf("CE/PE counts = %d/%d, want 2/2", s.CETrades, s.PETrades)
	}
	if s.CEWinRate != 50 || s.PEWinRate != 50 {
		t.Errorf("CE/PE win rates = %v/%v, want 50/50", s.CEWinRate, s.PEWinRate)
	}
	wantHold := (30 + 10 + 20 + 5) * time.Minute / 4
	if s.AvgHoldingTime != wantHold {
		t.Errorf("AvgHoldingTime = %v, want %v", s.AvgHoldingTime, wantHold)
	}
}

func TestComputeStats_MaxDrawdown(t *testing.T) {
	base := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	// Cumulative P&L path: +1000, +1500 (peak), +500 (drawdown 1000), +900 (still down 600)
	trades := []paper.Trade{
		mkClosedTrade("CE", 1000, base, base),
		mkClosedTrade("CE", 500, base, base),
		mkClosedTrade("CE", -1000, base, base),
		mkClosedTrade("CE", 400, base, base),
	}
	s := ComputeStats(trades)
	if !closeEnough(s.MaxDrawdown, 1000) {
		t.Errorf("MaxDrawdown = %v, want 1000 (peak 1500 -> trough 500)", s.MaxDrawdown)
	}
}

func TestComputeStats_ReadyForTuningThreshold(t *testing.T) {
	base := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	var few []paper.Trade
	for i := 0; i < 10; i++ {
		few = append(few, mkClosedTrade("CE", 100, base, base))
	}
	if ComputeStats(few).ReadyForTuning {
		t.Error("10 trades should not be ReadyForTuning (threshold is 30)")
	}
	var many []paper.Trade
	for i := 0; i < 30; i++ {
		many = append(many, mkClosedTrade("CE", 100, base, base))
	}
	if !ComputeStats(many).ReadyForTuning {
		t.Error("30 trades should be ReadyForTuning")
	}
}
