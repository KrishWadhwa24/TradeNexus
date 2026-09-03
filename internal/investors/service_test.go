package investors

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/rs/zerolog"
)

// xbrlNaming returns a minimal, schema-accurate SHP filing (same shape as
// client_test.go's sampleXBRL) naming a single shareholder.
func xbrlNaming(symbol, company, reportDate, shareholderName string) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<xbrli:xbrl xmlns:in-bse-shp="http://www.bseindia.com/xbrl/shp/2025-10-31/in-bse-shp" xmlns:xbrli="http://www.xbrl.org/2003/instance">
<xbrli:context id="MainD"></xbrli:context>
<xbrli:context id="MainI"></xbrli:context>
<xbrli:context id="D_IndividualsOrHUF_Context1"></xbrli:context>
<xbrli:context id="IndividualsOrHUF_Context1"></xbrli:context>
<in-bse-shp:Symbol contextRef="MainD">%s</in-bse-shp:Symbol>
<in-bse-shp:NameOfTheCompany contextRef="MainD">%s</in-bse-shp:NameOfTheCompany>
<in-bse-shp:DateOfReport contextRef="MainI">%s</in-bse-shp:DateOfReport>
<in-bse-shp:NameOfTheShareholder contextRef="D_IndividualsOrHUF_Context1">%s</in-bse-shp:NameOfTheShareholder>
<in-bse-shp:NumberOfShares contextRef="IndividualsOrHUF_Context1">50000</in-bse-shp:NumberOfShares>
<in-bse-shp:ShareholdingAsAPercentageOfTotalNumberOfShares contextRef="IndividualsOrHUF_Context1">0.015</in-bse-shp:ShareholdingAsAPercentageOfTotalNumberOfShares>
</xbrli:xbrl>`, symbol, company, reportDate, shareholderName)
}

// TestPoll_RemovesInvestorNotNamedInNewerFiling is the end-to-end regression
// test for the exit-detection gap: a tracked investor (Dolly Khanna, a
// plain-name entry in Tracked with no alias) is named in one quarter's
// filing, then absent from the next quarter's filing for the same stock —
// Poll must remove her position, not just leave the stale q1 snapshot in
// place forever.
func TestPoll_RemovesInvestorNotNamedInNewerFiling(t *testing.T) {
	repo := testRepo(t)
	ctx := context.Background()
	symbol := "TESTSVCSTALE"
	t.Cleanup(func() {
		repo.pool.Exec(ctx, `DELETE FROM investor_positions WHERE symbol=$1`, symbol)
		repo.pool.Exec(ctx, `DELETE FROM investor_seen_filings WHERE record_id IN ('SVCTEST1','SVCTEST2')`)
	})

	detail1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(xbrlNaming(symbol, "Test Co", "2026-03-31", "Dolly Khanna")))
	}))
	defer detail1.Close()
	detail2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// q2's filing names someone else entirely — Dolly Khanna dropped out.
		w.Write([]byte(xbrlNaming(symbol, "Test Co", "2026-06-30", "Some Unrelated Person")))
	}))
	defer detail2.Close()

	var call int32
	listServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&call, 1)
		if n == 1 {
			fmt.Fprintf(w, `[{"recordId":"SVCTEST1","symbol":"%s","name":"Test Co","isin":"","date":"31-MAR-2026","xbrl":"%s"}]`, symbol, detail1.URL+"/f.xml")
			return
		}
		fmt.Fprintf(w, `[{"recordId":"SVCTEST2","symbol":"%s","name":"Test Co","isin":"","date":"30-JUN-2026","xbrl":"%s"}]`, symbol, detail2.URL+"/f.xml")
	}))
	defer listServer.Close()

	client := NewClient()
	client.listBaseURL = listServer.URL
	svc := New(client, repo, zerolog.Nop())

	if err := svc.Poll(ctx); err != nil {
		t.Fatalf("first Poll: %v", err)
	}
	if !holdingExists(t, repo, normalize("Dolly Khanna"), symbol) {
		t.Fatal("Dolly Khanna's q1 holding should be tracked after the first poll")
	}

	if err := svc.Poll(ctx); err != nil {
		t.Fatalf("second Poll: %v", err)
	}
	if holdingExists(t, repo, normalize("Dolly Khanna"), symbol) {
		t.Error("Dolly Khanna should have been removed — she wasn't named in the newer (q2) filing")
	}
}
