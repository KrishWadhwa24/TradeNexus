package angel

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
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
	if err := c.rateLimitWait(ctx); err != nil {
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

	raw, err := c.doHistoricalRequestWithRetry(ctx, reqBody)
	if err != nil {
		return nil, err
	}

	var hr histResponse
	if err := json.Unmarshal(raw, &hr); err != nil {
		return nil, fmt.Errorf("angel historical decode (body: %q): %w", bodyPreview(raw), err)
	}
	if !hr.Status {
		return nil, fmt.Errorf("angel historical failed: %s (%s)", hr.Message, hr.ErrorCode)
	}

	rows, err := decodeHistoricalData(hr.Data)
	if err != nil {
		return nil, fmt.Errorf("angel historical failed: %s", err)
	}
	return parseCandles(rows)
}

func (c *Client) rateLimitWait(ctx context.Context) error {
	if c.limiter != nil {
		if err := c.limiter.Wait(ctx, 1); err != nil {
			return fmt.Errorf("angel historical rate wait: %w", err)
		}
	}
	return nil
}

func (c *Client) doHistoricalRequestWithRetry(ctx context.Context, reqBody []byte) ([]byte, error) {
	const maxAttempts = 3
	var lastErr error

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		raw, status, err := c.doHistoricalRequest(ctx, reqBody)
		if err != nil {
			// This is a network error or client timeout.
			lastErr = err
			if attempt < maxAttempts {
				time.Sleep(time.Duration(attempt) * 500 * time.Millisecond) // Simple backoff
				continue
			}
			return nil, err
		}

		if status == http.StatusUnauthorized || status == http.StatusForbidden {
			if err := c.refreshOrLogin(ctx); err == nil {
				// After successful re-login, retry the request immediately.
				return c.doHistoricalRequestWithRetry(ctx, reqBody)
			}
			return nil, fmt.Errorf("angel auth failed and could not be refreshed")
		}

		if status >= http.StatusMultipleChoices {
			lastErr = fmt.Errorf("angel historical HTTP %d: %s", status, bodyPreview(raw))
			// Don't retry on definitive client/server errors (4xx/5xx) other than auth.
			return nil, lastErr
		}

		if len(raw) == 0 {
			lastErr = fmt.Errorf("angel historical received empty body with status %d", status)
			continue // Retry on empty body
		}

		return raw, nil // Success
	}
	return nil, fmt.Errorf("angel historical request failed after %d attempts: %w", maxAttempts, lastErr)
}

func (c *Client) doHistoricalRequest(ctx context.Context, reqBody []byte) ([]byte, int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.cfg.APIBaseURL+pathHistorical, bytes.NewReader(reqBody))
	if err != nil {
		return nil, 0, err
	}
	c.commonHeaders(req)
	req.Header.Set("Authorization", "Bearer "+c.jwt())

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("angel historical: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return raw, resp.StatusCode, nil
}

func (c *Client) refreshOrLogin(ctx context.Context) error {
	if err := c.RefreshTokens(ctx); err == nil {
		return nil
	}
	return c.Login(ctx)
}

func bodyPreview(raw []byte) string {
	const max = 240
	if len(raw) == 0 {
		return "empty response body"
	}
	if len(raw) > max {
		raw = raw[:max]
	}
	return string(raw)
}

func decodeHistoricalData(data json.RawMessage) ([][]interface{}, error) {
	var rows [][]interface{}
	if err := json.Unmarshal(data, &rows); err == nil {
		return rows, nil
	}

	var msg string
	if err := json.Unmarshal(data, &msg); err == nil {
		if msg == "" {
			return nil, errors.New("empty data")
		}
		return nil, errors.New(msg)
	}
	return nil, fmt.Errorf("unexpected data payload: %s", string(data))
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
	case string:
		f, _ := strconv.ParseFloat(n, 64)
		return f
	default:
		return 0
	}
}
