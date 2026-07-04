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
	AngelHistRate            float64       `env:"ANGEL_HIST_RATE" envDefault:"3"`
	AngelHistBurst           int           `env:"ANGEL_HIST_BURST" envDefault:"3"`
	AngelScripMasterTimeout  time.Duration `env:"ANGEL_SCRIPMASTER_TIMEOUT" envDefault:"5m"`
	AngelScripMasterAttempts int           `env:"ANGEL_SCRIPMASTER_ATTEMPTS" envDefault:"3"`
	AngelScripMasterURL      string        `env:"ANGEL_SCRIPMASTER_URL" envDefault:""`

	// Exchange / calendar
	Exchange string `env:"EXCHANGE" envDefault:"NSE"`

	// Auth
	JWTSecret string `env:"JWT_SECRET" envDefault:"dev-change-me-please"`

	// Scanner / scheduler
	SchedulerEnabled   bool   `env:"SCHEDULER_ENABLED" envDefault:"true"`
	DailyScanCron      string `env:"DAILY_SCAN_CRON" envDefault:"20 15 * * 1-5"`
	CleanupCron        string `env:"CLEANUP_CRON" envDefault:"0 1 * * *"`
	FillScheduledCron  string `env:"FILL_SCHEDULED_CRON" envDefault:"16 9 * * 1-5"`
	RetentionDays      int    `env:"RETENTION_DAYS" envDefault:"30"`
	ReconcileOnStartup bool   `env:"RECONCILE_ON_STARTUP" envDefault:"true"`

	// Notifications (Module 7)
	NotifyEnabled    bool   `env:"NOTIFY_ENABLED" envDefault:"true"`
	NotifyWindowDays int    `env:"NOTIFY_WINDOW_DAYS" envDefault:"7"`
	TelegramBaseURL  string `env:"TELEGRAM_BASE_URL" envDefault:""`

	// Default/safety-net Telegram: receives every in-window signal (once per
	// stock+timeframe+day) even if a user hasn't configured their own bot.
	TelegramDefaultBotToken string `env:"TELEGRAM_DEFAULT_BOT_TOKEN" envDefault:""`
	TelegramDefaultChatID   string `env:"TELEGRAM_DEFAULT_CHAT_ID" envDefault:""`
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
