package analytics

import (
	"strings"
	"testing"
	"time"
)

func TestBuildWorkbook(t *testing.T) {
	conf := 3
	stats := Stats{
		Total:          5,
		ByTimeframe:    map[string]int{"1W": 3, "1D": 2},
		BySource:       map[string]int{"pine": 2, "weekly": 3},
		ByDirection:    map[string]int{"BUY": 5},
		ByScanner:      map[string]int{"pine": 2, "weekly_1": 3},
		ConfidenceDist: map[string]int{"3": 3},
	}
	rows := []Row{
		{ID: 1, InstrumentID: 10, Symbol: "RELIANCE-EQ", Source: "weekly", Scanner: "weekly_1",
			Timeframe: "1W", Direction: "BUY", CandleDate: time.Now(), Confidence: &conf, CreatedAt: time.Now()},
	}
	data, err := BuildWorkbook(stats, rows)
	if err != nil {
		t.Fatalf("BuildWorkbook: %v", err)
	}
	if len(data) < 100 {
		t.Fatalf("workbook too small: %d bytes", len(data))
	}
	// .xlsx is a zip archive — must start with the PK signature.
	if data[0] != 'P' || data[1] != 'K' {
		t.Fatalf("expected xlsx (zip) magic PK, got %q", string(data[:2]))
	}
}

func TestContentDisposition(t *testing.T) {
	if !strings.Contains(ContentDisposition(), ".xlsx") {
		t.Errorf("content disposition should reference an .xlsx filename, got %q", ContentDisposition())
	}
}
