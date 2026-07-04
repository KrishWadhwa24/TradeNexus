package angel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"tradenexus/internal/market"
)

// Interval constants (only ONE_DAY is used — higher TFs are derived locally).
const IntervalOneDay = "ONE_DAY"

const angelDateLayout = "2006-01-02 15:04"

// GetDailyCandles fetches daily OHLCV for [from, to]. It blocks on the shared
// rate limiter first so concurrent callers never exceed Angel's budget.
//
// Angel caps ONE_DAY at ~2000 candles per request; keep ranges within that.
func (c *Client) GetDailyCandles(ctx context.Context, exchange, symbolToken string, from, to time.Time) ([]market.Candle, error) {
	if err := c.ensureLogin(ctx); err != nil {
		return nil, err
	}
	if c.limiter != nil {
		if err := c.limiter.Wait(ctx, 1); err != nil {
			return nil, fmt.Errorf("angel historical rate wait: %w", err)
		}
	}

	reqBody, _ := json.Marshal(histRequest{
		Exchange:    exchange,
		SymbolToken: symbolToken,
		Interval:    IntervalOneDay,
		FromDate:    from.In(market.IST).Format(angelDateLayout),
		ToDate:      to.In(market.IST).Format(angelDateLayout),
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.cfg.APIBaseURL+pathHistorical, bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	c.commonHeaders(req)
	req.Header.Set("Authorization", "Bearer "+c.jwt())

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("angel historical: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)

	var hr histResponse
	if err := json.Unmarshal(raw, &hr); err != nil {
		return nil, fmt.Errorf("angel historical decode (status %d): %w", resp.StatusCode, err)
	}
	if !hr.Status {
		return nil, fmt.Errorf("angel historical failed: %s (%s)", hr.Message, hr.ErrorCode)
	}

	return parseCandles(hr.Data)
}

// parseCandles maps Angel's [ts, o, h, l, c, v] rows into market.Candle values.
func parseCandles(rows [][]interface{}) ([]market.Candle, error) {
	out := make([]market.Candle, 0, len(rows))
	for i, r := range rows {
		if len(r) < 6 {
			return nil, fmt.Errorf("candle row %d malformed: %v", i, r)
		}
		tsStr, ok := r[0].(string)
		if !ok {
			return nil, fmt.Errorf("candle row %d: bad timestamp %v", i, r[0])
		}
		t, err := time.Parse(time.RFC3339, tsStr)
		if err != nil {
			return nil, fmt.Errorf("candle row %d: parse time %q: %w", i, tsStr, err)
		}
		t = t.In(market.IST)
		day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, market.IST)

		out = append(out, market.Candle{
			Time:   day,
			Open:   toFloat(r[1]),
			High:   toFloat(r[2]),
			Low:    toFloat(r[3]),
			Close:  toFloat(r[4]),
			Volume: int64(toFloat(r[5])),
		})
	}
	return out, nil
}

// toFloat coerces a JSON number (float64) or numeric string to float64.
func toFloat(v interface{}) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case json.Number:
		f, _ := n.Float64()
		return f
	default:
		return 0
	}
}
