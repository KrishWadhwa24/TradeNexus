package angel

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
)

const pathOptionGreeks = "/rest/secure/angelbroking/marketData/v1/optionGreek"

type optionGreekRequest struct {
	Name       string `json:"name"`
	ExpiryDate string `json:"expirydate"`
}

// rawOptionGreek mirrors Angel's response fields verbatim — every numeric
// value comes back as a string (same convention as the scrip master's
// LotSize/Strike, see internal/instruments/derivatives.go), so this is
// parsed into OptionGreek's real float64 fields below rather than exposed
// directly.
type rawOptionGreek struct {
	Name              string `json:"name"`
	Expiry            string `json:"expiry"`
	StrikePrice       string `json:"strikePrice"`
	OptionType        string `json:"optionType"`
	Delta             string `json:"delta"`
	Gamma             string `json:"gamma"`
	Theta             string `json:"theta"`
	Vega              string `json:"vega"`
	ImpliedVolatility string `json:"impliedVolatility"`
	TradeVolume       string `json:"tradeVolume"`
}

// OptionGreek is one strike/option-type's Greeks, numeric fields parsed.
type OptionGreek struct {
	StrikePrice float64
	OptionType  string // "CE" | "PE"
	Delta       float64
	Gamma       float64
	Theta       float64
	Vega        float64
	IV          float64
}

type optionGreekResponse struct {
	Status  bool             `json:"status"`
	Message string           `json:"message"`
	Data    []rawOptionGreek `json:"data"`
}

func parseFloatLenient(s string) float64 {
	f, _ := strconv.ParseFloat(s, 64)
	return f
}

// GetOptionGreeks fetches delta/gamma/theta/vega/IV for every strike of one
// underlying+expiry in a single call. New, standalone integration — doesn't
// touch GetLTP/GetOptionQuoteFull or any existing Angel call.
func (c *Client) GetOptionGreeks(ctx context.Context, underlyingName, expiryDate string) ([]OptionGreek, error) {
	if err := c.ensureLogin(ctx); err != nil {
		return nil, err
	}
	if c.limiter != nil {
		if err := c.limiter.Wait(ctx, 1); err != nil {
			return nil, err
		}
	}
	body, _ := json.Marshal(optionGreekRequest{Name: underlyingName, ExpiryDate: expiryDate})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.APIBaseURL+pathOptionGreeks, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	c.commonHeaders(req)
	req.Header.Set("Authorization", "Bearer "+c.jwt())

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("angel option greeks: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)

	var gr optionGreekResponse
	if err := json.Unmarshal(raw, &gr); err != nil {
		return nil, fmt.Errorf("angel option greeks decode (status %d): %w", resp.StatusCode, err)
	}
	if !gr.Status {
		return nil, fmt.Errorf("angel option greeks unavailable: %s", gr.Message)
	}
	out := make([]OptionGreek, 0, len(gr.Data))
	for _, g := range gr.Data {
		out = append(out, OptionGreek{
			StrikePrice: parseFloatLenient(g.StrikePrice),
			OptionType:  g.OptionType,
			Delta:       parseFloatLenient(g.Delta),
			Gamma:       parseFloatLenient(g.Gamma),
			Theta:       parseFloatLenient(g.Theta),
			Vega:        parseFloatLenient(g.Vega),
			IV:          parseFloatLenient(g.ImpliedVolatility),
		})
	}
	return out, nil
}
