// Command server is the TradeNexus API entrypoint. It loads config, applies DB
// migrations, connects Postgres + Redis, builds the shared Angel rate limiter,
// and serves the HTTP API with graceful shutdown.
package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"tradenexus/internal/analytics"
	"tradenexus/internal/angel"
	"tradenexus/internal/api"
	"tradenexus/internal/calendar"
	"tradenexus/internal/candles"
	"tradenexus/internal/config"
	"tradenexus/internal/engine"
	"tradenexus/internal/instruments"
	"tradenexus/internal/live"
	"tradenexus/internal/logger"
	"tradenexus/internal/notify"
	"tradenexus/internal/paper"
	"tradenexus/internal/ratelimit"
	"tradenexus/internal/scanner"
	"tradenexus/internal/scheduler"
	"tradenexus/internal/signals"
	"tradenexus/internal/store"
	"tradenexus/internal/users"
	"tradenexus/internal/cacher"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		// Logger not built yet; use stdlib for this one fatal case.
		panic(err)
	}

	log := logger.New(cfg.LogLevel, cfg.IsLocal())
	log.Info().Str("env", cfg.AppEnv).Str("port", cfg.HTTPPort).Msg("starting tradenexus")

	// Root context cancelled on SIGINT/SIGTERM.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// 1) Migrations (idempotent). Fail fast if the schema can't be applied.
	if err := store.RunMigrations(cfg.DatabaseURL); err != nil {
		log.Fatal().Err(err).Msg("migrations failed")
	}
	log.Info().Msg("migrations up to date")

	// 2) Postgres.
	pg, err := store.NewPostgres(ctx, cfg.DatabaseURL, cfg.PGMaxConns, cfg.PGMinConns)
	if err != nil {
		log.Fatal().Err(err).Msg("connect postgres")
	}
	defer pg.Close()
	log.Info().Msg("postgres connected")

	// 3) Redis.
	rdb, err := store.NewRedis(ctx, cfg.RedisAddr, cfg.RedisPassword, cfg.RedisDB)
	if err != nil {
		log.Fatal().Err(err).Msg("connect redis")
	}
	defer func() { _ = rdb.Close() }()
	log.Info().Msg("redis connected")

	// 4) Shared Angel historical rate limiter (Redis token bucket).
	angelLimiter := ratelimit.New(rdb.Client, "angel:historical", cfg.AngelHistRate, cfg.AngelHistBurst)
	log.Info().Float64("rate", cfg.AngelHistRate).Int("burst", cfg.AngelHistBurst).Msg("angel rate limiter ready")

	// 5) Angel client + repositories.
	angelClient := angel.New(angel.Config{
		APIKey:              cfg.AngelAPIKey,
		ClientCode:          cfg.AngelClientCode,
		PIN:                 cfg.AngelPIN,
		TOTPSecret:          cfg.AngelTOTPSecret,
		ScripMasterURL:      cfg.AngelScripMasterURL,
		ScripMasterTimeout:  cfg.AngelScripMasterTimeout,
		ScripMasterAttempts: cfg.AngelScripMasterAttempts,
	}, angelLimiter, log)
	instRepo := instruments.NewRepo(pg.Pool)
	candleRepo := candles.NewRepo(pg.Pool)
	signalRepo := signals.NewRepo(pg.Pool)
	userRepo := users.NewRepo(pg.Pool)

	// 6) Calendar (load holidays from DB).
	calSvc := calendar.NewService(pg.Pool, cfg.Exchange)
	if err := calSvc.Reload(ctx); err != nil {
		log.Warn().Err(err).Msg("calendar reload failed (continuing with weekends-only)")
	}

	// 7) Notification dispatcher (fan-out + 7-day window + dedup). Optional.
	var dispatcher *notify.Dispatcher
	if cfg.NotifyEnabled {
		dispatcher = notify.New(
			pg.Pool, notify.NewTelegram(cfg.TelegramBaseURL), cfg.NotifyWindowDays,
			cfg.TelegramDefaultBotToken, cfg.TelegramDefaultChatID, log,
		)
	}

	// 8) Engine service (scan pipeline + reconciliation + notify).
	engineSvc := engine.New(
		candleRepo, signalRepo, instRepo, angelClient, calSvc, dispatcher, rdb,
		scanner.DefaultPineConfig(),
		time.Duration(cfg.RetentionDays)*24*time.Hour,
		log,
	)

	// Analytics service (dashboard + Excel export).
	analyticsSvc := analytics.NewService(pg.Pool)

	// Live price websocket fan-out. This owns the Angel stream connection and
	// keeps it separate from scanning/reconciliation services.
	liveHub := live.NewHub(angelClient, log)

	// Paper-trading service.
	paperSvc := paper.New(pg.Pool, angelClient, candleRepo, instRepo, signalRepo, calSvc, log)

	// Cacher service.
	cacherSvc := cacher.New(*cfg, log, instRepo, candleRepo, angelClient, rdb, calSvc)
	cacherSvc.Start()
	defer cacherSvc.Stop()

	// 8) Scheduler (daily scan + cleanup + startup reconciliation + fill).
	sched := scheduler.New(engineSvc, paperSvc, scheduler.Config{
		Enabled:            cfg.SchedulerEnabled,
		DailyScanCron:      cfg.DailyScanCron,
		CleanupCron:        cfg.CleanupCron,
		FillScheduledCron:  cfg.FillScheduledCron,
		RunReconcileOnBoot: cfg.ReconcileOnStartup,
	}, log)
	if err := sched.Start(ctx); err != nil {
		log.Fatal().Err(err).Msg("start scheduler")
	}
	defer sched.Stop()

	// 9) HTTP server.
	srv := &http.Server{
		Addr: ":" + cfg.HTTPPort,
		Handler: api.NewServer(api.Deps{
			Log:         log,
			PG:          pg,
			RDB:         rdb,
			Limiter:     angelLimiter,
			Angel:       angelClient,
			Instruments: instRepo,
			Candles:     candleRepo,
			Engine:      engineSvc,
			Signals:     signalRepo,
			Calendar:    calSvc,
			Users:       userRepo,
			Notifier:    dispatcher,
			Analytics:   analyticsSvc,
			Paper:       paperSvc,
			Live:        liveHub,
			JWTSecret:   cfg.JWTSecret,
		}).Router(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	serverErr := make(chan error, 1)
	go func() {
		log.Info().Str("addr", srv.Addr).Msg("http listening")
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	// Block until shutdown signal or a fatal server error.
	select {
	case err := <-serverErr:
		log.Error().Err(err).Msg("http server error")
	case <-ctx.Done():
		log.Info().Msg("shutdown signal received")
	}

	// Graceful shutdown.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Error().Err(err).Msg("graceful shutdown failed")
		os.Exit(1)
	}
	log.Info().Msg("bye")
}
