package angel

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func expiryStr(daysFromNow int) string {
	return strings.ToUpper(time.Now().AddDate(0, 0, daysFromNow).Format("02Jan2006"))
}

// TestFetchIndexDerivatives_FiltersByWindowAndUnderlying is a regression test
// for two bugs found live: (1) the index-spot "symbol" field is an
// inconsistent display string ("Nifty 50", not "NIFTY") so matching must use
// "name" instead; (2) BANKNIFTY/FINNIFTY/BANKEX are monthly-only since 2024,
// with nearest expiries up to ~29 days out, so the near-dated window has to
// be wide enough to still catch them, not just NIFTY/SENSEX's weeklies.
func TestFetchIndexDerivatives_FiltersByWindowAndUnderlying(t *testing.T) {
	body := fmt.Sprintf(`[
		{"token":"35352","symbol":"NIFTY%[1]sCE","name":"NIFTY","expiry":"%[1]s","strike":"2500000.000000","lotsize":"65","instrumenttype":"OPTIDX","exch_seg":"NFO"},
		{"token":"53252","symbol":"BANKNIFTY%[2]sCE","name":"BANKNIFTY","expiry":"%[2]s","strike":"5000000.000000","lotsize":"35","instrumenttype":"OPTIDX","exch_seg":"NFO"},
		{"token":"99999","symbol":"NIFTY%[3]sCE","name":"NIFTY","expiry":"%[3]s","strike":"2500000.000000","lotsize":"65","instrumenttype":"OPTIDX","exch_seg":"NFO"},
		{"token":"11111","symbol":"RELIANCE24AUGFUT","name":"RELIANCE","expiry":"%[1]s","strike":"0","lotsize":"250","instrumenttype":"FUTSTK","exch_seg":"NFO"},
		{"token":"99926000","symbol":"Nifty 50","name":"NIFTY","expiry":"","strike":"","lotsize":"1","instrumenttype":"AMXIDX","exch_seg":"NSE"},
		{"token":"99919000","symbol":"SENSEX","name":"SENSEX","expiry":"","strike":"","lotsize":"1","instrumenttype":"AMXIDX","exch_seg":"BSE"},
		{"token":"1","symbol":"BADEXP","name":"NIFTY","expiry":"not-a-date","strike":"100","lotsize":"65","instrumenttype":"OPTIDX","exch_seg":"NFO"}
	]`, expiryStr(10), expiryStr(29), expiryStr(60))

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	c := New(Config{ScripMasterURL: srv.URL, ScripMasterTimeout: 2 * time.Second, ScripMasterAttempts: 1}, nil, zerolog.Nop())
	scrips, err := c.FetchIndexDerivatives(context.Background())
	if err != nil {
		t.Fatalf("FetchIndexDerivatives: %v", err)
	}

	byToken := map[string]Scrip{}
	for _, s := range scrips {
		byToken[s.Token] = s
	}

	if _, ok := byToken["35352"]; !ok {
		t.Error("NIFTY option 10 days out should be included")
	}
	if _, ok := byToken["53252"]; !ok {
		t.Error("BANKNIFTY option 29 days out should be included (monthly-only, wider window needed)")
	}
	if _, ok := byToken["99999"]; ok {
		t.Error("NIFTY option 60 days out should be excluded (outside near-dated window)")
	}
	if _, ok := byToken["11111"]; ok {
		t.Error("a stock future (FUTSTK) should never be included — index options/spots only")
	}
	if _, ok := byToken["1"]; ok {
		t.Error("a row with an unparseable expiry should be excluded, not crash or pass through")
	}
	if _, ok := byToken["99926000"]; !ok {
		t.Error("Nifty 50 index spot should be included even though its symbol field is \"Nifty 50\", not \"NIFTY\"")
	}
	if _, ok := byToken["99919000"]; !ok {
		t.Error("SENSEX index spot should be included")
	}
	if len(scrips) != 4 {
		t.Errorf("got %d scrips, want 4 (2 options + 2 index spots): %+v", len(scrips), scrips)
	}
}
