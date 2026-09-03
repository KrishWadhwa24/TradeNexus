package instruments

import (
	"context"
	"strconv"
	"strings"
	"time"

	"tradenexus/internal/angel"
)

// DerivativesSyncResult summarizes one sync pass.
type DerivativesSyncResult struct {
	Fetched     int
	Upserted    int
	Options     int
	IndexSpots  int
	Deactivated int64
}

// optionTypeSuffix returns "CE"/"PE" from an option's trading symbol (e.g.
// "NIFTY08SEP2622400PE" → "PE") — Angel's scrip master has no separate CE/PE
// field, only the symbol string's trailing two characters (verified live).
func optionTypeSuffix(symbol string) string {
	if strings.HasSuffix(symbol, "CE") || strings.HasSuffix(symbol, "PE") {
		return symbol[len(symbol)-2:]
	}
	return ""
}

// SyncDerivatives pulls near-dated NIFTY/BANKNIFTY/FINNIFTY/SENSEX/BANKEX
// option chains plus the Nifty 50/SENSEX index-spot instruments (via
// angel.Client.FetchIndexDerivatives), upserts them, and deactivates any
// option contract past expiry. Shared by the admin sync endpoint and the
// weekly refresh cron — safe to re-run.
func SyncDerivatives(ctx context.Context, client *angel.Client, repo *Repo) (DerivativesSyncResult, error) {
	scrips, err := client.FetchIndexDerivatives(ctx)
	if err != nil {
		return DerivativesSyncResult{}, err
	}
	items := make([]Instrument, 0, len(scrips))
	var res DerivativesSyncResult
	res.Fetched = len(scrips)
	for _, sc := range scrips {
		lot, _ := strconv.Atoi(sc.LotSize)
		if lot == 0 {
			lot = 1
		}
		it := Instrument{
			SymbolToken:      sc.Token,
			Exchange:         sc.ExchSeg,
			TradingSymbol:    sc.Symbol,
			Name:             sc.Name,
			LotSize:          lot,
			UnderlyingSymbol: sc.Name,
		}
		if sc.InstrumentType == "OPTIDX" {
			if strike, err := strconv.ParseFloat(sc.Strike, 64); err == nil {
				strike = strike / 100 // Angel stores option strikes ×100 (paise) — verified live
				it.StrikePrice = &strike
			}
			if expiry, err := time.Parse("02Jan2006", sc.Expiry); err == nil {
				it.ExpiryDate = &expiry
			}
			it.OptionType = optionTypeSuffix(sc.Symbol)
			res.Options++
		} else {
			res.IndexSpots++
		}
		items = append(items, it)
	}
	n, err := repo.BulkUpsert(ctx, items)
	if err != nil {
		return res, err
	}
	res.Upserted = n

	deactivated, err := repo.DeactivateExpired(ctx)
	if err != nil {
		return res, err
	}
	res.Deactivated = deactivated
	return res, nil
}
