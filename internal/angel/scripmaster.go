package angel

import (
	"context"
	"encoding/json"
	"fmt"
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
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.cfg.ScripMasterURL, nil)
	if err != nil {
		return nil, err
	}

	// Dedicated longer timeout for this big download.
	httpc := &http.Client{Timeout: 90 * time.Second}
	resp, err := httpc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch scrip master: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("fetch scrip master: status %d", resp.StatusCode)
	}

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
