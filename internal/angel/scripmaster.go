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

// indexUnderlyings is the curated set of index option chains Step 1 tracks —
// deliberately narrow (not the full F&O universe) to keep instrument count
// and live-subscription load sane. NIFTY/BANKNIFTY/FINNIFTY trade in NFO
// (NSE F&O); SENSEX/BANKEX trade in BFO (BSE F&O) — a real, easy-to-miss
// distinction confirmed against the live scrip master.
var indexUnderlyings = map[string]bool{
	"NIFTY": true, "BANKNIFTY": true, "FINNIFTY": true,
	"SENSEX": true, "BANKEX": true,
}

// indexSpotTokens are the underlying index instruments themselves (not
// derivatives) — needed as the signal source. Keyed by (exch_seg, name) — the
// scrip master's "name" field is the clean ticker ("NIFTY"); "symbol" is an
// inconsistent display string ("Nifty 50" for this row, but plain "SENSEX"
// for that one — verified live), so name is the reliable match field.
var indexSpotTokens = map[[2]string]bool{
	{"NSE", "NIFTY"}:  true, // symbol field is "Nifty 50"
	{"BSE", "SENSEX"}: true, // symbol field is "SENSEX"
}

// nearDatedWindow bounds which expiries FetchIndexDerivatives keeps. NIFTY/
// SENSEX still have weekly expiries, but BANKNIFTY/FINNIFTY/BANKEX were
// rationalized to monthly-only in 2024 (verified live — their nearest expiry
// can be ~29 days out), so this has to be wide enough to always catch at
// least the current monthly contract for those, not just weeklies.
const nearDatedWindow = 35 * 24 * time.Hour

// FetchIndexDerivatives downloads the full Angel scrip master (a second,
// separate fetch from FetchScripMaster — the two apply different filters to
// the same source file) and returns near-dated NIFTY/BANKNIFTY/FINNIFTY/
// SENSEX/BANKEX option contracts plus the Nifty 50 and SENSEX index-spot
// instruments themselves.
func (c *Client) FetchIndexDerivatives(ctx context.Context) ([]Scrip, error) {
	resp, err := c.fetchScripMasterResponse(ctx)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var all []Scrip
	if err := json.NewDecoder(resp.Body).Decode(&all); err != nil {
		return nil, fmt.Errorf("decode scrip master: %w", err)
	}

	now := time.Now()
	cutoff := now.Add(nearDatedWindow)
	out := make([]Scrip, 0, 500)
	var options, spots, futures int
	for _, s := range all {
		switch {
		case (s.ExchSeg == "NFO" || s.ExchSeg == "BFO") && s.InstrumentType == "OPTIDX" && indexUnderlyings[s.Name]:
			expiry, err := time.Parse("02Jan2006", s.Expiry)
			if err != nil || expiry.Before(now) || expiry.After(cutoff) {
				continue
			}
			out = append(out, s)
			options++
		case s.InstrumentType == "AMXIDX" && indexSpotTokens[[2]string{s.ExchSeg, s.Name}]:
			out = append(out, s)
			spots++
		// NIFTY futures — tracked only as a real-volume proxy for VWAP (the
		// spot index itself always reports 0 volume, confirmed live), not for
		// trading. NIFTY-only for now (see optionsalgo plan); SENSEX futures
		// can be added the same way later if SENSEX rejoins the algo's scope.
		case s.ExchSeg == "NFO" && s.InstrumentType == "FUTIDX" && s.Name == "NIFTY":
			expiry, err := time.Parse("02Jan2006", s.Expiry)
			if err != nil || expiry.Before(now) || expiry.After(cutoff) {
				continue
			}
			out = append(out, s)
			futures++
		}
	}
	c.log.Info().Int("total", len(all)).Int("options", options).Int("index_spots", spots).Int("futures", futures).Msg("angel: index derivatives loaded")
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
