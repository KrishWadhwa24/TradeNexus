package angel

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// FetchScripMaster downloads the full Angel scrip master and returns the cash-
// equity rows for both NSE and BSE — the tradable universe users build
// watchlists from. NSE equities are the "-EQ" symbols; BSE equities live under
// exch_seg="BSE" with a blank instrumenttype (no "-EQ" suffix). The full file is
// large (tens of MB); this runs occasionally (daily), not on the hot path.
func (c *Client) FetchScripMaster(ctx context.Context) ([]Scrip, error) {
	resp, err := c.fetchScripMasterResponse(ctx)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var all []Scrip
	if err := json.NewDecoder(resp.Body).Decode(&all); err != nil {
		return nil, fmt.Errorf("decode scrip master: %w", err)
	}

	out := make([]Scrip, 0, 6000)
	var nse, bse int
	for _, s := range all {
		switch {
		case s.ExchSeg == "NSE" && strings.HasSuffix(s.Symbol, "-EQ"):
			out = append(out, s)
			nse++
		case s.ExchSeg == "BSE" && s.Expiry == "" && s.Symbol != "" &&
			(s.InstrumentType == "" || s.InstrumentType == "EQ"):
			// BSE cash equities: no expiry (excludes F&O), instrumenttype blank
			// or "EQ" (excludes indices like AMXIDX).
			out = append(out, s)
			bse++
		}
	}
	c.log.Info().Int("total", len(all)).Int("nse_eq", nse).Int("bse_eq", bse).Msg("angel: scrip master loaded")
	return out, nil
}

func (c *Client) fetchScripMasterResponse(ctx context.Context) (*http.Response, error) {
	httpc := &http.Client{Timeout: c.cfg.ScripMasterTimeout}
	var lastErr error
	attempts := c.cfg.ScripMasterAttempts
	for attempt := 1; attempt <= attempts; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.cfg.ScripMasterURL, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "TradeNexus/1.0")

		resp, err := httpc.Do(req)
		if err == nil && resp.StatusCode == http.StatusOK {
			return resp, nil
		}
		if resp != nil {
			_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 1024))
			_ = resp.Body.Close()
			lastErr = fmt.Errorf("fetch scrip master: status %d", resp.StatusCode)
		} else {
			lastErr = fmt.Errorf("fetch scrip master: %w", err)
		}
		if attempt == attempts || ctx.Err() != nil {
			break
		}
		c.log.Warn().Err(lastErr).Int("attempt", attempt).Int("max_attempts", attempts).Msg("angel: retrying scrip master download")
		time.Sleep(time.Duration(attempt) * time.Second)
	}
	return nil, lastErr
}
