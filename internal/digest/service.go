package digest

import (
	"context"
	"fmt"
	"time"

	"github.com/robfig/cron/v3"
	"github.com/rs/zerolog"

	"tradenexus/internal/cronx"
	"tradenexus/internal/deals"
	"tradenexus/internal/market"
	"tradenexus/internal/promoter"
	"tradenexus/internal/users"
)

const topN = 5

// Service builds and sends the weekly digest. Depends on the promoter and
// deals services directly (in-process calls, no HTTP round-trip) since
// they already own the exact data/ranking this digest reuses.
type Service struct {
	promoter *promoter.Service
	deals    *deals.Service
	users    *users.Repo
	smtp     SMTPConfig
	log      zerolog.Logger
}

func New(promoterSvc *promoter.Service, dealsSvc *deals.Service, usersRepo *users.Repo, smtp SMTPConfig, log zerolog.Logger) *Service {
	return &Service{promoter: promoterSvc, deals: dealsSvc, users: usersRepo, smtp: smtp, log: log.With().Str("component", "digest").Logger()}
}

// SendWeekly builds the PDF once and emails it to every signed-up user.
// A failure sending to one recipient is logged and skipped, not fatal to
// the batch — only a data/PDF-build failure aborts the whole run.
func (s *Service) SendWeekly(ctx context.Context) (sent, failed int, err error) {
	promoters, err := s.promoter.ListStockBuying(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("load promoter buying: %w", err)
	}
	if len(promoters) > topN {
		promoters = promoters[:topN]
	}

	funds, err := s.deals.RecentFundAcquisitions(ctx, topN)
	if err != nil {
		return 0, 0, fmt.Errorf("load fund acquisitions: %w", err)
	}

	now := time.Now().In(market.IST)
	dateRange := fmt.Sprintf("Week of %s", now.AddDate(0, 0, -7).Format("02 Jan 2006"))

	pdfBytes, err := buildDigestPDF(dateRange, promoters, funds)
	if err != nil {
		return 0, 0, fmt.Errorf("build pdf: %w", err)
	}

	recipients, err := s.users.ListUsers(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("load users: %w", err)
	}

	subject := "Your Weekly TradeNexus Digest"
	body := digestHTML(dateRange, len(funds), len(promoters))

	for _, u := range recipients {
		if err := sendMail(s.smtp, u.Email, subject, body, pdfBytes, "tradenexus-weekly-digest.pdf"); err != nil {
			s.log.Warn().Err(err).Str("to", u.Email).Msg("digest: send failed")
			failed++
			continue
		}
		sent++
	}

	s.log.Info().Int("sent", sent).Int("failed", failed).Msg("digest: weekly send done")
	return sent, failed, nil
}

func digestHTML(dateRange string, fundCount, promoterCount int) string {
	return fmt.Sprintf(`<div style="font-family:sans-serif;color:#12141a;max-width:480px">
<h2 style="margin:0 0 4px">&gt;_ TradeNexus</h2>
<p style="color:#5b6472;margin:0 0 18px">%s</p>
<p>Attached: this week's top %d mutual fund acquisitions and top %d promoter buying stocks.</p>
<p style="color:#7d8590;font-size:12px;margin-top:24px">via TradeNexus</p>
</div>`, dateRange, fundCount, promoterCount)
}

// StartCron schedules SendWeekly on cronExpr (IST), mirroring every other
// domain service's own private cron instance (see ipo.Service.StartPolling).
func (s *Service) StartCron(ctx context.Context, cronExpr string) {
	c := cron.New(cron.WithLocation(market.IST), cron.WithChain(cronx.Recover(s.log)))
	if _, err := c.AddFunc(cronExpr, func() {
		sc, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		if _, _, err := s.SendWeekly(sc); err != nil {
			s.log.Error().Err(err).Msg("digest: scheduled send failed")
		}
	}); err != nil {
		s.log.Error().Err(err).Str("cron", cronExpr).Msg("digest cron invalid")
		return
	}
	c.Start()
	go func() { <-ctx.Done(); c.Stop() }()
	s.log.Info().Str("cron", cronExpr).Msg("digest scheduler started")
}
