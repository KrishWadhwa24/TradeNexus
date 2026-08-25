package digest

import (
	"bytes"
	"testing"
	"time"

	"tradenexus/internal/deals"
	"tradenexus/internal/promoter"
)

func TestBuildDigestPDF(t *testing.T) {
	promoters := []promoter.StockSummary{
		{Symbol: "PONNIERODE", CompanyName: "Ponni Sugars (Erode) Limited", FirstPct: 10, LatestPct: 13.37, PointIncrease: 3.37, LatestDate: time.Now()},
		{Symbol: "SUMEETINDS", CompanyName: "Sumeet Industries Limited", FirstPct: 8, LatestPct: 10.27, PointIncrease: 2.27, LatestDate: time.Now()},
		{Symbol: "DIAMONDYD", CompanyName: "Prataap Snacks Limited", FirstPct: 5, LatestPct: 7.08, PointIncrease: 2.08, LatestDate: time.Now()},
		{Symbol: "OBCL", CompanyName: "OBCL Limited", FirstPct: 12, LatestPct: 13.46, PointIncrease: 1.46, LatestDate: time.Now()},
		{Symbol: "CONFIPET", CompanyName: "Confidence Petroleum India Limited", FirstPct: 9, LatestPct: 10.26, PointIncrease: 1.26, LatestDate: time.Now()},
	}
	funds := []deals.FundAcquisition{
		{FundName: "SBI MUTUAL FUND", Symbol: "MANYAVAR", SecurityName: "Vedant Fashions Limited", BuyValue: 1947934930, LastDealDate: time.Now()},
		{FundName: "HSBC MUTUAL FUND", Symbol: "MEESHO", SecurityName: "Meesho Limited", BuyValue: 1296752000.01, LastDealDate: time.Now()},
		{FundName: "FRANKLIN TEMPLETON MUTUAL FUND", Symbol: "MEESHO", SecurityName: "Meesho Limited", BuyValue: 1282899200.01, LastDealDate: time.Now()},
		{FundName: "EDELWEISS MUTUAL FUND", Symbol: "MEESHO", SecurityName: "Meesho Limited", BuyValue: 1178117305.53, LastDealDate: time.Now()},
		{FundName: "CANARA ROBECO MUTUAL FUND", Symbol: "MEESHO", SecurityName: "Meesho Limited", BuyValue: 300015400.02, LastDealDate: time.Now()},
	}

	b, err := buildDigestPDF("Week of 19 Aug 2026", promoters, funds)
	if err != nil {
		t.Fatalf("buildDigestPDF: %v", err)
	}
	if len(b) == 0 {
		t.Fatal("buildDigestPDF returned empty bytes")
	}
	if !bytes.HasPrefix(b, []byte("%PDF-")) {
		t.Fatalf("output doesn't look like a PDF (missing %%PDF- header): %q", b[:min(20, len(b))])
	}
}

func TestBuildDigestPDFEmpty(t *testing.T) {
	// Both lists empty (e.g. a quiet week) must still render, not error.
	b, err := buildDigestPDF("Week of 19 Aug 2026", nil, nil)
	if err != nil {
		t.Fatalf("buildDigestPDF with empty data: %v", err)
	}
	if !bytes.HasPrefix(b, []byte("%PDF-")) {
		t.Fatal("empty-data output doesn't look like a PDF")
	}
}
