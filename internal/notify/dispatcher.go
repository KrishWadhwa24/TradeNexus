package notify

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"tradenexus/internal/market"
	"tradenexus/internal/signals"
)

// systemUserID owns catch-all deliveries to the default safety-net chat, so the
// signal_deliveries UNIQUE constraint dedups that feed. Seeded by migration 0005.
const systemUserID = "00000000-0000-0000-0000-000000000000"

// Dispatcher fans a platform-wide signal out to the users who watch that stock
// and have the relevant scanner enabled, subject to the send window and dedup.
// A default (env) chat optionally receives every in-window signal as a safety net.
type Dispatcher struct {
	pool        *pgxpool.Pool
	tg          *Telegram
	windowDays  int
	defaultBot  string
	defaultChat string
	log         zerolog.Logger
}

// New builds a dispatcher. windowDays is the freshness window (e.g. 7): signals
// whose candle date is older than this are stored but NOT sent. defaultBot/
// defaultChat, if non-empty, receive every in-window signal once (safety net).
func New(pool *pgxpool.Pool, tg *Telegram, windowDays int, defaultBot, defaultChat string, log zerolog.Logger) *Dispatcher {
	if windowDays <= 0 {
		windowDays = 7
	}
	return &Dispatcher{
		pool: pool, tg: tg, windowDays: windowDays,
		defaultBot: defaultBot, defaultChat: defaultChat, log: log,
	}
}

// DispatchResult summarizes what happened for one signal.
type DispatchResult struct {
	Sent             int    `json:"sent"`
	SkippedDuplicate int    `json:"skipped_duplicate"`
	Recipients       int    `json:"recipients"`
	DefaultSent      bool   `json:"default_sent"`
	Dropped          bool   `json:"dropped"`
	Reason           string `json:"reason,omitempty"`
}

// Recipient is a user eligible to receive a signal.
type Recipient struct {
	UserID   string
	BotToken string
	ChatID   string
}

// Dispatch delivers one signal per the fan-out + window + dedup rules.
func (d *Dispatcher) Dispatch(ctx context.Context, sig signals.Signal) (DispatchResult, error) {
	// 7-day send window: drop (don't send) stale signals; they remain in audit.
	today := dateOnly(time.Now().In(market.IST))
	cutoff := today.AddDate(0, 0, -d.windowDays)
	if dateOnly(sig.CandleDate).Before(cutoff) {
		return DispatchResult{Dropped: true, Reason: fmt.Sprintf("candle_date older than %d-day window", d.windowDays)}, nil
	}

	symbol := d.symbol(ctx, sig.InstrumentID)
	keys := ScannerKeys(sig)
	recips, err := d.Recipients(ctx, sig.InstrumentID, keys)
	if err != nil {
		return DispatchResult{}, err
	}

	res := DispatchResult{Recipients: len(recips)}
	text := formatMessage(sig, symbol, d.cmp(ctx, sig.InstrumentID))

	for _, r := range recips {
		dup, err := d.alreadyDelivered(ctx, r.UserID, sig)
		if err != nil {
			return res, err
		}
		if dup {
			res.SkippedDuplicate++
			continue // same stock + timeframe + day already sent to this user
		}
		if err := d.tg.Send(ctx, r.BotToken, r.ChatID, text); err != nil {
			d.log.Error().Err(err).Str("user", r.UserID).Msg("telegram send failed; will retry next scan")
			continue // no delivery row recorded → retried on the next scan
		}
		if err := d.recordDelivery(ctx, r.UserID, sig); err != nil {
			d.log.Error().Err(err).Msg("record delivery failed")
			continue
		}
		res.Sent++
	}

	// Safety-net: default chat receives every in-window signal once
	// (deduped under the system user by stock+timeframe+day).
	if d.defaultBot != "" && d.defaultChat != "" {
		dup, err := d.alreadyDelivered(ctx, systemUserID, sig)
		if err != nil {
			return res, err
		}
		if !dup {
			if err := d.tg.Send(ctx, d.defaultBot, d.defaultChat, text); err != nil {
				d.log.Error().Err(err).Msg("default chat send failed; will retry next scan")
			} else if err := d.recordDelivery(ctx, systemUserID, sig); err != nil {
				d.log.Error().Err(err).Msg("record default delivery failed")
			} else {
				res.DefaultSent = true
			}
		}
	}
	return res, nil
}

// ForceResend re-sends a stored signal to all of its current recipients (and the
// safety-net chat, if configured), IGNORING the dedup ledger and the freshness
// window. This backs the admin "fire again" action, where a human deliberately
// wants the alert delivered a second time. Delivery rows are still recorded
// (idempotently) so the audit trail stays accurate.
func (d *Dispatcher) ForceResend(ctx context.Context, sig signals.Signal) (DispatchResult, error) {
	symbol := d.symbol(ctx, sig.InstrumentID)
	recips, err := d.Recipients(ctx, sig.InstrumentID, ScannerKeys(sig))
	if err != nil {
		return DispatchResult{}, err
	}

	res := DispatchResult{Recipients: len(recips)}
	text := formatMessage(sig, symbol, d.cmp(ctx, sig.InstrumentID))

	for _, r := range recips {
		if err := d.tg.Send(ctx, r.BotToken, r.ChatID, text); err != nil {
			d.log.Error().Err(err).Str("user", r.UserID).Msg("force resend: telegram send failed")
			continue
		}
		if err := d.recordDelivery(ctx, r.UserID, sig); err != nil {
			d.log.Error().Err(err).Msg("force resend: record delivery failed")
		}
		res.Sent++
	}

	if d.defaultBot != "" && d.defaultChat != "" {
		if err := d.tg.Send(ctx, d.defaultBot, d.defaultChat, text); err != nil {
			d.log.Error().Err(err).Msg("force resend: default chat send failed")
		} else {
			if err := d.recordDelivery(ctx, systemUserID, sig); err != nil {
				d.log.Error().Err(err).Msg("force resend: record default delivery failed")
			}
			res.DefaultSent = true
		}
	}
	return res, nil
}

// SendTest sends a plain connectivity-check message to a specific bot/chat.
func (d *Dispatcher) SendTest(ctx context.Context, botToken, chatID string) error {
	return d.tg.Send(ctx, botToken, chatID,
		"TradeNexus: test message. If you can read this, your Telegram is connected.")
}

// TestDefault sends a test message to the env safety-net chat.
func (d *Dispatcher) TestDefault(ctx context.Context) error {
	if d.defaultBot == "" || d.defaultChat == "" {
		return fmt.Errorf("default Telegram not configured (set TELEGRAM_DEFAULT_BOT_TOKEN / TELEGRAM_DEFAULT_CHAT_ID)")
	}
	return d.SendTest(ctx, d.defaultBot, d.defaultChat)
}

// Recipients returns users who watch the instrument AND have any of the given
// scanner keys enabled AND have Telegram enabled.
func (d *Dispatcher) Recipients(ctx context.Context, instrumentID int64, scannerKeys []string) ([]Recipient, error) {
	rows, err := d.pool.Query(ctx, `
		SELECT DISTINCT u.id::text, tc.bot_token, tc.chat_id
		FROM users u
		JOIN watchlists w        ON w.user_id = u.id
		JOIN watchlist_items wi  ON wi.watchlist_id = w.id AND wi.instrument_id = $1
		JOIN user_scanner_prefs p ON p.user_id = u.id AND p.enabled = TRUE AND p.scanner_key = ANY($2)
		JOIN telegram_configs tc ON tc.user_id = u.id AND tc.enabled = TRUE
		                        AND tc.bot_token <> '' AND tc.chat_id <> ''`,
		instrumentID, scannerKeys)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Recipient
	for rows.Next() {
		var r Recipient
		if err := rows.Scan(&r.UserID, &r.BotToken, &r.ChatID); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (d *Dispatcher) alreadyDelivered(ctx context.Context, userID string, sig signals.Signal) (bool, error) {
	var exists bool
	err := d.pool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM signal_deliveries
			WHERE user_id = $1::uuid AND signal_id = $2 AND channel = 'telegram')`,
		userID, sig.ID).Scan(&exists)
	return exists, err
}

func (d *Dispatcher) recordDelivery(ctx context.Context, userID string, sig signals.Signal) error {
	_, err := d.pool.Exec(ctx, `
		INSERT INTO signal_deliveries (signal_id, user_id, instrument_id, timeframe, candle_date, channel)
		VALUES ($1, $2::uuid, $3, $4, $5, 'telegram')
		ON CONFLICT (user_id, signal_id, channel) DO NOTHING`,
		sig.ID, userID, sig.InstrumentID, sig.Timeframe, sig.CandleDate)
	return err
}

// cmp returns the latest stored daily close as an approximate current price.
func (d *Dispatcher) cmp(ctx context.Context, instrumentID int64) float64 {
	var px float64
	_ = d.pool.QueryRow(ctx,
		`SELECT close FROM daily_candles WHERE instrument_id = $1 ORDER BY trade_date DESC LIMIT 1`,
		instrumentID).Scan(&px)
	return px
}

func (d *Dispatcher) symbol(ctx context.Context, instrumentID int64) string {
	var sym string
	_ = d.pool.QueryRow(ctx, `SELECT trading_symbol FROM instruments WHERE id = $1`, instrumentID).Scan(&sym)
	if sym == "" {
		return fmt.Sprintf("instrument#%d", instrumentID)
	}
	return sym
}

// ScannerKeys maps a signal to the scanner-pref keys that gate its delivery.
func ScannerKeys(sig signals.Signal) []string {
	if sig.Source == "pine" {
		return []string{"pine_" + strings.ToLower(sig.Timeframe)} // pine_1d | pine_1w | pine_1m
	}
	if sig.Source == "patterns" {
		return []string{sig.ScannerName}
	}
	return strings.Split(sig.ScannerName, ",") // weekly_1..weekly_4
}

// tfLabel maps 1D/1W/1M to a readable timeframe name.
func tfLabel(tf string) string {
	switch tf {
	case "1D":
		return "Daily"
	case "1W":
		return "Weekly"
	case "1M":
		return "Monthly"
	default:
		return tf
	}
}

// formatMessage builds a rich, emoji-decorated Telegram alert. It adapts to the
// scanner source and only prints fields we actually have (no fabricated numbers).
func formatMessage(sig signals.Signal, symbol string, cmp float64) string {
	var b strings.Builder
	dir := strings.ToUpper(sig.Direction)
	buy := dir == "BUY"

	mark := "🟢"
	if !buy {
		mark = "🔴"
	}
	fmt.Fprintf(&b, "%s %s SIGNAL — %s (%s)\n", mark, dir, symbol, sig.Timeframe)
	b.WriteString("━━━━━━━━━━━━━━━━━━━\n")
	fmt.Fprintf(&b, "📊 Strategy: %s\n", strategyName(sig))
	fmt.Fprintf(&b, "⏱ Timeframe: %s — %s\n", sig.Timeframe, tfPhase(sig.Timeframe))

	switch sig.Source {
	case "pine":
		if v, ok := sig.Metrics["breakout_len"]; ok {
			if buy {
				fmt.Fprintf(&b, "📈 Breakout: Close crossed above %.0f-bar high\n", v)
			} else {
				fmt.Fprintf(&b, "📉 Breakdown: Close crossed below %.0f-bar low\n", v)
			}
		}
		if v, ok := sig.Metrics["body_atr"]; ok {
			fmt.Fprintf(&b, "💪 Candle: Body strength %.2fx ATR\n", v)
		}
		if v, ok := sig.Metrics["rel_volume"]; ok {
			fmt.Fprintf(&b, "📊 Volume: Relative volume %.2fx\n", v)
		}
		if sig.RSI != nil {
			fmt.Fprintf(&b, "🔥 RSI: %.1f (%s)\n", *sig.RSI, rsiLabel(*sig.RSI, buy))
		}
		if buy {
			b.WriteString("📐 Trend: EMA 10 > 20 > SMA 40 (Bullish stack)\n")
		} else {
			b.WriteString("📐 Trend: EMA 10 < 20 < SMA 40 (Bearish stack)\n")
		}
	case "weekly":
		if sig.Confidence != nil {
			fmt.Fprintf(&b, "🎯 Scanners: %d of 4 confluences fired\n", *sig.Confidence)
		}
		if m := weeklyMatched(sig.ScannerName); m != "" {
			fmt.Fprintf(&b, "🧩 Matched: %s\n", m)
		}
		if sig.RSI != nil {
			fmt.Fprintf(&b, "🔥 RSI: %.1f (%s)\n", *sig.RSI, rsiLabel(*sig.RSI, buy))
		}
		if sig.Volume != nil {
			fmt.Fprintf(&b, "📊 Volume: %s\n", humanVol(*sig.Volume))
		}
	case "patterns":
		fmt.Fprintf(&b, "🔎 Pattern: %s\n", prettyScanner(sig))
		if sig.RSI != nil {
			fmt.Fprintf(&b, "🔥 RSI: %.1f (%s)\n", *sig.RSI, rsiLabel(*sig.RSI, buy))
		}
		if sig.Volume != nil {
			fmt.Fprintf(&b, "📊 Volume: %s\n", humanVol(*sig.Volume))
		}
	default:
		fmt.Fprintf(&b, "🧭 Scanner: %s\n", prettyScanner(sig))
	}

	fmt.Fprintf(&b, "⚡ Conviction: %s\n", convictionText(sig, buy))
	b.WriteString("━━━━━━━━━━━━━━━━━━━\n")
	if cmp > 0 {
		fmt.Fprintf(&b, "💰 Price: ₹%.2f\n", cmp)
	}
	fmt.Fprintf(&b, "🕐 Candle Close: %s, 00:00 IST", sig.CandleDate.Format("02 Jan 2006"))
	return b.String()
}

// strategyName is the human label for a signal's source.
func strategyName(sig signals.Signal) string {
	switch sig.Source {
	case "pine":
		return "Pine Script Momentum"
	case "weekly":
		return "Weekly Confluence"
	case "patterns":
		return "Chart Pattern"
	}
	return sig.Source
}

// tfPhase describes what a timeframe means for trade horizon.
func tfPhase(tf string) string {
	switch tf {
	case "1D":
		return "Swing Confirmation"
	case "1W":
		return "Positional"
	case "1M":
		return "Long-term"
	}
	return tfLabel(tf)
}

// rsiLabel gives a short momentum descriptor for an RSI value in context.
func rsiLabel(rsi float64, buy bool) string {
	if buy {
		switch {
		case rsi >= 70:
			return "Strong bullish"
		case rsi >= 60:
			return "Bullish momentum"
		case rsi >= 50:
			return "Mild bullish"
		default:
			return "Neutral / weak"
		}
	}
	switch {
	case rsi <= 30:
		return "Strong bearish"
	case rsi <= 40:
		return "Bearish momentum"
	case rsi <= 50:
		return "Mild bearish"
	default:
		return "Neutral"
	}
}

// convictionText renders LOW/MEDIUM/HIGH per source.
func convictionText(sig signals.Signal, buy bool) string {
	switch sig.Source {
	case "weekly":
		if sig.Confidence != nil {
			switch {
			case *sig.Confidence >= 3:
				return "HIGH"
			case *sig.Confidence == 2:
				return "MEDIUM"
			default:
				return "LOW"
			}
		}
	case "patterns":
		if sig.Confidence != nil {
			switch {
			case *sig.Confidence >= 70:
				return "HIGH"
			case *sig.Confidence >= 40:
				return "MEDIUM"
			default:
				return "LOW"
			}
		}
	case "pine":
		return pineConviction(sig.Metrics, sig.RSI, buy)
	}
	return "—"
}

// pineConviction grades a Pine signal from its supporting metrics.
func pineConviction(m map[string]float64, rsi *float64, buy bool) string {
	score := 0
	if v, ok := m["rel_volume"]; ok {
		if v >= 1.8 {
			score++
		}
		if v >= 2.5 {
			score++
		}
	}
	if v, ok := m["body_atr"]; ok {
		if v >= 0.6 {
			score++
		}
		if v >= 1.0 {
			score++
		}
	}
	if rsi != nil {
		if buy && *rsi >= 65 {
			score++
		}
		if buy && *rsi >= 75 {
			score++
		}
		if !buy && *rsi <= 35 {
			score++
		}
		if !buy && *rsi <= 25 {
			score++
		}
	}
	switch {
	case score >= 4:
		return "HIGH"
	case score >= 2:
		return "MEDIUM"
	default:
		return "LOW"
	}
}

// weeklyMatched turns "weekly_1,weekly_3" into a friendly comma list.
func weeklyMatched(names string) string {
	if names == "" {
		return ""
	}
	labels := map[string]string{
		"weekly_1": "52-wk high breakout + EMA stack",
		"weekly_2": "Continuation (higher low + inside-bar break)",
		"weekly_3": "52-wk high breakout (structure)",
		"weekly_4": "Price-action continuation",
	}
	parts := strings.Split(names, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if lbl, ok := labels[p]; ok {
			out = append(out, lbl)
		} else if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, ", ")
}

// prettyScanner renders a friendly scanner label.
func prettyScanner(sig signals.Signal) string {
	if sig.Source == "pine" {
		return "Chase Momentum (Pine)"
	}
	switch sig.ScannerName {
	case "pattern_cup_handle":
		return "Cup & Handle"
	case "pattern_downtrend_breakout":
		return "Downtrend Breakout"
	case "pattern_rectangle":
		return "Rectangle Box"
	}
	return sig.ScannerName
}

// humanVol formats large volumes compactly (e.g. 1.2M, 845K).
func humanVol(v float64) string {
	switch {
	case v >= 1e7:
		return fmt.Sprintf("%.2fCr", v/1e7)
	case v >= 1e5:
		return fmt.Sprintf("%.2fL", v/1e5)
	case v >= 1e3:
		return fmt.Sprintf("%.1fK", v/1e3)
	default:
		return fmt.Sprintf("%.0f", v)
	}
}

func dateOnly(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, market.IST)
}
