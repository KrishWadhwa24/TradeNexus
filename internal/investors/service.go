package investors

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
	"github.com/rs/zerolog"

	"tradenexus/internal/cronx"
	"tradenexus/internal/market"
)

// catchUpWindow bounds the boot-time poll and the admin RefreshNow endpoint
// alike, to the last 30 days — wide enough to cover a stretch of downtime,
// narrow enough to bound NSE load and stay fast on every restart/click.
// dailyPollWindow bounds the routine scheduled poll (dailyPollCron) to just
// today + yesterday: on a server that's been up and polling daily, that's
// all a routine check ever needs; yesterday is a one-day safety margin in
// case a filing lands right after the previous day's run. FilterUnseen's
// dedup means only genuinely new filings within whichever window is used
// ever get re-parsed.
const (
	catchUpWindow        = 30 * 24 * time.Hour
	dailyPollWindow      = 2 * 24 * time.Hour
	seenFilingsRetention = 40 * 24 * time.Hour
	dailyPollCron        = "0 18 * * *" // 6:00 PM IST
)

// Service polls the NSE SHP feed and keeps only the positions of curated,
// well-known investors (Tracked) — everything else in a filing is parsed and
// discarded. No alerting: this is a research/browse feature, not a signal
// feed, unlike promoter/deals.
type Service struct {
	client *Client
	repo   *Repo
	log    zerolog.Logger

	mu       sync.Mutex
	lastPoll time.Time
}

func New(client *Client, repo *Repo, log zerolog.Logger) *Service {
	return &Service{client: client, repo: repo, log: log}
}

// Poll fetches the SHP filing list for the last catchUpWindow days, parses
// any filing not yet inspected, and upserts a position for every named
// shareholder that matches a Tracked investor.
func (s *Service) Poll(ctx context.Context) error {
	return s.pollWindow(ctx, catchUpWindow)
}

// pollWindow is Poll with an explicit lookback window — shared by the
// boot-time/admin catch-up (catchUpWindow) and the routine scheduled poll
// (dailyPollWindow).
func (s *Service) pollWindow(ctx context.Context, window time.Duration) error {
	now := time.Now().In(market.IST)
	filings, err := s.client.FetchFilings(ctx, now.Add(-window), now)
	if err != nil {
		return err
	}

	ids := make([]string, len(filings))
	for i, f := range filings {
		ids[i] = f.RecordID
	}
	unseen, err := s.repo.FilterUnseen(ctx, ids)
	if err != nil {
		return err
	}
	unseenSet := make(map[string]bool, len(unseen))
	for _, id := range unseen {
		unseenSet[id] = true
	}

	matched := 0
	for _, f := range filings {
		if !unseenSet[f.RecordID] {
			continue
		}
		detail, err := s.client.FetchDetail(ctx, f.XBRLURL)
		if err != nil {
			s.log.Error().Err(err).Str("record_id", f.RecordID).Msg("investors: fetch detail failed, will retry next poll")
			continue // don't mark seen — retry on the next poll
		}

		symbol := f.Symbol
		if symbol == "" {
			symbol = detail.Symbol
		}
		company := f.CompanyName
		if company == "" {
			company = detail.CompanyName
		}
		reportDate := f.ReportDate
		if reportDate.IsZero() {
			reportDate = detail.ReportDate
		}

		stillHeld := make([]string, 0, len(detail.Shareholders))
		for _, sh := range detail.Shareholders {
			inv := match(sh.Name)
			if inv == nil {
				continue
			}
			key := normalize(inv.Name)
			stillHeld = append(stillHeld, key)
			h := Holding{
				InvestorName:  inv.Name,
				Symbol:        symbol,
				CompanyName:   company,
				Shares:        sh.Shares,
				PctHolding:    sh.PctHolding,
				ReportDate:    reportDate,
				FirstSeenDate: reportDate,
			}
			if err := s.repo.UpsertHolding(ctx, key, h); err != nil {
				s.log.Error().Err(err).Str("investor", inv.Name).Str("symbol", symbol).Msg("investors: upsert failed")
				continue
			}
			matched++
		}

		// Anyone we previously tracked in this stock, but who isn't named in
		// this newer filing, has sold out or dropped below NSE's disclosure
		// threshold since — see RemoveStaleHoldings.
		if removed, err := s.repo.RemoveStaleHoldings(ctx, symbol, reportDate, stillHeld); err != nil {
			s.log.Error().Err(err).Str("symbol", symbol).Msg("investors: remove stale holdings failed")
		} else if removed > 0 {
			s.log.Info().Str("symbol", symbol).Int64("removed", removed).Msg("investors: stale holdings removed")
		}

		if err := s.repo.MarkSeen(ctx, f.RecordID); err != nil {
			s.log.Error().Err(err).Str("record_id", f.RecordID).Msg("investors: mark-seen failed")
		}
		time.Sleep(300 * time.Millisecond) // be polite to NSE between filings
	}

	if _, err := s.repo.PruneSeenOlderThan(ctx, now.Add(-seenFilingsRetention)); err != nil {
		s.log.Error().Err(err).Msg("investors: prune seen failed")
	}
	s.log.Info().Int("filings", len(filings)).Int("matched_holdings", matched).Msg("investors: poll done")
	return nil
}

// RefreshNow triggers an out-of-band poll (admin action), cooldown-guarded
// so repeated clicks can't stack overlapping runs against NSE. The cooldown
// check is synchronous (so the caller learns immediately if one's already
// running); the poll itself runs in the background, since a first-ever run
// can take up to the 2-hour budget StartPolling gives it.
func (s *Service) RefreshNow() (bool, error) {
	s.mu.Lock()
	if wait := 2*time.Minute - time.Since(s.lastPoll); wait > 0 {
		s.mu.Unlock()
		return false, fmt.Errorf("a scan already ran recently — try again in %s", wait.Round(time.Second))
	}
	s.lastPoll = time.Now()
	s.mu.Unlock()

	go func() {
		c, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
		defer cancel()
		if err := s.pollWindow(c, catchUpWindow); err != nil {
			s.log.Error().Err(err).Msg("investors manual refresh failed")
		}
	}()
	return true, nil
}

// StartPolling runs an immediate 30-day catch-up poll on boot (covers
// whatever gap there was since the server last ran), then a narrow
// today+yesterday poll every day at dailyPollCron (6:00 PM IST) — that's the
// routine, steady-state check, not another wide catch-up. Both stop when ctx
// is cancelled.
func (s *Service) StartPolling(ctx context.Context) {
	go cronx.Safe(s.log, func() {
		c, cancel := context.WithTimeout(context.Background(), 2*time.Hour)
		defer cancel()
		if err := s.pollWindow(c, catchUpWindow); err != nil {
			s.log.Error().Err(err).Msg("investors boot catch-up poll failed")
		}
	})

	cr := cron.New(cron.WithLocation(market.IST), cron.WithChain(cronx.Recover(s.log)))
	if _, err := cr.AddFunc(dailyPollCron, func() {
		c, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		if err := s.pollWindow(c, dailyPollWindow); err != nil {
			s.log.Error().Err(err).Msg("investors daily poll failed")
		}
	}); err != nil {
		s.log.Error().Err(err).Msg("investors daily poll cron invalid")
		return
	}
	cr.Start()
	go func() { <-ctx.Done(); cr.Stop() }()
}

// ListInvestors returns tracked-investor summary cards for the list view.
func (s *Service) ListInvestors(ctx context.Context) ([]InvestorSummary, error) {
	return s.repo.ListInvestors(ctx)
}

// GetInvestor returns one tracked investor's current holdings.
func (s *Service) GetInvestor(ctx context.Context, name string) ([]Holding, error) {
	return s.repo.HoldingsForInvestor(ctx, normalize(name))
}

// GetStockHoldings returns every tracked investor currently holding one
// stock — unused today (no caller yet), kept for the eventual Stock 360
// integration once that branch merges.
func (s *Service) GetStockHoldings(ctx context.Context, symbol string) ([]Holding, error) {
	return s.repo.HoldingsForSymbol(ctx, symbol)
}
