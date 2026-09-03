package promoter

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// sampleFilingList mirrors the real corporates-pit-gg response shape.
const sampleFilingList = `{"data":[
	{"appId":"1001","broadcastDateTime":"23-Jul-2026 17:54:07","companyName":"AVENUE SUPERMARTS LIMITED","symbol":"DMART","regulation":"Regulation 7 (2)","ixbrl":"https://nsearchives.nseindia.com/corporate/ixbrl/IT_1_WEB.html","xmlFileName":"https://nsearchives.nseindia.com/corporate/xbrl/PIT_1.xml"},
	{"appId":"1002","broadcastDateTime":"23-Jul-2026 16:05:26","companyName":"RAMCO INDUSTRIES LIMITED","symbol":"RAMCOIND","regulation":"Regulation 7 (2)","ixbrl":"https://nsearchives.nseindia.com/corporate/ixbrl/IT_2_WEB.html","xmlFileName":"https://nsearchives.nseindia.com/corporate/xbrl/PIT_2.xml"}
]}`

// sampleXBRL is a synthetic but schema-accurate PIT filing (field names and
// structure confirmed against real NSE filings) with three disclosures: a
// tracked promoter buy, an untracked Designated Person ESOP transfer, and a
// tracked KMP sell — exercising both the multi-disclosure grouping and the
// category/mode filtering in one document.
const sampleXBRL = `<?xml version="1.0" encoding="UTF-8"?>
<xbrli:xbrl xmlns:in-bse-co="http://www.bseindia.com/xbrl/co/2017-09-15/in-bse-co" xmlns:xbrli="http://www.xbrl.org/2003/instance">
<xbrli:context id="MainI"></xbrli:context>
<xbrli:context id="Disclosure1"></xbrli:context>
<xbrli:context id="Disclosure2"></xbrli:context>
<xbrli:context id="Disclosure3"></xbrli:context>
<in-bse-co:Symbol contextRef="MainI">DMART</in-bse-co:Symbol>
<in-bse-co:NameOfTheCompany contextRef="MainI">Avenue Supermarts Limited</in-bse-co:NameOfTheCompany>
<in-bse-co:ISINCode contextRef="MainI">INE192R01011</in-bse-co:ISINCode>
<in-bse-co:DisclosureUnderRegulation contextRef="MainI">Regulation 7 (2)</in-bse-co:DisclosureUnderRegulation>
<in-bse-co:CategoryOfPerson contextRef="Disclosure1">Promoter</in-bse-co:CategoryOfPerson>
<in-bse-co:NameOfThePerson contextRef="Disclosure1">Test Promoter</in-bse-co:NameOfThePerson>
<in-bse-co:ModeOfAcquisitionOrDisposal contextRef="Disclosure1">Market Purchase</in-bse-co:ModeOfAcquisitionOrDisposal>
<in-bse-co:SecuritiesAcquiredOrDisposedTransactionType contextRef="Disclosure1">Buy</in-bse-co:SecuritiesAcquiredOrDisposedTransactionType>
<in-bse-co:SecuritiesAcquiredOrDisposedNumberOfSecurity contextRef="Disclosure1">1000</in-bse-co:SecuritiesAcquiredOrDisposedNumberOfSecurity>
<in-bse-co:SecuritiesAcquiredOrDisposedValueOfSecurity contextRef="Disclosure1">4500000</in-bse-co:SecuritiesAcquiredOrDisposedValueOfSecurity>
<in-bse-co:SecuritiesHeldPriorToAcquisitionOrDisposalNumberOfSecurity contextRef="Disclosure1">100000</in-bse-co:SecuritiesHeldPriorToAcquisitionOrDisposalNumberOfSecurity>
<in-bse-co:SecuritiesHeldPriorToAcquisitionOrDisposalPercentageOfShareholding contextRef="Disclosure1">0.1234</in-bse-co:SecuritiesHeldPriorToAcquisitionOrDisposalPercentageOfShareholding>
<in-bse-co:SecuritiesHeldPostAcquistionOrDisposalNumberOfSecurity contextRef="Disclosure1">101000</in-bse-co:SecuritiesHeldPostAcquistionOrDisposalNumberOfSecurity>
<in-bse-co:SecuritiesHeldPostAcquistionOrDisposalPercentageOfShareholding contextRef="Disclosure1">0.1238</in-bse-co:SecuritiesHeldPostAcquistionOrDisposalPercentageOfShareholding>
<in-bse-co:DateOfAllotmentAdviceOrAcquisitionOfSharesOrSaleOfSharesSpecifyToDate contextRef="Disclosure1">2026-07-21</in-bse-co:DateOfAllotmentAdviceOrAcquisitionOfSharesOrSaleOfSharesSpecifyToDate>
<in-bse-co:CategoryOfPerson contextRef="Disclosure2">Designated Person</in-bse-co:CategoryOfPerson>
<in-bse-co:NameOfThePerson contextRef="Disclosure2">Test ESOP Holder</in-bse-co:NameOfThePerson>
<in-bse-co:ModeOfAcquisitionOrDisposal contextRef="Disclosure2">ESOP</in-bse-co:ModeOfAcquisitionOrDisposal>
<in-bse-co:SecuritiesAcquiredOrDisposedTransactionType contextRef="Disclosure2">Buy</in-bse-co:SecuritiesAcquiredOrDisposedTransactionType>
<in-bse-co:CategoryOfPerson contextRef="Disclosure3">KMP</in-bse-co:CategoryOfPerson>
<in-bse-co:NameOfThePerson contextRef="Disclosure3">Test KMP</in-bse-co:NameOfThePerson>
<in-bse-co:ModeOfAcquisitionOrDisposal contextRef="Disclosure3">Market Sale</in-bse-co:ModeOfAcquisitionOrDisposal>
<in-bse-co:SecuritiesAcquiredOrDisposedTransactionType contextRef="Disclosure3">Sell</in-bse-co:SecuritiesAcquiredOrDisposedTransactionType>
<in-bse-co:SecuritiesAcquiredOrDisposedNumberOfSecurity contextRef="Disclosure3">500</in-bse-co:SecuritiesAcquiredOrDisposedNumberOfSecurity>
<in-bse-co:SecuritiesAcquiredOrDisposedValueOfSecurity contextRef="Disclosure3">2250000</in-bse-co:SecuritiesAcquiredOrDisposedValueOfSecurity>
<in-bse-co:SecuritiesHeldPriorToAcquisitionOrDisposalPercentageOfShareholding contextRef="Disclosure3">0.05</in-bse-co:SecuritiesHeldPriorToAcquisitionOrDisposalPercentageOfShareholding>
<in-bse-co:SecuritiesHeldPostAcquistionOrDisposalPercentageOfShareholding contextRef="Disclosure3">0.048</in-bse-co:SecuritiesHeldPostAcquistionOrDisposalPercentageOfShareholding>
<in-bse-co:DateOfAllotmentAdviceOrAcquisitionOfSharesOrSaleOfSharesSpecifyToDate contextRef="Disclosure3">2026-07-20</in-bse-co:DateOfAllotmentAdviceOrAcquisitionOfSharesOrSaleOfSharesSpecifyToDate>
</xbrli:xbrl>`

func TestFetchFilings(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(sampleFilingList))
	}))
	defer srv.Close()

	c := NewClient()
	c.listBaseURL = srv.URL
	day, _ := time.Parse("2006-01-02", "2026-07-23")
	filings, err := c.FetchFilings(context.Background(), day, day)
	if err != nil {
		t.Fatalf("FetchFilings: %v", err)
	}
	if len(filings) != 2 {
		t.Fatalf("got %d filings, want 2", len(filings))
	}
	if filings[0].AppID != 1001 || filings[0].Symbol != "DMART" {
		t.Errorf("unexpected first filing: %+v", filings[0])
	}
	if filings[0].BroadcastAt.IsZero() {
		t.Error("expected broadcastAt to be parsed")
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
	if d.CompanyName != "Avenue Supermarts Limited" || d.Symbol != "DMART" {
		t.Fatalf("unexpected company/symbol: %+v", d)
	}
	if len(d.Disclosures) != 3 {
		t.Fatalf("got %d disclosures, want 3", len(d.Disclosures))
	}

	byCtx := map[string]Disclosure{}
	for _, disc := range d.Disclosures {
		byCtx[disc.ContextRef] = disc
	}

	promoterBuy := byCtx["Disclosure1"]
	if promoterBuy.Category != "Promoter" || promoterBuy.Mode != "Market Purchase" || promoterBuy.TransactionType != "Buy" {
		t.Errorf("unexpected promoter disclosure: %+v", promoterBuy)
	}
	if promoterBuy.Quantity != 1000 || promoterBuy.Value != 4500000 {
		t.Errorf("unexpected qty/value: %+v", promoterBuy)
	}
	if got := promoterBuy.PctBefore; got < 12.33 || got > 12.35 {
		t.Errorf("PctBefore = %v, want ~12.34 (raw 0.1234 * 100)", got)
	}
	if got := promoterBuy.PctAfter; got < 12.37 || got > 12.39 {
		t.Errorf("PctAfter = %v, want ~12.38", got)
	}
	if promoterBuy.DateTo == nil || promoterBuy.DateTo.Format("2006-01-02") != "2026-07-21" {
		t.Errorf("unexpected DateTo: %v", promoterBuy.DateTo)
	}

	esop := byCtx["Disclosure2"]
	if esop.Category != "Designated Person" || esop.Mode != "ESOP" {
		t.Errorf("unexpected esop disclosure: %+v", esop)
	}

	kmpSell := byCtx["Disclosure3"]
	if kmpSell.Category != "KMP" || kmpSell.Mode != "Market Sale" || kmpSell.TransactionType != "Sell" {
		t.Errorf("unexpected kmp disclosure: %+v", kmpSell)
	}
}
