package angel

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rs/zerolog"
)

func TestParseCandles(t *testing.T) {
	rows := [][]interface{}{
		{"2024-01-01T00:00:00+05:30", 100.0, 110.0, 95.0, 105.0, 1000.0},
		{"2024-01-02T00:00:00+05:30", 105.0, 112.0, 101.0, 108.0, 1200.0},
	}
	cs, err := parseCandles(rows)
	if err != nil {
		t.Fatalf("parseCandles: %v", err)
	}
	if len(cs) != 2 {
		t.Fatalf("expected 2 candles, got %d", len(cs))
	}
	if cs[0].Open != 100 || cs[0].Close != 105 || cs[0].Volume != 1000 {
		t.Errorf("row 0 mismatch: %+v", cs[0])
	}
	if cs[0].Time.Year() != 2024 || cs[0].Time.Month() != 1 || cs[0].Time.Day() != 1 {
		t.Errorf("row 0 date mismatch: %v", cs[0].Time)
	}
}

func TestParseCandles_Malformed(t *testing.T) {
	if _, err := parseCandles([][]interface{}{{"only-two", 1.0}}); err == nil {
		t.Fatal("expected error for malformed row")
	}
}

func TestGetDailyCandles_HTTP(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			t.Error("missing Authorization header")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"status": true, "message": "SUCCESS", "errorcode": "",
			"data": [
				["2024-01-01T00:00:00+05:30", 100.0, 110.0, 95.0, 105.0, 1000.0],
				["2024-01-02T00:00:00+05:30", 105.0, 112.0, 101.0, 108.0, 1200.0]
			]
		}`))
	}))
	defer srv.Close()

	c := New(Config{APIBaseURL: srv.URL}, nil, zerolog.Nop())
	// Pretend we're already authenticated so ensureLogin is a no-op.
	c.mu.Lock()
	c.tokens = tokenData{JWTToken: "test-jwt"}
	c.tokenTime = time.Now()
	c.mu.Unlock()

	cs, err := c.GetDailyCandles(context.Background(), "NSE", "3045",
		time.Now().AddDate(0, 0, -5), time.Now())
	if err != nil {
		t.Fatalf("GetDailyCandles: %v", err)
	}
	if len(cs) != 2 {
		t.Fatalf("expected 2 candles, got %d", len(cs))
	}
}
