package angel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

const pathLTP = "/rest/secure/angelbroking/order/v1/getLtpData"

type ltpRequest struct {
	Exchange      string `json:"exchange"`
	TradingSymbol string `json:"tradingsymbol"`
	SymbolToken   string `json:"symboltoken"`
}

type ltpResponse struct {
	Status  bool   `json:"status"`
	Message string `json:"message"`
	Data    struct {
		LTP float64 `json:"ltp"`
	} `json:"data"`
}

// GetLTP fetches the last traded price for a symbol. Rate-limited like other
// secure calls. Callers should fall back to the last daily close on error.
func (c *Client) GetLTP(ctx context.Context, exchange, tradingSymbol, symbolToken string) (float64, error) {
	if err := c.ensureLogin(ctx); err != nil {
		return 0, err
	}
	if c.limiter != nil {
		if err := c.limiter.Wait(ctx, 1); err != nil {
			return 0, err
		}
	}
	body, _ := json.Marshal(ltpRequest{Exchange: exchange, TradingSymbol: tradingSymbol, SymbolToken: symbolToken})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.APIBaseURL+pathLTP, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	c.commonHeaders(req)
	req.Header.Set("Authorization", "Bearer "+c.jwt())

	resp, err := c.http.Do(req)
	if err != nil {
		return 0, fmt.Errorf("angel ltp: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)

	var lr ltpResponse
	if err := json.Unmarshal(raw, &lr); err != nil {
		return 0, fmt.Errorf("angel ltp decode (status %d): %w", resp.StatusCode, err)
	}
	if !lr.Status || lr.Data.LTP <= 0 {
		return 0, fmt.Errorf("angel ltp unavailable: %s", lr.Message)
	}
	return lr.Data.LTP, nil
}
