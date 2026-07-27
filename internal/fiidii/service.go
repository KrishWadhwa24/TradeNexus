package fiidii

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"tradenexus/internal/market"
)

// marketCloseHour is when NSE's cash session closes (4:00 PM IST) — the
// earliest we ever attempt a fetch on a trading day.
const marketCloseHour = 16

// retryInterval is how long to wait before re-trying when today's data isn't
// published yet.
const retryInterval = time.Hour

// Broadcaster sends a plain message to every Telegram destination.
// Implemented by notify.Dispatcher.
type Broadcaster interface {
	Broadcast(ctx context.Context, text string) (int, error)
}

// Calendar reports whether a given day is an NSE trading day. Implemented by
// *calendar.Calendar (via calendar.Service.Cal()).
type Calendar interface {
	IsTradingDay(t time.Time) bool
}

// Service holds only the most recently fetched snapshot in memory — only the
// per-date auto-alert ledger (Repo) survives a restart.
type Service struct {
	client *Client
	bc     Broadcaster // optional; nil disables the Telegram alert
	cal    Calendar
	repo   *Repo
	log    zerolog.Logger

	mu                sync.RWMutex
	latest            Snapshot
	readyDate         string // IST date (2006-01-02) we've confirmed THAT DAY's data for
	reconcileDoneDate string // IST date the daily reconcile+scan+dispatch pipeline finished for
	alertedDate       string // IST date the auto Telegram alert has already been sent for, this process
}

// New builds the FII/DII service. bc may be nil (disables the Telegram alert
// entirely; the API/table still works off whatever's fetched).
func New(client *Client, bc Broadcaster, cal Calendar, repo *Repo, log zerolog.Logger) *Service {
	return &Service{client: client, bc: bc, cal: cal, repo: repo, log: log}
}

// Latest returns the most recently fetched snapshot, and false if nothing has
// been fetched yet (e.g. the very first moment after boot).
func (s *Service) Latest() (Snapshot, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.latest, s.latest.Date != ""
}

// SendAlert immediately Telegrams whatever snapshot is currently cached —
// the manual admin override. It bypasses the reconcile-done gate entirely,
// same spirit as the other admin "send alert now" endpoints.
func (s *Service) SendAlert(ctx context.Context) error {
	if s.bc == nil {
		return fmt.Errorf("notifications disabled")
	}
	s.mu.RLock()
	snap := s.latest
	s.mu.RUnlock()
	if snap.Date == "" {
		return fmt.Errorf("no FII/DII data fetched yet")
	}
	if _, err := s.bc.Broadcast(ctx, formatMessage(snap)); err != nil {
		return err
	}
	if isTodayIST(snap.Date) {
		s.mu.Lock()
		s.alertedDate = dateKey(time.Now().In(market.IST))
		s.mu.Unlock()
	}
	return nil
}

// MarkReconcileDone records that the daily candle-reconcile + scan + signal-
// dispatch pipeline has finished for today, then sends the auto Telegram
// alert if today's FII/DII data is already in hand. NSE usually publishes
// FII/DII later in the evening than reconcile finishes, so most days this
// just sets the flag; the poll loop's fetchAndStore is what actually
// triggers the send once the data lands.
func (s *Service) MarkReconcileDone(now time.Time) {
	s.mu.Lock()
	s.reconcileDoneDate = dateKey(now.In(market.IST))
	s.mu.Unlock()
	s.maybeSendAlert()
}

// StartPolling fetches once immediately (so the UI has something to show
// right away, even stale weekend data), then loops: only attempting a fetch
// after 4pm IST on a trading day, retrying hourly until that day's data is
// published, and going quiet for the rest of the day once it is.
func (s *Service) StartPolling(ctx context.Context) {
	go func() {
		s.bootstrap(ctx)
		for {
			now := time.Now().In(market.IST)
			d := s.nextDelay(now)
			select {
			case <-ctx.Done():
				return
			case <-time.After(d):
			}
			s.tick(ctx)
		}
	}()
	s.log.Info().Msg("fiidii poller started")
}

func (s *Service) bootstrap(ctx context.Context) {
	c, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := s.fetchAndStore(c); err != nil {
		s.log.Warn().Err(err).Msg("fiidii: startup fetch failed")
	}
}

// nextDelay decides how long to sleep before the next check:
//   - already have today's data, or today isn't a trading day → nothing to
//     wait for; sleep until tomorrow's close time.
//   - before 4pm IST on a trading day → sleep until 4pm.
//   - after 4pm IST, trading day, not yet published → retry in an hour.
func (s *Service) nextDelay(now time.Time) time.Duration {
	tomorrowClose := time.Date(now.Year(), now.Month(), now.Day()+1, marketCloseHour, 0, 0, 0, market.IST)

	s.mu.RLock()
	ready := s.readyDate == dateKey(now)
	s.mu.RUnlock()
	if ready || !s.cal.IsTradingDay(now) {
		return tomorrowClose.Sub(now)
	}

	closeToday := time.Date(now.Year(), now.Month(), now.Day(), marketCloseHour, 0, 0, 0, market.IST)
	if now.Before(closeToday) {
		return closeToday.Sub(now)
	}
	return retryInterval
}

func (s *Service) tick(ctx context.Context) {
	now := time.Now().In(market.IST)
	if !s.cal.IsTradingDay(now) || now.Hour() < marketCloseHour {
		return // shouldn't normally happen (nextDelay already gates this), stay safe anyway
	}
	c, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := s.fetchAndStore(c); err != nil {
		s.log.Warn().Err(err).Msg("fiidii: fetch failed, will retry in an hour")
	}
}

func (s *Service) fetchAndStore(ctx context.Context) error {
	snap, err := s.client.Fetch(ctx)
	if err != nil {
		return err
	}

	isToday := isTodayIST(snap.Date)
	s.mu.Lock()
	s.latest = snap
	if isToday {
		s.readyDate = dateKey(time.Now().In(market.IST))
	}
	s.mu.Unlock()

	if isToday {
		s.maybeSendAlert()
	}
	return nil
}

// maybeSendAlert sends the auto Telegram alert once both the FII/DII data AND
// the daily reconcile pipeline are confirmed done for the same date, and it
// hasn't already been sent for that date — checked against the DB ledger so
// a server restart never re-sends a date that already went out. Whichever of
// the two finishes second is what actually triggers the send.
func (s *Service) maybeSendAlert() {
	if s.bc == nil {
		return
	}
	s.mu.Lock()
	date := s.readyDate
	ready := date != "" && date == s.reconcileDoneDate && date != s.alertedDate
	snap := s.latest
	if ready {
		s.alertedDate = date // claim it now so a concurrent trigger in this process can't double-send
	}
	s.mu.Unlock()
	if !ready {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	day, err := parseNSEDate(snap.Date)
	if err != nil {
		s.log.Error().Err(err).Str("date", snap.Date).Msg("fiidii: bad snapshot date")
		return
	}
	// Ledger check guards against a restart: the in-memory claim above only
	// protects this process, but the alert may already have gone out for this
	// date in a previous run.
	if done, err := s.repo.AlreadyAlerted(ctx, day); err != nil {
		s.log.Error().Err(err).Msg("fiidii: alert-ledger check failed")
		s.unclaimAlert(date)
		return
	} else if done {
		s.log.Info().Str("date", snap.Date).Msg("fiidii: auto alert already sent, skipping")
		return
	}

	if _, err := s.bc.Broadcast(ctx, formatMessage(snap)); err != nil {
		s.log.Error().Err(err).Msg("fiidii: auto alert send failed")
		s.unclaimAlert(date)
		return
	}
	if err := s.repo.MarkAlerted(ctx, day); err != nil {
		s.log.Error().Err(err).Msg("fiidii: mark alerted failed")
	}
	s.log.Info().Str("date", snap.Date).Msg("fiidii: auto alert sent")
}

// unclaimAlert releases the in-memory claim so the next trigger can retry.
func (s *Service) unclaimAlert(date string) {
	s.mu.Lock()
	if s.alertedDate == date {
		s.alertedDate = ""
	}
	s.mu.Unlock()
}

// parseNSEDate parses an NSE-formatted date ("02-Jan-2006") as used in
// Snapshot.Date.
func parseNSEDate(nseDate string) (time.Time, error) {
	return time.Parse("02-Jan-2006", nseDate)
}

// formatMessage builds the Telegram alert for a DII/FII snapshot.
func formatMessage(snap Snapshot) string {
	var b strings.Builder
	fmt.Fprintf(&b, "📊 FII / DII ACTIVITY — %s\n", snap.Date)
	b.WriteString("━━━━━━━━━━━━━━━━━━━\n")
	writeFlow(&b, "🏦 DII", snap.DII)
	writeFlow(&b, "🌍 FII", snap.FII)
	return strings.TrimRight(b.String(), "\n")
}

func writeFlow(b *strings.Builder, label string, f Flow) {
	mark := "🟢"
	if f.NetValue < 0 {
		mark = "🔴"
	}
	fmt.Fprintf(b, "%s\nBuy: ₹%.2f Cr  ·  Sell: ₹%.2f Cr\nNet: %s ₹%.2f Cr\n\n", label, f.BuyValue, f.SellValue, mark, f.NetValue)
}

func isTodayIST(nseDate string) bool {
	t, err := time.Parse("02-Jan-2006", nseDate)
	if err != nil {
		return false
	}
	now := time.Now().In(market.IST)
	return t.Year() == now.Year() && t.Month() == now.Month() && t.Day() == now.Day()
}

func dateKey(t time.Time) string { return t.Format("2006-01-02") }
