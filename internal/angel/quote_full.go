package angel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"tradenexus/internal/market"
)

const pathQuoteFull = "/rest/secure/angelbroking/market/v1/quote/"

type quoteFullRequest struct {
	Mode           string              `json:"mode"`
	ExchangeTokens map[string][]string `json:"exchangeTokens"`
}

type quoteDepthLevel struct {
	Price    float64 `json:"price"`
	Quantity int64   `json:"quantity"`
	Orders   int     `json:"orders"`
}

// QuoteFull is one instrument's full-mode market snapshot — LTP plus the
// fields GetLTP doesn't return (bid/ask, volume, OI), needed for the
// options-algo liquidity filter and premium tracking.
type QuoteFull struct {
	Exchange      string  `json:"exchange"`
	TradingSymbol string  `json:"tradingSymbol"`
	SymbolToken   string  `json:"symbolToken"`
	LTP           float64 `json:"ltp"`
	Open          float64 `json:"open"`
	High          float64 `json:"high"`
	Low           float64 `json:"low"`
	Close         float64 `json:"close"`
	TradeVolume   int64   `json:"tradeVolume"`
	OpenInterest  float64 `json:"opnInterest"`
	Depth         struct {
		Buy  []quoteDepthLevel `json:"buy"`
		Sell []quoteDepthLevel `json:"sell"`
	} `json:"depth"`
}

// Bid returns the best (top-of-book) buy price, or 0 if depth is empty.
func (q QuoteFull) Bid() float64 {
	if len(q.Depth.Buy) == 0 {
		return 0
	}
	return q.Depth.Buy[0].Price
}

// EffectivePrice prefers the live bid-ask midpoint over LTP — see
// market.EffectivePrice's doc comment for why.
func (q QuoteFull) EffectivePrice() float64 {
	return market.EffectivePrice(q.LTP, q.Bid(), q.Ask())
}

// Ask returns the best (top-of-book) sell price, or 0 if depth is empty.
func (q QuoteFull) Ask() float64 {
	if len(q.Depth.Sell) == 0 {
		return 0
	}
	return q.Depth.Sell[0].Price
}

type quoteFullResponse struct {
	Status  bool   `json:"status"`
	Message string `json:"message"`
	Data    struct {
		Fetched   []QuoteFull `json:"fetched"`
		Unfetched []struct {
			SymbolToken string `json:"symbolToken"`
			Message     string `json:"message"`
		} `json:"unfetched"`
	} `json:"data"`
}

// GetOptionQuoteFull fetches bid/ask/volume/OI/LTP for up to a batch of
// symbol tokens on one exchange in a single call (Angel's FULL quote mode) —
// a separate, new call from GetLTP (internal/angel/quote.go), which only
// returns LTP; that function is untouched, this is purely additive.
func (c *Client) GetOptionQuoteFull(ctx context.Context, exchange string, symbolTokens []string) ([]QuoteFull, error) {
	if err := c.ensureLogin(ctx); err != nil {
		return nil, err
	}
	if c.limiter != nil {
		if err := c.limiter.Wait(ctx, 1); err != nil {
			return nil, err
		}
	}
	body, _ := json.Marshal(quoteFullRequest{
		Mode:           "FULL",
		ExchangeTokens: map[string][]string{exchange: symbolTokens},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.APIBaseURL+pathQuoteFull, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	c.commonHeaders(req)
	req.Header.Set("Authorization", "Bearer "+c.jwt())

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("angel quote full: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)

	var qr quoteFullResponse
	if err := json.Unmarshal(raw, &qr); err != nil {
		return nil, fmt.Errorf("angel quote full decode (status %d): %w", resp.StatusCode, err)
	}
	if !qr.Status {
		return nil, fmt.Errorf("angel quote full unavailable: %s", qr.Message)
	}
	return qr.Data.Fetched, nil
}
