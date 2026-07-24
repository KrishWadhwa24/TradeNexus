package ipo

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
	"github.com/rs/zerolog"

	"tradenexus/internal/market"
)

// Broadcaster sends a plain message to every Telegram destination (users +
// safety-net chat). Implemented by notify.Dispatcher.
type Broadcaster interface {
	Broadcast(ctx context.Context, text string) (int, error)
}

// Service polls the IPO feed, keeps only open/upcoming IPOs, and fires last-day
// "apply" signals over Telegram.
type Service struct {
	client     *Client
	repo       *Repo
	bc         Broadcaster // optional; nil disables signalling
	interval   time.Duration
	signalCron string // IST cron for the close-day GMP check (e.g. "30 14 * * *")
	log        zerolog.Logger

	// signalMu serializes RunClosingDaySignals. It's now triggered from the
	// cron AND opportunistically at startup/every poll tick (catchUpClosing-
	// DaySignals), so without this, two overlapping calls could both read an
	// IPO's not-yet-updated signal state and both broadcast.
	signalMu sync.Mutex
}

// New builds the IPO service. bc may be nil. signalCron is the IST cron for the
// close-day auto-signal check; empty → 14:30 daily (2:30 PM IST).
func New(client *Client, repo *Repo, bc Broadcaster, interval time.Duration, signalCron string, log zerolog.Logger) *Service {
	if interval <= 0 {
		interval = 3 * time.Hour
	}
	if signalCron == "" {
		signalCron = "30 14 * * *"
	}
	return &Service{client: client, repo: repo, bc: bc, interval: interval, signalCron: signalCron, log: log}
}

// ListActive returns the open + upcoming IPOs for the UI.
func (s *Service) ListActive(ctx context.Context) ([]IPO, error) {
	return s.repo.ListActive(ctx)
}

// RefreshNow triggers an immediate poll.
func (s *Service) RefreshNow(ctx context.Context) error { return s.Poll(ctx) }

// Poll fetches the feed, merges in the latest subscription snapshot, upserts
// open/upcoming IPOs, prunes everything else (closed/listed/gone), then
// evaluates last-day signals.
func (s *Service) Poll(ctx context.Context) error {
	now := time.Now().In(market.IST)
	items, err := s.client.Fetch(ctx, now)
	if err != nil {
		return err
	}

	// Subscription data is supplementary — an outage on this call shouldn't
	// fail the whole poll, just leave subscription fields at their prior
	// (or zero) value for this cycle.
	subs, err := s.client.FetchSubscriptions(ctx, now)
	if err != nil {
		s.log.Warn().Err(err).Msg("ipo: fetch subscriptions failed, skipping merge this cycle")
	}
	for i := range items {
		if sub, ok := subs[items[i].ID]; ok {
			items[i].QIB = sub.QIB
			items[i].SHNI = sub.SHNI
			items[i].BHNI = sub.BHNI
			items[i].NII = sub.NII
			items[i].RII = sub.RII
			items[i].TotalSubscription = sub.Total
			items[i].AnchorPositive = sub.AnchorPositive
		}
	}

	keep := make([]int64, 0, len(items))
	for _, x := range items {
		if x.Status != "open" && x.Status != "upcoming" {
			continue // drop closed / listed / unknown
		}
		keep = append(keep, x.ID)
		if err := s.repo.Upsert(ctx, x); err != nil {
			s.log.Error().Err(err).Int64("ipo", x.ID).Msg("ipo upsert failed")
		}
	}
	if deleted, err := s.repo.PruneExcept(ctx, keep); err != nil {
		s.log.Error().Err(err).Msg("ipo prune failed")
	} else if deleted > 0 {
		s.log.Info().Int64("removed", deleted).Msg("ipo: pruned closed/listed")
	}
	s.log.Info().Int("active", len(keep)).Msg("ipo: poll done")
	return nil
}

// closingSignalCutoffHour is the last IST hour (24h) at which the close-day GMP
// check is still allowed to fire. IPO bidding effectively ends well before this,
// so an alert this late has no value even if the server only just started.
const closingSignalCutoffHour = 17 // 5:00 PM IST

// qibAlertThreshold is the minimum QIB subscription (times subscribed)
// required, in addition to the GMP tier, before the close-day signal fires.
// GMP alone is sentiment; QIB crossing this confirms institutional demand.
const qibAlertThreshold = 5.0

// RunClosingDaySignals is the authoritative auto-signal check. It is run on a
// schedule (2:30 PM IST by default) AND opportunistically at startup/poll time
// (see StartPolling) so a signal still goes out even if the process wasn't
// running at 2:30 PM — as long as it's checked before 5:00 PM IST. For every
// MAINBOARD IPO that is open and closes today, it requires BOTH the GMP tier
// to qualify (≥10%) AND QIB subscription > qibAlertThreshold before sending
// the Telegram signal. It deliberately ignores any prior admin "clear" — the
// close-day check always fires. SME IPOs are skipped entirely (admin-only).
// An IPO already signaled today (at any tier) is skipped, so running this
// more than once a day — or a GMP/QIB swing crossing thresholds multiple
// times in one day — never sends more than one Telegram message.
func (s *Service) RunClosingDaySignals(ctx context.Context) {
	if s.bc == nil {
		return
	}
	s.signalMu.Lock()
	defer s.signalMu.Unlock()

	today := dateOnly(time.Now().In(market.IST))
	active, err := s.repo.ListActive(ctx)
	if err != nil {
		s.log.Error().Err(err).Msg("ipo: list for signals failed")
		return
	}
	for _, x := range active {
		if x.Status != "open" || x.CloseDate == nil || !sameDate(*x.CloseDate, today) {
			continue
		}
		if isSME(x) {
			continue // SME IPOs get no automatic signal — admin only
		}
		tier := tierFor(x.GMPPercent)
		if tier == "" {
			continue // below 10% → no signal
		}
		if x.QIB <= qibAlertThreshold {
			continue // GMP tier alone isn't enough — QIB subscription must also confirm demand
		}
		// Once signaled today (any tier), never signal again today. This now
		// runs on every poll tick (not just the 2:30 PM cron), so gating on
		// "same tier" alone would let a GMP swing across tier boundaries fire
		// a fresh Telegram message each time it crosses — one signal per IPO
		// per day is the promise.
		if x.SignaledAt != nil && sameDate(*x.SignaledAt, today) {
			continue // already signaled today — avoid a repeat Telegram send
		}
		text := formatIPOMessage(x, signalLabel(tier), true)
		if _, err := s.bc.Broadcast(ctx, text); err != nil {
			s.log.Error().Err(err).Int64("ipo", x.ID).Msg("ipo signal broadcast failed")
			continue
		}
		if err := s.repo.SetSignalTier(ctx, x.ID, tier, time.Now()); err != nil {
			s.log.Error().Err(err).Msg("ipo: set signal tier failed")
		}
		s.log.Info().Str("ipo", x.Name).Str("tier", tier).Msg("ipo close-day signal sent")
	}
}

// isSME reports whether an IPO is an SME issue (NSE SME / BSE SME), which is
// excluded from automatic signalling.
func isSME(x IPO) bool {
	return strings.Contains(strings.ToUpper(x.Board), "SME") || strings.EqualFold(x.Category, "SME")
}

// AdminApply lets an admin push an "Apply (said by admin)" signal for any IPO,
// regardless of GMP or close date.
func (s *Service) AdminApply(ctx context.Context, id int64) error {
	if s.bc == nil {
		return fmt.Errorf("notifications disabled")
	}
	x, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return err
	}
	closingToday := x.CloseDate != nil && sameDate(*x.CloseDate, dateOnly(time.Now().In(market.IST)))
	text := formatIPOMessage(x, "Apply (said by admin)", closingToday)
	if _, err := s.bc.Broadcast(ctx, text); err != nil {
		return err
	}
	return s.repo.SetSignalTier(ctx, id, "admin_apply", time.Now())
}

// ClearSignal removes an IPO's on-site signal badge for all users. It does NOT
// touch Telegram (already-sent messages are deleted there manually).
func (s *Service) ClearSignal(ctx context.Context, id int64) error {
	return s.repo.ClearSignal(ctx, id)
}

// catchUpClosingDaySignals runs the close-day GMP check immediately, but only
// if it's still before the 5:00 PM IST cutoff. This covers the case where the
// process wasn't running at the scheduled 2:30 PM cron (e.g. server was down
// or only just started) — as long as someone brings it up before 5:00 PM on
// the IPO's last day, the alert still goes out. RunClosingDaySignals is
// idempotent (skips IPOs already signaled today), so this is safe to run
// alongside the cron without double-sending.
func (s *Service) catchUpClosingDaySignals() {
	if time.Now().In(market.IST).Hour() >= closingSignalCutoffHour {
		return
	}
	sc, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	s.RunClosingDaySignals(sc)
}

// StartPolling runs an immediate poll, then re-polls every interval until ctx is
// cancelled. Each poll uses its own timeout so it can't hang the loop.
func (s *Service) StartPolling(ctx context.Context) {
	poll := func() {
		c, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if err := s.Poll(c); err != nil {
			s.log.Error().Err(err).Msg("ipo poll failed")
		}
	}
	go func() {
		poll()                       // startup
		s.catchUpClosingDaySignals() // catch up on today's close-day signal if we missed the cron
		t := time.NewTicker(s.interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				poll()
				s.catchUpClosingDaySignals()
			}
		}
	}()

	// Authoritative close-day GMP check on an IST cron (default 2:30 PM).
	c := cron.New(cron.WithLocation(market.IST))
	if _, err := c.AddFunc(s.signalCron, func() {
		sc, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		s.RunClosingDaySignals(sc)
	}); err != nil {
		s.log.Error().Err(err).Str("cron", s.signalCron).Msg("ipo signal cron invalid")
	} else {
		c.Start()
		go func() { <-ctx.Done(); c.Stop() }()
	}

	s.log.Info().Dur("interval", s.interval).Str("signal_cron", s.signalCron).Msg("ipo poller started")
}

// ---- signal tiers -------------------------------------------------------

// tierFor maps a GMP percent to a signal tier. >=20% apply, 10–20% your_choice,
// below 10% no automatic signal.
func tierFor(pct float64) string {
	switch {
	case pct >= 20:
		return "apply"
	case pct >= 10:
		return "your_choice"
	default:
		return ""
	}
}

func signalLabel(tier string) string {
	switch tier {
	case "apply":
		return "Apply for IPO"
	case "your_choice":
		return "Your Choice"
	case "admin_apply":
		return "Apply (said by admin)"
	default:
		return ""
	}
}

// formatIPOMessage builds the Telegram alert for an IPO.
func formatIPOMessage(x IPO, signalText string, closingToday bool) string {
	var b strings.Builder
	board := x.Board
	if board == "" {
		board = x.Category
	}
	fmt.Fprintf(&b, "🚀 IPO — %s", x.Name)
	if board != "" {
		fmt.Fprintf(&b, " (%s)", board)
	}
	b.WriteString("\n━━━━━━━━━━━━━━━━━━━\n")

	if x.GMP > 0 || x.GMPPercent > 0 {
		fmt.Fprintf(&b, "📊 GMP: ₹%s (%s%%)\n", trimNum(x.GMP), trimNum(x.GMPPercent))
		// Approximate listing profit = GMP per share × lot size (1 lot applied).
		if lot := parseLot(x.Lot); lot > 0 && x.GMP > 0 {
			fmt.Fprintf(&b, "💵 Est. profit: ₹%s / lot (GMP × %d)\n", trimNum(x.GMP*float64(lot)), lot)
		}
	} else {
		b.WriteString("📊 GMP: —\n")
	}
	if x.Subscription != "" && x.Subscription != "-" {
		fmt.Fprintf(&b, "📈 Subscription: %s\n", x.Subscription)
	}
	if x.QIB > 0 {
		fmt.Fprintf(&b, "🏦 QIB: %sx", trimNum(x.QIB))
		if x.TotalSubscription > 0 {
			fmt.Fprintf(&b, "  ·  Total: %sx", trimNum(x.TotalSubscription))
		}
		b.WriteString("\n")
	}
	if x.Price != "" {
		fmt.Fprintf(&b, "💰 Price: ₹%s", x.Price)
		if x.Lot != "" {
			fmt.Fprintf(&b, "  ·  Lot: %s", x.Lot)
		}
		b.WriteString("\n")
	}
	if x.CloseDate != nil {
		fmt.Fprintf(&b, "🗓 Closes: %s", x.CloseDate.Format("02 Jan 2006"))
		if closingToday {
			b.WriteString(" (today)")
		}
		b.WriteString("\n")
	}
	b.WriteString("━━━━━━━━━━━━━━━━━━━\n")
	fmt.Fprintf(&b, "✅ Signal: %s", signalText)
	return b.String()
}

// parseLot reads the lot size (shares per lot) from its display string.
func parseLot(s string) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// trimNum prints a float without a trailing ".00".
func trimNum(f float64) string {
	s := strconv.FormatFloat(f, 'f', 2, 64)
	s = strings.TrimSuffix(s, ".00")
	if strings.Contains(s, ".") {
		s = strings.TrimRight(s, "0")
		s = strings.TrimSuffix(s, ".")
	}
	return s
}

func dateOnly(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, market.IST)
}

func sameDate(a, b time.Time) bool {
	return a.Year() == b.Year() && a.Month() == b.Month() && a.Day() == b.Day()
}
