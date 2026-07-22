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
	"strings"
	"time"

	"tradenexus/internal/market"
)

// maxHistAttempts bounds retries when Angel rejects a historical request with a
// transient rate-limit error (its server-side cap is stricter than our bucket).
const maxHistAttempts = 5

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

	reqBody, _ := json.Marshal(histRequest{
		Exchange:    exchange,
		SymbolToken: symbolToken,
		Interval:    IntervalOneDay,
		FromDate:    from.In(market.IST).Format(angelDateLayout),
		ToDate:      to.In(market.IST).Format(angelDateLayout),
	})

	var lastErr error
	for attempt := 1; attempt <= maxHistAttempts; attempt++ {
		if c.limiter != nil {
			if err := c.limiter.Wait(ctx, 1); err != nil {
				return nil, fmt.Errorf("angel historical rate wait: %w", err)
			}
		}

		raw, status, err := c.doHistoricalRequest(ctx, reqBody)
		if err != nil {
			return nil, err
		}
		// Angel throttling: HTTP 429, or a 403 whose body is a rate-limit
		// message (not an expired-auth 403) → back off and retry. This must be
		// checked BEFORE the auth-refresh branch below, since a 403 body of
		// "Access denied because of exceeding access rate" is not an auth
		// failure and re-logging in for it is pointless.
		if status == http.StatusTooManyRequests ||
			(status == http.StatusForbidden && isRateLimited(string(raw), "")) {
			lastErr = fmt.Errorf("angel historical HTTP %d (rate limited): %s", status, bodyPreview(raw))
			if attempt < maxHistAttempts && sleepBackoff(ctx, attempt) {
				continue
			}
			return nil, lastErr
		}
		if status == http.StatusUnauthorized || status == http.StatusForbidden {
			if err := c.refreshOrLogin(ctx); err == nil {
				raw, status, err = c.doHistoricalRequest(ctx, reqBody)
				if err != nil {
					return nil, err
				}
			}
		}
		if status < http.StatusOK || status >= http.StatusMultipleChoices {
			return nil, fmt.Errorf("angel historical HTTP %d: %s", status, bodyPreview(raw))
		}

		var hr histResponse
		if err := json.Unmarshal(raw, &hr); err != nil {
			return nil, fmt.Errorf("angel historical decode (status %d): %w", status, err)
		}
		if !hr.Status {
			// Angel's application-level rate error ("exceeding access rate",
			// errorcode AB1004) is transient — retry with backoff.
			if isRateLimited(hr.Message, hr.ErrorCode) {
				lastErr = fmt.Errorf("angel historical failed: %s (%s)", hr.Message, hr.ErrorCode)
				if attempt < maxHistAttempts && sleepBackoff(ctx, attempt) {
					continue
				}
			}
			return nil, fmt.Errorf("angel historical failed: %s (%s)", hr.Message, hr.ErrorCode)
		}

		rows, err := decodeHistoricalData(hr.Data)
		if err != nil {
			return nil, fmt.Errorf("angel historical failed: %s", err)
		}
		return parseCandles(rows)
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("angel historical: exhausted %d attempts", maxHistAttempts)
}

// isRateLimited reports whether an Angel error looks like throttling.
func isRateLimited(msg, code string) bool {
	m := strings.ToLower(msg)
	return code == "AB1004" ||
		strings.Contains(m, "access rate") ||
		strings.Contains(m, "rate limit") ||
		strings.Contains(m, "too many")
}

// sleepBackoff waits with an exponential ramp (5s, 10s, 20s, 40s, ...),
// respecting ctx. Returns false if cancelled. Angel's access-rate block seems
// to need real cooldown time, not just a 1-3s retry.
func sleepBackoff(ctx context.Context, attempt int) bool {
	wait := 5 * time.Second << (attempt - 1)
	t := time.NewTimer(wait)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
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
