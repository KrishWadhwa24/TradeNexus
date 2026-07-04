package angel

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func TestFetchScripMaster_RetryAndFilter(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			http.Error(w, "try again", http.StatusBadGateway)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"token":"3045","symbol":"SBIN-EQ","name":"SBIN","expiry":"","lotsize":"1","instrumenttype":"","exch_seg":"NSE"},
			{"token":"500325","symbol":"RELIANCE","name":"RELIANCE","expiry":"","lotsize":"1","instrumenttype":"","exch_seg":"BSE"},
			{"token":"999","symbol":"NIFTY","name":"NIFTY","expiry":"","lotsize":"1","instrumenttype":"AMXIDX","exch_seg":"BSE"},
			{"token":"123","symbol":"NIFTY24JULFUT","name":"NIFTY","expiry":"25JUL2024","lotsize":"25","instrumenttype":"FUTIDX","exch_seg":"NFO"}
		]`))
	}))
	defer srv.Close()

	c := New(Config{
		ScripMasterURL:      srv.URL,
		ScripMasterTimeout:  2 * time.Second,
		ScripMasterAttempts: 2,
	}, nil, zerolog.Nop())

	scrips, err := c.FetchScripMaster(context.Background())
	if err != nil {
		t.Fatalf("FetchScripMaster: %v", err)
	}
	if calls != 2 {
		t.Fatalf("expected 2 calls after retry, got %d", calls)
	}
	if len(scrips) != 2 {
		t.Fatalf("expected 2 cash equity rows, got %d: %+v", len(scrips), scrips)
	}
	if scrips[0].ExchSeg != "NSE" || scrips[0].Symbol != "SBIN-EQ" {
		t.Errorf("unexpected NSE row: %+v", scrips[0])
	}
	if scrips[1].ExchSeg != "BSE" || scrips[1].Symbol != "RELIANCE" {
		t.Errorf("unexpected BSE row: %+v", scrips[1])
	}
}
