package angel

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestParseCandles_NumericStrings(t *testing.T) {
	rows := [][]interface{}{
		{"2024-01-01T00:00:00+05:30", "100.5", "110.5", "95.5", "105.5", "1000"},
	}
	cs, err := parseCandles(rows)
	if err != nil {
		t.Fatalf("parseCandles: %v", err)
	}
	if cs[0].Open != 100.5 || cs[0].High != 110.5 || cs[0].Low != 95.5 || cs[0].Close != 105.5 || cs[0].Volume != 1000 {
		t.Fatalf("numeric strings were not parsed: %+v", cs[0])
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

func TestGetDailyCandles_DataString(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"status": true, "message": "SUCCESS", "errorcode": "",
			"data": "Invalid Token"
		}`))
	}))
	defer srv.Close()

	c := New(Config{APIBaseURL: srv.URL}, nil, zerolog.Nop())
	c.mu.Lock()
	c.tokens = tokenData{JWTToken: "test-jwt"}
	c.tokenTime = time.Now()
	c.mu.Unlock()

	_, err := c.GetDailyCandles(context.Background(), "NSE", "3045",
		time.Now().AddDate(0, 0, -5), time.Now())
	if err == nil {
		t.Fatal("expected data string error")
	}
	if !strings.Contains(err.Error(), "Invalid Token") {
		t.Fatalf("expected Angel data message, got %v", err)
	}
}

func TestGetDailyCandles_NonJSONHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Access denied", http.StatusForbidden)
	}))
	defer srv.Close()

	c := New(Config{APIBaseURL: srv.URL}, nil, zerolog.Nop())
	c.mu.Lock()
	c.tokens = tokenData{JWTToken: "test-jwt"}
	c.tokenTime = time.Now()
	c.mu.Unlock()

	_, err := c.GetDailyCandles(context.Background(), "NSE", "3045",
		time.Now().AddDate(0, 0, -5), time.Now())
	if err == nil {
		t.Fatal("expected HTTP error")
	}
	if !strings.Contains(err.Error(), "angel historical HTTP 403") ||
		!strings.Contains(err.Error(), "Access denied") {
		t.Fatalf("expected status and body preview, got %v", err)
	}
}
