package investors

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// sampleFilingList mirrors the real corporate-share-holdings-master response
// shape (verified live against NSE).
const sampleFilingList = `[
	{"recordId":"213088","symbol":"LALITHAA","name":"Lalithaa Jewellery Mart Limited","isin":"INE0K9O01026","date":"21-AUG-2026","xbrl":"https://nsearchives.nseindia.com/corporate/xbrl/SHP_1.xml"},
	{"recordId":"213099","symbol":"ATULAUTO","name":"Atul Auto Limited","isin":"INE951D01028","date":"20-AUG-2026","xbrl":"https://nsearchives.nseindia.com/corporate/xbrl/SHP_2.xml"}
]`

// sampleXBRL is a synthetic but schema-accurate SHP filing (field names and
// the paired D_/instant context-id convention confirmed live against a real
// NSE filing) with three named shareholders: a tracked investor matched by
// exact personal name, one matched by entity alias, and an untracked one —
// exercising the name/alias matching alongside the context-pairing parse.
const sampleXBRL = `<?xml version="1.0" encoding="UTF-8"?>
<xbrli:xbrl xmlns:in-bse-shp="http://www.bseindia.com/xbrl/shp/2025-10-31/in-bse-shp" xmlns:xbrli="http://www.xbrl.org/2003/instance">
<xbrli:context id="MainD"></xbrli:context>
<xbrli:context id="MainI"></xbrli:context>
<xbrli:context id="D_IndividualsOrHUF_Context1"></xbrli:context>
<xbrli:context id="IndividualsOrHUF_Context1"></xbrli:context>
<xbrli:context id="D_OthersIndianShareholders_Context1"></xbrli:context>
<xbrli:context id="OthersIndianShareholders_Context1"></xbrli:context>
<xbrli:context id="D_IndividualsOrHUF_Context2"></xbrli:context>
<xbrli:context id="IndividualsOrHUF_Context2"></xbrli:context>
<in-bse-shp:Symbol contextRef="MainD">ATULAUTO</in-bse-shp:Symbol>
<in-bse-shp:NameOfTheCompany contextRef="MainD">Atul Auto Limited</in-bse-shp:NameOfTheCompany>
<in-bse-shp:ISIN contextRef="MainD">INE951D01028</in-bse-shp:ISIN>
<in-bse-shp:DateOfReport contextRef="MainI">2026-08-20</in-bse-shp:DateOfReport>
<in-bse-shp:NameOfTheShareholder contextRef="D_IndividualsOrHUF_Context1">Vijay Kedia</in-bse-shp:NameOfTheShareholder>
<in-bse-shp:NumberOfShares contextRef="IndividualsOrHUF_Context1">1500000</in-bse-shp:NumberOfShares>
<in-bse-shp:ShareholdingAsAPercentageOfTotalNumberOfShares contextRef="IndividualsOrHUF_Context1">0.0325</in-bse-shp:ShareholdingAsAPercentageOfTotalNumberOfShares>
<in-bse-shp:NameOfTheShareholder contextRef="D_OthersIndianShareholders_Context1">Kedia Securities Private Limited</in-bse-shp:NameOfTheShareholder>
<in-bse-shp:NumberOfShares contextRef="OthersIndianShareholders_Context1">800000</in-bse-shp:NumberOfShares>
<in-bse-shp:ShareholdingAsAPercentageOfTotalNumberOfShares contextRef="OthersIndianShareholders_Context1">0.0173</in-bse-shp:ShareholdingAsAPercentageOfTotalNumberOfShares>
<in-bse-shp:NameOfTheShareholder contextRef="D_IndividualsOrHUF_Context2">Some Unrelated Person</in-bse-shp:NameOfTheShareholder>
<in-bse-shp:NumberOfShares contextRef="IndividualsOrHUF_Context2">10000</in-bse-shp:NumberOfShares>
<in-bse-shp:ShareholdingAsAPercentageOfTotalNumberOfShares contextRef="IndividualsOrHUF_Context2">0.0002</in-bse-shp:ShareholdingAsAPercentageOfTotalNumberOfShares>
</xbrli:xbrl>`

func TestFetchFilings(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(sampleFilingList))
	}))
	defer srv.Close()

	c := NewClient()
	c.listBaseURL = srv.URL
	day, _ := time.Parse("2006-01-02", "2026-08-21")
	filings, err := c.FetchFilings(context.Background(), day, day)
	if err != nil {
		t.Fatalf("FetchFilings: %v", err)
	}
	if len(filings) != 2 {
		t.Fatalf("got %d filings, want 2", len(filings))
	}
	if filings[0].RecordID != "213088" || filings[0].Symbol != "LALITHAA" {
		t.Errorf("unexpected first filing: %+v", filings[0])
	}
	if filings[0].ReportDate.Format("2006-01-02") != "2026-08-21" {
		t.Errorf("unexpected report date: %v", filings[0].ReportDate)
	}
}

func TestFetchDetail(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(sampleXBRL))
	}))
	defer srv.Close()

	c := NewClient()
	d, err := c.FetchDetail(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("FetchDetail: %v", err)
	}
	if d.CompanyName != "Atul Auto Limited" || d.Symbol != "ATULAUTO" {
		t.Fatalf("unexpected company/symbol: %+v", d)
	}
	if len(d.Shareholders) != 3 {
		t.Fatalf("got %d shareholders, want 3", len(d.Shareholders))
	}

	byName := map[string]ShareholderHolding{}
	for _, sh := range d.Shareholders {
		byName[sh.Name] = sh
	}

	kedia := byName["Vijay Kedia"]
	if kedia.Shares != 1500000 {
		t.Errorf("unexpected Vijay Kedia shares: %+v", kedia)
	}
	if got := kedia.PctHolding; got < 3.24 || got > 3.26 {
		t.Errorf("PctHolding = %v, want ~3.25 (raw 0.0325 * 100)", got)
	}

	entity := byName["Kedia Securities Private Limited"]
	if entity.Shares != 800000 {
		t.Errorf("unexpected entity shares: %+v", entity)
	}
}
