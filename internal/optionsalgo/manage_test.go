package optionsalgo

import "testing"

func TestInitialStop(t *testing.T) {
	if got := InitialStop(100, 20); !closeEnough(got, 80) {
		t.Errorf("InitialStop(100, 20) = %v, want 80", got)
	}
}

func baseManagementInputs() ManagementInputs {
	return ManagementInputs{
		EntryPrice:              100,
		CurrentPrice:            105,
		HighestPrice:            105,
		CurrentStop:             80, // InitialStop(100, 20)
		BreakevenTriggerPercent: 25,
		TrailingTriggerPercent:  40,
		TrailingDistancePercent: 25,
	}
}

func TestManagePosition_NoTriggerYet(t *testing.T) {
	in := baseManagementInputs() // profit = 5%, below breakeven trigger
	got := ManagePosition(in)
	if got.ShouldExit {
		t.Error("shouldn't exit — price well above the initial stop")
	}
	if got.NewStop != 80 {
		t.Errorf("stop shouldn't have moved yet, got %v", got.NewStop)
	}
}

func TestManagePosition_BreakevenTrigger(t *testing.T) {
	in := baseManagementInputs()
	in.CurrentPrice, in.HighestPrice = 126, 126 // profit = 26%, past 25% trigger
	got := ManagePosition(in)
	if got.NewStop != 100 {
		t.Errorf("expected stop moved to breakeven (100), got %v", got.NewStop)
	}
	if got.ShouldExit {
		t.Error("shouldn't exit — price is above the new breakeven stop")
	}
}

func TestManagePosition_TrailingTrigger(t *testing.T) {
	in := baseManagementInputs()
	in.CurrentPrice, in.HighestPrice = 141, 141 // profit = 41%, past 40% trigger
	got := ManagePosition(in)
	wantStop := 141 * 0.75 // trail 25% behind highest
	if !closeEnough(got.NewStop, wantStop) {
		t.Errorf("NewStop = %v, want %v (trailing 25%% behind highest 141)", got.NewStop, wantStop)
	}
}

func TestManagePosition_TrailingNeverLoosensStop(t *testing.T) {
	in := baseManagementInputs()
	// Price peaked at 200 (highest), pulled back to 145 now — still past
	// trailing trigger, but the trailing stop should be computed off the
	// HIGHEST price (200), not the current pullback price.
	in.HighestPrice = 200
	in.CurrentPrice = 145
	in.CurrentStop = 100 // already at breakeven from an earlier tick
	got := ManagePosition(in)
	wantStop := 200 * 0.75 // 150
	if !closeEnough(got.NewStop, wantStop) {
		t.Errorf("NewStop = %v, want %v (trail off highest-seen, not current price)", got.NewStop, wantStop)
	}
	if !got.ShouldExit {
		t.Error("current price 145 is below the new trailing stop 150 — should exit")
	}
}

func TestManagePosition_HighestPriceTracksUp(t *testing.T) {
	in := baseManagementInputs()
	in.HighestPrice = 105
	in.CurrentPrice = 110
	got := ManagePosition(in)
	if got.NewHighestPrice != 110 {
		t.Errorf("NewHighestPrice = %v, want 110 (new peak)", got.NewHighestPrice)
	}
}

func TestManagePosition_HighestPriceNeverDecreases(t *testing.T) {
	in := baseManagementInputs()
	in.HighestPrice = 150
	in.CurrentPrice = 130 // pulled back, but highest-seen must stay 150
	got := ManagePosition(in)
	if got.NewHighestPrice != 150 {
		t.Errorf("NewHighestPrice = %v, want 150 (must not decrease on a pullback)", got.NewHighestPrice)
	}
}

func TestManagePosition_MFEMAETracking(t *testing.T) {
	in := baseManagementInputs()
	in.MFE, in.MAE = 3, -2 // prior peaks from earlier ticks
	in.EntryPrice = 100
	in.CurrentPrice = 108 // excursion = +8, new MFE
	got := ManagePosition(in)
	if got.NewMFE != 8 {
		t.Errorf("NewMFE = %v, want 8 (new peak)", got.NewMFE)
	}
	if got.NewMAE != -2 {
		t.Errorf("NewMAE = %v, want -2 (unchanged, no new low)", got.NewMAE)
	}
}

func TestManagePosition_MFEMAE_NewLow(t *testing.T) {
	in := baseManagementInputs()
	in.MFE, in.MAE = 5, -1
	in.EntryPrice = 100
	in.CurrentPrice = 90 // excursion = -10, new MAE
	got := ManagePosition(in)
	if got.NewMAE != -10 {
		t.Errorf("NewMAE = %v, want -10 (new low)", got.NewMAE)
	}
	if got.NewMFE != 5 {
		t.Errorf("NewMFE = %v, want 5 (unchanged)", got.NewMFE)
	}
}

func TestManagePosition_ExitOnStopHit(t *testing.T) {
	in := baseManagementInputs()
	in.CurrentPrice = 79 // below the initial stop of 80
	in.HighestPrice = 105
	got := ManagePosition(in)
	if !got.ShouldExit {
		t.Error("expected exit — price fell below the initial stop")
	}
	if got.ExitReason == "" {
		t.Error("expected a non-empty exit reason")
	}
}
