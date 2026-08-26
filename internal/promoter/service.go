package promoter

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
	"github.com/rs/zerolog"

	"tradenexus/internal/cronx"
	"tradenexus/internal/market"
)

// Broadcaster sends a plain message to every Telegram destination.
// Implemented by notify.Dispatcher.
type Broadcaster interface {
	Broadcast(ctx context.Context, text string) (int, error)
}

// catchUpWindow is how far back the filing list is queried on every poll
// (not just "today"). NSE's list endpoint is cheap to query for a wider
// range — the seen-filings dedup skips anything already processed — so this
// buys resilience against downtime (a deploy, a crash, a skipped poll)
// without re-parsing XML we've already read. seenFilingsRetention must
// outlive catchUpWindow, or filings older than the seen-filings table's
// retention would look "unseen" again on every poll.
const (
	catchUpWindow        = 7 * 24 * time.Hour
	seenFilingsRetention = 10 * 24 * time.Hour
)

// Service polls the NSE PIT feed, keeps only promoter/director/KMP market
// buys and sells, and Telegram-alerts on new ones (unless they're older than
// the alert window, in which case they're stored silently).
type Service struct {
	client       *Client
	repo         *Repo
	bc           Broadcaster // optional; nil disables Telegram alerts
	interval     time.Duration
	alertWindow  time.Duration
	retention    time.Duration
	minManualGap time.Duration
	log          zerolog.Logger

	mu       sync.Mutex
	lastPoll time.Time
}

// New builds the promoter-trade service. bc may be nil.
func New(client *Client, repo *Repo, bc Broadcaster, interval time.Duration, alertWindowDays, retentionDays int, log zerolog.Logger) *Service {
	if interval <= 0 {
		interval = 90 * time.Minute
	}
	if alertWindowDays <= 0 {
		alertWindowDays = 15
	}
	if retentionDays <= 0 {
		retentionDays = 60
	}
	return &Service{
		client:       client,
		repo:         repo,
		bc:           bc,
		interval:     interval,
		alertWindow:  time.Duration(alertWindowDays) * 24 * time.Hour,
		retention:    time.Duration(retentionDays) * 24 * time.Hour,
		minManualGap: 2 * time.Minute,
		log:          log,
	}
}

// ListRecent returns tracked trades from the last `days` days for the UI.
func (s *Service) ListRecent(ctx context.Context, days int) ([]Trade, error) {
	return s.repo.ListRecent(ctx, days)
}

// ScanNow triggers an out-of-band poll for the manual "Scan now" button,
// open to every logged-in user. It's cooldown-guarded so concurrent clicks
// from multiple users can't hammer NSE. Returns false (with an error
// describing the remaining wait) if a scan already ran too recently.
func (s *Service) ScanNow(ctx context.Context) (bool, error) {
	s.mu.Lock()
	if wait := s.minManualGap - time.Since(s.lastPoll); wait > 0 {
		s.mu.Unlock()
		return false, fmt.Errorf("a scan already ran recently — try again in %s", wait.Round(time.Second))
	}
	s.lastPoll = time.Now()
	s.mu.Unlock()

	go func() {
		c, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if err := s.Poll(c); err != nil {
			s.log.Error().Err(err).Msg("promoter manual scan failed")
		}
	}()
	return true, nil
}

// SendAlert force-sends the Telegram alert for one trade (admin action).
func (s *Service) SendAlert(ctx context.Context, id string) error {
	if s.bc == nil {
		return fmt.Errorf("notifications disabled")
	}
	t, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	if _, err := s.bc.Broadcast(ctx, formatTradeMessage(t)); err != nil {
		return err
	}
	return s.repo.MarkAlerted(ctx, id, time.Now())
}

// Poll fetches the filing list for the last catchUpWindow days (not just
// today — see its doc comment), parses any filing not yet inspected, stores
// tracked promoter/director/KMP buys and sells, and alerts on the ones still
// within the alert window.
func (s *Service) Poll(ctx context.Context) error {
	now := time.Now().In(market.IST)
	filings, err := s.client.FetchFilings(ctx, now.Add(-catchUpWindow), now)
	if err != nil {
		return err
	}

	ids := make([]int64, len(filings))
	for i, f := range filings {
		ids[i] = f.AppID
	}
	unseen, err := s.repo.FilterUnseen(ctx, ids)
	if err != nil {
		return err
	}
	unseenSet := make(map[int64]bool, len(unseen))
	for _, id := range unseen {
		unseenSet[id] = true
	}

	tracked, alerted := 0, 0
	for _, f := range filings {
		if !unseenSet[f.AppID] {
			continue
		}
		detail, err := s.client.FetchDetail(ctx, f.XMLFileName)
		if err != nil {
			s.log.Error().Err(err).Int64("app_id", f.AppID).Msg("promoter: fetch detail failed, will retry next poll")
			continue // don't mark seen — retry on the next poll
		}

		for _, d := range detail.Disclosures {
			eventType := classify(d.Category, d.Mode, d.TransactionType)
			if eventType == "" {
				s.log.Debug().Str("category", d.Category).Str("mode", d.Mode).Str("tx_type", d.TransactionType).Int64("app_id", f.AppID).Msg("promoter: disclosure untracked")
				continue
			}
			trade := buildTrade(f, detail, d, eventType)
			inserted, err := s.repo.InsertTrade(ctx, trade)
			if err != nil {
				s.log.Error().Err(err).Str("id", trade.ID).Msg("promoter: insert failed")
				continue
			}
			if !inserted {
				continue // already tracked — never alert twice for the same disclosure
			}
			tracked++
			if s.bc != nil && s.withinAlertWindow(trade, now) {
				if _, err := s.bc.Broadcast(ctx, formatTradeMessage(trade)); err != nil {
					s.log.Error().Err(err).Str("id", trade.ID).Msg("promoter: broadcast failed")
				} else {
					alerted++
					_ = s.repo.MarkAlerted(ctx, trade.ID, time.Now())
				}
			}
		}

		if err := s.repo.MarkSeen(ctx, f.AppID); err != nil {
			s.log.Error().Err(err).Int64("app_id", f.AppID).Msg("promoter: mark-seen failed")
		}
		time.Sleep(300 * time.Millisecond) // be polite to NSE between filings
	}

	if _, err := s.repo.PruneSeenOlderThan(ctx, now.Add(-seenFilingsRetention)); err != nil {
		s.log.Error().Err(err).Msg("promoter: prune seen failed")
	}
	s.log.Info().Int("filings", len(filings)).Int("new_trades", tracked).Int("alerted", alerted).Msg("promoter: poll done")
	return nil
}

// withinAlertWindow reports whether a newly-discovered trade is recent
// enough to alert on. Older trades (e.g. found because a poll was skipped)
// are stored silently instead of flooding Telegram with stale signals.
func (s *Service) withinAlertWindow(t Trade, now time.Time) bool {
	ref := t.BroadcastAt
	if t.TradeTo != nil {
		ref = *t.TradeTo
	}
	return now.Sub(ref) <= s.alertWindow
}

// pruneOld deletes trades past the retention window.
func (s *Service) pruneOld(ctx context.Context) {
	cutoff := time.Now().Add(-s.retention)
	n, err := s.repo.PruneOlderThan(ctx, cutoff)
	if err != nil {
		s.log.Error().Err(err).Msg("promoter: prune old trades failed")
		return
	}
	if n > 0 {
		s.log.Info().Int64("removed", n).Msg("promoter: pruned trades past retention")
	}
}

// StartPolling runs an immediate poll, then re-polls every interval, and
// prunes trades past retention once a day, until ctx is cancelled.
func (s *Service) StartPolling(ctx context.Context) {
	poll := func() {
		c, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if err := s.Poll(c); err != nil {
			s.log.Error().Err(err).Msg("promoter poll failed")
		}
	}
	go func() {
		cronx.Safe(s.log, poll) // startup
		t := time.NewTicker(s.interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				cronx.Safe(s.log, poll)
			}
		}
	}()

	c := cron.New(cron.WithLocation(market.IST), cron.WithChain(cronx.Recover(s.log)))
	if _, err := c.AddFunc("20 2 * * *", func() {
		pc, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		s.pruneOld(pc)
	}); err != nil {
		s.log.Error().Err(err).Msg("promoter prune cron invalid")
	} else {
		c.Start()
		go func() { <-ctx.Done(); c.Stop() }()
	}

	s.log.Info().Dur("interval", s.interval).Dur("alert_window", s.alertWindow).Dur("retention", s.retention).Msg("promoter poller started")
}

// buildTrade assembles a Trade from a filing + its parsed detail + one
// disclosure block.
func buildTrade(f FilingMeta, detail Detail, d Disclosure, eventType string) Trade {
	symbol := f.Symbol
	if symbol == "" {
		symbol = detail.Symbol
	}
	company := f.CompanyName
	if company == "" {
		company = detail.CompanyName
	}
	return Trade{
		ID:          f.idFor(d.ContextRef),
		AppID:       f.AppID,
		Symbol:      symbol,
		CompanyName: company,
		ISIN:        detail.ISIN,
		PersonName:  d.PersonName,
		Category:    d.Category,
		EventType:   eventType,
		Mode:        d.Mode,
		Quantity:    d.Quantity,
		Value:       d.Value,
		QtyBefore:   d.QtyBefore,
		PctBefore:   d.PctBefore,
		QtyAfter:    d.QtyAfter,
		PctAfter:    d.PctAfter,
		TradeFrom:   d.DateFrom,
		TradeTo:     d.DateTo,
		Regulation:  detail.Regulation,
		FilingURL:   f.IXBRL,
		BroadcastAt: f.BroadcastAt,
	}
}

func (f FilingMeta) idFor(contextRef string) string {
	return strconv.FormatInt(f.AppID, 10) + ":" + contextRef
}

// formatTradeMessage builds the Telegram alert for a tracked trade.
func formatTradeMessage(t Trade) string {
	emoji, who, verb, action := "🟢", "Promoter", "Bought", "Buy"
	if strings.HasPrefix(t.EventType, "kmp") {
		who = "Director/KMP"
	}
	if strings.HasSuffix(t.EventType, "_sell") {
		emoji, verb, action = "🔴", "Sold", "Sell"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s %s %s — %s (%s)\n", emoji, who, action, t.Symbol, t.CompanyName)
	fmt.Fprintf(&b, "👤 %s (%s)\n", t.PersonName, t.Category)
	fmt.Fprintf(&b, "📈 %s %s shares · ₹%s\n", verb, formatInt(t.Quantity), formatInt(int64(t.Value)))
	fmt.Fprintf(&b, "📊 Holding: %.2f%% → %.2f%%\n", t.PctBefore, t.PctAfter)
	if t.TradeTo != nil {
		fmt.Fprintf(&b, "🗓 Trade date: %s\n", t.TradeTo.Format("02-Jan-2006"))
	}
	if t.FilingURL != "" {
		fmt.Fprintf(&b, "🔗 %s", t.FilingURL)
	}
	return strings.TrimRight(b.String(), "\n")
}

// formatInt prints an integer with thousands separators, e.g. 1234567 → "12,34,567"-free plain "1,234,567".
func formatInt(n int64) string {
	s := strconv.FormatInt(n, 10)
	neg := strings.HasPrefix(s, "-")
	if neg {
		s = s[1:]
	}
	var parts []string
	for len(s) > 3 {
		parts = append([]string{s[len(s)-3:]}, parts...)
		s = s[:len(s)-3]
	}
	parts = append([]string{s}, parts...)
	out := strings.Join(parts, ",")
	if neg {
		out = "-" + out
	}
	return out
}
