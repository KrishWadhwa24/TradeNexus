// Package scheduler runs the recurring jobs: the daily post-close scan (with
// reconciliation/backfill) and the signal retention cleanup. Times are IST.
package scheduler

import (
	"context"
	"time"

	"github.com/robfig/cron/v3"
	"github.com/rs/zerolog"

	"tradenexus/internal/cronx"
	"tradenexus/internal/engine"
	"tradenexus/internal/fiidii"
	"tradenexus/internal/intraday"
	"tradenexus/internal/market"
	"tradenexus/internal/paper"
)

// Config controls the schedule.
type Config struct {
	Enabled                bool
	DailyScanCron          string        // e.g. "0 16 * * 1-5" (16:00 IST, Mon-Fri)
	CleanupCron            string        // e.g. "0 1 * * *"    (01:00 IST daily)
	FillScheduledCron      string        // e.g. "16 9 * * 1-5" (09:16 IST, at market open)
	SquareOffIntradayCron  string        // e.g. "20 15 * * 1-5" (15:20 IST, intraday cutoff)
	PaperFillRetryInterval time.Duration // backstop retry cadence for scheduled paper fills
	IntradayInterval       time.Duration // refresh cadence for the intraday cache
	RunReconcileOnBoot     bool
}

// Scheduler wraps a cron instance bound to the engine + paper services.
type Scheduler struct {
	cron     *cron.Cron
	svc      *engine.Service
	paper    *paper.Service
	intraday *intraday.Cache // optional
	fiidii   *fiidii.Service // optional
	cfg      Config
	log      zerolog.Logger
}

// New builds a scheduler (IST-based cron). intradayCache and fiidiiSvc may be nil.
func New(svc *engine.Service, paperSvc *paper.Service, intradayCache *intraday.Cache, fiidiiSvc *fiidii.Service, cfg Config, log zerolog.Logger) *Scheduler {
	return &Scheduler{
		cron:     cron.New(cron.WithLocation(market.IST), cron.WithChain(cronx.Recover(log))),
		svc:      svc,
		paper:    paperSvc,
		intraday: intradayCache,
		fiidii:   fiidiiSvc,
		cfg:      cfg,
		log:      log,
	}
}

// notifyReconcileDone tells the FII/DII service the daily reconcile+scan+
// dispatch pipeline finished for today, so it can send its own alert now if
// today's FII/DII data is already in hand.
func (s *Scheduler) notifyReconcileDone() {
	if s.fiidii != nil {
		s.fiidii.MarkReconcileDone(time.Now())
	}
}

// Start registers jobs and (optionally) kicks off a startup reconciliation.
func (s *Scheduler) Start(ctx context.Context) error {
	if !s.cfg.Enabled {
		s.log.Info().Msg("scheduler disabled")
		return nil
	}

	if _, err := s.cron.AddFunc(s.cfg.DailyScanCron, func() {
		s.log.Info().Msg("scheduler: daily scan starting")
		jobCtx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		res, err := s.svc.ReconcileAll(jobCtx)
		if err != nil {
			s.log.Error().Err(err).Msg("scheduler: daily scan failed")
			return
		}
		s.log.Info().Int("instruments", len(res)).Msg("scheduler: daily scan done")
		s.notifyReconcileDone()
	}); err != nil {
		return err
	}

	if _, err := s.cron.AddFunc(s.cfg.CleanupCron, func() {
		jobCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		deleted, err := s.svc.Cleanup(jobCtx)
		if err != nil {
			s.log.Error().Err(err).Msg("scheduler: cleanup failed")
			return
		}
		s.log.Info().Int64("deleted", deleted).Msg("scheduler: retention cleanup done")
	}); err != nil {
		return err
	}

	// Fill SCHEDULED paper trades at market open.
	if s.paper != nil && s.cfg.FillScheduledCron != "" {
		if _, err := s.cron.AddFunc(s.cfg.FillScheduledCron, func() {
			jobCtx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer cancel()
			n, err := s.paper.FillScheduled(jobCtx)
			if err != nil {
				s.log.Error().Err(err).Msg("scheduler: fill scheduled trades failed")
				return
			}
			s.log.Info().Int("filled", n).Msg("scheduler: scheduled paper trades filled")
		}); err != nil {
			return err
		}
		// Same tick fills DELIVERY closes scheduled while the market was
		// shut — same "market just opened" timing as scheduled buys above.
		if _, err := s.cron.AddFunc(s.cfg.FillScheduledCron, func() {
			jobCtx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer cancel()
			n, err := s.paper.FillScheduledCloses(jobCtx)
			if err != nil {
				s.log.Error().Err(err).Msg("scheduler: fill scheduled closes failed")
				return
			}
			s.log.Info().Int("filled", n).Msg("scheduler: scheduled paper closes filled")
		}); err != nil {
			return err
		}
	}

	// Auto-square-off OPEN intraday paper positions at the daily cutoff.
	if s.paper != nil && s.cfg.SquareOffIntradayCron != "" {
		if _, err := s.cron.AddFunc(s.cfg.SquareOffIntradayCron, func() {
			jobCtx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			defer cancel()
			n, err := s.paper.SquareOffIntraday(jobCtx)
			if err != nil {
				s.log.Error().Err(err).Msg("scheduler: square off intraday failed")
				return
			}
			s.log.Info().Int("closed", n).Msg("scheduler: intraday positions squared off")
		}); err != nil {
			return err
		}
	}

	// Backstop retry for scheduled paper fills: FillScheduledCron above is
	// the normal, low-latency path (fires once, right at market open), but
	// a single tick can fail wholesale (a DB hiccup) or fill some rows and
	// silently skip others (a transient price-lookup error) — and nothing
	// else would retry it until tomorrow's cron. This ticker re-runs the
	// same two (idempotent — they only ever touch rows still pending) fill
	// functions every PaperFillRetryInterval throughout market hours, so a
	// stuck order gets picked up again within minutes instead of a full day.
	if s.paper != nil && s.cfg.PaperFillRetryInterval > 0 {
		go func() {
			t := time.NewTicker(s.cfg.PaperFillRetryInterval)
			defer t.Stop()
			for range t.C {
				s.safeCall(func() {
					if !s.paper.MarketOpen(time.Now().In(market.IST)) {
						return
					}
					jobCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
					defer cancel()
					if n, err := s.paper.FillScheduled(jobCtx); err != nil {
						s.log.Error().Err(err).Msg("scheduler: retry fill scheduled trades failed")
					} else if n > 0 {
						s.log.Info().Int("filled", n).Msg("scheduler: retry filled scheduled trades")
					}
					if n, err := s.paper.FillScheduledCloses(jobCtx); err != nil {
						s.log.Error().Err(err).Msg("scheduler: retry fill scheduled closes failed")
					} else if n > 0 {
						s.log.Info().Int("filled", n).Msg("scheduler: retry filled scheduled closes")
					}
				})
			}
		}()
		s.log.Info().Dur("interval", s.cfg.PaperFillRetryInterval).Msg("paper fill retry backstop started")
	}

	s.cron.Start()
	s.log.Info().Str("daily", s.cfg.DailyScanCron).Str("cleanup", s.cfg.CleanupCron).Msg("scheduler started")

	// Intraday cache refresher: every IntradayInterval, but only while the
	// market is open. The initial warm is NOT done here — it runs in the startup
	// goroutine below, sequenced after reconcile, so the two don't hammer Angel
	// at the same time. A Redis lock in Refresh also prevents overlap.
	if s.intraday != nil && s.cfg.IntradayInterval > 0 {
		go func() {
			t := time.NewTicker(s.cfg.IntradayInterval)
			defer t.Stop()
			for range t.C {
				s.safeCall(func() {
					if !s.intraday.MarketOpen(time.Now().In(market.IST)) {
						return
					}
					c, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
					if _, err := s.intraday.Refresh(c); err != nil {
						s.log.Error().Err(err).Msg("scheduler: intraday refresh failed")
					}
					cancel()
				})
			}
		}()
		s.log.Info().Dur("interval", s.cfg.IntradayInterval).Msg("intraday cache refresher started")
	}

	// Startup work runs SEQUENTIALLY: fill any SCHEDULED paper trades if the
	// market is already open (covers a server restart or downtime that spans
	// market open, so trades don't sit stranded until the next cron tick),
	// then reconcile/backfill, then (only if the market is open) warm the
	// intraday cache. Running them one after the other avoids concurrent bulk
	// Angel workflows tripping the rate limiter.
	go s.safeCall(func() {
		if s.paper != nil {
			bootCtx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
			if n, err := s.paper.FillScheduledIfMarketOpen(bootCtx); err != nil {
				s.log.Error().Err(err).Msg("scheduler: startup fill scheduled trades failed")
			} else if n > 0 {
				s.log.Info().Int("filled", n).Msg("scheduler: startup filled scheduled trades (market already open)")
			}
			cancel()

			closesCtx, cancelCloses := context.WithTimeout(context.Background(), 10*time.Minute)
			if n, err := s.paper.FillScheduledClosesIfMarketOpen(closesCtx); err != nil {
				s.log.Error().Err(err).Msg("scheduler: startup fill scheduled closes failed")
			} else if n > 0 {
				s.log.Info().Int("filled", n).Msg("scheduler: startup filled scheduled closes (market already open)")
			}
			cancelCloses()

			// Crash-recovery for the intraday cutoff: a cron trigger only
			// fires if the process is running at that exact moment, so if
			// the server was down at 3:20pm and comes back up any time
			// later, this catches up immediately instead of leaving
			// intraday positions open until the next trading day's cron.
			squareOffCtx, cancel2 := context.WithTimeout(context.Background(), 10*time.Minute)
			if n, err := s.paper.SquareOffIntradayIfPastCutoff(squareOffCtx); err != nil {
				s.log.Error().Err(err).Msg("scheduler: startup square off intraday failed")
			} else if n > 0 {
				s.log.Info().Int("closed", n).Msg("scheduler: startup squared off intraday positions (past cutoff)")
			}
			cancel2()
		}
		if s.cfg.RunReconcileOnBoot {
			bootCtx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
			s.log.Info().Msg("scheduler: startup reconciliation starting")
			if res, err := s.svc.ReconcileAll(bootCtx); err != nil {
				s.log.Error().Err(err).Msg("scheduler: startup reconciliation failed")
			} else {
				s.log.Info().Int("instruments", len(res)).Msg("scheduler: startup reconciliation done")
				s.notifyReconcileDone()
			}
			cancel()
		}
		if s.intraday != nil && s.intraday.MarketOpen(time.Now().In(market.IST)) {
			c, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
			if _, err := s.intraday.Refresh(c); err != nil {
				s.log.Error().Err(err).Msg("scheduler: startup intraday warm failed")
			}
			cancel()
		}
	})
	return nil
}

// safeCall recovers a panic in fn so one bad startup/tick step logs and the
// scheduler keeps running instead of taking down the whole process.
func (s *Scheduler) safeCall(fn func()) {
	defer func() {
		if r := recover(); r != nil {
			s.log.Error().Interface("panic", r).Msg("scheduler: recovered panic")
		}
	}()
	fn()
}

// Stop halts the cron scheduler, waiting for running jobs to finish.
func (s *Scheduler) Stop() {
	if s.cron != nil {
		ctx := s.cron.Stop()
		<-ctx.Done()
	}
}
