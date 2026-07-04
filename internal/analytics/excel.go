package analytics

import (
	"strconv"

	"github.com/xuri/excelize/v2"
)

// BuildWorkbook renders the analytics into an .xlsx with two sheets:
// "Summary" (aggregated counts) and "Signals" (the flattened rows).
func BuildWorkbook(stats Stats, rows []Row) ([]byte, error) {
	f := excelize.NewFile()
	defer f.Close()

	// --- Signals sheet ---
	const signalsSheet = "Signals"
	f.SetSheetName("Sheet1", signalsSheet)
	headers := []string{"ID", "Instrument ID", "Symbol", "Source", "Scanner", "Timeframe", "Direction", "Candle Date", "Confidence", "Created At"}
	for i, h := range headers {
		cell, _ := excelize.CoordinatesToCellName(i+1, 1)
		_ = f.SetCellValue(signalsSheet, cell, h)
	}
	for r, row := range rows {
		conf := ""
		if row.Confidence != nil {
			conf = strconv.Itoa(*row.Confidence) + "/4"
		}
		vals := []any{
			row.ID, row.InstrumentID, row.Symbol, row.Source, row.Scanner,
			row.Timeframe, row.Direction, row.CandleDate.Format("2006-01-02"),
			conf, row.CreatedAt.Format("2006-01-02 15:04:05"),
		}
		for c, v := range vals {
			cell, _ := excelize.CoordinatesToCellName(c+1, r+2)
			_ = f.SetCellValue(signalsSheet, cell, v)
		}
	}
	// Header style + autofilter.
	style, _ := f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}})
	_ = f.SetCellStyle(signalsSheet, "A1", "J1", style)
	_ = f.AutoFilter(signalsSheet, "A1:J1", []excelize.AutoFilterOptions{})
	_ = f.SetColWidth(signalsSheet, "C", "C", 18)
	_ = f.SetColWidth(signalsSheet, "H", "J", 20)

	// --- Summary sheet ---
	const summarySheet = "Summary"
	_, _ = f.NewSheet(summarySheet)
	writeSummary(f, summarySheet, stats)

	buf, err := f.WriteToBuffer()
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func writeSummary(f *excelize.File, sheet string, s Stats) {
	row := 1
	put := func(a, b string) {
		_ = f.SetCellValue(sheet, "A"+strconv.Itoa(row), a)
		_ = f.SetCellValue(sheet, "B"+strconv.Itoa(row), b)
		row++
	}
	put("Total signals", strconv.Itoa(s.Total))
	row++
	section := func(title string, m map[string]int) {
		_ = f.SetCellValue(sheet, "A"+strconv.Itoa(row), title)
		row++
		for k, v := range m {
			put(k, strconv.Itoa(v))
		}
		row++
	}
	section("By timeframe", s.ByTimeframe)
	section("By source", s.BySource)
	section("By direction", s.ByDirection)
	section("By scanner", s.ByScanner)

	_ = f.SetCellValue(sheet, "A"+strconv.Itoa(row), "Confidence distribution (N/4)")
	row++
	for k, v := range s.ConfidenceDist {
		put(k+"/4", strconv.Itoa(v))
	}
	_ = f.SetColWidth(sheet, "A", "A", 30)
}

// ContentDisposition returns the download header value.
func ContentDisposition() string {
	return `attachment; filename="tradenexus_analytics.xlsx"`
}
