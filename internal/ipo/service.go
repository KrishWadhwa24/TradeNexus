package ipo

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

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
	client   *Client
	repo     *Repo
	bc       Broadcaster // optional; nil disables signalling
	interval time.Duration
	log      zerolog.Logger
}

// New builds the IPO service. bc may be nil.
func New(client *Client, repo *Repo, bc Broadcaster, interval time.Duration, log zerolog.Logger) *Service {
	if interval <= 0 {
		interval = 3 * time.Hour
	}
	return &Service{client: client, repo: repo, bc: bc, interval: interval, log: log}
}

// ListActive returns the open + upcoming IPOs for the UI.
func (s *Service) ListActive(ctx context.Context) ([]IPO, error) {
	return s.repo.ListActive(ctx)
}

// RefreshNow triggers an immediate poll.
func (s *Service) RefreshNow(ctx context.Context) error { return s.Poll(ctx) }

// Poll fetches the feed, upserts open/upcoming IPOs, prunes everything else
// (closed/listed/gone), then evaluates last-day signals.
func (s *Service) Poll(ctx context.Context) error {
	now := time.Now().In(market.IST)
	items, err := s.client.Fetch(ctx, now)
	if err != nil {
		return err
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

	s.evaluateSignals(ctx, now)
	return nil
}

// evaluateSignals sends the last-day GMP signal (upgrade-only) for open IPOs
// whose close date is today.
func (s *Service) evaluateSignals(ctx context.Context, now time.Time) {
	if s.bc == nil {
		return
	}
	today := dateOnly(now)
	active, err := s.repo.ListActive(ctx)
	if err != nil {
		s.log.Error().Err(err).Msg("ipo: list for signals failed")
		return
	}
	for _, x := range active {
		if x.Status != "open" || x.CloseDate == nil || !sameDate(*x.CloseDate, today) {
			continue
		}
		tier := tierFor(x.GMPPercent)
		if tier == "" || tierRank(tier) <= tierRank(x.SignalTier) {
			continue // below threshold, or an equal/higher signal already sent
		}
		text := formatIPOMessage(x, signalLabel(tier), true)
		if _, err := s.bc.Broadcast(ctx, text); err != nil {
			s.log.Error().Err(err).Int64("ipo", x.ID).Msg("ipo signal broadcast failed")
			continue
		}
		if err := s.repo.SetSignalTier(ctx, x.ID, tier, time.Now()); err != nil {
			s.log.Error().Err(err).Msg("ipo: set signal tier failed")
		}
		s.log.Info().Str("ipo", x.Name).Str("tier", tier).Msg("ipo signal sent")
	}
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
		poll() // startup
		t := time.NewTicker(s.interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				poll()
			}
		}
	}()
	s.log.Info().Dur("interval", s.interval).Msg("ipo poller started")
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

func tierRank(tier string) int {
	switch tier {
	case "your_choice":
		return 1
	case "apply":
		return 2
	case "admin_apply":
		return 3
	default:
		return 0
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
