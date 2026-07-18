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
	text := formatMessage(sig, symbol)

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

func formatMessage(sig signals.Signal, symbol string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s signal — %s\n", sig.Direction, symbol)
	fmt.Fprintf(&b, "Timeframe: %s\n", sig.Timeframe)
	fmt.Fprintf(&b, "Scanner(s): %s\n", sig.ScannerName)
	if sig.Confidence != nil {
		fmt.Fprintf(&b, "Confidence: %d/4\n", *sig.Confidence)
	}
	fmt.Fprintf(&b, "Candle date: %s", sig.CandleDate.Format("2006-01-02"))
	return b.String()
}

func dateOnly(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, market.IST)
}
