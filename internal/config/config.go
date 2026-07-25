// Package config loads all runtime configuration from environment variables
// (optionally seeded from a .env file). Everything the app needs to boot lives
// here so wiring in main.go stays declarative.
package config

import (
	"fmt"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/joho/godotenv"
)

// Config is the fully-parsed application configuration.
type Config struct {
	// Server
	AppEnv          string        `env:"APP_ENV" envDefault:"local"`
	HTTPPort        string        `env:"HTTP_PORT" envDefault:"8080"`
	LogLevel        string        `env:"LOG_LEVEL" envDefault:"info"`
	ShutdownTimeout time.Duration `env:"SHUTDOWN_TIMEOUT" envDefault:"10s"`

	// Postgres
	DatabaseURL string `env:"DATABASE_URL,required"`
	PGMaxConns  int32  `env:"PG_MAX_CONNS" envDefault:"10"`
	PGMinConns  int32  `env:"PG_MIN_CONNS" envDefault:"2"`

	// Redis
	RedisAddr     string `env:"REDIS_ADDR" envDefault:"localhost:6379"`
	RedisPassword string `env:"REDIS_PASSWORD" envDefault:""`
	RedisDB       int    `env:"REDIS_DB" envDefault:"0"`

	// Angel SmartAPI (unused until Module 2, parsed here so it's ready).
	AngelAPIKey     string `env:"ANGEL_API_KEY" envDefault:""`
	AngelClientCode string `env:"ANGEL_CLIENT_CODE" envDefault:""`
	AngelPIN        string `env:"ANGEL_PIN" envDefault:""`
	AngelTOTPSecret string `env:"ANGEL_TOTP_SECRET" envDefault:""`

	// Angel rate limiting
	AngelHistRate float64 `env:"ANGEL_HIST_RATE" envDefault:"2"`
	// Burst is deliberately 1: with >1 the bucket can release two requests to
	// Angel at the same instant, and Angel's real server-side cap is stricter
	// than our configured rate — bursts of 2+ are what triggers its rate-limit
	// errors. Keeping burst at 1 still lets concurrent workers overlap request
	// latency (see intraday.Cache.Refresh) without ever dispatching two calls
	// at once.
	AngelHistBurst           int           `env:"ANGEL_HIST_BURST" envDefault:"1"`
	AngelScripMasterTimeout  time.Duration `env:"ANGEL_SCRIPMASTER_TIMEOUT" envDefault:"5m"`
	AngelScripMasterAttempts int           `env:"ANGEL_SCRIPMASTER_ATTEMPTS" envDefault:"3"`
	AngelScripMasterURL      string        `env:"ANGEL_SCRIPMASTER_URL" envDefault:""`

	// Exchange / calendar
	Exchange string `env:"EXCHANGE" envDefault:"NSE"`

	// MarketCloseBufferMin is the grace period (minutes) after the 15:30 IST
	// close before a daily candle is treated as finalized (Angel EOD lag).
	MarketCloseBufferMin int `env:"MARKET_CLOSE_BUFFER_MIN" envDefault:"15"`

	// Auth
	JWTSecret string `env:"JWT_SECRET" envDefault:"dev-change-me-please"`

	// Admin bootstrap: if both are set, an admin account is upserted on boot.
	AdminEmail    string `env:"ADMIN_EMAIL" envDefault:""`
	AdminPassword string `env:"ADMIN_PASSWORD" envDefault:""`

	// Scanner / scheduler
	SchedulerEnabled   bool   `env:"SCHEDULER_ENABLED" envDefault:"true"`
	DailyScanCron      string `env:"DAILY_SCAN_CRON" envDefault:"0 16 * * 1-5"`
	CleanupCron        string `env:"CLEANUP_CRON" envDefault:"0 1 * * *"`
	FillScheduledCron  string `env:"FILL_SCHEDULED_CRON" envDefault:"16 9 * * 1-5"`
	RetentionDays      int    `env:"RETENTION_DAYS" envDefault:"30"`
	ReconcileOnStartup bool   `env:"RECONCILE_ON_STARTUP" envDefault:"true"`

	// Intraday cache (today's forming candle in Redis, market hours only)
	IntradayCacheEnabled  bool          `env:"INTRADAY_CACHE_ENABLED" envDefault:"true"`
	IntradayCacheInterval time.Duration `env:"INTRADAY_CACHE_INTERVAL" envDefault:"20m"`

	// IPO tracker (open + upcoming IPOs + GMP signals from the InvestorGain feed)
	IPOEnabled      bool          `env:"IPO_ENABLED" envDefault:"true"`
	IPOPollInterval time.Duration `env:"IPO_POLL_INTERVAL" envDefault:"40m"`
	// IST cron for the authoritative close-day GMP signal check (mainboard only).
	// Default 14:30 (2:30 PM IST).
	IPOSignalCron string `env:"IPO_SIGNAL_CRON" envDefault:"30 14 * * *"`

	// Promoter/Director/KMP insider-trading tracker (NSE PIT disclosure feed)
	PromoterEnabled         bool          `env:"PROMOTER_ENABLED" envDefault:"true"`
	PromoterPollInterval    time.Duration `env:"PROMOTER_POLL_INTERVAL" envDefault:"90m"`
	PromoterAlertWindowDays int           `env:"PROMOTER_ALERT_WINDOW_DAYS" envDefault:"15"`
	PromoterRetentionDays   int           `env:"PROMOTER_RETENTION_DAYS" envDefault:"60"`

	// Notifications (Module 7)
	NotifyEnabled    bool   `env:"NOTIFY_ENABLED" envDefault:"true"`
	NotifyWindowDays int    `env:"NOTIFY_WINDOW_DAYS" envDefault:"7"`
	TelegramBaseURL  string `env:"TELEGRAM_BASE_URL" envDefault:""`

	// Default/safety-net Telegram: receives every in-window signal (once per
	// stock+timeframe+day) even if a user hasn't configured their own bot.
	TelegramDefaultBotToken string `env:"TELEGRAM_DEFAULT_BOT_TOKEN" envDefault:""`
	TelegramDefaultChatID   string `env:"TELEGRAM_DEFAULT_CHAT_ID" envDefault:""`

	// If the default chat is a forum supergroup, these route each signal
	// category to its own topic instead of the General topic. 0 = General.
	TelegramStockSignalsThreadID int `env:"TELEGRAM_STOCK_SIGNALS_THREAD_ID" envDefault:"0"`
	TelegramIPOAlertsThreadID    int `env:"TELEGRAM_IPO_ALERTS_THREAD_ID" envDefault:"0"`
	TelegramPromoterThreadID     int `env:"TELEGRAM_PROMOTER_THREAD_ID" envDefault:"0"`
	TelegramBulkDealsThreadID    int `env:"TELEGRAM_BULK_DEALS_THREAD_ID" envDefault:"0"`
	TelegramBlockDealsThreadID   int `env:"TELEGRAM_BLOCK_DEALS_THREAD_ID" envDefault:"0"`

	// Bulk & block deals tracker (NSE historical bulk-block CSV feed).
	DealsEnabled         bool    `env:"DEALS_ENABLED" envDefault:"true"`
	DealsRetentionDays   int     `env:"DEALS_RETENTION_DAYS" envDefault:"30"`
	DealsAlertWindowDays int     `env:"DEALS_ALERT_WINDOW_DAYS" envDefault:"7"`        // alert stocks dealt within N days
	DealsAlertCron       string  `env:"DEALS_ALERT_CRON" envDefault:"0 19 * * *"`      // 19:00 IST daily
	BulkDealMinNetValue  float64 `env:"BULK_DEAL_MIN_NET_VALUE" envDefault:"50000000"` // ₹5cr net-value filter

	// FII/DII daily cash-market activity (NSE fiidiiTradeReact feed). No history
	// is kept — only the latest snapshot — and no interval/cron is configurable:
	// it only ever polls after 4pm IST on a trading day, hourly until published.
	FiiDiiEnabled bool `env:"FIIDII_ENABLED" envDefault:"true"`
}

// IsLocal reports whether we're running in the local dev profile.
func (c Config) IsLocal() bool { return c.AppEnv == "local" }

// Load reads .env (if present) then parses the environment into a Config.
// A missing .env file is not an error — real env vars take precedence anyway.
func Load() (*Config, error) {
	// Best-effort: ignore "file not found", surface other load errors.
	_ = godotenv.Load()

	var cfg Config
	if err := env.Parse(&cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return &cfg, nil
}
